# after

A fast command-line countdown tool with live terminal feedback, graceful cancellation,
and optional completion alarms.

<img src="assets/demo.gif" width="600" alt="after 3s demo" />
<img src="assets/demo-wall-clock.gif" width="600" alt="after 1pm wall clock demo" />

## Features

- Live countdown display in terminal status output (`stderr`) and title bar (when supported)
- Graceful cancellation via Ctrl+C
- Audio alert on completion (best-effort, platform-specific backend)
- Wall clock target mode: count down to a time of day in 24-hour or 12-hour AM/PM format
- Optional `-q`/`--quiet` mode for inline countdown only
- Optional `-s`/`--sound` to force alarm playback on completion
- Optional `-c`/`--caffeinate` to force sleep-inhibition attempt in non-TTY mode (macOS only)
- Ceiling-based display (never shows 00:00:00 while time remains)
- Non-TTY lifecycle logging by default (`started`/`complete`/`cancelled`)
- Clean, minimal interface

## Quick Start

Install and run in under a minute (requires [Homebrew](https://brew.sh)):

```bash
brew tap Mtn-Man/tools
brew install Mtn-Man/tools/after
after --help
after 10m
```

If `after` is not found after install, see [Troubleshooting](#troubleshooting).

## Platform Support

- Prebuilt release binaries are published for tested platforms: macOS and Linux
  (`amd64` and `arm64`).
- BSD systems are expected to work when building from source, but prebuilt BSD
  binaries are not currently published.
- Windows is currently unsupported.

## Installation

### Install With Homebrew (Recommended)

```bash
brew tap Mtn-Man/tools
brew install Mtn-Man/tools/after
```

Verify:
```bash
after --version
```

### Install Prebuilt Release Binary (Tested Platforms: macOS/Linux)

1. Download your platform archive and `checksums.txt` from the
   [latest release](https://github.com/Mtn-Man/after/releases/latest).
   Replace `<version>` in the examples below with the release tag
   (for example, `vX.Y.Z`).
   Archive naming pattern:
   - `after_<version>_darwin_amd64.tar.gz`
   - `after_<version>_darwin_arm64.tar.gz`
   - `after_<version>_linux_amd64.tar.gz`
   - `after_<version>_linux_arm64.tar.gz`
   Note: release filenames use `darwin` to refer to macOS.
   The examples below use macOS Apple Silicon filenames; swap in the archive
   and binary names for your OS/architecture.
2. Open a terminal and change to the folder where you downloaded the release files
   (for example, `~/Downloads`):
   ```bash
   cd ~/Downloads
   ```
3. Verify checksum (optional but recommended):
   Example for macOS Apple Silicon:
   ```bash
   grep "after_<version>_darwin_arm64.tar.gz$" checksums.txt | shasum -a 256 -c -
   ```
   Example for Linux:
   ```bash
   grep "after_<version>_linux_amd64.tar.gz$" checksums.txt | sha256sum -c -
   ```
4. Extract your archive (example shown for macOS Apple Silicon):
   ```bash
   tar -xzf after_<version>_darwin_arm64.tar.gz
   ```
5. Install the extracted binary into `/usr/local/bin` (default):
   ```bash
   sudo install -m 0755 after_darwin_arm64 /usr/local/bin/after
   ```
   If you are on a different platform, replace the archive and binary filenames
   above with the matching release files for your OS/architecture.
6. Alternative (no `sudo`): install to `~/.local/bin`:
   ```bash
   mkdir -p ~/.local/bin
   install -m 0755 after_darwin_arm64 ~/.local/bin/after
   ```
   If `~/.local/bin` is not in your `PATH`, add it to your shell startup file
   (for example, `~/.zshrc` or `~/.bashrc`), then reload your shell:
   ```bash
   # zsh
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
   source ~/.zshrc

   # bash
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
   source ~/.bashrc
   ```
7. Verify:
   ```bash
   after --version
   ```

### Install With Go

Prerequisite: Go 1.20+ installed. Get Go: https://go.dev/dl/

Install the latest version:
```bash
go install github.com/Mtn-Man/after@latest
```

Or install a specific release:
```bash
go install github.com/Mtn-Man/after@<version>
```

Or clone and build locally:
```bash
git clone https://github.com/Mtn-Man/after.git
cd after
go build -o after .
./after --version
```

Build with an injected version (recommended for releases):
```bash
go build -ldflags "-X main.version=vX.Y.Z" -o after .
./after --version
```

For local source builds without `-ldflags`, `--version` reports `after dev`.
When installed with `go install github.com/Mtn-Man/after@<version>`, `--version`
typically reports that module version.

## Usage
```bash
after [options] <duration>
after [options] <time>
after --help
after --version
after --quiet <duration>
after --sound <duration>
after --sound-file <path> <duration>
after -f <path> <duration>
after -s <duration>
after --caffeinate <duration>
after -c <duration>
after -qs <duration>
after -- <duration>
```

For ergonomics, options may be placed before or after the duration operand
(for example, `after -q 5m` and `after 5m -q` are both supported).
You can also combine short flags such as `-qs`.

### Examples
```bash
after 30s       # 30 seconds
after 30        # 30 seconds (bare numbers are seconds)
after 5m        # 5 minutes
after 1.5h      # 1.5 hours
after 0.5       # 500 milliseconds
after 90m       # 90 minutes
after 14:30     # count down to 2:30 PM today (or tomorrow if already past)
after 9:00      # count down to 9:00 AM today (or tomorrow if already past)
after 9am       # count down to 9:00 AM (12-hour shorthand)
after 2:30pm    # count down to 2:30 PM
after 9:30:30am # count down to 9:30:30 AM
after "2:30 PM" # space-separated AM/PM suffix (quotes optional)
after 12pm      # count down to noon
after 12am      # count down to midnight
after --help    # Show help
after -v        # Show version (e.g. after dev or after vX.Y.Z)
after -q 5m     # Quiet mode: inline countdown only
after -s 5m     # Force alarm playback even in quiet/non-TTY mode
after -qs 5m    # Inline countdown + alarm, no title bar updates
after --sound 5m                              # Force alarm even in quiet/non-TTY mode
after --sound-file ~/Sounds/bell.mp3 5m       # Play custom sound on completion
after -f ~/Sounds/bell.mp3 5m                 # Play custom sound (short flag)
after -f /System/Library/Sounds/Funk.aiff 5  # macOS: play a built-in alert sound
after -f "~/Music/Alarm Sounds/bell.mp3" 5m  # Quoted path with spaces
after -c 10m 2> /tmp/after.log               # Force macOS sleep inhibition in non-TTY
after -- 10s    # End option parsing; treat following token as positional duration
after -- --help # Treat --help as positional token (invalid duration)
after 10m 2> /tmp/after.status              # Capture lifecycle output
```

after accepts any duration format supported by Go's `time.ParseDuration`,
including combinations like `1h30m` or `2h15m30s`. Bare integers and decimals
(for example `30`, `0.5`, `.5`) are also accepted and treated as seconds.

In wall clock mode, pass a time of day instead of a duration. after counts
down to the next occurrence of that time, wrapping to the following day if it has
already passed. 24-hour (`14:30`) and 12-hour AM/PM formats (`2:30pm`, `"2:30 PM"`)
are both supported, as are bare hour shorthands (`9am`). `12am` is midnight and
`12pm` is noon.

### Flags

- `-h`, `--help`: Show help and exit
- `-v`, `--version`: Show version and exit (reports injected build version, module
  version when available, or `after dev` for local non-injected builds)
- `-q`, `--quiet`: TTY: inline countdown only (no title bar updates, completion line,
  alarm, or cancel text). Non-TTY: suppress lifecycle status output. Combine with
  `-s` (`-qs`) to keep the alarm while still suppressing the title bar.
- `-s`, `--sound`: Force alarm playback on completion even in `--quiet` or non-TTY mode
- `-f`, `--sound-file <path>`: Path to a custom audio file to play on completion
  (implies `--sound`; supported on macOS, Linux, and FreeBSD). If the file cannot
  be resolved or used, after falls back to the default alarm backend.
  OpenBSD/NetBSD always use the default alarm backend.
- `-c`, `--caffeinate`: Force sleep-inhibition attempt even in non-TTY mode (macOS only)
- `--`: End option parsing; all following tokens are treated as positional arguments

## Requirements

- Go 1.20+ required only for building from source
- A Unix-like OS (macOS, Linux, or BSD) for source builds
- Prebuilt binaries are currently published for macOS/Linux only

## Troubleshooting

- `after` not found after install (`after: command not found`): Ensure your install
  location is in `PATH` (`/opt/homebrew/bin` or `/usr/local/bin` for Homebrew,
  `$(go env GOPATH)/bin` or `GOBIN` for `go install`, `/usr/local/bin` or
  `~/.local/bin` for manual install), then restart or reload your shell.
- `Permission denied` while installing to `/usr/local/bin`: Use `sudo install ...`
  or install to `~/.local/bin` instead.
- Homebrew command ambiguity with an existing `after` formula: use
  `brew install Mtn-Man/tools/after` and `brew info Mtn-Man/tools/after`.
- `after --version` shows `after dev`: This is expected for local source builds
  without `-ldflags "-X main.version=vX.Y.Z"` (for example, `go build .`).

## How It Works

Status output is written to `stderr`, leaving `stdout` clean for pipeline use.

The countdown updates every 500ms in `HH:MM:SS` format. In a normal terminal session,
the title bar also updates alongside the inline countdown. On completion, `after complete`
is printed and an audio alert plays. Press Ctrl+C at any time to cancel gracefully.

With `-q` / `--quiet`, title bar updates, completion text, and the alarm are all
suppressed. Combine with `-s` to keep the alarm while still running quietly.

When output is redirected (for example `2> /tmp/after.log`), the countdown is
suppressed and only lifecycle lines are emitted: `after: started (...)`,
`after: complete`, and `after: cancelled`. The alarm does not play automatically
in this mode unless `--sound` is specified.

On macOS, after prevents the system from sleeping for its duration. This
requires a normal interactive session by default; use `--caffeinate` to force it
when output is redirected.

## License

MIT License. See [LICENSE](LICENSE) file for details.
