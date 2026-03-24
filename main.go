//go:build !windows

package main

// after is a simple countdown utility with visual feedback and audio alerts.
// Usage: after [options] <duration|time>
// Examples: after 30, after 30s, after 10m, after 1.5h, after 1h2m3s, after --quiet 3m, after -q 3m
//           after 14:30, after 9:00, after 23:59:00
//           after 9am, after 9:30pm, after 12:00 AM, after 9 PM
//
// Features:
// - Live countdown display in stderr and terminal title bar
// - Graceful cancellation via Ctrl+C
// - Audio alert on completion (best-effort, platform-specific backend)
// - Ceiling-based display (never shows 00:00:00 while time remains)
// - Wall clock target mode: counts down to a 24-hour time (e.g. 14:30) or 12-hour time with AM/PM
//   (e.g. 9am, 2:30 PM); always wraps to the next day if the time has already passed
// - Prevent sleep on macOS while after is active (when both streams are interactive by default, or forced with --caffeinate)
// - Non-TTY-safe lifecycle logging (started/complete/cancelled) in stderr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

const internalAlarmArg = "__after_internal_alarm_worker"
const (
	usageText             = "Usage: after [options] <duration|time>\nExamples: after 30, after 30s, after 10m, after 1.5h, after --quiet 5m, after 14:30, after 9am, after 2:30 PM"
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
	mode            invocationMode
	duration        time.Duration
	wallClockTarget time.Time
	quiet           bool
	noTitle         bool
	forceAlarm      bool
	forceAwake      bool
	soundFile       string
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
	{short: "-t", long: "--no-title", description: "Disable terminal title bar updates"},
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
		original := inv.soundFile
		inv.soundFile = resolveUsableSoundFilePath(inv.soundFile)
		if inv.soundFile == "" {
			fmt.Fprintln(os.Stderr, soundFileWarning(original))
		}
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

	if err := runTimer(ctx, cancel, inv.duration, inv.wallClockTarget, status, sideEffectsInteractive, inv.quiet, inv.noTitle, inv.forceAlarm, inv.forceAwake, inv.soundFile); err != nil {
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

func awakeUnsupportedWarning() string {
	return "Warning: --caffeinate sleep inhibition is only supported on darwin; continuing without sleep inhibition"
}

func soundFileWarning(path string) string {
	return fmt.Sprintf("Warning: sound file not found or unreadable: %s; using default alarm", path)
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
