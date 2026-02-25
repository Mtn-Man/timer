# Timer

A simple countdown timer utility for the command line with visual feedback and audio alerts. It runs best on macOS, but supports other Unix-like systems like Linux. No Windows support at present.

## Features

- Live countdown display in both the terminal and title bar
- Graceful cancellation via Ctrl+C
- Audio alert on completion (best-effort, platform-specific backend)
- Optional `-q`/`--quiet` mode for inline countdown only
- Optional `--alarm` to force alarm playback on completion
- Ceiling-based display (never shows 00:00:00 while time remains)
- Quiet mode when piped or redirected (no countdown output, no audio unless `--alarm`)
- Clean, minimal interface

## Installation

### Install Release Binary (No Go Required)

1. Download your platform archive and `checksums.txt` from the
   [latest release](https://github.com/Mtn-Man/timer/releases/latest).
   Replace `<version>` in the examples below with the release tag
   (for example, `v1.0.0`).
   Archive naming pattern:
   - `timer_<version>_darwin_amd64.tar.gz`
   - `timer_<version>_darwin_arm64.tar.gz`
   - `timer_<version>_linux_amd64.tar.gz`
   - `timer_<version>_linux_arm64.tar.gz`
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
go build -ldflags "-X main.version=v1.0.0" -o timer .
./timer --version
```

Without `-ldflags`, `--version` reports `timer dev`.

## Usage
```bash
timer <duration>
timer --help
timer --version
timer --quiet <duration>
timer --alarm <duration>
```

### Examples
```bash
timer 30s      # 30 seconds
timer 5m       # 5 minutes
timer 1.5h     # 1.5 hours
timer 90m      # 90 minutes
timer --help   # Show help
timer -v       # Show version (e.g. timer dev or timer v1.0.0)
timer -q 5m    # Quiet mode: inline countdown only
timer --alarm 5m # Force alarm playback even in quiet/non-TTY mode
```

The timer accepts any duration format supported by Go's `time.ParseDuration`, including combinations like `1h30m` or `2h15m30s`.

### Flags

- `-h`, `--help`: Show help and exit
- `-v`, `--version`: Show version and exit (`timer dev` unless injected at build time)
- `-q`, `--quiet`: Interactive inline countdown only (no title updates, completion line, alarm, or cancel text)
- `--alarm`: Force alarm playback on completion even in `--quiet` or non-TTY mode

## Requirements

- Go 1.20+ required only for building from source
- A Unix-like OS (macOS, Linux, or BSD)
- macOS provides the best out-of-the-box audio/terminal experience

## How It Works

The timer updates every 500ms, displaying the remaining time in `HH:MM:SS` format. The countdown appears both in your terminal output and in the terminal window's title bar.

In normal interactive mode, completion prints `timer complete`, plays an alert using the best available backend for your platform, and exits.

With `-q` / `--quiet` in interactive mode, timer output is limited to the inline countdown:
no title updates, no completion line, no alarm, and no cancel text.

When stdout is not a TTY (for example, redirected or piped), the timer switches to a quiet mode:
it does not emit countdown/title updates, completion output, or alarm audio.

When `--alarm` is provided, alarm playback is still attempted on completion in `--quiet` and non-TTY modes.
This only affects alarm behavior; output suppression remains unchanged.

Press Ctrl+C at any time to cancel the timer gracefully. In interactive normal mode, the current line is cleared and `timer cancelled` is printed, then the process exits with code 130. In `--quiet` mode and non-TTY mode, cancellation text is suppressed.
If the process receives SIGTERM, it exits with code 143.
Note that in normal interactive mode, the terminal title bar may retain the last displayed time after cancellation depending on your terminal emulator.

## License

MIT License. See [LICENSE](LICENSE) file for details.
