package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
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
		"  -q, --quiet      TTY: inline countdown only; non-TTY: suppress lifecycle/completion/cancel/alarm\n" +
		"  -s, --sound      Force alarm playback on completion even in quiet/non-TTY mode\n" +
		"  -c, --caffeinate Force sleep inhibition attempt even in non-TTY mode (darwin only)"

	got := renderHelpText()
	if got != want {
		t.Fatalf("renderHelpText() = %q, want %q", got, want)
	}
}

func TestFormatVersionLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "default dev version",
			version: "dev",
			want:    "timer dev\n",
		},
		{
			name:    "injected release version",
			version: "v1.2.3",
			want:    "timer v1.2.3\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatVersionLine(tc.version)
			if got != tc.want {
				t.Fatalf("formatVersionLine(%q) = %q, want %q", tc.version, got, tc.want)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		buildVersion  string
		moduleVersion string
		want          string
	}{
		{
			name:          "injected build version wins over module version",
			buildVersion:  "v1.2.3",
			moduleVersion: "v1.2.2",
			want:          "v1.2.3",
		},
		{
			name:          "default dev build version falls back to module version",
			buildVersion:  defaultVersion,
			moduleVersion: "v1.2.3",
			want:          "v1.2.3",
		},
		{
			name:          "devel module version falls back to build version",
			buildVersion:  defaultVersion,
			moduleVersion: develBuildInfoVersion,
			want:          defaultVersion,
		},
		{
			name:          "empty module version falls back to build version",
			buildVersion:  defaultVersion,
			moduleVersion: "",
			want:          defaultVersion,
		},
		{
			name:          "empty build version with module version uses module version",
			buildVersion:  "",
			moduleVersion: "v1.2.3",
			want:          "v1.2.3",
		},
		{
			name:          "empty build and module version falls back to default dev",
			buildVersion:  "",
			moduleVersion: "",
			want:          defaultVersion,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := resolveVersion(tc.buildVersion, tc.moduleVersion)
			if got != tc.want {
				t.Fatalf("resolveVersion(%q, %q) = %q, want %q", tc.buildVersion, tc.moduleVersion, got, tc.want)
			}
		})
	}
}

func TestAwakeUnsupportedWarning(t *testing.T) {
	t.Parallel()

	want := "Warning: --caffeinate sleep inhibition is only supported on darwin; continuing without sleep inhibition"
	got := awakeUnsupportedWarning()
	if got != want {
		t.Fatalf("awakeUnsupportedWarning() = %q, want %q", got, want)
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
		wantAwake    bool
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
			args:         []string{"timer", "--sound", "1s"},
			wantMode:     modeRun,
			wantDuration: 1 * time.Second,
			wantAlarm:    true,
		},
		{
			name:         "alarm short flag with duration",
			args:         []string{"timer", "-s", "1s"},
			wantMode:     modeRun,
			wantDuration: 1 * time.Second,
			wantAlarm:    true,
		},
		{
			name:         "alarm and quiet with duration",
			args:         []string{"timer", "--sound", "--quiet", "1s"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
			wantAlarm:    true,
		},
		{
			name:         "alarm short and quiet with duration",
			args:         []string{"timer", "-s", "-q", "1s"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
			wantAlarm:    true,
		},
		{
			name:         "awake long flag with duration",
			args:         []string{"timer", "--caffeinate", "1s"},
			wantMode:     modeRun,
			wantDuration: 1 * time.Second,
			wantAwake:    true,
		},
		{
			name:         "awake short flag with duration",
			args:         []string{"timer", "-c", "1s"},
			wantMode:     modeRun,
			wantDuration: 1 * time.Second,
			wantAwake:    true,
		},
		{
			name:         "awake and quiet with duration",
			args:         []string{"timer", "--caffeinate", "--quiet", "1s"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
			wantAwake:    true,
		},
		{
			name:         "awake short and quiet with duration",
			args:         []string{"timer", "-c", "-q", "1s"},
			wantMode:     modeRun,
			wantQuiet:    true,
			wantDuration: 1 * time.Second,
			wantAwake:    true,
		},
		{
			name:         "quiet and alarm with duration",
			args:         []string{"timer", "--quiet", "--sound", "1s"},
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
			args:    []string{"timer", "--sound"},
			wantErr: errUsage,
		},
		{
			name:    "alarm short without duration is usage error",
			args:    []string{"timer", "-s"},
			wantErr: errUsage,
		},
		{
			name:    "awake without duration is usage error",
			args:    []string{"timer", "--caffeinate"},
			wantErr: errUsage,
		},
		{
			name:    "awake short without duration is usage error",
			args:    []string{"timer", "-c"},
			wantErr: errUsage,
		},
		{
			name:     "help takes precedence over version",
			args:     []string{"timer", "--help", "--version"},
			wantMode: modeHelp,
		},
		{
			name:     "help takes precedence over alarm",
			args:     []string{"timer", "--help", "--sound"},
			wantMode: modeHelp,
		},
		{
			name:     "help takes precedence over awake",
			args:     []string{"timer", "--help", "--caffeinate"},
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
			args:      []string{"timer", "--version", "--sound"},
			wantMode:  modeVersion,
			wantAlarm: true,
		},
		{
			name:      "version with short alarm returns version mode with alarm set",
			args:      []string{"timer", "--version", "-s"},
			wantMode:  modeVersion,
			wantAlarm: true,
		},
		{
			name:      "version with awake returns version mode with awake set",
			args:      []string{"timer", "--version", "--caffeinate"},
			wantMode:  modeVersion,
			wantAwake: true,
		},
		{
			name:      "version with short awake returns version mode with awake set",
			args:      []string{"timer", "--version", "-c"},
			wantMode:  modeVersion,
			wantAwake: true,
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
			if got.forceAwake != tc.wantAwake {
				t.Fatalf("parseInvocation() awake = %v, want %v", got.forceAwake, tc.wantAwake)
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

	status := statusDisplay{
		writer:           io.Discard,
		interactive:      false,
		supportsAdvanced: false,
	}
	err := runTimer(ctx, time.Hour, status, false, false, false, false)
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

	status := statusDisplay{
		writer:           io.Discard,
		interactive:      false,
		supportsAdvanced: false,
	}

	err := runTimerWithAlarmStarter(ctx, 0, status, false, true, true, false, func() {
		alarmCalls++
	})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}
	if alarmCalls != 1 {
		t.Fatalf("runTimerWithAlarmStarter() alarm calls = %d, want 1", alarmCalls)
	}
}

func TestRunTimerWithAlarmStarter_DefaultAlarmRequiresBothStreamsTTY(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                   string
		statusInteractive      bool
		sideEffectsInteractive bool
		wantAlarmCalls         int
	}{
		{
			name:                   "both streams interactive triggers alarm",
			statusInteractive:      true,
			sideEffectsInteractive: true,
			wantAlarmCalls:         1,
		},
		{
			name:                   "stderr redirected suppresses default alarm",
			statusInteractive:      false,
			sideEffectsInteractive: true,
			wantAlarmCalls:         0,
		},
		{
			name:                   "stdout redirected suppresses default alarm",
			statusInteractive:      true,
			sideEffectsInteractive: false,
			wantAlarmCalls:         0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			alarmCalls := 0
			status := statusDisplay{
				writer:           io.Discard,
				interactive:      tc.statusInteractive,
				supportsAdvanced: false,
			}

			err := runTimerWithAlarmStarter(ctx, 0, status, tc.sideEffectsInteractive, false, false, false, func() {
				alarmCalls++
			})
			if err != nil {
				t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
			}
			if alarmCalls != tc.wantAlarmCalls {
				t.Fatalf("runTimerWithAlarmStarter() alarm calls = %d, want %d", alarmCalls, tc.wantAlarmCalls)
			}
		})
	}
}

func TestShouldTriggerAlarm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		sideEffectsInteractive bool
		quiet                  bool
		forceAlarm             bool
		want                   bool
	}{
		{
			name:                   "interactive non quiet without force",
			sideEffectsInteractive: true,
			quiet:                  false,
			forceAlarm:             false,
			want:                   true,
		},
		{
			name:                   "interactive quiet without force",
			sideEffectsInteractive: true,
			quiet:                  true,
			forceAlarm:             false,
			want:                   false,
		},
		{
			name:                   "non interactive non quiet without force",
			sideEffectsInteractive: false,
			quiet:                  false,
			forceAlarm:             false,
			want:                   false,
		},
		{
			name:                   "non interactive quiet without force",
			sideEffectsInteractive: false,
			quiet:                  true,
			forceAlarm:             false,
			want:                   false,
		},
		{
			name:                   "non interactive quiet with force",
			sideEffectsInteractive: false,
			quiet:                  true,
			forceAlarm:             true,
			want:                   true,
		},
		{
			name:                   "interactive quiet with force",
			sideEffectsInteractive: true,
			quiet:                  true,
			forceAlarm:             true,
			want:                   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldTriggerAlarm(tc.sideEffectsInteractive, tc.quiet, tc.forceAlarm)
			if got != tc.want {
				t.Fatalf("shouldTriggerAlarm(%v, %v, %v) = %v, want %v", tc.sideEffectsInteractive, tc.quiet, tc.forceAlarm, got, tc.want)
			}
		})
	}
}

func TestShouldStartSleepInhibitor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		goos                   string
		sideEffectsInteractive bool
		forceAwake             bool
		want                   bool
	}{
		{
			name:                   "darwin interactive",
			goos:                   "darwin",
			sideEffectsInteractive: true,
			forceAwake:             false,
			want:                   true,
		},
		{
			name:                   "darwin non interactive",
			goos:                   "darwin",
			sideEffectsInteractive: false,
			forceAwake:             false,
			want:                   false,
		},
		{
			name:                   "darwin non interactive with awake force",
			goos:                   "darwin",
			sideEffectsInteractive: false,
			forceAwake:             true,
			want:                   true,
		},
		{
			name:                   "linux interactive with awake force",
			goos:                   "linux",
			sideEffectsInteractive: true,
			forceAwake:             true,
			want:                   false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldStartSleepInhibitor(tc.goos, tc.sideEffectsInteractive, tc.forceAwake)
			if got != tc.want {
				t.Fatalf("shouldStartSleepInhibitor(%q, %v, %v) = %v, want %v", tc.goos, tc.sideEffectsInteractive, tc.forceAwake, got, tc.want)
			}
		})
	}
}

func TestRunTimerWithAlarmStarter_NonTTYLifecycleOutput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var out bytes.Buffer
	status := statusDisplay{
		writer:           &out,
		interactive:      false,
		supportsAdvanced: false,
	}

	err := runTimerWithAlarmStarter(ctx, 0, status, false, false, false, false, func() {})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}

	want := "timer: started (0s)\ntimer: complete\n"
	if got := out.String(); got != want {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want %q", got, want)
	}
}

func TestRunTimerWithAlarmStarter_NonTTYQuietSuppressesLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var out bytes.Buffer
	status := statusDisplay{
		writer:           &out,
		interactive:      false,
		supportsAdvanced: false,
	}

	err := runTimerWithAlarmStarter(ctx, 0, status, false, true, false, false, func() {})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want empty output", got)
	}
}

func TestRunTimerWithAlarmStarter_NonTTYCancelLifecycleOutput(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(signalCause{sig: os.Interrupt})

	var out bytes.Buffer
	status := statusDisplay{
		writer:           &out,
		interactive:      false,
		supportsAdvanced: false,
	}

	err := runTimerWithAlarmStarter(ctx, 10*time.Second, status, false, false, false, false, func() {})
	if err == nil {
		t.Fatal("runTimerWithAlarmStarter() error = nil, want cancellation cause")
	}

	want := "timer: cancelled\n"
	if got := out.String(); got != want {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want %q", got, want)
	}
}

func TestRunTimerWithAlarmStarter_InteractiveWritesToStatusWriter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var out bytes.Buffer
	status := statusDisplay{
		writer:           &out,
		interactive:      true,
		supportsAdvanced: false,
	}

	err := runTimerWithAlarmStarter(ctx, 0, status, false, false, false, false, func() {})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "timer complete\n") {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want timer completion text", out.String())
	}
}

func TestRunTimerWithAlarmStarter_InteractiveQuietClearsStatusLine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var out bytes.Buffer
	status := statusDisplay{
		writer:           &out,
		interactive:      true,
		supportsAdvanced: true,
	}

	err := runTimerWithAlarmStarter(ctx, 0, status, false, true, false, false, func() {})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}
	if got := out.String(); got != "\r\033[K" {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want %q", got, "\r\\033[K")
	}
}

func TestShouldPrintLifecycleStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		interactive bool
		quiet       bool
		want        bool
	}{
		{name: "non interactive non quiet", interactive: false, quiet: false, want: true},
		{name: "non interactive quiet", interactive: false, quiet: true, want: false},
		{name: "interactive non quiet", interactive: true, quiet: false, want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldPrintLifecycleStart(tc.interactive, tc.quiet)
			if got != tc.want {
				t.Fatalf("shouldPrintLifecycleStart(%v, %v) = %v, want %v", tc.interactive, tc.quiet, got, tc.want)
			}
		})
	}
}

func TestSupportsAdvancedTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		term string
		want bool
	}{
		{name: "xterm supports advanced", term: "xterm-256color", want: true},
		{name: "dumb does not support advanced", term: "dumb", want: false},
		{name: "empty term does not support advanced", term: "", want: false},
		{name: "whitespace and case are normalized", term: " DUMB ", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := supportsAdvancedTerminal(tc.term)
			if got != tc.want {
				t.Fatalf("supportsAdvancedTerminal(%q) = %v, want %v", tc.term, got, tc.want)
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
