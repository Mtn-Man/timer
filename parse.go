package main

import (
	"strconv"
	"strings"
	"time"
)

// parseInvocation resolves CLI mode with explicit precedence:
// unknown options (before "--") beat help/version, then help beats version.
// Run mode requires exactly one duration token.
func parseInvocation(args []string) (invocation, error) {
	if len(args) <= 1 {
		return invocation{mode: modeRun}, errUsage
	}

	args = preprocessCombinedShortFlags(args)

	inv := invocation{
		mode: modeRun,
	}
	hasHelp := false
	hasVersion := false
	seenDoubleDash := false
	var firstUnknownOption string
	var durationToken string

	for i := 1; i < len(args); i++ {
		arg := args[i]
		if !seenDoubleDash && arg == "--" {
			seenDoubleDash = true
			continue
		}

		if !seenDoubleDash {
			switch arg {
			case "-h", "--help":
				hasHelp = true
				continue
			case "-v", "--version":
				hasVersion = true
				continue
			case "-q", "--quiet":
				inv.quiet = true
				continue
			case "-s", "--sound":
				inv.forceAlarm = true
				continue
			case "-f", "--sound-file":
				if i+1 >= len(args) {
					return invocation{mode: modeRun}, errUsage
				}
				inv.soundFile = args[i+1]
				inv.forceAlarm = true
				i++ // skip path
				continue
			case "-t", "--no-title":
				inv.noTitle = true
				continue
			case "-c", "--caffeinate":
				inv.forceAwake = true
				continue
			}

			if len(arg) > 0 && arg[0] == '-' && !isPotentialNegativeDuration(arg) {
				if firstUnknownOption == "" {
					firstUnknownOption = arg
				}
				continue
			}
		}

		if durationToken != "" {
			return invocation{mode: modeRun}, errUsage
		}
		durationToken = arg
		if i+1 < len(args) && isAMPMToken(args[i+1]) {
			i++
			durationToken = arg + " " + args[i]
		}
	}

	if firstUnknownOption != "" {
		return invocation{mode: modeRun}, unknownOptionError{option: firstUnknownOption}
	}
	if hasHelp {
		return invocation{mode: modeHelp}, nil
	}
	if hasVersion {
		return invocation{mode: modeVersion, quiet: inv.quiet, forceAlarm: inv.forceAlarm, forceAwake: inv.forceAwake}, nil
	}
	if durationToken == "" {
		return invocation{mode: modeRun}, errUsage
	}

	duration, err := parseDurationToken(durationToken)
	if err != nil {
		return invocation{mode: modeRun}, err
	}
	inv.duration = duration
	return inv, nil
}

func preprocessCombinedShortFlags(args []string) []string {
	if len(args) <= 1 {
		return args
	}

	shortFlags := knownShortFlagsSet(cliFlags)
	normalized := make([]string, 0, len(args))
	normalized = append(normalized, args[0])

	seenDoubleDash := false
	for _, arg := range args[1:] {
		if !seenDoubleDash && arg == "--" {
			seenDoubleDash = true
			normalized = append(normalized, arg)
			continue
		}

		if !seenDoubleDash {
			if expanded, ok := expandCombinedShortFlag(arg, shortFlags); ok {
				normalized = append(normalized, expanded...)
				continue
			}
		}

		normalized = append(normalized, arg)
	}

	return normalized
}

func knownShortFlagsSet(flags []cliFlag) map[rune]cliFlag {
	known := make(map[rune]cliFlag)
	for _, flag := range flags {
		if len(flag.short) != 2 || flag.short[0] != '-' {
			continue
		}
		known[rune(flag.short[1])] = flag
	}
	return known
}

func expandCombinedShortFlag(arg string, knownShortFlags map[rune]cliFlag) ([]string, bool) {
	if len(arg) < 3 || arg[0] != '-' || arg[1] == '-' {
		return nil, false
	}

	if isPotentialNegativeDuration(arg) {
		return nil, false
	}

	expanded := make([]string, 0, len(arg)-1)
	valueFlags := make([]string, 0, 1)

	for _, shortRune := range arg[1:] {
		flag, ok := knownShortFlags[shortRune]
		if !ok {
			return nil, false
		}
		if flag.takesValue {
			valueFlags = append(valueFlags, flag.short)
			continue
		}
		expanded = append(expanded, flag.short)
	}

	if len(valueFlags) > 1 {
		return nil, false
	}
	if len(valueFlags) == 1 {
		expanded = append(expanded, valueFlags[0])
	}

	return expanded, true
}

func parseDurationToken(token string) (time.Duration, error) {
	if d, ok, err := parseWallClockTime(token, time.Now()); ok {
		return d, err
	}

	duration, err := time.ParseDuration(token)
	if err != nil {
		if !isBareDecimalSecondsToken(token) {
			return 0, errInvalidDuration
		}

		duration, err = time.ParseDuration(token + "s")
		if err != nil {
			return 0, errInvalidDuration
		}
	}
	if duration < 0 {
		return 0, errDurationMustBeAtLeastZero
	}
	return duration, nil
}

// parseWallClockTime parses wall clock time tokens and returns the duration from
// now until the next occurrence of that time (target.Sub(now)).
//
// Accepted formats:
//   - 24-hour: H:MM, HH:MM, H:MM:SS, HH:MM:SS (hours [0,23], minutes/seconds [0,59])
//   - 12-hour: the above with a trailing AM/PM suffix, case-insensitive, optionally
//     space-separated (e.g. "9am", "9:30 PM", "12:00:00AM")
//   - Bare hour shorthand with AM/PM suffix only (e.g. "9am", "9 pm")
//   - Special case: 24:00 and 24:00:00 are accepted and normalized to 00:00(:00)
//
// 12-hour clock conventions: 12:00 AM is midnight (00:00), 12:00 PM is noon (12:00).
// Valid 12-hour hours are [1,12]; 0am and 13pm are rejected.
//
// If the resolved time is not strictly after now (already passed or exact match),
// it wraps to the same time the following day using date arithmetic, which is DST-safe.
//
// The boolean return indicates whether the token claimed to be a wall clock time at all.
// false means no colon and no AM/PM suffix were present; the caller should try other formats.
// A token that looks like a time but fails validation returns true with errInvalidTime.
func parseWallClockTime(token string, now time.Time) (time.Duration, bool, error) {
	stripped, isPM, hasSuffix := stripAMPM(token)

	switch strings.ToLower(token) {
	case "noon":
		stripped = "12:00"
	case "midnight":
		stripped = "00:00"
	}

	hasColon := strings.ContainsRune(stripped, ':')
	if !hasSuffix && !hasColon {
		return 0, false, nil
	}

	switch stripped {
	case "24:00":
		stripped = "00:00"
	case "24:00:00":
		stripped = "00:00:00"
	}

	var parts []string
	if hasColon {
		parts = strings.Split(stripped, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return 0, true, errInvalidTime
		}
	} else {
		parts = []string{stripped}
	}

	hourRange := [2]int{0, 23}
	if hasSuffix {
		hourRange = [2]int{1, 12}
	}

	hour, ok := parseTimeField(parts[0], hourRange[0], hourRange[1])
	if !ok {
		return 0, true, errInvalidTime
	}

	min := 0
	sec := 0

	if len(parts) >= 2 {
		min, ok = parseTimeField(parts[1], 0, 59)
		if !ok {
			return 0, true, errInvalidTime
		}
	}
	if len(parts) == 3 {
		sec, ok = parseTimeField(parts[2], 0, 59)
		if !ok {
			return 0, true, errInvalidTime
		}
	}

	if hasSuffix {
		hour, ok = applyAMPM(hour, isPM)
		if !ok {
			return 0, true, errInvalidTime
		}
	}

	target := time.Date(now.Year(), now.Month(), now.Day(), hour, min, sec, 0, now.Location())
	if !target.After(now) {
		target = time.Date(target.Year(), target.Month(), target.Day()+1, target.Hour(), target.Minute(), target.Second(), 0, target.Location())
	}

	return target.Sub(now), true, nil
}

// stripAMPM removes a trailing AM or PM suffix from token, case-insensitively.
// The suffix may be directly attached ("9am", "9a") or preceded by a single space ("9 am", "9 a").
// Returns the stripped token, whether the suffix was PM, and whether any suffix was found.
// The space-prefixed suffixes are checked first to ensure "9 am" strips " am" in full
// rather than just "am", which would leave a trailing space in the result.
// The two-letter forms ("am"/"pm") are checked before the one-letter shorthands ("a"/"p")
// so that "9am" never accidentally matches only "a".
// The one-letter shorthands are only accepted when the character immediately preceding
// the suffix is a digit, preventing false matches on unrelated tokens like "--help".
func stripAMPM(token string) (string, bool, bool) {
	lower := strings.ToLower(token)
	for _, suffix := range []string{" am", " pm", "am", "pm", " a", " p", "a", "p"} {
		if !strings.HasSuffix(lower, suffix) {
			continue
		}
		stripped := token[:len(token)-len(suffix)]
		// For single-letter shorthands, require the preceding character to be a digit.
		if (suffix == "a" || suffix == "p" || suffix == " a" || suffix == " p") && (len(stripped) == 0 || stripped[len(stripped)-1] < '0' || stripped[len(stripped)-1] > '9') {
			continue
		}
		isPM := suffix == "pm" || suffix == " pm" || suffix == "p" || suffix == " p"
		return stripped, isPM, true
	}
	return token, false, false
}

// applyAMPM converts a 12-hour clock hour to a 24-hour clock hour.
// Valid input hours are [1, 12]. Returns false if the hour is out of that range.
// 12 AM maps to 0 (midnight); 12 PM maps to 12 (noon); all others follow standard convention.
func applyAMPM(hour int, isPM bool) (int, bool) {
	if hour < 1 || hour > 12 {
		return 0, false
	}
	if isPM {
		if hour == 12 {
			return 12, true
		}
		return hour + 12, true
	}
	if hour == 12 {
		return 0, true
	}
	return hour, true
}

// isAMPMToken reports whether s is exactly "am", "pm", "a", or "p", case-insensitively.
// Used by parseInvocation to detect a space-separated AM/PM token following a time argument.
func isAMPMToken(s string) bool {
	lower := strings.ToLower(s)
	return lower == "am" || lower == "pm" || lower == "a" || lower == "p"
}

// parseTimeField parses a numeric string and checks it falls within [min, max].
// Leading zeros are accepted (e.g. "09" parses as 9). Empty strings and
// non-numeric characters (including signs and decimal points) are rejected.
func parseTimeField(s string, min, max int) (int, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < min || v > max {
		return 0, false
	}
	return v, true
}

func isBareDecimalSecondsToken(token string) bool {
	if token == "" {
		return false
	}

	start := 0
	if token[0] == '+' || token[0] == '-' {
		start = 1
	}
	if start >= len(token) {
		return false
	}

	hasDigit := false
	dotCount := 0

	for i := start; i < len(token); i++ {
		switch c := token[i]; {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '.':
			dotCount++
			if dotCount > 1 {
				return false
			}
		default:
			return false
		}
	}

	return hasDigit
}

// isPotentialNegativeDuration distinguishes duration-like inputs (e.g. "-1s")
// from unknown flags so negative durations flow through normal duration validation.
func isPotentialNegativeDuration(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}

	next := arg[1]
	return (next >= '0' && next <= '9') || next == '.'
}
