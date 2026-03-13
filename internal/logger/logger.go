package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// CustomHandler is a slog.Handler that adds colorization based on log levels.
type CustomHandler struct {
	level  slog.Leveler
	output *os.File
}

// NewCustomHandler creates a new CustomHandler with the specified minimum log level.
func NewCustomHandler(level slog.Leveler, output *os.File) *CustomHandler {
	return &CustomHandler{
		level:  level,
		output: output,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *CustomHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Column widths for fixed-layout output.
const (
	levelWidth   = 7  // [ERROR] is widest
	msgWidth     = 33 // "Skipping (failed fetching tags)" = 31
	imageWidth   = 29 // "127.0.0.1:5000/myrepo/myapp"  = 27
	versionWidth = 22 // "v2.9.3 -> 3.6.10" = ~17
)

// Handle formats and writes the Record to the output.
func (h *CustomHandler) Handle(_ context.Context, r slog.Record) error {
	levelStr, levelColor := getLevelStringAndColor(r.Level)

	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = fmt.Sprint(a.Value)
		return true
	})

	message := r.Message
	image := attrs["image"]
	path := attrs["path"]
	if path == "" {
		path = attrs["file"]
	}
	current := attrs["current"]
	latest := attrs["latest"]
	updateLevel := attrs["update_level"]

	const reset = "\033[0m"

	if updateLevel != "" {
		message += " (" + updateLevel + ")"
	}

	var sb strings.Builder
	sb.WriteString(levelColor + padRight("["+levelStr+"]", levelWidth) + reset + "  ")
	sb.WriteString(padRight(message, msgWidth) + "  ")
	if image != "" {
		sb.WriteString("Image: " + padRight(image, imageWidth) + "  ")
	}
	if current != "" && latest != "" {
		versionStr := current + " -> " + colorizeChangedSegments(current, latest, updateLevel)
		sb.WriteString(padRight(versionStr, versionWidth) + "  ")
	}
	if path != "" {
		sb.WriteString("Path: " + path)
	}

	var extra []string
	for k, v := range attrs {
		switch k {
		case "image", "path", "file", "current", "latest", "update_level":
		default:
			extra = append(extra, k+"="+v)
		}
	}
	if len(extra) > 0 {
		sb.WriteString("  " + strings.Join(extra, " "))
	}

	fmt.Fprintln(h.output, sb.String())
	return nil
}

func getUpdateLevelColor(updateLevel string) string {
	switch strings.ToLower(updateLevel) {
	case "major":
		return "\033[31m" // Red
	case "minor":
		return "\033[34m" // Blue
	case "patch":
		return "\033[32m" // Green
	default:
		return ""
	}
}

// colorizeChangedSegments colors only the version segments that differ between
// current and latest, mimicking ncu-style output (e.g. 1.23.1 -> 1.[blue]29.6[reset]).
func colorizeChangedSegments(current, latest, updateLevel string) string {
	color := getUpdateLevelColor(updateLevel)
	if color == "" {
		return latest
	}
	reset := "\033[0m"

	// Preserve a leading 'v' prefix on the latest tag
	prefix := ""
	if strings.HasPrefix(latest, "v") {
		prefix = "v"
	}
	latestNorm := strings.TrimPrefix(latest, "v")
	currentNorm := strings.TrimPrefix(current, "v")

	currentParts := strings.Split(currentNorm, ".")
	latestParts := strings.Split(latestNorm, ".")

	// Find the index of the first segment that differs
	diffIdx := 0
	for diffIdx < len(currentParts) && diffIdx < len(latestParts) {
		if currentParts[diffIdx] != latestParts[diffIdx] {
			break
		}
		diffIdx++
	}

	if diffIdx == 0 {
		// All segments changed (major bump from the very first segment)
		return color + prefix + latestNorm + reset
	}

	// Keep the unchanged leading segments white, color the rest
	unchanged := prefix + strings.Join(latestParts[:diffIdx], ".") + "."
	changed := strings.Join(latestParts[diffIdx:], ".")
	return unchanged + color + changed + reset
}

// WithAttrs returns a new handler with the given attributes.
func (h *CustomHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Create a new handler with the additional attributes
	newHandler := *h
	return &newHandler
}

// WithGroup returns a new handler with the given group.
func (h *CustomHandler) WithGroup(name string) slog.Handler {
	// Create a new handler with the group name
	newHandler := *h
	return &newHandler
}

// getLevelStringAndColor returns the string representation and color for a given log level.
func getLevelStringAndColor(level slog.Level) (string, string) {
	switch level {
	case slog.LevelDebug:
		return "DEBUG", "\033[34m" // Blue
	case slog.LevelInfo:
		return "INFO", "\033[32m" // Green
	case slog.LevelWarn:
		return "WARN", "\033[33m" // Yellow
	case slog.LevelError:
		return "ERROR", "\033[31m" // Red
	default:
		return "INFO", "\033[32m" // Default to INFO level
	}
}

// ansiEscape matches ANSI color escape sequences so we can measure visible string length.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visibleLen returns the number of visible runes in s, ignoring ANSI escape codes.
func visibleLen(s string) int {
	return len([]rune(ansiEscape.ReplaceAllString(s, "")))
}

// padRight pads s with spaces on the right until its visible length equals width.
func padRight(s string, width int) string {
	vl := visibleLen(s)
	if vl >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vl)
}
