package main

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

func runTimer(ctx context.Context, cancel context.CancelCauseFunc, duration time.Duration, wallClockTarget time.Time, status statusDisplay, sideEffectsInteractive bool, quiet bool, noTitle bool, forceAlarm bool, forceAwake bool, soundFile string) error {
	return runTimerWithAlarmStarter(ctx, cancel, duration, wallClockTarget, status, sideEffectsInteractive, quiet, noTitle, forceAlarm, forceAwake, soundFile, startAlarmProcess)
}

func runTimerWithAlarmStarter(ctx context.Context, cancel context.CancelCauseFunc, duration time.Duration, wallClockTarget time.Time, status statusDisplay, sideEffectsInteractive bool, quiet bool, noTitle bool, forceAlarm bool, forceAwake bool, soundFile string, alarmStarter func(string)) error {
	bothStreamsInteractive := sideEffectsInteractive && status.interactive

	if shouldStartSleepInhibitor(runtime.GOOS, sideEffectsInteractive, status.interactive, forceAwake) {
		pid := strconv.Itoa(os.Getpid())
		cmd := quietCmd("caffeinate", sleepInhibitorArgs(sideEffectsInteractive, status.interactive, pid)...)
		go func() { _ = cmd.Run() }() // best-effort; -w <pid> ensures caffeinate exits when we do
	}

	isWallClock := !wallClockTarget.IsZero()

	var deadline time.Time
	if isWallClock {
		deadline = wallClockTarget
	} else {
		deadline = time.Now().Add(duration)
	}

	done := time.NewTimer(time.Until(deadline))
	defer done.Stop()

	if shouldPrintLifecycleStart(status.interactive, quiet) && ctx.Err() == nil {
		writeStatusln(status.writer, formatLifecycleStarted(duration, wallClockTarget))
	}

	var tickC <-chan time.Time
	if status.interactive {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		tickC = ticker.C
	}

	var resyncC <-chan time.Time
	if isWallClock {
		resync := time.NewTicker(1 * time.Second)
		defer resync.Stop()
		resyncC = resync.C
	}

	if status.interactive {
		renderInteractiveCountdown(status, formatRemainingTime(duration), noTitle)
	}

	var keyCh <-chan struct{}
	restoreTerminal := func() {}
	if status.interactive && stdinIsTTY() {
		tty, err := os.Open("/dev/tty")
		if err == nil {
			oldState, err := term.MakeRaw(int(tty.Fd()))
			if err == nil {
				var once sync.Once
				restoreTerminal = func() {
					once.Do(func() {
						_ = term.Restore(int(tty.Fd()), oldState)
						tty.Close()
					})
				}
				defer restoreTerminal()

				ch := make(chan struct{}, 1)
				keyCh = ch
				go func() {
					buf := make([]byte, 1)
					for {
						n, err := tty.Read(buf)
						if err != nil || n == 0 {
							return
						}
						b := buf[0]
						if b == 'q' || b == 'Q' || b == 0x1B || b == 0x03 || b == 0x04 {
							select {
							case ch <- struct{}{}:
							default:
							}
							return
						}
					}
				}()
			} else {
				tty.Close()
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			restoreTerminal()
			printCancelled(status, quiet)
			return context.Cause(ctx)

		case <-keyCh:
			restoreTerminal()
			cancel(signalCause{sig: os.Interrupt})
			printCancelled(status, quiet)
			return context.Cause(ctx)

		case <-done.C:
			restoreTerminal()
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

			renderInteractiveCountdown(status, formatRemainingTime(remaining), noTitle)

		case <-resyncC:
			remaining := time.Until(deadline)
			if remaining < 0 {
				remaining = 0
			}
			done.Reset(remaining)
		}
	}
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

func stdinIsTTY() bool {
	return isTerminal(os.Stdin.Fd())
}
