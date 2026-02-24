# Timer

A simple countdown timer utility for the command line with visual feedback and audio alerts. It runs best on macOS, but supports other Unix-like systems like Linux. No Windows support at present.

## Features

- Live countdown display in both the terminal and title bar
- Graceful cancellation via Ctrl+C
- Audio alert on completion (best-effort, platform-specific backend)
- Ceiling-based display (never shows 00:00:00 while time remains)
- Quiet mode when piped or redirected (no escape codes, no audio)
- Clean, minimal interface

## Installation

### Install Release Binary (No Go Required)

1. Download the archive for your platform from the
   [latest release](https://github.com/Mtn-Man/timer/releases/latest).
2. Extract it:
   ```bash
   tar -xzf <release-archive>.tar.gz
   ```
3. Move the extracted binary into your `PATH` as `timer`:
   ```bash
   sudo install -m 0755 <extracted-binary> /usr/local/bin/timer
   ```
4. Verify:
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
go install github.com/Mtn-Man/timer@v1.0.0
```

Or clone and build locally:
```bash
git clone https://github.com/Mtn-Man/timer.git
cd timer
go build -o timer .
./timer --version
```

## Usage
```bash
timer <duration>
timer --help
timer --version
```

### Examples
```bash
timer 30s      # 30 seconds
timer 5m       # 5 minutes
timer 1.5h     # 1.5 hours
timer 90m      # 90 minutes
timer --help   # Show help
timer -v       # Show version (timer v1.0.0)
```

The timer accepts any duration format supported by Go's `time.ParseDuration`, including combinations like `1h30m` or `2h15m30s`.

### Flags

- `-h`, `--help`: Show help and exit
- `-v`, `--version`: Show version (`timer v1.0.0`) and exit

## Requirements

- Go 1.20+ required only for building from source
- A Unix-like OS (macOS, Linux, or BSD)
- macOS provides the best out-of-the-box audio/terminal experience

## How It Works

The timer updates every 500ms, displaying the remaining time in `HH:MM:SS` format. The countdown appears both in your terminal output and in the terminal window's title bar.

When the timer completes, it prints `timer complete`, plays an alert using the best available backend for your platform, and exits.

When stdout is not a TTY (for example, redirected or piped), the timer switches to a quiet mode:
it does not emit countdown/title updates or alarm audio, and prints a single `timer complete` line when done.

Press Ctrl+C at any time to cancel the timer gracefully. This prints `timer cancelled` and exits with code 130. 
Note that the terminal title bar may retain the last displayed time after cancellation depending on your terminal emulator.

## License

MIT License. See [LICENSE](LICENSE) file for details.
