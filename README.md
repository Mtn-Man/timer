# Timer

A fast command-line countdown timer with live terminal feedback, graceful cancellation, and optional completion alarms.

## Features

- Live countdown display in terminal status output (`stderr`) and title bar (when supported)
- Graceful cancellation via Ctrl+C
- Audio alert on completion (best-effort, platform-specific backend)
- Optional `-q`/`--quiet` mode for inline countdown only
- Optional `--sound` to force alarm playback on completion
- Optional `--caffeinate` to force sleep-inhibition attempt in non-TTY mode (macOS only)
- Ceiling-based display (never shows 00:00:00 while time remains)
- Non-TTY lifecycle logging by default (`started`/`complete`/`cancelled`)
- Clean, minimal interface

## Quick Start

Prerequisite: Go 1.20+ installed. Get Go: https://go.dev/dl/

Install and run in under a minute:

```bash
go install github.com/Mtn-Man/timer@latest
timer 10m
timer --help
```

A Homebrew tap is planned; for now, `go install` is the quickest setup path.
If `timer` is not found after install, see [Troubleshooting](#troubleshooting).

## Platform Support

- Prebuilt release binaries are published for tested platforms: macOS and Linux (`amd64` and `arm64`).
- BSD systems are expected to work when building from source, but prebuilt BSD binaries are not currently published.
- Windows is not supported currently.

## Installation

### Install Prebuilt Release Binary (Tested Platforms: macOS/Linux)

1. Download your platform archive and `checksums.txt` from the
   [latest release](https://github.com/Mtn-Man/timer/releases/latest).
   Replace `<version>` in the examples below with the release tag
   (for example, `vX.Y.Z`).
   Archive naming pattern:
   - `timer_<version>_darwin_amd64.tar.gz`
   - `timer_<version>_darwin_arm64.tar.gz`
   - `timer_<version>_linux_amd64.tar.gz`
   - `timer_<version>_linux_arm64.tar.gz`
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
   grep "timer_<version>_darwin_arm64.tar.gz$" checksums.txt | shasum -a 256 -c -
   ```
   Example for Linux:
   ```bash
   grep "timer_<version>_linux_amd64.tar.gz$" checksums.txt | sha256sum -c -
   ```
4. Extract your archive (example shown for macOS Apple Silicon):
   ```bash
   tar -xzf timer_<version>_darwin_arm64.tar.gz
   ```
5. Install the extracted binary into `/usr/local/bin` (default):
   ```bash
   sudo install -m 0755 timer_darwin_arm64 /usr/local/bin/timer
   ```
   If you are on a different platform, replace the archive and binary filenames
   above with the matching release files for your OS/architecture.
6. Alternative (no `sudo`): install to `~/.local/bin`:
   ```bash
   mkdir -p ~/.local/bin
   install -m 0755 timer_darwin_arm64 ~/.local/bin/timer
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
   timer --version
   ```

### Install With Go

Install the latest version:
```bash
go install github.com/Mtn-Man/timer@latest
```

Or install a specific release:
```bash
go install github.com/Mtn-Man/timer@<version>
```

Or clone and build locally:
```bash
git clone https://github.com/Mtn-Man/timer.git
cd timer
go build -o timer .
./timer --version
```

Build with an injected version (recommended for releases):
```bash
go build -ldflags "-X main.version=vX.Y.Z" -o timer .
./timer --version
```

For local source builds without `-ldflags`, `--version` reports `timer dev`.
When installed with `go install github.com/Mtn-Man/timer@<version>`, `--version` typically reports that module version.

## Usage
```bash
timer [options] <duration>
timer --help
timer --version
timer --quiet <duration>
timer --sound <duration>
timer -s <duration>
timer --caffeinate <duration>
timer -c <duration>
timer -qs <duration>
timer -- <duration>
```

For ergonomics, options may be placed before or after the duration operand (for example, `timer -q 5m` and `timer 5m -q` are both supported). You can also combine short flags such as `-qs`.

### Examples
```bash
timer 30s      # 30 seconds
timer 30       # 30 seconds (bare numbers are seconds)
timer 5m       # 5 minutes
timer 1.5h     # 1.5 hours
timer 0.5      # 500 milliseconds
timer 90m      # 90 minutes
timer --help   # Show help
timer -v       # Show version (e.g. timer dev or timer vX.Y.Z)
timer -q 5m    # Quiet mode: inline countdown only
timer -s 5m    # Force alarm playback even in quiet/non-TTY mode
timer -qs 5m   # Combined short flags: quiet + force alarm
timer --sound 5m # Force alarm playback even in quiet/non-TTY mode
timer -c 10m > /tmp/timer.log # Force macOS sleep inhibition attempt in non-TTY mode
timer --caffeinate 10m > /tmp/timer.log # Force macOS sleep inhibition attempt in non-TTY mode
timer -- 10s   # End option parsing; treat following token as positional duration
timer -- --help # Treat --help as positional token (invalid duration)
timer 10m > /tmp/timer.out 2> /tmp/timer.status # Keep data (stdout) and status (stderr) separate
```

The timer accepts any duration format supported by Go's `time.ParseDuration`, including combinations like `1h30m` or `2h15m30s`. Bare integers and decimals (for example `30`, `0.5`, `.5`) are also accepted and treated as seconds.

### Flags

- `-h`, `--help`: Show help and exit
- `-v`, `--version`: Show version and exit (reports injected build version, module version when available, or `timer dev` for local non-injected builds)
- `-q`, `--quiet`: TTY: inline countdown only (no title updates, completion line, alarm, or cancel text). Non-TTY: suppress lifecycle status output.
- `-s`, `--sound`: Force alarm playback on completion even in `--quiet` or non-TTY mode
- `-c`, `--caffeinate`: Force sleep-inhibition attempt even in non-TTY mode (macOS only)
- `--`: End option parsing; all following tokens are treated as positional arguments

## Requirements

- Go 1.20+ required only for building from source
- A Unix-like OS (macOS, Linux, or BSD) for source builds
- Prebuilt binaries are currently published for macOS/Linux only

## Troubleshooting

- `timer` not found after install (`timer: command not found`): Ensure your install location is in `PATH` (`$(go env GOPATH)/bin` or `GOBIN` for `go install`, `/usr/local/bin` or `~/.local/bin` for manual install), then restart or reload your shell.
- `Permission denied` while installing to `/usr/local/bin`: Use `sudo install ...` or install to `~/.local/bin` instead.
- `timer --version` shows `timer dev`: This is expected for local source builds without `-ldflags "-X main.version=vX.Y.Z"` (for example, `go build .`).

## How It Works

Run-mode status output is written to `stderr`, while `stdout` remains available for pipeline/data use.

In interactive status mode (`stderr` is a TTY), the timer updates every 500ms in `HH:MM:SS` format. In normal mode, it updates both the terminal line and title bar (when terminal capabilities allow it).

In normal interactive mode, completion prints `timer complete`, plays an alert using the best available backend for your platform, and exits.

With `-q` / `--quiet` in interactive mode, timer output is limited to the inline countdown:
no title updates, no completion line, no alarm, and no cancel text.

When `stderr` is not a TTY (for example, redirected), the timer emits lifecycle-only status lines:
`timer: started (...)`, `timer: complete`, and `timer: cancelled`.
With `--quiet`, those non-TTY lifecycle lines are suppressed.

Default alarm playback requires both `stdout` and `stderr` to be TTYs.
So if either stream is piped or redirected, alarm does not auto-run unless explicitly requested with `--sound`.

When `--sound` is provided, alarm playback is still attempted on completion in `--quiet` and non-TTY modes.

On macOS, default sleep inhibition is attempted only when both `stdout` and `stderr` are TTYs. With `--caffeinate`, the timer also attempts sleep inhibition in non-TTY/piped runs (best effort).
On non-macOS systems, `--caffeinate` prints a warning and continues normally without sleep inhibition.

Press Ctrl+C at any time to cancel the timer gracefully. In interactive normal mode, the current line is cleared and `timer cancelled` is printed, then the process exits with code 130. In non-TTY normal mode, `timer: cancelled` is emitted. In `--quiet` mode, cancellation text is suppressed.
If the process receives SIGTERM, it exits with code 143.
Note that in normal interactive mode, the terminal title bar may retain the last displayed time after cancellation depending on your terminal emulator.
In `TERM=dumb`/minimal environments, advanced terminal control sequences are disabled.

## License

MIT License. See [LICENSE](LICENSE) file for details.
