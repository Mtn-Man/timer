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
			args: []string{"after", internalAlarmArg},
			want: true,
		},
		{
			name: "worker mode with sound file arg",
			args: []string{"after", internalAlarmArg, "path/to/sound.mp3"},
			want: true,
		},
		{
			name: "normal mode when no args",
			args: []string{"after"},
			want: false,
		},
		{
			name: "normal mode with duration arg",
			args: []string{"after", "1s"},
			want: false,
		},
		{
			name: "normal mode when hidden worker arg has trailing args",
			args: []string{"after", internalAlarmArg, "path/to/sound.mp3", "1s"},
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

	t.Run("without sound file", func(t *testing.T) {
		cmd := newInternalAlarmCmd("/tmp/after-bin", "")
		if len(cmd.Args) != 2 {
			t.Fatalf("newInternalAlarmCmd() args length = %d, want 2", len(cmd.Args))
		}
		if cmd.Args[0] != "/tmp/after-bin" {
			t.Fatalf("newInternalAlarmCmd() args[0] = %q, want %q", cmd.Args[0], "/tmp/after-bin")
		}
		if cmd.Args[1] != internalAlarmArg {
			t.Fatalf("newInternalAlarmCmd() args[1] = %q, want %q", cmd.Args[1], internalAlarmArg)
		}
	})

	t.Run("with sound file", func(t *testing.T) {
		cmd := newInternalAlarmCmd("/tmp/after-bin", "path/to/sound.mp3")
		if len(cmd.Args) != 3 {
			t.Fatalf("newInternalAlarmCmd() args length = %d, want 3", len(cmd.Args))
		}
		if cmd.Args[1] != internalAlarmArg {
			t.Fatalf("newInternalAlarmCmd() args[1] = %q, want %q", cmd.Args[1], internalAlarmArg)
		}
		if cmd.Args[2] != "path/to/sound.mp3" {
			t.Fatalf("newInternalAlarmCmd() args[2] = %q, want %q", cmd.Args[2], "path/to/sound.mp3")
		}
	})

	cmd := newInternalAlarmCmd("/tmp/after-bin", "")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("newInternalAlarmCmd() should set Setpgid=true")
	}
}

func TestRenderHelpText(t *testing.T) {
	t.Parallel()

	want := usageText + "\n\nFlags:\n" +
		"  -h, --help       Show help and exit\n" +
		"  -v, --version    Show version and exit\n" +
		"  -q, --quiet      TTY: inline countdown only; non-TTY: suppress lifecycle/completion/cancel/alarm\n" +
		"  -s, --sound      Force alarm playback on completion even in quiet/non-TTY mode\n" +
		"  -f, --sound-file Path to a custom audio file to play on completion (implies --sound)\n" +
		"  -t, --no-title   Disable terminal title bar updates\n" +
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
			want:    "after dev\n",
		},
		{
			name:    "injected release version",
			version: "v1.2.3",
			want:    "after v1.2.3\n",
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

func TestSoundFileWarning(t *testing.T) {
	t.Parallel()

	want := "Warning: sound file not found or unreadable: ~/sounds/bell.mp3; using default alarm"
	got := soundFileWarning("~/sounds/bell.mp3")
	if got != want {
		t.Fatalf("soundFileWarning() = %q, want %q", got, want)
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
			name:         "invalid time keeps prior message",
			err:          errInvalidTime,
			wantMessage:  "Error: invalid time format",
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

func TestResolveSoundFilePath(t *testing.T) {
	t.Run("leaves plain path unchanged", func(t *testing.T) {
		got, err := resolveSoundFilePath("path/to/sound.mp3")
		if err != nil {
			t.Fatalf("resolveSoundFilePath() error = %v, want nil", err)
		}
		if got != "path/to/sound.mp3" {
			t.Fatalf("resolveSoundFilePath() = %q, want %q", got, "path/to/sound.mp3")
		}
	})

	t.Run("expands bare home", func(t *testing.T) {
		t.Setenv("HOME", "/tmp/test-home")

		got, err := resolveSoundFilePath("~")
		if err != nil {
			t.Fatalf("resolveSoundFilePath() error = %v, want nil", err)
		}
		if got != "/tmp/test-home" {
			t.Fatalf("resolveSoundFilePath() = %q, want %q", got, "/tmp/test-home")
		}
	})

	t.Run("expands home relative path", func(t *testing.T) {
		t.Setenv("HOME", "/tmp/test-home")

		got, err := resolveSoundFilePath("~/Music/AudioBooks YT/book.mp3")
		if err != nil {
			t.Fatalf("resolveSoundFilePath() error = %v, want nil", err)
		}
		if got != "/tmp/test-home/Music/AudioBooks YT/book.mp3" {
			t.Fatalf("resolveSoundFilePath() = %q, want %q", got, "/tmp/test-home/Music/AudioBooks YT/book.mp3")
		}
	})
}

func TestResolveUsableSoundFilePath(t *testing.T) {
	t.Run("keeps existing file", func(t *testing.T) {
		tempFile, err := os.CreateTemp(t.TempDir(), "sound-*.mp3")
		if err != nil {
			t.Fatalf("CreateTemp() error = %v", err)
		}
		defer func() { _ = tempFile.Close() }()

		got := resolveUsableSoundFilePath(tempFile.Name())
		if got != tempFile.Name() {
			t.Fatalf("resolveUsableSoundFilePath() = %q, want %q", got, tempFile.Name())
		}
	})

	t.Run("falls back when file does not exist", func(t *testing.T) {
		got := resolveUsableSoundFilePath("/definitely/missing/sound.mp3")
		if got != "" {
			t.Fatalf("resolveUsableSoundFilePath() = %q, want empty string", got)
		}
	})

	t.Run("falls back when path is a directory", func(t *testing.T) {
		got := resolveUsableSoundFilePath(t.TempDir())
		if got != "" {
			t.Fatalf("resolveUsableSoundFilePath() = %q, want empty string", got)
		}
	})

	t.Run("falls back when home-relative file does not exist", func(t *testing.T) {
		t.Setenv("HOME", "/tmp/test-home")

		got := resolveUsableSoundFilePath("~/missing.mp3")
		if got != "" {
			t.Fatalf("resolveUsableSoundFilePath() = %q, want empty string", got)
		}
	})
}

type parseInvocationTestCase struct {
	name              string
	args              []string
	want              invocation
	wantUnknown       string
	wantErr           error
	skipDurationCheck bool
}

func cliArgs(parts ...string) []string {
	return append([]string{"after"}, parts...)
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
				if tc.skipDurationCheck {
					got.duration = tc.want.duration
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
		{name: "help from combined short flags", args: cliArgs("-hv"), want: invocation{mode: modeHelp}},
		{name: "help flag wins with extra args", args: cliArgs("--help", "10s"), want: invocation{mode: modeHelp}},
		{name: "help before double dash still returns help", args: cliArgs("--help", "--", "10s"), want: invocation{mode: modeHelp}},
		{name: "help takes precedence over version", args: cliArgs("--help", "--version"), want: invocation{mode: modeHelp}},
		{name: "help takes precedence over alarm", args: cliArgs("--help", "--sound"), want: invocation{mode: modeHelp}},
		{name: "help takes precedence over awake", args: cliArgs("--help", "--caffeinate"), want: invocation{mode: modeHelp}},
		{name: "quiet and help returns help mode", args: cliArgs("--quiet", "--help"), want: invocation{mode: modeHelp}},
		{name: "version short flag", args: cliArgs("-v"), want: invocation{mode: modeVersion}},
		{name: "version long flag", args: cliArgs("--version"), want: invocation{mode: modeVersion}},
		{name: "version from combined short flags keeps quiet", args: cliArgs("-vq"), want: invocation{mode: modeVersion, quiet: true}},
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
		{name: "bare integer duration is seconds", args: cliArgs("5"), want: invocation{mode: modeRun, duration: 5 * time.Second}},
		{name: "bare decimal duration is seconds", args: cliArgs("0.5"), want: invocation{mode: modeRun, duration: 500 * time.Millisecond}},
		{name: "bare leading-dot decimal duration is seconds", args: cliArgs(".5"), want: invocation{mode: modeRun, duration: 500 * time.Millisecond}},
		{name: "bare trailing-dot decimal duration is seconds", args: cliArgs("5."), want: invocation{mode: modeRun, duration: 5 * time.Second}},
		{name: "bare positive-signed integer duration is seconds", args: cliArgs("+5"), want: invocation{mode: modeRun, duration: 5 * time.Second}},
		{name: "double dash allows duration token", args: cliArgs("--", "1s"), want: invocation{mode: modeRun, duration: time.Second}},
		{name: "double dash allows negative duration validation", args: cliArgs("--", "-1s"), wantErr: errDurationMustBeAtLeastZero},
		{name: "quiet short flag with duration", args: cliArgs("-q", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true}},
		{name: "quiet long flag with duration", args: cliArgs("--quiet", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true}},
		{name: "combined quiet and alarm short flags with duration", args: cliArgs("-qs", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true}},
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
		{name: "no-title long flag with duration", args: cliArgs("--no-title", "1s"), want: invocation{mode: modeRun, duration: time.Second, noTitle: true}},
		{name: "no-title short flag with duration", args: cliArgs("-t", "1s"), want: invocation{mode: modeRun, duration: time.Second, noTitle: true}},
		{name: "no-title and quiet with duration", args: cliArgs("--no-title", "--quiet", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, noTitle: true}},
		{name: "quiet alone does not set noTitle", args: cliArgs("-q", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, noTitle: false}},
		{name: "combined no-title and quiet short flags", args: cliArgs("-tq", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, noTitle: true}},
		{name: "quiet and alarm with duration", args: cliArgs("--quiet", "--sound", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true}},
		{name: "alarm and awake together", args: cliArgs("--sound", "--caffeinate", "1s"), want: invocation{mode: modeRun, duration: time.Second, forceAlarm: true, forceAwake: true}},
		{name: "sound file long flag", args: cliArgs("--sound-file", "path/to/sound.mp3", "1s"), want: invocation{mode: modeRun, duration: time.Second, forceAlarm: true, soundFile: "path/to/sound.mp3"}},
		{name: "sound file short flag", args: cliArgs("-f", "path/to/sound.mp3", "1s"), want: invocation{mode: modeRun, duration: time.Second, forceAlarm: true, soundFile: "path/to/sound.mp3"}},
		{name: "combined quiet and sound file short flags", args: cliArgs("-qf", "path/to/sound.mp3", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true, soundFile: "path/to/sound.mp3"}},
		{name: "combined sound file then quiet short flags", args: cliArgs("-fq", "path/to/sound.mp3", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true, soundFile: "path/to/sound.mp3"}},
		{name: "combined quiet sound file awake short flags", args: cliArgs("-qfc", "path/to/sound.mp3", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true, forceAwake: true, soundFile: "path/to/sound.mp3"}},
		{name: "combined awake sound file quiet short flags", args: cliArgs("-cfq", "path/to/sound.mp3", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true, forceAwake: true, soundFile: "path/to/sound.mp3"}},
		{name: "sound file and quiet", args: cliArgs("--quiet", "--sound-file", "path/to/sound.mp3", "1s"), want: invocation{mode: modeRun, duration: time.Second, quiet: true, forceAlarm: true, soundFile: "path/to/sound.mp3"}},
		{name: "sound file as last arg returns usage error", args: cliArgs("1s", "--sound-file"), wantErr: errUsage},
		{name: "short sound file as last arg returns usage error", args: cliArgs("1s", "-f"), wantErr: errUsage},
		{name: "space-separated AM/PM token is consumed as part of time arg", args: cliArgs("3:00", "pm"), wantErr: nil, want: invocation{mode: modeRun}, skipDurationCheck: true},
		{name: "space-separated AM/PM with leading flag still parses", args: cliArgs("-q", "3:00", "pm"), wantErr: nil, want: invocation{mode: modeRun, quiet: true}, skipDurationCheck: true},
		{name: "space-separated AM/PM with trailing flag still parses", args: cliArgs("3:00", "pm", "-q"), wantErr: nil, want: invocation{mode: modeRun, quiet: true}, skipDurationCheck: true},
		{name: "invalid time with space-separated AM/PM returns invalid time error", args: cliArgs("13:00", "pm"), wantErr: errInvalidTime},
		{name: "a shorthand attached", args: cliArgs("7a"), wantErr: nil, want: invocation{mode: modeRun}, skipDurationCheck: true},
		{name: "p shorthand attached", args: cliArgs("7p"), wantErr: nil, want: invocation{mode: modeRun}, skipDurationCheck: true},
		{name: "space-separated p shorthand token is consumed as part of time arg", args: cliArgs("3:00", "p"), wantErr: nil, want: invocation{mode: modeRun}, skipDurationCheck: true},
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
		{name: "combined short flag with unknown member remains single unknown", args: cliArgs("-qx"), wantUnknown: "-qx"},
		{name: "combined short flag with repeated sound file returns usage", args: cliArgs("-ff", "path/to/sound.mp3", "1s"), wantErr: errUsage},
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
		{name: "double dash then combined short token remains positional invalid duration", args: cliArgs("--", "-qs"), wantErr: errInvalidDuration},
		{name: "invalid duration format", args: cliArgs("abc"), wantErr: errInvalidDuration},
		{name: "negative duration remains duration validation error", args: cliArgs("-1s"), wantErr: errDurationMustBeAtLeastZero},
		{name: "bare negative integer remains duration validation error", args: cliArgs("-1"), wantErr: errDurationMustBeAtLeastZero},
		{name: "bare negative decimal remains duration validation error", args: cliArgs("-.5"), wantErr: errDurationMustBeAtLeastZero},
		{name: "bare exponent duration format is invalid", args: cliArgs("1e3"), wantErr: errInvalidDuration},
		{name: "bare dot duration format is invalid", args: cliArgs("."), wantErr: errInvalidDuration},
		{name: "invalid wall clock time format returns invalid time error", args: cliArgs("25:99"), wantErr: errInvalidTime},
		{name: "invalid wall clock seconds field returns invalid time error", args: cliArgs("12:00:99"), wantErr: errInvalidTime},
	})
}

func TestIsBareDecimalSecondsToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "integer", token: "5", want: true},
		{name: "decimal", token: "0.5", want: true},
		{name: "leading dot", token: ".5", want: true},
		{name: "trailing dot", token: "5.", want: true},
		{name: "positive sign", token: "+5", want: true},
		{name: "negative sign", token: "-1", want: true},
		{name: "just dot", token: ".", want: false},
		{name: "just plus", token: "+", want: false},
		{name: "just minus", token: "-", want: false},
		{name: "multiple dots", token: "1.2.3", want: false},
		{name: "exponent notation", token: "1e3", want: false},
		{name: "with unit suffix", token: "1s", want: false},
		{name: "alphabetic", token: "abc", want: false},
		{name: "empty", token: "", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := isBareDecimalSecondsToken(tc.token)
			if got != tc.want {
				t.Fatalf("isBareDecimalSecondsToken(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}

func TestParseWallClockTime(t *testing.T) {
	t.Parallel()

	// A fixed reference point: Tuesday 2024-03-05 at 14:30:00 local time.
	// Using a fixed now makes all expected durations deterministic.
	loc := time.Local
	now := time.Date(2024, 3, 5, 14, 30, 0, 0, loc)

	tests := []struct {
		name    string
		token   string
		wantOk  bool
		wantErr error
		wantDur time.Duration // only checked when wantOk && wantErr == nil
	}{
		// --- format recognition ---
		{
			name:   "token without colon is not a wall clock token",
			token:  "30s",
			wantOk: false,
		},
		{
			name:   "bare integer token is not a wall clock token",
			token:  "30",
			wantOk: false,
		},

		// --- HH:MM valid, future same day ---
		{
			name:    "future time same day HH:MM",
			token:   "15:00",
			wantOk:  true,
			wantDur: 30 * time.Minute,
		},
		{
			name:    "single digit hour H:MM",
			token:   "9:00",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute, // wraps to next day 09:00
		},
		{
			name:    "zero-padded single digit hour 09:MM",
			token:   "09:00",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute, // same as 9:00
		},
		{
			name:    "midnight 00:00 wraps to next day",
			token:   "00:00",
			wantOk:  true,
			wantDur: 9*time.Hour + 30*time.Minute,
		},
		{
			name:    "end of day 23:59 same day",
			token:   "23:59",
			wantOk:  true,
			wantDur: 9*time.Hour + 29*time.Minute,
		},

		// --- HH:MM:SS valid ---
		{
			name:    "future time same day HH:MM:SS",
			token:   "15:00:30",
			wantOk:  true,
			wantDur: 30*time.Minute + 30*time.Second,
		},
		{
			name:    "single digit hour with seconds H:MM:SS",
			token:   "9:00:00",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute,
		},
		{
			name:    "HH:MM:SS zero seconds same as HH:MM",
			token:   "15:00:00",
			wantOk:  true,
			wantDur: 30 * time.Minute,
		},

		// --- 24:00 normalization ---
		{
			name:    "24:00 normalizes to 00:00 and wraps to tomorrow",
			token:   "24:00",
			wantOk:  true,
			wantDur: 9*time.Hour + 30*time.Minute,
		},
		{
			name:    "24:00:00 normalizes to 00:00:00 and wraps to tomorrow",
			token:   "24:00:00",
			wantOk:  true,
			wantDur: 9*time.Hour + 30*time.Minute,
		},
		{
			name:    "24:01 is rejected as invalid",
			token:   "24:01",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "24:00:01 is rejected as invalid",
			token:   "24:00:01",
			wantOk:  true,
			wantErr: errInvalidTime,
		},

		// --- wrap-to-tomorrow cases ---
		{
			name:    "past time same day wraps to next day",
			token:   "13:00",
			wantOk:  true,
			wantDur: 22*time.Hour + 30*time.Minute,
		},
		{
			name:    "exact match on now wraps to next day",
			token:   "14:30",
			wantOk:  true,
			wantDur: 24 * time.Hour,
		},
		{
			name:    "one second in the past wraps to next day",
			token:   "14:29:59",
			wantOk:  true,
			wantDur: 23*time.Hour + 59*time.Minute + 59*time.Second,
		},

		// --- invalid field values ---
		{
			name:    "hour out of range returns invalid time error",
			token:   "25:00",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "minute out of range returns invalid time error",
			token:   "12:60",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "second out of range returns invalid time error",
			token:   "12:00:60",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "negative hour returns invalid time error",
			token:   "-1:00",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "non-numeric hour returns invalid time error",
			token:   "ab:00",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "non-numeric minute returns invalid time error",
			token:   "12:xx",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "non-numeric second returns invalid time error",
			token:   "12:00:xx",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "empty hour field returns invalid time error",
			token:   ":00",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "empty minute field returns invalid time error",
			token:   "12:",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "empty second field returns invalid time error",
			token:   "12:00:",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "too many colon-separated fields returns invalid time error",
			token:   "12:00:00:00",
			wantOk:  true,
			wantErr: errInvalidTime,
		},

		// --- AM/PM: bare hour shorthand ---
		{
			name:    "bare hour with am suffix",
			token:   "9am",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute, // 09:00 next day (now is 14:30)
		},
		{
			name:    "bare hour with pm suffix future same day",
			token:   "3pm",
			wantOk:  true,
			wantDur: 30 * time.Minute, // 15:00 same day
		},
		{
			name:    "bare hour with pm suffix past wraps to next day",
			token:   "1pm",
			wantOk:  true,
			wantDur: 22*time.Hour + 30*time.Minute, // 13:00 next day
		},

		// --- AM/PM: case variants ---
		{
			name:    "uppercase AM suffix",
			token:   "9AM",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute,
		},
		{
			name:    "uppercase PM suffix",
			token:   "3PM",
			wantOk:  true,
			wantDur: 30 * time.Minute,
		},
		{
			name:    "mixed case Am suffix",
			token:   "9Am",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute,
		},

		// --- AM/PM: shorthand a/p ---
		{
			name:    "bare hour with a shorthand wraps to next day",
			token:   "7a",
			wantOk:  true,
			wantDur: 16*time.Hour + 30*time.Minute, // 07:00 next day (now is 14:30)
		},
		{
			name:    "bare hour with p shorthand future same day",
			token:   "7p",
			wantOk:  true,
			wantDur: 4*time.Hour + 30*time.Minute, // 19:00 same day
		},
		{
			name:    "HH:MM with p shorthand future",
			token:   "3:30p",
			wantOk:  true,
			wantDur: time.Hour, // 15:30 same day
		},
		{
			name:    "HH:MM with a shorthand wraps to next day",
			token:   "9:00a",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute, // 09:00 next day
		},
		{
			name:    "space-separated a shorthand wraps to next day",
			token:   "7 a",
			wantOk:  true,
			wantDur: 16*time.Hour + 30*time.Minute,
		},
		{
			name:    "space-separated p shorthand future same day",
			token:   "7 p",
			wantOk:  true,
			wantDur: 4*time.Hour + 30*time.Minute,
		},
		{
			name:    "uppercase A shorthand",
			token:   "7A",
			wantOk:  true,
			wantDur: 16*time.Hour + 30*time.Minute,
		},
		{
			name:    "uppercase P shorthand",
			token:   "7P",
			wantOk:  true,
			wantDur: 4*time.Hour + 30*time.Minute,
		},

		// --- AM/PM: with minutes ---
		{
			name:    "HH:MM with pm suffix future",
			token:   "3:30pm",
			wantOk:  true,
			wantDur: time.Hour, // 15:30 same day
		},
		{
			name:    "HH:MM with am suffix wraps to next day",
			token:   "9:00am",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute,
		},
		{
			name:    "HH:MM with space-separated pm",
			token:   "3:30 pm",
			wantOk:  true,
			wantDur: time.Hour,
		},
		{
			name:    "HH:MM with space-separated AM uppercase",
			token:   "9:00 AM",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute,
		},

		// --- AM/PM: with seconds ---
		{
			name:    "HH:MM:SS with pm suffix",
			token:   "3:30:30pm",
			wantOk:  true,
			wantDur: time.Hour + 30*time.Second,
		},
		{
			name:    "HH:MM:SS with space-separated am",
			token:   "9:00:00 am",
			wantOk:  true,
			wantDur: 18*time.Hour + 30*time.Minute,
		},

		// --- AM/PM: noon and midnight ---
		{
			name:    "12pm is noon",
			token:   "12pm",
			wantOk:  true,
			wantDur: 21*time.Hour + 30*time.Minute, // 12:00 next day (now is 14:30)
		},
		{
			name:    "12:00 PM is noon",
			token:   "12:00 PM",
			wantOk:  true,
			wantDur: 21*time.Hour + 30*time.Minute,
		},
		{
			name:    "12am is midnight",
			token:   "12am",
			wantOk:  true,
			wantDur: 9*time.Hour + 30*time.Minute, // 00:00 next day
		},
		{
			name:    "12:00 AM is midnight",
			token:   "12:00 AM",
			wantOk:  true,
			wantDur: 9*time.Hour + 30*time.Minute,
		},

		// --- AM/PM: invalid values ---
		{
			name:    "0am is rejected",
			token:   "0am",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "13pm is rejected",
			token:   "13pm",
			wantOk:  true,
			wantErr: errInvalidTime,
		},
		{
			name:    "invalid minute with pm suffix is rejected",
			token:   "3:60pm",
			wantOk:  true,
			wantErr: errInvalidTime,
		},

		// --- bare integer without suffix still falls through ---
		{
			name:   "bare integer without suffix is not a wall clock token",
			token:  "9",
			wantOk: false,
		},

		// --- named aliases: noon and midnight ---
		{
			name:    "noon resolves to 12:00 and wraps to next day when past",
			token:   "noon",
			wantOk:  true,
			wantDur: 21*time.Hour + 30*time.Minute,
		},
		{
			name:    "midnight resolves to 00:00 and wraps to next day",
			token:   "midnight",
			wantOk:  true,
			wantDur: 9*time.Hour + 30*time.Minute,
		},
		{name: "Noon is case-insensitive", token: "Noon", wantOk: true, wantDur: 21*time.Hour + 30*time.Minute},
		{name: "NOON is case-insensitive", token: "NOON", wantOk: true, wantDur: 21*time.Hour + 30*time.Minute},
		{name: "Midnight is case-insensitive", token: "Midnight", wantOk: true, wantDur: 9*time.Hour + 30*time.Minute},
		{name: "MIDNIGHT is case-insensitive", token: "MIDNIGHT", wantOk: true, wantDur: 9*time.Hour + 30*time.Minute},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotDur, gotOk, gotErr := parseWallClockTime(tc.token, now)

			if gotOk != tc.wantOk {
				t.Fatalf("parseWallClockTime(%q) ok = %v, want %v", tc.token, gotOk, tc.wantOk)
			}
			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("parseWallClockTime(%q) err = %v, want %v", tc.token, gotErr, tc.wantErr)
			}
			if tc.wantOk && tc.wantErr == nil {
				// Compute expected duration relative to the same now used in the call,
				// matching the target.Sub(now) contract of the function.
				wantTarget := now.Add(tc.wantDur)
				gotTarget := now.Add(gotDur)
				if !gotTarget.Equal(wantTarget) {
					t.Fatalf("parseWallClockTime(%q) resolves to %v, want %v (diff %v)",
						tc.token, gotTarget, wantTarget, gotTarget.Sub(wantTarget))
				}
			}
		})
	}
}

func TestParseWallClockTimeNoonFuture(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 3, 5, 10, 0, 0, 0, time.Local) // 10:00, before noon
	d, ok, err := parseWallClockTime("noon", now)
	if !ok || err != nil {
		t.Fatalf("parseWallClockTime(noon) ok=%v err=%v", ok, err)
	}
	if want := 2 * time.Hour; d != want {
		t.Fatalf("got %v, want %v", d, want)
	}
}

func TestParseTimeField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		min     int
		max     int
		wantVal int
		wantOk  bool
	}{
		{name: "valid value in range", input: "30", min: 0, max: 59, wantVal: 30, wantOk: true},
		{name: "minimum boundary", input: "0", min: 0, max: 23, wantVal: 0, wantOk: true},
		{name: "maximum boundary", input: "23", min: 0, max: 23, wantVal: 23, wantOk: true},
		{name: "zero-padded value is valid", input: "09", min: 0, max: 59, wantVal: 9, wantOk: true},
		{name: "value below minimum is rejected", input: "0", min: 1, max: 59, wantOk: false},
		{name: "value above maximum is rejected", input: "60", min: 0, max: 59, wantOk: false},
		{name: "non-numeric input is rejected", input: "ab", min: 0, max: 59, wantOk: false},
		{name: "empty string is rejected", input: "", min: 0, max: 59, wantOk: false},
		{name: "negative value string is rejected", input: "-1", min: 0, max: 59, wantOk: false},
		{name: "float value string is rejected", input: "1.5", min: 0, max: 59, wantOk: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotVal, gotOk := parseTimeField(tc.input, tc.min, tc.max)
			if gotOk != tc.wantOk {
				t.Fatalf("parseTimeField(%q, %d, %d) ok = %v, want %v", tc.input, tc.min, tc.max, gotOk, tc.wantOk)
			}
			if gotOk && gotVal != tc.wantVal {
				t.Fatalf("parseTimeField(%q, %d, %d) val = %d, want %d", tc.input, tc.min, tc.max, gotVal, tc.wantVal)
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

			got := alarmCandidatesForGOOS(tc.goos, "")
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

	got := alarmCandidatesForGOOS("unknown-os", "")
	if got != nil {
		t.Fatalf("alarmCandidatesForGOOS() = %v, want nil", got)
	}
}

func TestAlarmCandidatesWithSoundFile(t *testing.T) {
	t.Parallel()

	t.Run("darwin custom sound", func(t *testing.T) {
		got := alarmCandidatesForGOOS("darwin", "custom.mp3")
		if len(got) != 1 || got[0].name != "afplay" || got[0].args[0] != "custom.mp3" {
			t.Fatalf("alarmCandidatesForGOOS(darwin, custom.mp3) = %v", got)
		}
	})

	t.Run("linux custom sound", func(t *testing.T) {
		got := alarmCandidatesForGOOS("linux", "custom.mp3")
		if len(got) != 2 {
			t.Fatalf("alarmCandidatesForGOOS(linux, custom.mp3) length = %d, want 2", len(got))
		}
		if got[0].name != "canberra-gtk-play" || got[0].args[1] != "custom.mp3" {
			t.Fatalf("alarmCandidatesForGOOS(linux, custom.mp3) first = %v", got[0])
		}
		if got[1].name != "paplay" || got[1].args[0] != "custom.mp3" {
			t.Fatalf("alarmCandidatesForGOOS(linux, custom.mp3) second = %v", got[1])
		}
	})

	t.Run("freebsd custom sound", func(t *testing.T) {
		got := alarmCandidatesForGOOS("freebsd", "custom.mp3")
		if len(got) != 1 || got[0].name != "canberra-gtk-play" || got[0].args[1] != "custom.mp3" {
			t.Fatalf("alarmCandidatesForGOOS(freebsd, custom.mp3) = %v", got)
		}
	})

	t.Run("openbsd custom sound falls back to beep", func(t *testing.T) {
		got := alarmCandidatesForGOOS("openbsd", "custom.mp3")
		if len(got) != 1 || got[0].name != "beep" {
			t.Fatalf("alarmCandidatesForGOOS(openbsd, custom.mp3) = %v", got)
		}
	})

	t.Run("netbsd custom sound falls back to beep", func(t *testing.T) {
		got := alarmCandidatesForGOOS("netbsd", "custom.mp3")
		if len(got) != 1 || got[0].name != "beep" {
			t.Fatalf("alarmCandidatesForGOOS(netbsd, custom.mp3) = %v", got)
		}
	})
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
	err := runTimer(ctx, cancel, time.Hour, status, false, false, false, false, false, "")
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
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	alarmCalls := 0

	status := newStatusDisplay(io.Discard, false, false)

	err := runTimerWithAlarmStarter(ctx, cancel, 0, status, false, true, false, true, false, "", func(string) {
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

			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)

			alarmCalls := 0
			status := newStatusDisplay(io.Discard, tc.statusInteractive, false)

			err := runTimerWithAlarmStarter(ctx, cancel, 0, status, tc.sideEffectsInteractive, false, false, false, false, "", func(string) {
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

func TestSleepInhibitorArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		stdoutInteractive bool
		statusInteractive bool
		pid               string
		want              []string
	}{
		{
			name:              "both streams interactive includes display flag",
			stdoutInteractive: true,
			statusInteractive: true,
			pid:               "123",
			want:              []string{"-i", "-d", "-w", "123"},
		},
		{
			name:              "forced non interactive omits display flag",
			stdoutInteractive: false,
			statusInteractive: false,
			pid:               "123",
			want:              []string{"-i", "-w", "123"},
		},
		{
			name:              "mixed interactivity omits display flag",
			stdoutInteractive: true,
			statusInteractive: false,
			pid:               "456",
			want:              []string{"-i", "-w", "456"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := sleepInhibitorArgs(tc.stdoutInteractive, tc.statusInteractive, tc.pid)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sleepInhibitorArgs(%v, %v, %q) = %v, want %v", tc.stdoutInteractive, tc.statusInteractive, tc.pid, got, tc.want)
			}
		})
	}
}

func TestRunTimerWithAlarmStarter_NonTTYLifecycleOutput(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	out, status := newCapturedStatus(false, false)

	err := runTimerWithAlarmStarter(ctx, cancel, 0, status, false, false, false, false, false, "", func(string) {})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}

	want := "after: started (0s)\nafter: complete\n"
	if got := out.String(); got != want {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want %q", got, want)
	}
}

func TestRunTimerWithAlarmStarter_NonTTYQuietSuppressesLifecycle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	out, status := newCapturedStatus(false, false)

	err := runTimerWithAlarmStarter(ctx, cancel, 0, status, false, true, false, false, false, "", func(string) {})
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

	err := runTimerWithAlarmStarter(ctx, cancel, 10*time.Second, status, false, false, false, false, false, "", func(string) {})
	if err == nil {
		t.Fatal("runTimerWithAlarmStarter() error = nil, want cancellation cause")
	}

	want := "after: cancelled\n"
	if got := out.String(); got != want {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want %q", got, want)
	}
}

func TestRunTimerWithAlarmStarter_InteractiveWritesToStatusWriter(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	out, status := newCapturedStatus(true, false)

	err := runTimerWithAlarmStarter(ctx, cancel, 0, status, false, false, false, false, false, "", func(string) {})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "after complete\n") {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want after completion text", out.String())
	}
}

func TestRunTimerWithAlarmStarter_InteractiveQuietClearsStatusLine(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	out, status := newCapturedStatus(true, true)

	err := runTimerWithAlarmStarter(ctx, cancel, 0, status, false, true, false, false, false, "", func(string) {})
	if err != nil {
		t.Fatalf("runTimerWithAlarmStarter() error = %v, want nil", err)
	}
	if got := out.String(); got != "\033]0;00:00:00\007\r\033[K00:00:00\r\033[K" {
		t.Fatalf("runTimerWithAlarmStarter() output = %q, want %q", got, "\033]0;00:00:00\007\r\\033[K00:00:00\r\\033[K")
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

func TestStripAMPM(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		token     string
		wantStrip string
		wantIsPM  bool
		wantFound bool
	}{
		// --- no suffix ---
		{name: "no suffix returns token unchanged", token: "9:00", wantStrip: "9:00", wantIsPM: false, wantFound: false},
		{name: "bare integer no suffix falls through", token: "9", wantStrip: "9", wantIsPM: false, wantFound: false},

		// --- attached AM ---
		{name: "lowercase am attached", token: "9am", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "uppercase AM attached", token: "9AM", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "mixed case Am attached", token: "9Am", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "HH:MM with attached am", token: "9:00am", wantStrip: "9:00", wantIsPM: false, wantFound: true},
		{name: "HH:MM:SS with attached am", token: "9:00:00am", wantStrip: "9:00:00", wantIsPM: false, wantFound: true},

		// --- attached PM ---
		{name: "lowercase pm attached", token: "1pm", wantStrip: "1", wantIsPM: true, wantFound: true},
		{name: "uppercase PM attached", token: "1PM", wantStrip: "1", wantIsPM: true, wantFound: true},
		{name: "HH:MM with attached pm", token: "1:30pm", wantStrip: "1:30", wantIsPM: true, wantFound: true},
		{name: "HH:MM:SS with attached pm", token: "1:30:00pm", wantStrip: "1:30:00", wantIsPM: true, wantFound: true},

		// --- space-separated AM ---
		{name: "space separated am", token: "9 am", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "space separated AM uppercase", token: "9 AM", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "HH:MM with space separated am", token: "9:00 am", wantStrip: "9:00", wantIsPM: false, wantFound: true},

		// --- space-separated PM ---
		{name: "space separated pm", token: "1 pm", wantStrip: "1", wantIsPM: true, wantFound: true},
		{name: "space separated PM uppercase", token: "1 PM", wantStrip: "1", wantIsPM: true, wantFound: true},
		{name: "HH:MM with space separated pm", token: "1:30 pm", wantStrip: "1:30", wantIsPM: true, wantFound: true},
		{name: "HH:MM:SS with space separated pm", token: "1:30:00 pm", wantStrip: "1:30:00", wantIsPM: true, wantFound: true},

		// --- shorthand: attached a/p ---
		{name: "lowercase a shorthand attached", token: "9a", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "uppercase A shorthand attached", token: "9A", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "lowercase p shorthand attached", token: "1p", wantStrip: "1", wantIsPM: true, wantFound: true},
		{name: "uppercase P shorthand attached", token: "1P", wantStrip: "1", wantIsPM: true, wantFound: true},
		{name: "HH:MM with attached a", token: "9:00a", wantStrip: "9:00", wantIsPM: false, wantFound: true},
		{name: "HH:MM with attached p", token: "3:30p", wantStrip: "3:30", wantIsPM: true, wantFound: true},

		// --- shorthand: space-separated a/p ---
		{name: "space separated a shorthand", token: "9 a", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "space separated A uppercase", token: "9 A", wantStrip: "9", wantIsPM: false, wantFound: true},
		{name: "space separated p shorthand", token: "1 p", wantStrip: "1", wantIsPM: true, wantFound: true},
		{name: "space separated P uppercase", token: "1 P", wantStrip: "1", wantIsPM: true, wantFound: true},

		// --- am/p suffix does not match only 'a' inside "am" ---
		{name: "9am still strips am not just a", token: "9am", wantStrip: "9", wantIsPM: false, wantFound: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotStrip, gotIsPM, gotFound := stripAMPM(tc.token)
			if gotFound != tc.wantFound {
				t.Fatalf("stripAMPM(%q) found = %v, want %v", tc.token, gotFound, tc.wantFound)
			}
			if gotStrip != tc.wantStrip {
				t.Fatalf("stripAMPM(%q) stripped = %q, want %q", tc.token, gotStrip, tc.wantStrip)
			}
			if tc.wantFound && gotIsPM != tc.wantIsPM {
				t.Fatalf("stripAMPM(%q) isPM = %v, want %v", tc.token, gotIsPM, tc.wantIsPM)
			}
		})
	}
}

func TestRenderInteractiveCountdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		supportsAdvanced bool
		noTitle          bool
		want             string
	}{
		{
			name:             "advanced terminal without noTitle emits OSC title sequence",
			supportsAdvanced: true,
			noTitle:          false,
			want:             "\033]0;00:01:00\007\r\033[K00:01:00",
		},
		{
			name:             "advanced terminal with noTitle suppresses OSC title sequence",
			supportsAdvanced: true,
			noTitle:          true,
			want:             "\r\033[K00:01:00",
		},
		{
			name:             "dumb terminal never emits OSC title sequence regardless of noTitle",
			supportsAdvanced: false,
			noTitle:          false,
			want:             "\r00:01:00",
		},
		{
			name:             "dumb terminal with noTitle still uses simple carriage return",
			supportsAdvanced: false,
			noTitle:          true,
			want:             "\r00:01:00",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			status := newStatusDisplay(&buf, true, tc.supportsAdvanced)
			renderInteractiveCountdown(status, "00:01:00", tc.noTitle)
			if got := buf.String(); got != tc.want {
				t.Fatalf("renderInteractiveCountdown() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyAMPM(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hour     int
		isPM     bool
		wantHour int
		wantOk   bool
	}{
		// --- AM conversions ---
		{name: "1am", hour: 1, isPM: false, wantHour: 1, wantOk: true},
		{name: "11am", hour: 11, isPM: false, wantHour: 11, wantOk: true},
		{name: "12am is midnight", hour: 12, isPM: false, wantHour: 0, wantOk: true},

		// --- PM conversions ---
		{name: "12pm is noon", hour: 12, isPM: true, wantHour: 12, wantOk: true},
		{name: "1pm", hour: 1, isPM: true, wantHour: 13, wantOk: true},
		{name: "11pm", hour: 11, isPM: true, wantHour: 23, wantOk: true},

		// --- out of range ---
		{name: "0am is rejected", hour: 0, isPM: false, wantOk: false},
		{name: "13pm is rejected", hour: 13, isPM: true, wantOk: false},
		{name: "negative hour is rejected", hour: -1, isPM: false, wantOk: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotHour, gotOk := applyAMPM(tc.hour, tc.isPM)
			if gotOk != tc.wantOk {
				t.Fatalf("applyAMPM(%d, %v) ok = %v, want %v", tc.hour, tc.isPM, gotOk, tc.wantOk)
			}
			if gotOk && gotHour != tc.wantHour {
				t.Fatalf("applyAMPM(%d, %v) hour = %d, want %d", tc.hour, tc.isPM, gotHour, tc.wantHour)
			}
		})
	}
}

func TestIsAMPMToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		token string
		want  bool
	}{
		{token: "am", want: true},
		{token: "pm", want: true},
		{token: "AM", want: true},
		{token: "PM", want: true},
		{token: "Am", want: true},
		{token: "Pm", want: true},
		{token: "a", want: true},
		{token: "p", want: true},
		{token: "A", want: true},
		{token: "P", want: true},
		{token: "9am", want: false},
		{token: "9a", want: false},
		{token: "9:00", want: false},
		{token: "-q", want: false},
		{token: "", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.token, func(t *testing.T) {
			t.Parallel()

			got := isAMPMToken(tc.token)
			if got != tc.want {
				t.Fatalf("isAMPMToken(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}
