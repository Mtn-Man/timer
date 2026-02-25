package main

// timer is a simple countdown utility with visual feedback and audio alerts.
// Usage: timer <duration>
// Examples: timer 30s, timer 10m, timer 1.5h, timer 1h2m3s
//
// Features:
// - Live countdown display in stdout and terminal title bar
// - Graceful cancellation via Ctrl+C
// - Audio alert on completion (best-effort, platform-specific backend)
// - Ceiling-based display (never shows 00:00:00 while time remains)
// - Prevent sleep on macOS while timer is active in interactive mode
// - Non-TTY-safe behavior: disables interactive UI/output/alerts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/term"
)

const internalAlarmEnv = "TIMER_INTERNAL_ALARM"
const (
	appVersion = "v1.0.0"
	usageText  = "Usage: timer <duration>\nExamples: timer 30s, timer 10m, timer 1.5h"
	helpText   = usageText + "\n\nFlags:\n  -h, --help       Show help and exit\n  -v, --version    Show version and exit\n  -q, --quiet      Suppress title, completion text, alarm, and cancel text\n      --alarm      Force alarm playback on completion even in quiet/non-TTY mode"
)

var (
	errUsage              = errors.New("usage")
	errInvalidDuration    = errors.New("invalid duration format")
	errDurationMustBeOver = errors.New("duration must be > 0")
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

type invocationMode int

const (
	modeRun invocationMode = iota
	modeHelp
	modeVersion
)

func main() {
	if shouldRunInternalAlarm(os.Args, os.Getenv(internalAlarmEnv)) {
		runAlarmWorker()
		return
	}

	mode, duration, quiet, forceAlarm, err := parseInvocation(os.Args)
	if err != nil {
		switch {
		case errors.Is(err, errUsage):
			fmt.Fprintln(os.Stderr, usageText)
		case errors.Is(err, errInvalidDuration):
			fmt.Fprintln(os.Stderr, "Error: invalid duration format")
		case errors.Is(err, errDurationMustBeOver):
			fmt.Fprintln(os.Stderr, "Error: duration must be > 0")
		default:
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
	if mode == modeHelp {
		fmt.Println(helpText)
		return
	}
	if mode == modeVersion {
		fmt.Printf("timer %s\n", appVersion)
		return
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

	interactive := stdoutIsTTY()

	if err := runTimer(ctx, duration, interactive, quiet, forceAlarm); err != nil {
		if interactive {
			fmt.Print("\r\033[K")
		}
		if interactive && !quiet {
			fmt.Println("timer cancelled")
		}
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
// The args check distinguishes a worker invocation from a user invocation that inherits the env var.
func shouldRunInternalAlarm(args []string, envValue string) bool {
	return envValue == "1" && len(args) == 1
}

// parseInvocation resolves CLI mode with explicit precedence:
// help returns immediately, version beats unknown flags, and run mode
// requires exactly one duration token with no unknown flags.
func parseInvocation(args []string) (invocationMode, time.Duration, bool, bool, error) {
	if len(args) <= 1 {
		return modeRun, 0, false, false, errUsage
	}

	hasVersion := false
	isQuiet := false
	hasAlarm := false
	hasUnknownFlag := false
	var durationToken string

	for _, arg := range args[1:] {
		switch arg {
		case "-h", "--help":
			return modeHelp, 0, false, false, nil
		case "-v", "--version":
			hasVersion = true
			continue
		case "-q", "--quiet":
			isQuiet = true
			continue
		case "--alarm":
			hasAlarm = true
			continue
		}

		if len(arg) > 0 && arg[0] == '-' && !isPotentialNegativeDuration(arg) {
			hasUnknownFlag = true
			continue
		}

		if durationToken != "" {
			return modeRun, 0, false, false, errUsage
		}
		durationToken = arg
	}

	if hasVersion {
		return modeVersion, 0, isQuiet, hasAlarm, nil
	}
	if hasUnknownFlag || durationToken == "" {
		return modeRun, 0, false, false, errUsage
	}

	duration, err := parseDurationToken(durationToken)
	if err != nil {
		return modeRun, 0, false, false, err
	}
	return modeRun, duration, isQuiet, hasAlarm, nil
}

func parseDurationToken(token string) (time.Duration, error) {
	duration, err := time.ParseDuration(token)
	if err != nil {
		return 0, errInvalidDuration
	}
	if duration <= 0 {
		return 0, errDurationMustBeOver
	}
	return duration, nil
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

func runTimer(ctx context.Context, duration time.Duration, interactive bool, quiet bool, forceAlarm bool) error {
	var sleepInhibitor *exec.Cmd
	if shouldStartSleepInhibitor(runtime.GOOS, interactive) {
		sleepInhibitor = quietCmd("caffeinate", "-i")
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

	var tickC <-chan time.Time
	if interactive {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		tickC = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)

		case <-done.C:
			if interactive {
				fmt.Print("\r\033[K")
				if !quiet {
					fmt.Println("timer complete")
				}
			}
			if shouldTriggerAlarm(interactive, quiet, forceAlarm) {
				startAlarmProcess()
			}
			return nil

		case <-tickC:
			remaining := time.Until(deadline)

			if remaining <= 0 {
				// done is the authoritative completion signal; ticks are UI-only.
				continue
			}

			// Ceiling-based calculation for whole seconds
			totalSeconds := int((remaining + time.Second - 1) / time.Second)

			h := totalSeconds / 3600
			m := (totalSeconds % 3600) / 60
			s := totalSeconds % 60

			timeStr := fmt.Sprintf("%02d:%02d:%02d", h, m, s)

			if quiet {
				fmt.Printf("\r\033[K%s", timeStr)
			} else {
				// Update title bar and terminal line in a single operation
				// \033]0; sets title, \007 terminates the OSC sequence, \r returns to start of line
				fmt.Printf("\033]0;%s\007\r\033[K%s", timeStr, timeStr)
			}
		}
	}
}

func shouldStartSleepInhibitor(goos string, interactive bool) bool {
	return goos == "darwin" && interactive
}

func shouldTriggerAlarm(interactive bool, quiet bool, forceAlarm bool) bool {
	return forceAlarm || (interactive && !quiet)
}

func stdoutIsTTY() bool {
	return isTerminal(os.Stdout.Fd())
}

func isTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

// startAlarmProcess launches a detached child process that plays alert audio.
// The parent does not wait so the prompt returns immediately on completion.
// Alarm is best-effort; silently skip if we can't locate the executable.
func startAlarmProcess() {
	exe, err := os.Executable()
	if err != nil {
		return
	}

	cmd := quietCmd(exe)
	cmd.Env = append(os.Environ(), internalAlarmEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	_ = cmd.Start()
}

// runAlarmWorker plays an available alarm backend 4 times with 100ms pauses.
func runAlarmWorker() {
	playAlarmAttempts(resolveAlarmCommands(), 4, 100*time.Millisecond, runAlarmCommand)
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

func resolveAlarmCommands() []alarmCommand {
	candidates := alarmCandidatesForGOOS(runtime.GOOS)
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

func alarmCandidatesForGOOS(goos string) []alarmCommand {
	switch goos {
	case "darwin":
		return []alarmCommand{
			{name: "afplay", args: []string{"/System/Library/Sounds/Submarine.aiff"}},
		}
	case "linux":
		return []alarmCommand{
			{name: "canberra-gtk-play", args: []string{"-i", "bell"}},
			{name: "timeout", args: []string{"0.15s", "speaker-test", "-t", "sine", "-f", "1200", "-c", "1", "-s", "1"}},
		}
	case "freebsd":
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
