//go:build !windows

package main

// timer is a simple countdown utility with visual feedback and audio alerts.
// Usage: timer [options] <duration|time>
// Examples: timer 30, timer 30s, timer 10m, timer 1.5h, timer 1h2m3s, timer --quiet 3m, timer -q 3m
//           timer 14:30, timer 9:00, timer 23:59:00
//           timer 9am, timer 9:30pm, timer 12:00 AM, timer 9 PM
//
// Features:
// - Live countdown display in stderr and terminal title bar
// - Graceful cancellation via Ctrl+C
// - Audio alert on completion (best-effort, platform-specific backend)
// - Ceiling-based display (never shows 00:00:00 while time remains)
// - Wall clock target mode: counts down to a 24-hour time (e.g. 14:30) or 12-hour time with AM/PM
//   (e.g. 9am, 2:30 PM); always wraps to the next day if the time has already passed
// - Prevent sleep on macOS while timer is active (when both streams are interactive by default, or forced with --caffeinate)
// - Non-TTY-safe lifecycle logging (started/complete/cancelled) in stderr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const internalAlarmArg = "__timer_internal_alarm_worker"
const (
	usageText             = "Usage: timer [options] <duration|time>\nExamples: timer 30, timer 30s, timer 10m, timer 1.5h, timer --quiet 5m, timer 14:30, timer 9am, timer 2:30 PM"
	defaultVersion        = "dev"
	develBuildInfoVersion = "(devel)"
)

var (
	errUsage                     = errors.New("usage")
	errInvalidDuration           = errors.New("invalid duration format")
	errInvalidTime               = errors.New("invalid time format")
	errDurationMustBeAtLeastZero = errors.New("duration must be >= 0")
	// version is overridden in release builds via:
	// go build -ldflags "-X main.version=vX.Y.Z"
	version = defaultVersion
)

type alarmCommand struct {
	name string
	args []string
}

type signalCause struct {
	sig os.Signal
}

func (c signalCause) Error() string {
	return fmt.Sprintf("cancelled by signal %v", c.sig)
}

type unknownOptionError struct {
	option string
}

func (e unknownOptionError) Error() string {
	return fmt.Sprintf("unknown option: %s", e.option)
}

type invocationMode int

const (
	modeRun invocationMode = iota
	modeHelp
	modeVersion
)

type invocation struct {
	mode       invocationMode
	duration   time.Duration
	quiet      bool
	forceAlarm bool
	forceAwake bool
	soundFile  string
}

type cliFlag struct {
	short       string
	long        string
	description string
	takesValue  bool
}

type statusDisplay struct {
	writer           io.Writer
	interactive      bool
	supportsAdvanced bool
}

var cliFlags = []cliFlag{
	{short: "-h", long: "--help", description: "Show help and exit"},
	{short: "-v", long: "--version", description: "Show version and exit"},
	{short: "-q", long: "--quiet", description: "TTY: inline countdown only; non-TTY: suppress lifecycle/completion/cancel/alarm"},
	{short: "-s", long: "--sound", description: "Force alarm playback on completion even in quiet/non-TTY mode"},
	{short: "-f", long: "--sound-file", description: "Path to a custom audio file to play on completion (implies --sound)", takesValue: true},
	{short: "-c", long: "--caffeinate", description: "Force sleep inhibition attempt even in non-TTY mode (darwin only)"},
}

func main() {
	if shouldRunInternalAlarm(os.Args) {
		runAlarmWorker()
		return
	}

	inv, err := parseInvocation(os.Args)
	if err != nil {
		message, exitCode := renderInvocationError(err)
		fmt.Fprintln(os.Stderr, message)
		os.Exit(exitCode)
	}
	if inv.mode == modeHelp {
		fmt.Println(renderHelpText())
		return
	}
	if inv.mode == modeVersion {
		fmt.Print(formatVersionLine(resolveVersion(version, mainModuleVersion())))
		return
	}
	if inv.forceAwake && runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, awakeUnsupportedWarning())
	}

	if inv.soundFile != "" {
		inv.soundFile = resolveUsableSoundFilePath(inv.soundFile)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	defer cancel(nil)

	go func() {
		sig, ok := <-sigCh
		if !ok {
			return
		}
		cancel(signalCause{sig: sig})
	}()

	status := statusDisplay{
		writer:           os.Stderr,
		interactive:      stderrIsTTY(),
		supportsAdvanced: supportsAdvancedTerminal(os.Getenv("TERM")),
	}
	sideEffectsInteractive := stdoutIsTTY()

	if err := runTimer(ctx, inv.duration, status, sideEffectsInteractive, inv.quiet, inv.forceAlarm, inv.forceAwake, inv.soundFile); err != nil {
		os.Exit(exitCodeForCancelError(err))
	}
}

func exitCodeForCancelError(err error) int {
	var cause signalCause
	if errors.As(err, &cause) {
		switch cause.sig {
		case os.Interrupt:
			return 130
		case syscall.SIGTERM:
			return 143
		}
	}
	return 130
}

// shouldRunInternalAlarm reports whether to run as an internal alarm worker.
// Internal mode is activated only by an exact hidden sentinel argument.
func shouldRunInternalAlarm(args []string) bool {
	return (len(args) == 2 || len(args) == 3) && args[1] == internalAlarmArg
}

func renderHelpText() string {
	var b strings.Builder
	b.WriteString(usageText)
	b.WriteString("\n\nFlags:\n")

	for i, flag := range cliFlags {
		label := fmt.Sprintf("%s, %s", flag.short, flag.long)
		if flag.short == "" {
			label = "    " + flag.long
		}
		fmt.Fprintf(&b, "  %-17s%s", label, flag.description)
		if i < len(cliFlags)-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n\nNote: -- ends option parsing; subsequent tokens are treated as positional arguments.\n")

	return b.String()
}

func formatVersionLine(v string) string {
	return fmt.Sprintf("timer %s\n", v)
}

func mainModuleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return ""
	}
	return info.Main.Version
}

func resolveVersion(buildVersion, moduleVersion string) string {
	if buildVersion != "" && buildVersion != defaultVersion {
		return buildVersion
	}
	if moduleVersion != "" && moduleVersion != develBuildInfoVersion {
		return moduleVersion
	}
	if buildVersion != "" {
		return buildVersion
	}
	return defaultVersion
}

func awakeUnsupportedWarning() string {
	return "Warning: --caffeinate sleep inhibition is only supported on darwin; continuing without sleep inhibition"
}

func resolveSoundFilePath(path string) (string, error) {
	switch {
	case path == "~":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return homeDir, nil
	case strings.HasPrefix(path, "~/"):
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return homeDir + path[1:], nil
	default:
		return path, nil
	}
}

func resolveUsableSoundFilePath(path string) string {
	resolvedPath, err := resolveSoundFilePath(path)
	if err != nil {
		return ""
	}

	info, err := os.Stat(resolvedPath)
	if err != nil || info.IsDir() {
		return ""
	}

	return resolvedPath
}

func renderInvocationError(err error) (string, int) {
	var unknownErr unknownOptionError
	switch {
	case errors.As(err, &unknownErr):
		return fmt.Sprintf("%s\n\n%s", unknownErr.Error(), renderHelpText()), 2
	case errors.Is(err, errUsage):
		return usageText + "\n", 2
	case errors.Is(err, errInvalidDuration):
		return "Error: invalid duration format", 2
	case errors.Is(err, errInvalidTime):
		return "Error: invalid time format", 2
	case errors.Is(err, errDurationMustBeAtLeastZero):
		return "Error: duration must be >= 0", 2
	default:
		return fmt.Sprintf("Error: %v", err), 2
	}
}

// parseInvocation resolves CLI mode with explicit precedence:
// unknown options (before "--") beat help/version, then help beats version.
// Run mode requires exactly one duration token.
func parseInvocation(args []string) (invocation, error) {
	if len(args) <= 1 {
		return invocation{mode: modeRun}, errUsage
	}

	args = preprocessCombinedShortFlags(args)

	inv := invocation{
		mode: modeRun,
	}
	hasHelp := false
	hasVersion := false
	seenDoubleDash := false
	var firstUnknownOption string
	var durationToken string

	for i := 1; i < len(args); i++ {
		arg := args[i]
		if !seenDoubleDash && arg == "--" {
			seenDoubleDash = true
			continue
		}

		if !seenDoubleDash {
			switch arg {
			case "-h", "--help":
				hasHelp = true
				continue
			case "-v", "--version":
				hasVersion = true
				continue
			case "-q", "--quiet":
				inv.quiet = true
				continue
			case "-s", "--sound":
				inv.forceAlarm = true
				continue
			case "-f", "--sound-file":
				if i+1 >= len(args) {
					return invocation{mode: modeRun}, errUsage
				}
				inv.soundFile = args[i+1]
				inv.forceAlarm = true
				i++ // skip path
				continue
			case "-c", "--caffeinate":
				inv.forceAwake = true
				continue
			}

			if len(arg) > 0 && arg[0] == '-' && !isPotentialNegativeDuration(arg) {
				if firstUnknownOption == "" {
					firstUnknownOption = arg
				}
				continue
			}
		}

		if durationToken != "" {
			return invocation{mode: modeRun}, errUsage
		}
		durationToken = arg
		if i+1 < len(args) && isAMPMToken(args[i+1]) {
			i++
			durationToken = arg + " " + args[i]
		}
	}

	if firstUnknownOption != "" {
		return invocation{mode: modeRun}, unknownOptionError{option: firstUnknownOption}
	}
	if hasHelp {
		return invocation{mode: modeHelp}, nil
	}
	if hasVersion {
		return invocation{mode: modeVersion, quiet: inv.quiet, forceAlarm: inv.forceAlarm, forceAwake: inv.forceAwake}, nil
	}
	if durationToken == "" {
		return invocation{mode: modeRun}, errUsage
	}

	duration, err := parseDurationToken(durationToken)
	if err != nil {
		return invocation{mode: modeRun}, err
	}
	inv.duration = duration
	return inv, nil
}

func preprocessCombinedShortFlags(args []string) []string {
	if len(args) <= 1 {
		return args
	}

	shortFlags := knownShortFlagsSet(cliFlags)
	normalized := make([]string, 0, len(args))
	normalized = append(normalized, args[0])

	seenDoubleDash := false
	for _, arg := range args[1:] {
		if !seenDoubleDash && arg == "--" {
			seenDoubleDash = true
			normalized = append(normalized, arg)
			continue
		}

		if !seenDoubleDash {
			if expanded, ok := expandCombinedShortFlag(arg, shortFlags); ok {
				normalized = append(normalized, expanded...)
				continue
			}
		}

		normalized = append(normalized, arg)
	}

	return normalized
}

func knownShortFlagsSet(flags []cliFlag) map[rune]cliFlag {
	known := make(map[rune]cliFlag)
	for _, flag := range flags {
		if len(flag.short) != 2 || flag.short[0] != '-' {
			continue
		}
		known[rune(flag.short[1])] = flag
	}
	return known
}

func expandCombinedShortFlag(arg string, knownShortFlags map[rune]cliFlag) ([]string, bool) {
	if len(arg) < 3 || arg[0] != '-' || arg[1] == '-' {
		return nil, false
	}

	if isPotentialNegativeDuration(arg) {
		return nil, false
	}

	expanded := make([]string, 0, len(arg)-1)
	valueFlags := make([]string, 0, 1)

	for _, shortRune := range arg[1:] {
		flag, ok := knownShortFlags[shortRune]
		if !ok {
			return nil, false
		}
		if flag.takesValue {
			valueFlags = append(valueFlags, flag.short)
			continue
		}
		expanded = append(expanded, flag.short)
	}

	if len(valueFlags) > 1 {
		return nil, false
	}
	if len(valueFlags) == 1 {
		expanded = append(expanded, valueFlags[0])
	}

	return expanded, true
}

func parseDurationToken(token string) (time.Duration, error) {
	if d, ok, err := parseWallClockTime(token, time.Now()); ok {
		return d, err
	}

	duration, err := time.ParseDuration(token)
	if err != nil {
		if !isBareDecimalSecondsToken(token) {
			return 0, errInvalidDuration
		}

		duration, err = time.ParseDuration(token + "s")
		if err != nil {
			return 0, errInvalidDuration
		}
	}
	if duration < 0 {
		return 0, errDurationMustBeAtLeastZero
	}
	return duration, nil
}

// parseWallClockTime parses wall clock time tokens and returns the duration from
// now until the next occurrence of that time (target.Sub(now)).
//
// Accepted formats:
//   - 24-hour: H:MM, HH:MM, H:MM:SS, HH:MM:SS (hours [0,23], minutes/seconds [0,59])
//   - 12-hour: the above with a trailing AM/PM suffix, case-insensitive, optionally
//     space-separated (e.g. "9am", "9:30 PM", "12:00:00AM")
//   - Bare hour shorthand with AM/PM suffix only (e.g. "9am", "9 pm")
//   - Special case: 24:00 and 24:00:00 are accepted and normalized to 00:00(:00)
//
// 12-hour clock conventions: 12:00 AM is midnight (00:00), 12:00 PM is noon (12:00).
// Valid 12-hour hours are [1,12]; 0am and 13pm are rejected.
//
// If the resolved time is not strictly after now (already passed or exact match),
// it wraps to the same time the following day using date arithmetic, which is DST-safe.
//
// The boolean return indicates whether the token claimed to be a wall clock time at all.
// false means no colon and no AM/PM suffix were present; the caller should try other formats.
// A token that looks like a time but fails validation returns true with errInvalidTime.
func parseWallClockTime(token string, now time.Time) (time.Duration, bool, error) {
	stripped, isPM, hasSuffix := stripAMPM(token)

	hasColon := strings.ContainsRune(stripped, ':')
	if !hasSuffix && !hasColon {
		return 0, false, nil
	}

	if stripped == "24:00" || stripped == "24:00:00" {
		stripped = strings.Replace(stripped, "24", "00", 1)
	}

	var parts []string
	if hasColon {
		parts = strings.Split(stripped, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return 0, true, errInvalidTime
		}
	} else {
		parts = []string{stripped}
	}

	hourRange := [2]int{0, 23}
	if hasSuffix {
		hourRange = [2]int{1, 12}
	}

	hour, ok := parseTimeField(parts[0], hourRange[0], hourRange[1])
	if !ok {
		return 0, true, errInvalidTime
	}

	min := 0
	sec := 0

	if len(parts) >= 2 {
		min, ok = parseTimeField(parts[1], 0, 59)
		if !ok {
			return 0, true, errInvalidTime
		}
	}
	if len(parts) == 3 {
		sec, ok = parseTimeField(parts[2], 0, 59)
		if !ok {
			return 0, true, errInvalidTime
		}
	}

	if hasSuffix {
		hour, ok = applyAMPM(hour, isPM)
		if !ok {
			return 0, true, errInvalidTime
		}
	}

	target := time.Date(now.Year(), now.Month(), now.Day(), hour, min, sec, 0, now.Location())
	if !target.After(now) {
		target = time.Date(target.Year(), target.Month(), target.Day()+1, target.Hour(), target.Minute(), target.Second(), 0, target.Location())
	}

	return target.Sub(now), true, nil
}

// stripAMPM removes a trailing AM or PM suffix from token, case-insensitively.
// The suffix may be directly attached ("9am") or preceded by a single space ("9 am").
// Returns the stripped token, whether the suffix was PM, and whether any suffix was found.
// The space-prefixed suffixes are checked first to ensure "9 am" strips " am" in full
// rather than just "am", which would leave a trailing space in the result.
func stripAMPM(token string) (string, bool, bool) {
	lower := strings.ToLower(token)
	for _, suffix := range []string{" am", " pm", "am", "pm"} {
		if strings.HasSuffix(lower, suffix) {
			isPM := strings.HasSuffix(lower, "pm")
			return strings.TrimSuffix(token, token[len(token)-len(suffix):]), isPM, true
		}
	}
	return token, false, false
}

// applyAMPM converts a 12-hour clock hour to a 24-hour clock hour.
// Valid input hours are [1, 12]. Returns false if the hour is out of that range.
// 12 AM maps to 0 (midnight); 12 PM maps to 12 (noon); all others follow standard convention.
func applyAMPM(hour int, isPM bool) (int, bool) {
	if hour < 1 || hour > 12 {
		return 0, false
	}
	if isPM {
		if hour == 12 {
			return 12, true
		}
		return hour + 12, true
	}
	if hour == 12 {
		return 0, true
	}
	return hour, true
}

// isAMPMToken reports whether s is exactly "am" or "pm", case-insensitively.
// Used by parseInvocation to detect a space-separated AM/PM token following a time argument.
func isAMPMToken(s string) bool {
	lower := strings.ToLower(s)
	return lower == "am" || lower == "pm"
}

// parseTimeField parses a numeric string and checks it falls within [min, max].
// Leading zeros are accepted (e.g. "09" parses as 9). Empty strings and
// non-numeric characters (including signs and decimal points) are rejected.
func parseTimeField(s string, min, max int) (int, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < min || v > max {
		return 0, false
	}
	return v, true
}

func isBareDecimalSecondsToken(token string) bool {
	if token == "" {
		return false
	}

	start := 0
	if token[0] == '+' || token[0] == '-' {
		start = 1
	}
	if start >= len(token) {
		return false
	}

	hasDigit := false
	dotCount := 0

	for i := start; i < len(token); i++ {
		switch c := token[i]; {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '.':
			dotCount++
			if dotCount > 1 {
				return false
			}
		default:
			return false
		}
	}

	return hasDigit
}

// isPotentialNegativeDuration distinguishes duration-like inputs (e.g. "-1s")
// from unknown flags so negative durations flow through normal duration validation.
func isPotentialNegativeDuration(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}

	next := arg[1]
	return (next >= '0' && next <= '9') || next == '.'
}

func runTimer(ctx context.Context, duration time.Duration, status statusDisplay, sideEffectsInteractive bool, quiet bool, forceAlarm bool, forceAwake bool, soundFile string) error {
	return runTimerWithAlarmStarter(ctx, duration, status, sideEffectsInteractive, quiet, forceAlarm, forceAwake, soundFile, startAlarmProcess)
}

func runTimerWithAlarmStarter(ctx context.Context, duration time.Duration, status statusDisplay, sideEffectsInteractive bool, quiet bool, forceAlarm bool, forceAwake bool, soundFile string, alarmStarter func(string)) error {
	bothStreamsInteractive := sideEffectsInteractive && status.interactive

	var sleepInhibitor *exec.Cmd
	if shouldStartSleepInhibitor(runtime.GOOS, sideEffectsInteractive, status.interactive, forceAwake) {
		pid := strconv.Itoa(os.Getpid())
		sleepInhibitor = quietCmd("caffeinate", sleepInhibitorArgs(sideEffectsInteractive, status.interactive, pid)...)
		if err := sleepInhibitor.Start(); err != nil {
			sleepInhibitor = nil
		} else {
			defer func() {
				if sleepInhibitor.Process != nil {
					_ = sleepInhibitor.Process.Kill()
				}
				_ = sleepInhibitor.Wait()
			}()
		}
	}

	deadline := time.Now().Add(duration)
	done := time.NewTimer(duration)
	defer done.Stop()

	if shouldPrintLifecycleStart(status.interactive, quiet) && ctx.Err() == nil {
		writeStatusf(status.writer, "timer: started (%s)\n", duration)
	}

	var tickC <-chan time.Time
	if status.interactive {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		tickC = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			printCancelled(status, quiet)
			return context.Cause(ctx)

		case <-done.C:
			printComplete(status, quiet)
			shouldAlarm := shouldTriggerAlarm(bothStreamsInteractive, quiet, forceAlarm)
			if shouldAlarm {
				alarmStarter(soundFile)
			}
			return nil

		case <-tickC:
			if !status.interactive {
				continue
			}

			remaining := time.Until(deadline)

			if remaining <= 0 {
				// done is the authoritative completion signal; ticks are UI-only.
				continue
			}

			renderInteractiveCountdown(status, formatRemainingTime(remaining), quiet)
		}
	}
}

func renderInteractiveCountdown(status statusDisplay, timeStr string, quiet bool) {
	if status.supportsAdvanced {
		if quiet {
			writeStatusf(status.writer, "\r\033[K%s", timeStr)
			return
		}
		// Update title bar and terminal line in a single operation.
		// \033]0; sets title, \007 terminates the OSC sequence, \r returns to start of line.
		writeStatusf(status.writer, "\033]0;%s\007\r\033[K%s", timeStr, timeStr)
		return
	}
	writeStatusf(status.writer, "\r%s", timeStr)
}

func formatRemainingTime(remaining time.Duration) string {
	// Ceiling-based calculation for whole seconds.
	totalSeconds := int((remaining + time.Second - 1) / time.Second)
	h := totalSeconds / 3600
	m := (totalSeconds % 3600) / 60
	s := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func printComplete(status statusDisplay, quiet bool) {
	if quiet {
		clearInteractiveStatusLine(status)
		return
	}

	if status.interactive {
		clearInteractiveStatusLine(status)
		writeStatusln(status.writer, "timer complete")
		return
	}
	writeStatusln(status.writer, "timer: complete")
}

func printCancelled(status statusDisplay, quiet bool) {
	if quiet {
		clearInteractiveStatusLine(status)
		return
	}

	if status.interactive {
		clearInteractiveStatusLine(status)
		writeStatusln(status.writer, "timer cancelled")
		return
	}
	writeStatusln(status.writer, "timer: cancelled")
}

func clearInteractiveStatusLine(status statusDisplay) {
	if !status.interactive {
		return
	}
	if status.supportsAdvanced {
		writeStatus(status.writer, "\r\033[K")
		return
	}
	writeStatus(status.writer, "\r")
}

func writeStatus(writer io.Writer, s string) {
	_, _ = fmt.Fprint(writer, s)
}

func writeStatusln(writer io.Writer, a ...any) {
	_, _ = fmt.Fprintln(writer, a...)
}

func writeStatusf(writer io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(writer, format, a...)
}

func shouldPrintLifecycleStart(interactive bool, quiet bool) bool {
	return !interactive && !quiet
}

func shouldStartSleepInhibitor(goos string, stdoutInteractive bool, statusInteractive bool, forceAwake bool) bool {
	return goos == "darwin" && ((stdoutInteractive && statusInteractive) || forceAwake)
}

func sleepInhibitorArgs(stdoutInteractive bool, statusInteractive bool, pid string) []string {
	args := []string{"-i"}
	if stdoutInteractive && statusInteractive {
		args = append(args, "-d")
	}
	args = append(args, "-w", pid)
	return args
}

func shouldTriggerAlarm(sideEffectsInteractive bool, quiet bool, forceAlarm bool) bool {
	return forceAlarm || (sideEffectsInteractive && !quiet)
}

func stdoutIsTTY() bool {
	return isTerminal(os.Stdout.Fd())
}

func stderrIsTTY() bool {
	return isTerminal(os.Stderr.Fd())
}

func supportsAdvancedTerminal(termName string) bool {
	normalized := strings.TrimSpace(strings.ToLower(termName))
	return normalized != "" && normalized != "dumb"
}

func isTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

// startAlarmProcess launches a detached child process that plays alert audio.
// The parent does not wait so the prompt returns immediately on completion.
// Alarm is best-effort; silently skip if we can't locate the executable.
func startAlarmProcess(soundFile string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}

	cmd := newInternalAlarmCmd(exe, soundFile)
	_ = cmd.Start()
}

func newInternalAlarmCmd(exe string, soundFile string) *exec.Cmd {
	args := []string{internalAlarmArg}
	if soundFile != "" {
		args = append(args, soundFile)
	}
	cmd := quietCmd(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// runAlarmWorker plays an available alarm backend 4 times with 100ms pauses.
func runAlarmWorker() {
	soundFile := ""
	if len(os.Args) == 3 {
		soundFile = os.Args[2]
	}
	playAlarmAttempts(resolveAlarmCommands(soundFile), 4, 100*time.Millisecond, runAlarmCommand)
}

// playAlarmAttempts plays a sound up to attempts times, removing any backend that fails.
// interval is the pause after each sound completes, not between start times.
func playAlarmAttempts(commands []alarmCommand, attempts int, interval time.Duration, runner func(alarmCommand) error) {
	if len(commands) == 0 {
		return
	}

	for i := 0; i < attempts && len(commands) > 0; i++ {
		played := false

		for idx := 0; idx < len(commands); {
			if err := runner(commands[idx]); err == nil {
				played = true
				break
			}
			commands = append(commands[:idx], commands[idx+1:]...)
		}

		if !played {
			return
		}

		time.Sleep(interval)
	}
}

func resolveAlarmCommands(soundFile string) []alarmCommand {
	candidates := alarmCandidatesForGOOS(runtime.GOOS, soundFile)
	commands := make([]alarmCommand, 0, len(candidates))

	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate.name); err == nil {
			commands = append(commands, candidate)
		}
	}
	return commands
}

func runAlarmCommand(command alarmCommand) error {
	cmd := quietCmd(command.name, command.args...)
	return cmd.Run()
}

// quietCmd creates an exec.Cmd with stdio disconnected/discarded.
func quietCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd
}

func alarmCandidatesForGOOS(goos string, soundFile string) []alarmCommand {
	switch goos {
	case "darwin":
		if soundFile != "" {
			return []alarmCommand{{name: "afplay", args: []string{soundFile}}}
		}
		return []alarmCommand{
			{name: "afplay", args: []string{"/System/Library/Sounds/Submarine.aiff"}},
		}
	case "linux":
		if soundFile != "" {
			return []alarmCommand{
				{name: "canberra-gtk-play", args: []string{"--file", soundFile}},
				{name: "paplay", args: []string{soundFile}},
			}
		}
		return []alarmCommand{
			{name: "canberra-gtk-play", args: []string{"-i", "bell"}},
			{name: "timeout", args: []string{"0.15s", "speaker-test", "-t", "sine", "-f", "1200", "-c", "1", "-s", "1"}},
		}
	case "freebsd":
		if soundFile != "" {
			return []alarmCommand{
				{name: "canberra-gtk-play", args: []string{"--file", soundFile}},
			}
		}
		return []alarmCommand{
			{name: "beep"},
			{name: "canberra-gtk-play", args: []string{"-i", "bell"}},
		}
	case "openbsd", "netbsd":
		return []alarmCommand{
			{name: "beep"},
		}
	default:
		return nil
	}
}
