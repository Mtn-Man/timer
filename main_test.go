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
		want bool
	}{
		{
			name: "worker mode with exact hidden worker arg",
			args: []string{"timer", internalAlarmArg},
			want: true,
		},
		{
			name: "normal mode when no args",
			args: []string{"timer"},
			want: false,
		},
		{
			name: "normal mode with duration arg",
			args: []string{"timer", "1s"},
			want: false,
		},
		{
			name: "normal mode when hidden worker arg has trailing args",
			args: []string{"timer", internalAlarmArg, "1s"},
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldRunInternalAlarm(tc.args)
			if got != tc.want {
				t.Fatalf("shouldRunInternalAlarm() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewInternalAlarmCmd(t *testing.T) {
	t.Parallel()

	cmd := newInternalAlarmCmd("/tmp/timer-bin")
	if len(cmd.Args) != 2 {
		t.Fatalf("newInternalAlarmCmd() args length = %d, want 2", len(cmd.Args))
	}
	if cmd.Args[0] != "/tmp/timer-bin" {
		t.Fatalf("newInternalAlarmCmd() args[0] = %q, want %q", cmd.Args[0], "/tmp/timer-bin")
	}
	if cmd.Args[1] != internalAlarmArg {
		t.Fatalf("newInternalAlarmCmd() args[1] = %q, want %q", cmd.Args[1], internalAlarmArg)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("newInternalAlarmCmd() should set Setpgid=true")
	}
	for _, envVar := range cmd.Env {
		if strings.HasPrefix(envVar, "TIMER_INTERNAL_ALARM=") {
			t.Fatalf("newInternalAlarmCmd() should not set TIMER_INTERNAL_ALARM env, got %q", envVar)
		}
	}
}

func TestRenderHelpText(t *testing.T) {
	t.Parallel()

	want := usageText + "\n\nFlags:\n" +
		"  -h, --help       Show help and exit\n" +
		"  -v, --version    Show version and exit\n" +
		"  -q, --quiet      TTY: inline countdown only; non-TTY: suppress lifecycle/completion/cancel/alarm\n" +
		"  -s, --sound      Force alarm playback on completion even in quiet/non-TTY mode\n" +
		"  -c, --caffeinate Force sleep inhibition attempt even in non-TTY mode (darwin only)\n\n" +
		"Note: -- ends option parsing; subsequent tokens are treated as positional arguments.\n"

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

func TestRenderInvocationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantMessage  string
		wantExitCode int
	}{
		{
			name:         "unknown option includes help and exit code 2",
			err:          unknownOptionError{option: "--wat"},
			wantMessage:  "unknown option: --wat\n\n" + renderHelpText(),
			wantExitCode: 2,
		},
		{
			name:         "usage error keeps usage text",
			err:          errUsage,
			wantMessage:  usageText + "\n",
			wantExitCode: 2,
		},
		{
			name:         "invalid duration keeps prior message",
			err:          errInvalidDuration,
			wantMessage:  "Error: invalid duration format",
			wantExitCode: 2,
		},
		{
			name:         "negative duration keeps prior message",
			err:          errDurationMustBeAtLeastZero,
			wantMessage:  "Error: duration must be >= 0",
			wantExitCode: 2,
		},
		{
			name:         "fallback parse rendering returns exit code 2",
			err:          errors.New("x"),
			wantMessage:  "Error: x",
			wantExitCode: 2,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotMessage, gotExitCode := renderInvocationError(tc.err)
			if gotMessage != tc.wantMessage {
				t.Fatalf("renderInvocationError() message = %q, want %q", gotMessage, tc.wantMessage)
			}
			if gotExitCode != tc.wantExitCode {
				t.Fatalf("renderInvocationError() exit code = %d, want %d", gotExitCode, tc.wantExitCode)
			}
		})
	}
}

type parseInvocationTestCase struct {
	name        string
	args        []string
	want        invocation
	wantUnknown string
	wantErr     error
}

func cliArgs(parts ...string) []string {
	return append([]string{"timer"}, parts...)
}

func runParseInvocationCases(t *testing.T, tests []parseInvocationTestCase) {
	t.Helper()

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseInvocation(tc.args)
			switch {
			case tc.wantUnknown != "":
				var unknownErr unknownOptionError
				if !errors.As(err, &unknownErr) {
					t.Fatalf("parseInvocation() error = %v, want unknown option error", err)
				}
				if unknownErr.option != tc.wantUnknown {
					t.Fatalf("parseInvocation() unknown option = %q, want %q", unknownErr.option, tc.wantUnknown)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseInvocation() error = %v, want %v", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("parseInvocation() unexpected error = %v", err)
				}
				if got != tc.want {
					t.Fatalf("parseInvocation() = %+v, want %+v", got, tc.want)
				}
			}
		})
	}
}

func TestParseInvocation_HelpAndVersionModes(t *testing.T) {
	t.Parallel()

	runParseInvocationCases(t, []parseInvocationTestCase{
		{name: "help short flag", args: cliArgs("-h"), want: invocation{mode: modeHelp}},
		{name: "help long flag", args: cliArgs("--help"), want: invocation{mode: modeHelp}},
		{name: "help flag wins with extra args", args: cliArgs("--help", "10s"), want: invocation{mode: modeHelp}},
		{name: "help before double dash still returns help", args: cliArgs("--help", "--", "10s"), want: invocation{mode: modeHelp}},
		{name: "help takes precedence over version", args: cliArgs("--help", "--version"), want: invocation{mode: modeHelp}},
		{name: "help takes precedence over alarm", args: cliArgs("--help", "--sound"), want: invocation{mode: modeHelp}},
		{name: "help takes precedence over awake", args: cliArgs("--help", "--caffeinate"), want: invocation{mode: modeHelp}},
		{name: "quiet and help returns help mode", args: cliArgs("--quiet", "--help"), want: invocation{mode: modeHelp}},
		{name: "version short flag", args: cliArgs("-v"), want: invocation{mode: modeVersion}},
		{name: "version long flag", args: cliArgs("--version"), want: invocation{mode: modeVersion}},
		{name: "version flag wins with extra args", args: cliArgs("--version", "10s"), want: invocation{mode: modeVersion}},
		{name: "version before double dash ignores post-option-like token", args: cliArgs("--version", "--", "--wat"), want: invocation{mode: modeVersion}},
		{name: "quiet and version returns version mode with quiet set", args: cliArgs("--quiet", "--version"), want: invocation{mode: modeVersion, quiet: true}},
		{name: "version with alarm returns version mode with alarm set", args: cliArgs("--version", "--sound"), want: invocation{mode: modeVersion, forceAlarm: true}},
		{name: "version with short alarm returns version mode with alarm set", args: cliArgs("--version", "-s"), want: invocation{mode: modeVersion, forceAlarm: true}},
		{name: "version with awake returns version mode with awake set", args: cliArgs("--version", "--caffeinate"), want: invocation{mode: modeVersion, forceAwake: true}},
		{name: "version with short awake returns version mode with awake set", args: cliArgs("--version", "-c"), want: invocation{mode: modeVersion, forceAwake: true}},
		{name: "double dash then help token is positional and invalid duration", args: cliArgs("--", "--help"), wantErr: errInvalidDuration},
		{name: "double dash then version token is positional and invalid duration", args: cliArgs("--", "--version"), wantErr: errInvalidDuration},
	})
}

func TestParseInvocation_RunModeFlagsAndDuration(t *testing.T) {
	t.Parallel()

	runParseInvocationCases(t, []parseInvocationTestCase{
		{name: "valid duration invocation", args: cliArgs("1s"), want: invocation{mode: modeRun, duration: time.Second}},
		{name: "zero duration invocation", args: cliArgs("0s"), want: invocation{mode: modeRun, duration: 0}},
		{name: "double dash allows duration token", args: cliArgs("--", "1s"), want: invocation{mode: modeRun, duration: time.Second}},
		{name: "double dash allows negative duration validation", args: cliArgs("--", "-1s"), wantErr: errDurationMustBeAtLeastZero},
		{name: "quiet short flag with duration", args: cliArgs("-q", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true}},
		{name: "quiet long flag with duration", args: cliArgs("--quiet", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true}},
		{name: "quiet before double dash still applies", args: cliArgs("--quiet", "--", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true}},
		{name: "duration then quiet flag", args: cliArgs("1s", "-q"), want: invocation{mode: modeRun, duration: time.Second, quiet: true}},
		{name: "alarm long flag with duration", args: cliArgs("--sound", "1s"), want: invocation{mode: modeRun, duration: time.Second, forceAlarm: true}},
		{name: "alarm short flag with duration", args: cliArgs("-s", "1s"), want: invocation{mode: modeRun, duration: time.Second, forceAlarm: true}},
		{name: "alarm and quiet with duration", args: cliArgs("--sound", "--quiet", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true}},
		{name: "alarm short and quiet with duration", args: cliArgs("-s", "-q", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true}},
		{name: "awake long flag with duration", args: cliArgs("--caffeinate", "1s"), want: invocation{mode: modeRun, duration: time.Second, forceAwake: true}},
		{name: "awake short flag with duration", args: cliArgs("-c", "1s"), want: invocation{mode: modeRun, duration: time.Second, forceAwake: true}},
		{name: "awake and quiet with duration", args: cliArgs("--caffeinate", "--quiet", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAwake: true}},
		{name: "awake short and quiet with duration", args: cliArgs("-c", "-q", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAwake: true}},
		{name: "quiet and alarm with duration", args: cliArgs("--quiet", "--sound", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true}},
		{name: "alarm and awake together", args: cliArgs("--sound", "--caffeinate", "1s"), want: invocation{mode: modeRun, duration: time.Second, forceAlarm: true, forceAwake: true}},
	})
}

func TestParseInvocation_UnknownOptions(t *testing.T) {
	t.Parallel()

	runParseInvocationCases(t, []parseInvocationTestCase{
		{name: "unknown short flag returns unknown option", args: cliArgs("-x"), wantUnknown: "-x"},
		{name: "unknown long flag returns unknown option", args: cliArgs("--wat"), wantUnknown: "--wat"},
		{name: "unknown before double dash still returns unknown option", args: cliArgs("--wat", "--", "1s"), wantUnknown: "--wat"},
		{name: "unknown flag takes precedence over help", args: cliArgs("--help", "--wat"), wantUnknown: "--wat"},
		{name: "unknown flag takes precedence over help when unknown comes first", args: cliArgs("--wat", "--help"), wantUnknown: "--wat"},
		{name: "unknown flag takes precedence over version", args: cliArgs("--version", "--wat"), wantUnknown: "--wat"},
		{name: "unknown flag takes precedence over version when unknown comes first", args: cliArgs("--wat", "--version"), wantUnknown: "--wat"},
		{name: "first unknown option is retained", args: cliArgs("--wat", "--oops", "1s"), wantUnknown: "--wat"},
		{name: "double dash then unknown-looking token is positional invalid duration", args: cliArgs("--", "--wat"), wantErr: errInvalidDuration},
		{name: "double dash positional unknown then extra positional is usage", args: cliArgs("--", "--wat", "--oops"), wantErr: errUsage},
	})
}

func TestParseInvocation_UsageAndDurationErrors(t *testing.T) {
	t.Parallel()

	runParseInvocationCases(t, []parseInvocationTestCase{
		{name: "usage when no args", args: cliArgs(), wantErr: errUsage},
		{name: "double dash alone is usage error", args: cliArgs("--"), wantErr: errUsage},
		{name: "quiet without duration is usage error", args: cliArgs("-q"), wantErr: errUsage},
		{name: "alarm without duration is usage error", args: cliArgs("--sound"), wantErr: errUsage},
		{name: "alarm short without duration is usage error", args: cliArgs("-s"), wantErr: errUsage},
		{name: "awake without duration is usage error", args: cliArgs("--caffeinate"), wantErr: errUsage},
		{name: "awake short without duration is usage error", args: cliArgs("-c"), wantErr: errUsage},
		{name: "multiple duration tokens is usage error", args: cliArgs("1s", "2s"), wantErr: errUsage},
		{name: "double dash then multiple duration tokens is usage error", args: cliArgs("--", "1s", "2s"), wantErr: errUsage},
		{name: "invalid duration format", args: cliArgs("abc"), wantErr: errInvalidDuration},
		{name: "negative duration remains duration validation error", args: cliArgs("-1s"), wantErr: errDurationMustBeAtLeastZero},
	})
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

func newStatusDisplay(writer io.Writer, interactive bool, supportsAdvanced bool) statusDisplay {
	return statusDisplay{
		writer:           writer,
		interactive:      interactive,
		supportsAdvanced: supportsAdvanced,
	}
}

func newCapturedStatus(interactive bool, supportsAdvanced bool) (*bytes.Buffer, statusDisplay) {
	var out bytes.Buffer
	return &out, newStatusDisplay(&out, interactive, supportsAdvanced)
}

func TestRunTimerWithAlarmStarter_ForceAlarmInNonTTY(t *testing.T) {
	ctx := context.Background()
	alarmCalls := 0

	status := newStatusDisplay(io.Discard, false, false)

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
			status := newStatusDisplay(io.Discard, tc.statusInteractive, false)

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
		name              string
		goos              string
		stdoutInteractive bool
		statusInteractive bool
		forceAwake        bool
		want              bool
	}{
		{
			name:              "darwin both streams interactive",
			goos:              "darwin",
			stdoutInteractive: true,
			statusInteractive: true,
			forceAwake:        false,
			want:              true,
		},
		{
			name:              "darwin stdout interactive only",
			goos:              "darwin",
			stdoutInteractive: true,
			statusInteractive: false,
			forceAwake:        false,
			want:              false,
		},
		{
			name:              "darwin stderr interactive only",
			goos:              "darwin",
			stdoutInteractive: false,
			statusInteractive: true,
			forceAwake:        false,
			want:              false,
		},
		{
			name:              "darwin non interactive with awake force",
			goos:              "darwin",
			stdoutInteractive: false,
			statusInteractive: false,
			forceAwake:        true,
			want:              true,
		},
		{
			name:              "linux interactive with awake force",
			goos:              "linux",
			stdoutInteractive: true,
			statusInteractive: true,
			forceAwake:        true,
			want:              false,
		},
		{
			name:              "linux both streams interactive without force",
			goos:              "linux",
			stdoutInteractive: true,
			statusInteractive: true,
			forceAwake:        false,
			want:              false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldStartSleepInhibitor(tc.goos, tc.stdoutInteractive, tc.statusInteractive, tc.forceAwake)
			if got != tc.want {
				t.Fatalf("shouldStartSleepInhibitor(%q, %v, %v, %v) = %v, want %v", tc.goos, tc.stdoutInteractive, tc.statusInteractive, tc.forceAwake, got, tc.want)
			}
		})
	}
}

func TestRunTimerWithAlarmStarter_NonTTYLifecycleOutput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	out, status := newCapturedStatus(false, false)

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
	out, status := newCapturedStatus(false, false)

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

	out, status := newCapturedStatus(false, false)

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
	out, status := newCapturedStatus(true, false)

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
	out, status := newCapturedStatus(true, true)

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
