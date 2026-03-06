//go:build !windows

package main

// timer is a simple countdown utility with visual feedback and audio alerts.
// Usage: timer [options] <duration>
// Examples: timer 30, timer 30s, timer 10m, timer 1.5h, timer 1h2m3s, timer --quiet 3m, timer -q 3m
//
// Features:
// - Live countdown display in stderr and terminal title bar
// - Graceful cancellation via Ctrl+C
// - Audio alert on completion (best-effort, platform-specific backend)
// - Ceiling-based display (never shows 00:00:00 while time remains)
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
	usageText             = "Usage: timer [options] <duration>\nExamples: timer 30, timer 30s, timer 10m, timer 1.5h, timer --quiet 5m"
	defaultVersion        = "dev"
	develBuildInfoVersion = "(devel)"
)

var (
	errUsage                     = errors.New("usage")
	errInvalidDuration           = errors.New("invalid duration format")
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
