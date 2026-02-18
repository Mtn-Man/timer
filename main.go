package main

// timer is a simple countdown utility with visual feedback and audio alerts.
// Usage: timer <duration>
// Examples: timer 30s, timer 10m, timer 1.5h
//
// Features:
// - Live countdown display in stdout and terminal title bar
// - Graceful cancellation via Ctrl+C with title restoration
// - Audio alert on completion (best-effort, platform-specific backend)
// - Ceiling-based display (never shows 00:00:00 while time remains)

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
)

const internalAlarmEnv = "TIMER_INTERNAL_ALARM"

var (
	errUsage              = errors.New("usage")
	errInvalidDuration    = errors.New("invalid duration format")
	errDurationMustBeOver = errors.New("duration must be > 0")
)

type alarmCommand struct {
	name string
	args []string
}

func main() {
	if shouldRunInternalAlarm(os.Args, os.Getenv(internalAlarmEnv)) {
		runAlarmWorker()
		return
	}

	duration, err := parseRequestedDuration(os.Args)
	if errors.Is(err, errUsage) {
		fmt.Fprintln(os.Stderr, "Usage: timer <duration>\nExamples: timer 30s, timer 10m, timer 1.5h")
		os.Exit(1)
	}
	if errors.Is(err, errInvalidDuration) {
		fmt.Fprintln(os.Stderr, "Error: invalid duration format")
		os.Exit(1)
	}
	if errors.Is(err, errDurationMustBeOver) {
		fmt.Fprintln(os.Stderr, "Error: duration must be > 0")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runTimer(ctx, duration); err != nil {
		fmt.Print("\r\033[K")
		fmt.Println("Timer cancelled")
		os.Exit(130)
	}
}

func shouldRunInternalAlarm(args []string, envValue string) bool {
	return envValue == "1" && len(args) == 1
}

func parseRequestedDuration(args []string) (time.Duration, error) {
	if len(args) != 2 {
		return 0, errUsage
	}

	duration, err := time.ParseDuration(args[1])
	if err != nil {
		return 0, errInvalidDuration
	}
	if duration <= 0 {
		return 0, errDurationMustBeOver
	}
	return duration, nil
}

func runTimer(ctx context.Context, duration time.Duration) error {
	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Defer restoration of the terminal title bar upon exit
	defer fmt.Print("\033]0;Terminal\007")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			remaining := time.Until(deadline)

			if remaining <= 0 {
				fmt.Print("\r\033[K")
				fmt.Println("Timer Complete")
				startAlarmProcess()

				return nil
			}

			// Ceiling-based calculation for whole seconds
			totalSeconds := int((remaining + time.Second - 1) / time.Second)

			h := totalSeconds / 3600
			m := (totalSeconds % 3600) / 60
			s := totalSeconds % 60

			timeStr := fmt.Sprintf("%02d:%02d:%02d", h, m, s)

			// Update title bar and terminal line in a single operation
			// \033]0; sets title, \007 terminates the OSC sequence, \r returns to start of line
			fmt.Printf("\033]0;%s\007\r%s", timeStr, timeStr)
		}
	}
}

// startAlarmProcess launches a detached child process that plays alert audio.
// The parent does not wait so the prompt returns immediately on completion.
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

func playAlarmAttempts(commands []alarmCommand, attempts int, interval time.Duration, runner func(alarmCommand) error) {
	if len(commands) == 0 {
		return
	}

	for i := 0; i < attempts && len(commands) > 0; i++ {
		played := false

		for idx := 0; idx < len(commands); {
			// Alarm is best effort by design; failures stay silent to avoid noisy UX.
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
			{name: "aplay", args: []string{"/usr/share/sounds/alsa/Front_Center.wav"}},
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
