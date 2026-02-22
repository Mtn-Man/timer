package main

import (
	"errors"
	"reflect"
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

func TestParseRequestedDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantDuration time.Duration
		wantErr      error
	}{
		{
			name:    "usage when no duration arg",
			args:    []string{"timer"},
			wantErr: errUsage,
		},
		{
			name:    "usage when extra arg is provided",
			args:    []string{"timer", "1s", "extra"},
			wantErr: errUsage,
		},
		{
			name:    "invalid duration format",
			args:    []string{"timer", "abc"},
			wantErr: errInvalidDuration,
		},
		{
			name:    "duration must be positive for zero",
			args:    []string{"timer", "0s"},
			wantErr: errDurationMustBeOver,
		},
		{
			name:    "duration must be positive for negative",
			args:    []string{"timer", "-1s"},
			wantErr: errDurationMustBeOver,
		},
		{
			name:         "valid short duration",
			args:         []string{"timer", "1s"},
			wantDuration: 1 * time.Second,
		},
		{
			name:         "valid compound duration",
			args:         []string{"timer", "1h30m"},
			wantDuration: 90 * time.Minute,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotDuration, err := parseRequestedDuration(tc.args)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseRequestedDuration() error = %v, want %v", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseRequestedDuration() unexpected error = %v", err)
			}
			if gotDuration != tc.wantDuration {
				t.Fatalf("parseRequestedDuration() duration = %v, want %v", gotDuration, tc.wantDuration)
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
			name:     "help takes precedence over version",
			args:     []string{"timer", "--help", "--version"},
			wantMode: modeHelp,
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

			gotMode, gotDuration, err := parseInvocation(tc.args)
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
