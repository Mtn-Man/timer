package main

// timer is a simple countdown utility with visual feedback and audio alerts.
// Usage: timer <duration>
// Examples: timer 30s, timer 10m, timer 1.5h
//
// Features:
// - Live countdown display in stdout and terminal title bar
// - Graceful cancellation via Ctrl+C with title restoration
// - Audio alert on completion (4x Submarine.aiff sound)
// - Ceiling-based display (never shows 00:00:00 while time remains)

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: timer <duration>\nExamples: timer 30s, timer 10m, timer 1.5h")
		os.Exit(1)
	}

	duration, err := time.ParseDuration(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: invalid duration format")
		os.Exit(1)
	}
	if duration <= 0 {
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
				_ = exec.Command("/bin/bash", "-c",
					"for i in 1 2 3 4; do /usr/bin/afplay /System/Library/Sounds/Submarine.aiff; sleep 0.1; done >/dev/null 2>&1 &",
				).Start()

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
