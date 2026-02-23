# Timer

A simple countdown timer utility for the command line with visual feedback and audio alerts. It runs best on macOS, but supports other Unix-like systems like Linux. No Windows support at present.

## Features

- Live countdown display in both the terminal and title bar
- Graceful cancellation via Ctrl+C
- Audio alert on completion (plays system sound 4 times)
- Ceiling-based display (never shows 00:00:00 while time remains)
- Quiet mode when piped or redirected (no escape codes, no audio)
- Clean, minimal interface

## Installation

Install directly from GitHub:
```bash
go install github.com/Mtn-Man/timer@latest
```

Or clone and install:
```bash
git clone https://github.com/Mtn-Man/timer.git
cd timer
go install
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
timer -v       # Show version (timer v1.0)
```

The timer accepts any duration format supported by Go's `time.ParseDuration`, including combinations like `1h30m` or `2h15m30s`.

### Flags

- `-h`, `--help`: Show help and exit
- `-v`, `--version`: Show version (`timer v1.0`) and exit

## Requirements

- Go 1.16+ required only for building from source
- A Unix-like OS (macOS, Linux, or BSD)
- macOS provides the best out-of-the-box audio/terminal experience

## How It Works

The timer updates every 500ms, displaying the remaining time in `HH:MM:SS` format. The countdown appears both in your terminal output and in the terminal window's title bar.

When the timer completes, it prints `timer complete`, plays the system "Submarine" sound four times, and exits.

When stdout is not a TTY (for example, redirected or piped), the timer switches to a quiet mode:
it does not emit countdown/title updates or alarm audio, and prints a single `timer complete` line when done.

Press Ctrl+C at any time to cancel the timer gracefully. This prints `timer cancelled` and exits with code 130. 
Note that the terminal title bar may retain the last displayed time after cancellation depending on your terminal emulator.

## License

MIT License. See [LICENSE](LICENSE) file for details.
