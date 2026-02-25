package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func TestShouldRunInternalAlarm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		env  string
		want bool
	}{
		{
			name: "worker mode when env is set and no user args",
			args: []string{"timer"},
			env:  "1",
			want: true,
		},
		{
			name: "normal mode when env is set but duration arg present",
			args: []string{"timer", "1s"},
			env:  "1",
			want: false,
		},
		{
			name: "normal mode when env is not set",
			args: []string{"timer"},
			env:  "",
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldRunInternalAlarm(tc.args, tc.env)
			if got != tc.want {
				t.Fatalf("shouldRunInternalAlarm() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderHelpText(t *testing.T) {
	t.Parallel()

	want := usageText + "\n\nFlags:\n" +
		"  -h, --help       Show help and exit\n" +
		"  -v, --version    Show version and exit\n" +
		"  -q, --quiet      Suppress title, completion text, alarm, and cancel text\n" +
		"      --alarm      Force alarm playback on completion even in quiet/non-TTY mode"

	got := renderHelpText()
	if got != want {
		t.Fatalf("renderHelpText() = %q, want %q", got, want)
	}
}

func TestParseInvocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantMode     invocationMode
		wantDuration time.Duration
		wantQuiet    bool
		wantAlarm    bool
		wantErr      error
	}{
		{
			name:     "help short flag",
			args:     []string{"timer", "-h"},
			wantMode: modeHelp,
		},
		{
			name:     "help long flag",
			args:     []string{"timer", "--help"},
			wantMode: modeHelp,
		},
		{
			name:     "help flag wins with extra args",
			args:     []string{"timer", "--help", "10s"},
			wantMode: modeHelp,
		},
		{
			name:     "version short flag",
			args:     []string{"timer", "-v"},
			wantMode: modeVersion,
		},
		{
			name:     "version long flag",
			args:     []string{"timer", "--version"},
			wantMode: modeVersion,
		},
		{
			name:     "version flag wins with extra args",
			args:     []string{"timer", "--version", "10s"},
			wantMode: modeVersion,
		},
		{
			name:         "alarm long flag with duration",
			args:         []string{"timer", "--alarm", "1s"},
			wantMode:     modeRun,
			wantDuration: 1 * time.Second,
			wantAlarm:    true,
		},
		{
			name:         "alarm and quiet with duration",
			args:         []string{"timer", "--alarm", "--quiet", "1s"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
			wantAlarm:    true,
		},
		{
			name:         "quiet and alarm with duration",
			args:         []string{"timer", "--quiet", "--alarm", "1s"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
			wantAlarm:    true,
		},
		{
			name:         "quiet short flag with duration",
			args:         []string{"timer", "-q", "1s"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
		},
		{
			name:         "quiet long flag with duration",
			args:         []string{"timer", "--quiet", "1s"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
		},
		{
			name:         "duration then quiet flag",
			args:         []string{"timer", "1s", "-q"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
		},
		{
			name:      "quiet and version returns version mode with quiet set",
			args:      []string{"timer", "--quiet", "--version"},
			wantMode:  modeVersion,
			wantQuiet: true,
		},
		{
			name:     "quiet and help returns help mode",
			args:     []string{"timer", "--quiet", "--help"},
			wantMode: modeHelp,
		},
		{
			name:    "quiet without duration is usage error",
			args:    []string{"timer", "-q"},
			wantErr: errUsage,
		},
		{
			name:    "alarm without duration is usage error",
			args:    []string{"timer", "--alarm"},
			wantErr: errUsage,
		},
		{
			name:     "help takes precedence over version",
			args:     []string{"timer", "--help", "--version"},
			wantMode: modeHelp,
		},
		{
			name:     "help takes precedence over alarm",
			args:     []string{"timer", "--help", "--alarm"},
			wantMode: modeHelp,
		},
		{
			name:     "help takes precedence over unknown flag",
			args:     []string{"timer", "--help", "--wat"},
			wantMode: modeHelp,
		},
		{
			name:     "help takes precedence over prior unknown flag",
			args:     []string{"timer", "--wat", "--help"},
			wantMode: modeHelp,
		},
		{
			name:     "version takes precedence over unknown flag",
			args:     []string{"timer", "--version", "--wat"},
			wantMode: modeVersion,
		},
		{
			name:      "version with alarm returns version mode with alarm set",
			args:      []string{"timer", "--version", "--alarm"},
			wantMode:  modeVersion,
			wantAlarm: true,
		},
		{
			name:     "version takes precedence over prior unknown flag",
			args:     []string{"timer", "--wat", "--version"},
			wantMode: modeVersion,
		},
		{
			name:    "unknown short flag is usage error",
			args:    []string{"timer", "-x"},
			wantErr: errUsage,
		},
		{
			name:    "unknown long flag is usage error",
			args:    []string{"timer", "--wat"},
			wantErr: errUsage,
		},
		{
			name:    "usage when no duration arg",
			args:    []string{"timer"},
			wantErr: errUsage,
		},
		{
			name:         "valid duration invocation",
			args:         []string{"timer", "1s"},
			wantMode:     modeRun,
			wantDuration: 1 * time.Second,
		},
		{
			name:         "zero duration invocation",
			args:         []string{"timer", "0s"},
			wantMode:     modeRun,
			wantDuration: 0,
		},
		{
			name:    "invalid duration format",
			args:    []string{"timer", "abc"},
			wantErr: errInvalidDuration,
		},
		{
			name:    "negative duration remains duration validation error",
			args:    []string{"timer", "-1s"},
			wantErr: errDurationMustBeAtLeastZero,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseInvocation(tc.args)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseInvocation() error = %v, want %v", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseInvocation() unexpected error = %v", err)
			}
			if got.mode != tc.wantMode {
				t.Fatalf("parseInvocation() mode = %v, want %v", got.mode, tc.wantMode)
			}
			if got.duration != tc.wantDuration {
				t.Fatalf("parseInvocation() duration = %v, want %v", got.duration, tc.wantDuration)
			}
			if got.quiet != tc.wantQuiet {
				t.Fatalf("parseInvocation() quiet = %v, want %v", got.quiet, tc.wantQuiet)
			}
			if got.forceAlarm != tc.wantAlarm {
				t.Fatalf("parseInvocation() alarm = %v, want %v", got.forceAlarm, tc.wantAlarm)
			}
		})
	}
}

func TestAlarmCandidatesForGOOS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goos      string
		wantCount int
		wantFirst string
	}{
		{goos: "darwin", wantCount: 1, wantFirst: "afplay"},
		{goos: "linux", wantCount: 2, wantFirst: "canberra-gtk-play"},
		{goos: "freebsd", wantCount: 2, wantFirst: "beep"},
		{goos: "openbsd", wantCount: 1, wantFirst: "beep"},
		{goos: "netbsd", wantCount: 1, wantFirst: "beep"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()

			got := alarmCandidatesForGOOS(tc.goos)
			if len(got) != tc.wantCount {
				t.Fatalf("alarmCandidatesForGOOS(%q) count = %d, want %d", tc.goos, len(got), tc.wantCount)
			}
			if len(got) > 0 && got[0].name != tc.wantFirst {
				t.Fatalf("alarmCandidatesForGOOS(%q) first = %q, want %q", tc.goos, got[0].name, tc.wantFirst)
			}
		})
	}
}

func TestAlarmCandidatesForUnknownGOOS(t *testing.T) {
	t.Parallel()

	got := alarmCandidatesForGOOS("unknown-os")
	if got != nil {
		t.Fatalf("alarmCandidatesForGOOS() = %v, want nil", got)
	}
}

func TestIsTerminal_NonTTYDescriptors(t *testing.T) {
	t.Parallel()

	tempFile, err := os.CreateTemp(t.TempDir(), "stdout-like")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer func() { _ = tempFile.Close() }()

	if isTerminal(tempFile.Fd()) {
		t.Fatal("isTerminal() = true for regular file, want false")
	}

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer func() { _ = pipeReader.Close() }()
	defer func() { _ = pipeWriter.Close() }()

	if isTerminal(pipeWriter.Fd()) {
		t.Fatal("isTerminal() = true for pipe writer, want false")
	}
}

func TestExitCodeForCancelError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "sigint maps to 130",
			err:  signalCause{sig: os.Interrupt},
			want: 130,
		},
		{
			name: "sigterm maps to 143",
			err:  signalCause{sig: syscall.SIGTERM},
			want: 143,
		},
		{
			name: "wrapped signal cause maps by contained cause",
			err:  fmt.Errorf("wrapped: %w", signalCause{sig: syscall.SIGTERM}),
			want: 143,
		},
		{
			name: "unknown error falls back to 130",
			err:  errors.New("cancelled"),
			want: 130,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := exitCodeForCancelError(tc.err)
			if got != tc.want {
				t.Fatalf("exitCodeForCancelError() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRunTimerReturnsCancelCause(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(signalCause{sig: syscall.SIGTERM})

	err := runTimer(ctx, time.Hour, false, false, false)
	if err == nil {
		t.Fatal("runTimer() error = nil, want cancellation cause")
	}

	if got := exitCodeForCancelError(err); got != 143 {
		t.Fatalf("runTimer() cancellation exit code = %d, want 143", got)
	}
}

func TestRunTimerWithAlarmStarter_ForceAlarmInNonTTY(t *testing.T) {
	ctx := context.Background()
	alarmCalls := 0

	err := runTimerWithAlarmStarter(ctx, 0, false, true, true, func() {
		alarmCalls++
	})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}
	if alarmCalls != 1 {
		t.Fatalf("runTimerWithAlarmStarter() alarm calls = %d, want 1", alarmCalls)
	}
}

func TestShouldTriggerAlarm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		interactive bool
		quiet       bool
		forceAlarm  bool
		want        bool
	}{
		{
			name:        "interactive non quiet without force",
			interactive: true,
			quiet:       false,
			forceAlarm:  false,
			want:        true,
		},
		{
			name:        "interactive quiet without force",
			interactive: true,
			quiet:       true,
			forceAlarm:  false,
			want:        false,
		},
		{
			name:        "non interactive quiet without force",
			interactive: false,
			quiet:       true,
			forceAlarm:  false,
			want:        false,
		},
		{
			name:        "non interactive quiet with force",
			interactive: false,
			quiet:       true,
			forceAlarm:  true,
			want:        true,
		},
		{
			name:        "interactive quiet with force",
			interactive: true,
			quiet:       true,
			forceAlarm:  true,
			want:        true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldTriggerAlarm(tc.interactive, tc.quiet, tc.forceAlarm)
			if got != tc.want {
				t.Fatalf("shouldTriggerAlarm(%v, %v, %v) = %v, want %v", tc.interactive, tc.quiet, tc.forceAlarm, got, tc.want)
			}
		})
	}
}

func TestShouldStartSleepInhibitor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		goos        string
		interactive bool
		want        bool
	}{
		{
			name:        "darwin interactive",
			goos:        "darwin",
			interactive: true,
			want:        true,
		},
		{
			name:        "darwin non interactive",
			goos:        "darwin",
			interactive: false,
			want:        false,
		},
		{
			name:        "linux interactive",
			goos:        "linux",
			interactive: true,
			want:        false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldStartSleepInhibitor(tc.goos, tc.interactive)
			if got != tc.want {
				t.Fatalf("shouldStartSleepInhibitor(%q, %v) = %v, want %v", tc.goos, tc.interactive, got, tc.want)
			}
		})
	}
}

func TestPlayAlarmAttempts_RemovesFailingBackendsAndFallsBack(t *testing.T) {
	t.Parallel()

	commands := []alarmCommand{
		{name: "broken-backend"},
		{name: "working-backend"},
	}
	var calls []string

	runner := func(command alarmCommand) error {
		calls = append(calls, command.name)
		if command.name == "broken-backend" {
			return errors.New("boom")
		}
		return nil
	}

	playAlarmAttempts(commands, 4, 0, runner)

	wantCalls := []string{
		"broken-backend",
		"working-backend",
		"working-backend",
		"working-backend",
		"working-backend",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("playAlarmAttempts() calls = %v, want %v", calls, wantCalls)
	}
}
