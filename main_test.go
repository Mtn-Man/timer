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

func TestParseInvocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantMode     invocationMode
		wantDuration time.Duration
		wantQuiet    bool
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
			name:     "help takes precedence over version",
			args:     []string{"timer", "--help", "--version"},
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
			name:    "invalid duration format",
			args:    []string{"timer", "abc"},
			wantErr: errInvalidDuration,
		},
		{
			name:    "negative duration remains duration validation error",
			args:    []string{"timer", "-1s"},
			wantErr: errDurationMustBeOver,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotMode, gotDuration, gotQuiet, err := parseInvocation(tc.args)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseInvocation() error = %v, want %v", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseInvocation() unexpected error = %v", err)
			}
			if gotMode != tc.wantMode {
				t.Fatalf("parseInvocation() mode = %v, want %v", gotMode, tc.wantMode)
			}
			if gotDuration != tc.wantDuration {
				t.Fatalf("parseInvocation() duration = %v, want %v", gotDuration, tc.wantDuration)
			}
			if gotQuiet != tc.wantQuiet {
				t.Fatalf("parseInvocation() quiet = %v, want %v", gotQuiet, tc.wantQuiet)
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

	err := runTimer(ctx, time.Hour, false, false)
	if err == nil {
		t.Fatal("runTimer() error = nil, want cancellation cause")
	}

	if got := exitCodeForCancelError(err); got != 143 {
		t.Fatalf("runTimer() cancellation exit code = %d, want 143", got)
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
