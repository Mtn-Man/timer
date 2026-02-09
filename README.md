# Timer

A simple countdown timer utility for the command line with visual feedback and audio alerts.

## Features

- Live countdown display in both the terminal and title bar
- Graceful cancellation via Ctrl+C with automatic title restoration
- Audio alert on completion (plays system sound 4 times)
- Ceiling-based display (never shows 00:00:00 while time remains)
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
```

### Examples
```bash
timer 30s      # 30 seconds
timer 5m       # 5 minutes
timer 1.5h     # 1.5 hours
timer 90m      # 90 minutes
```

The timer accepts any duration format supported by Go's `time.ParseDuration`, including combinations like `1h30m` or `2h15m30s`.

## Requirements

- Go 1.16 or later
- macOS (uses `afplay` for audio alerts)

## How It Works

The timer updates every 500ms, displaying the remaining time in `HH:MM:SS` format. The countdown appears both in your terminal output and in the terminal window's title bar. When the timer completes, it plays the system "Submarine" sound four times and displays "Timer Complete".

Press Ctrl+C at any time to cancel the timer gracefully.

## License

MIT License. See [LICENSE](LICENSE) file for details.
