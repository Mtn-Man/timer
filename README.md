# after

A simple countdown timer for the terminal.

<img src="assets/demo.gif" width="600" alt="after demo" />

## Quick Start

Install and run in under a minute (requires [Homebrew](https://brew.sh)):

```bash
brew install mtn-man/tools/after
after --help
after 10m
```

If `after` is not found once installed, see [Troubleshooting](#troubleshooting).

## Features

- Live countdown display in the terminal and title bar
- Audio alert on completion (best-effort, platform-specific)
- Count down to a time of day in 24-hour or 12-hour AM/PM format
- Keeps macOS awake while the timer runs (except when piped or backgrounded)
- Plays well in scripts and pipelines

## Installation

### Install With Homebrew (Recommended)

```bash
brew install mtn-man/tools/after
```

Verify:
```bash
after --version
```

### Install Prebuilt Release Binary (Tested Platforms: macOS/Linux)

1. Download your platform archive and `checksums.txt` from the
   [latest release](https://github.com/mtn-man/after/releases/latest).
   Replace `<version>` in the examples below with the release tag
   (for example, `vX.Y.Z`).
   Archive naming pattern:
   - `after_<version>_darwin_amd64.tar.gz`
   - `after_<version>_darwin_arm64.tar.gz`
   - `after_<version>_linux_amd64.tar.gz`
   - `after_<version>_linux_arm64.tar.gz`

   Note: release filenames use `darwin` to refer to macOS.
   The examples below use macOS Apple Silicon filenames; swap in the
   archive and binary names for your OS/architecture.
2. Verify checksum (optional but recommended):
   Example for macOS Apple Silicon:
   ```bash
   grep "after_<version>_darwin_arm64.tar.gz$" checksums.txt | shasum -a 256 -c -
   ```
   Example for Linux:
   ```bash
   grep "after_<version>_linux_amd64.tar.gz$" checksums.txt | sha256sum -c -
   ```
3. Extract your archive (example shown for macOS Apple Silicon):
   ```bash
   tar -xzf after_<version>_darwin_arm64.tar.gz
   ```
4. Install the extracted binary into `/usr/local/bin` (default):
   ```bash
   sudo install -m 0755 after_darwin_arm64 /usr/local/bin/after
   ```
   If you are on a different platform, replace the archive and binary
   filenames above with the matching release files for your
   OS/architecture.
5. Alternative (no `sudo`): install to `~/.local/bin`:
   ```bash
   mkdir -p ~/.local/bin
   install -m 0755 after_darwin_arm64 ~/.local/bin/after
   ```
   If `~/.local/bin` is not in your `PATH`, add it to your shell
   startup file (for example, `~/.zshrc` or `~/.bashrc`), then reload
   your shell:
   ```bash
   # zsh
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
   source ~/.zshrc

   # bash
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
   source ~/.bashrc
   ```
6. Verify:
   ```bash
   after --version
   ```

### Install With Go

Prerequisite: Go 1.23+ installed. Get Go: https://go.dev/dl/

Install the latest version:
```bash
go install github.com/mtn-man/after@latest
```

Or install a specific release:
```bash
go install github.com/mtn-man/after@<version>
```

Or clone and build locally:
```bash
git clone https://github.com/mtn-man/after.git
cd after
go build -o after .
./after --version
```

## Usage

```bash
after [options] <duration|time>
```

Durations are relative. Times refer to the next occurrence —
wrapping to tomorrow if already passed.

### Examples

```bash
after 30s       # 30 seconds
after 5m        # 5 minutes
after 1h30m     # 1 hour 30 minutes
after 30        # bare numbers are seconds

after 14:30     # next 2:30 PM (24-hour time)
after 9am       # next 9:00 AM
after 9p        # next 9:00 PM

after -q 5m     # quiet (no alarm or status messages)
after -s 5m     # force alarm
```

### More examples

```bash
after 90m       # 90 minutes
after 1.5h      # 1.5 hours

after 9a        # shorthand for 9am
after 2:30pm    # 12-hour time
after 2:30 PM   # space-separated AM/PM
after noon      # 12:00 PM
after midnight  # 12:00 AM

after -qs 5m    # quiet, but keep alarm
after -qt 5m    # quiet + no title bar updates
after -qts 5m   # quiet + no title bar + force alarm

after --sound-file ~/Sounds/bell.mp3 5m
after -f /System/Library/Sounds/Funk.aiff 5

after 10m 2> /tmp/after.log    # capture lifecycle output
after -s 10m 2> /dev/null &    # background with alarm
```

Options may be placed before or after the time value. Short flags
can be combined: `-qt`, `-qs`, `-qts`.

### Flags

- `-h`, `--help`: Show help and exit
- `-v`, `--version`: Show version and exit
- `-q`, `--quiet`: TTY: suppress alarm, completion text, and cancel
  text (inline countdown still runs, title bar still updates). Non-TTY:
  suppress lifecycle status output. Combine with `-s` (`-qs`) to keep
  the alarm while suppressing other output.
- `-t`, `--no-title`: Suppress terminal title bar updates. Useful in
  multiplexers like tmux or screen where title changes affect window
  names. Combine with `-q` (`-qt`) to suppress both.
- `-s`, `--sound`: Force alarm playback on completion even in
  `--quiet` or non-TTY mode
- `-f`, `--sound-file <path>`: Path to a custom audio file to play on
  completion (implies `--sound`; supported on macOS, Linux, and
  FreeBSD). If the file cannot be resolved or used, after falls back to
  the default alarm backend. OpenBSD/NetBSD always use the default
  alarm backend.
- `-c`, `--caffeinate`: Force sleep-inhibition attempt even in non-TTY
  mode (macOS only)
- `--`: End option parsing; all following tokens are treated as
  positional arguments

## How It Works

Status output is written to `stderr`, leaving `stdout` clean for
pipeline use.

The countdown updates every 500ms, showing only significant fields
(`1:23` for 83 seconds, `1:02:03` for just over an hour). In a normal
terminal session, the title bar also updates alongside the inline
countdown. On completion, `after complete` is printed and an audio
alert plays. Press Ctrl+C at any time to cancel gracefully. In an
interactive terminal session, `q`, Esc, and Ctrl+D also cancel.

With `-q` / `--quiet`, the alarm, completion text, and cancel text are
suppressed; the inline countdown and title bar updates continue. Combine
with `-s` to keep the alarm while still suppressing other output. Use
`-t` / `--no-title` to suppress title bar updates independently —
handy in tmux or screen sessions. Combine `-q` and `-t` (`-qt`) to
suppress both.

When output is redirected (for example `2> /tmp/after.log`), the
countdown is suppressed and only lifecycle lines are emitted:
`after: started (...)`, `after: complete`, and `after: cancelled`. The
alarm does not play automatically in this mode unless `--sound` is
specified.

On macOS, after prevents the system from sleeping for its duration.
This requires a normal interactive session by default; use
`--caffeinate` to force it when output is redirected.

## Requirements

- Go 1.23+ required only for building from source
- A Unix-like OS (macOS, Linux, or BSD) for source builds
- Prebuilt binaries are published for macOS and Linux (`amd64` and
  `arm64`). BSD is expected to work from source; 
  Windows is currently unsupported.

## Troubleshooting

- `after` not found after install (`after: command not found`): Ensure
  your install location is in `PATH` (`/opt/homebrew/bin` or
  `/usr/local/bin` for Homebrew, `$(go env GOPATH)/bin` or `GOBIN` for
  `go install`, `/usr/local/bin` or `~/.local/bin` for manual install),
  then restart or reload your shell.
- `Permission denied` while installing to `/usr/local/bin`: Use
  `sudo install ...` or install to `~/.local/bin` instead.
- Homebrew command ambiguity with an existing `after` formula: use
  `brew install mtn-man/tools/after` and
  `brew info mtn-man/tools/after`.

## License

MIT License. See [LICENSE](LICENSE) file for details.
