package main

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// shouldRunInternalAlarm reports whether to run as an internal alarm worker.
// Internal mode is activated only by an exact hidden sentinel argument.
func shouldRunInternalAlarm(args []string) bool {
	return (len(args) == 2 || len(args) == 3) && args[1] == internalAlarmArg
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
