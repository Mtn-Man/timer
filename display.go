package main

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"time"
)

func renderInteractiveCountdown(status statusDisplay, timeStr string, noTitle bool) {
	if status.supportsAdvanced {
		if noTitle {
			writeStatusf(status.writer, "\r\033[K%s", timeStr)
			return
		}
		// Update title bar and terminal line in a single operation.
		// \033]0; sets title, \007 terminates the OSC sequence, \r returns to start of line.
		writeStatusf(status.writer, "\033]0;%s\007\r\033[K%s", timeStr, timeStr)
		return
	}
	writeStatusf(status.writer, "\r%s", timeStr)
}

func formatRemainingTime(remaining time.Duration) string {
	// Ceiling-based calculation for whole seconds.
	totalSeconds := int((remaining + time.Second - 1) / time.Second)
	h := totalSeconds / 3600
	m := (totalSeconds % 3600) / 60
	s := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func printComplete(status statusDisplay, quiet bool) {
	printFinalStatus(status, quiet, "after complete", "after: complete")
}

func printCancelled(status statusDisplay, quiet bool) {
	printFinalStatus(status, quiet, "after cancelled", "after: cancelled")
}

func printFinalStatus(status statusDisplay, quiet bool, interactiveMsg, nonTTYMsg string) {
	if quiet {
		clearInteractiveStatusLine(status)
		return
	}

	if status.interactive {
		clearInteractiveStatusLine(status)
		writeStatusln(status.writer, interactiveMsg)
		return
	}
	writeStatusln(status.writer, nonTTYMsg)
}

func clearInteractiveStatusLine(status statusDisplay) {
	if !status.interactive {
		return
	}
	if status.supportsAdvanced {
		writeStatus(status.writer, "\r\033[K")
		return
	}
	writeStatus(status.writer, "\r")
}

func writeStatus(writer io.Writer, s string) {
	_, _ = fmt.Fprint(writer, s)
}

func writeStatusln(writer io.Writer, a ...any) {
	_, _ = fmt.Fprintln(writer, a...)
}

func writeStatusf(writer io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(writer, format, a...)
}

func renderHelpText() string {
	var b strings.Builder
	b.WriteString(usageText)
	b.WriteString("\n\nFlags:\n")

	for i, flag := range cliFlags {
		label := fmt.Sprintf("%s, %s", flag.short, flag.long)
		if flag.short == "" {
			label = "    " + flag.long
		}
		fmt.Fprintf(&b, "  %-17s%s", label, flag.description)
		if i < len(cliFlags)-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n\nNote: -- ends option parsing; subsequent tokens are treated as positional arguments.\n")

	return b.String()
}

func formatVersionLine(v string) string {
	return fmt.Sprintf("after %s\n", v)
}

func mainModuleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return ""
	}
	return info.Main.Version
}

func resolveVersion(buildVersion, moduleVersion string) string {
	if buildVersion != "" && buildVersion != defaultVersion {
		return buildVersion
	}
	if moduleVersion != "" && moduleVersion != develBuildInfoVersion {
		return moduleVersion
	}
	if buildVersion != "" {
		return buildVersion
	}
	return defaultVersion
}
