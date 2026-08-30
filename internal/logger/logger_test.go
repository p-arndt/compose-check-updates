package logger

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	reset = "\033[0m"
	red   = "\033[31m"
	blue  = "\033[34m"
	green = "\033[32m"
)

func TestColorizeChangedSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		current     string
		latest      string
		updateLevel string
		want        string
	}{
		{
			// Without an update level there is nothing to color by, so the tag
			// has to survive untouched rather than pick up a stray escape.
			name:    "no update level leaves the tag untouched",
			current: "1.2.3",
			latest:  "1.2.4",
			want:    "1.2.4",
		},
		{
			name:        "patch change colors only the last segment",
			current:     "1.2.3",
			latest:      "1.2.4",
			updateLevel: "patch",
			want:        "1.2." + green + "4" + reset,
		},
		{
			// The trailing segments after the first difference are colored as one
			// run, so a minor bump takes the patch digit with it.
			name:        "minor change colors from the minor segment on",
			current:     "1.2.3",
			latest:      "1.3.0",
			updateLevel: "minor",
			want:        "1." + blue + "3.0" + reset,
		},
		{
			name:        "major change colors the whole version",
			current:     "1.2.3",
			latest:      "2.0.0",
			updateLevel: "major",
			want:        red + "2.0.0" + reset,
		},
		{
			// A leading v belongs to the unchanged prefix, otherwise the color
			// would start one character too early.
			name:        "v prefix stays outside the colored run",
			current:     "v1.2.3",
			latest:      "v1.2.9",
			updateLevel: "patch",
			want:        "v1.2." + green + "9" + reset,
		},
		{
			name:        "v prefix on a major change is colored with the version",
			current:     "v1.2.3",
			latest:      "v2.0.0",
			updateLevel: "major",
			want:        red + "v2.0.0" + reset,
		},
		{
			// The tag gained a segment; the comparison stops at the shorter one
			// and everything beyond it counts as changed.
			name:        "differing segment counts",
			current:     "1.2",
			latest:      "1.2.3",
			updateLevel: "patch",
			want:        "1.2." + green + "3" + reset,
		},
		{
			name:        "latest shorter than current",
			current:     "1.2.3",
			latest:      "1.3",
			updateLevel: "minor",
			want:        "1." + blue + "3" + reset,
		},
		{
			// Nothing differs, so the whole tag is the unchanged prefix and the
			// separator it is joined with is left dangling.
			name:        "identical versions",
			current:     "1.2.3",
			latest:      "1.2.3",
			updateLevel: "patch",
			want:        "1.2.3." + green + reset,
		},
		{
			// Tags that are not versions at all still reach this code; they must
			// come out whole rather than sliced on a dot that is not there.
			name:        "non-semver tag",
			current:     "latest",
			latest:      "stable",
			updateLevel: "major",
			want:        red + "stable" + reset,
		},
		{
			name:        "unknown update level is not colored",
			current:     "1.2.3",
			latest:      "1.2.4",
			updateLevel: "sideways",
			want:        "1.2.4",
		},
		{
			// The level arrives as a plain attribute string, so casing is not
			// guaranteed to be normalized upstream.
			name:        "update level is matched case-insensitively",
			current:     "1.2.3",
			latest:      "2.0.0",
			updateLevel: "MAJOR",
			want:        red + "2.0.0" + reset,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, colorizeChangedSegments(tt.current, tt.latest, tt.updateLevel))
		})
	}
}

func TestVisibleLen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "empty string", in: "", want: 0},
		{name: "plain string", in: "1.2.3", want: 5},
		{name: "colorized string ignores the escapes", in: red + "1.2.3" + reset, want: 5},
		{name: "escapes only", in: red + reset, want: 0},
		// Column layout is measured in characters on screen, not bytes.
		{name: "multi-byte runes count once each", in: "äöü", want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, visibleLen(tt.in))
		})
	}
}

func TestPadRight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{name: "plain string is padded to the width", in: "abc", width: 6, want: "abc   "},
		{name: "empty string becomes all spaces", in: "", width: 3, want: "   "},
		{name: "string at the width is untouched", in: "abc", width: 3, want: "abc"},
		// Truncating would hide data, so an over-long cell is allowed to push the
		// column instead.
		{name: "string wider than the width is returned as is", in: "abcdef", width: 3, want: "abcdef"},
		{
			name:  "colorized string is padded by visible length",
			in:    red + "abc" + reset,
			width: 6,
			want:  red + "abc" + reset + "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, padRight(tt.in, tt.width))
		})
	}
}

// A colorized cell and its plain equivalent must occupy the same number of
// columns; if the escapes were counted the terminal layout would smear.
func TestPadRightColorizedMatchesPlainVisibleWidth(t *testing.T) {
	t.Parallel()

	plain := padRight("1.2.3", versionWidth)
	colored := padRight("1.2."+green+"3"+reset, versionWidth)

	assert.Equal(t, visibleLen(plain), visibleLen(colored))
	assert.Equal(t, versionWidth, visibleLen(colored))
}

func TestEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		min     slog.Level
		level   slog.Level
		enabled bool
	}{
		{name: "below the minimum is dropped", min: slog.LevelInfo, level: slog.LevelDebug, enabled: false},
		{name: "at the minimum is kept", min: slog.LevelInfo, level: slog.LevelInfo, enabled: true},
		{name: "above the minimum is kept", min: slog.LevelInfo, level: slog.LevelError, enabled: true},
		{name: "debug minimum keeps everything", min: slog.LevelDebug, level: slog.LevelDebug, enabled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := NewCustomHandler(tt.min, os.Stdout)
			assert.Equal(t, tt.enabled, h.Enabled(context.Background(), tt.level))
		})
	}
}

func TestGetLevelStringAndColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		level     slog.Level
		wantLabel string
		wantColor string
	}{
		{name: "debug", level: slog.LevelDebug, wantLabel: "DEBUG", wantColor: blue},
		{name: "info", level: slog.LevelInfo, wantLabel: "INFO", wantColor: green},
		{name: "warn", level: slog.LevelWarn, wantLabel: "WARN", wantColor: "\033[33m"},
		{name: "error", level: slog.LevelError, wantLabel: "ERROR", wantColor: red},
		// Custom levels are legal in slog and must not produce an empty label.
		{name: "custom level falls back to info", level: slog.Level(3), wantLabel: "INFO", wantColor: green},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			label, color := getLevelStringAndColor(tt.level)
			assert.Equal(t, tt.wantLabel, label)
			assert.Equal(t, tt.wantColor, color)
			// The widest label still has to fit the level column.
			assert.LessOrEqual(t, len(label)+2, levelWidth)
		})
	}
}

func TestGetUpdateLevelColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		updateLevel string
		want        string
	}{
		{name: "major", updateLevel: "major", want: red},
		{name: "minor", updateLevel: "minor", want: blue},
		{name: "patch", updateLevel: "patch", want: green},
		{name: "mixed case", updateLevel: "Patch", want: green},
		{name: "empty", updateLevel: "", want: ""},
		{name: "unknown", updateLevel: "rewrite", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, getUpdateLevelColor(tt.updateLevel))
		})
	}
}

func TestWithAttrsAndWithGroup(t *testing.T) {
	t.Parallel()

	h := NewCustomHandler(slog.LevelWarn, os.Stdout)

	withAttrs := h.WithAttrs([]slog.Attr{slog.String("image", "nginx")})
	withGroup := h.WithGroup("group")

	// Both return a copy, so the original must not be aliased, but the level and
	// output they carry have to survive the copy.
	assert.NotSame(t, h, withAttrs)
	assert.NotSame(t, h, withGroup)
	for _, got := range []slog.Handler{withAttrs, withGroup} {
		assert.True(t, got.Enabled(context.Background(), slog.LevelError))
		assert.False(t, got.Enabled(context.Background(), slog.LevelInfo))
	}
}

// handle renders one record through a handler writing to a temp file and returns
// the single line it produced.
func handle(t *testing.T, level slog.Level, msg string, attrs ...slog.Attr) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "out.log")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	h := NewCustomHandler(slog.LevelDebug, f)
	r := slog.NewRecord(time.Time{}, level, msg, 0)
	r.AddAttrs(attrs...)
	require.NoError(t, h.Handle(context.Background(), r))

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.TrimSuffix(string(out), "\n")
}

func TestHandle(t *testing.T) {
	t.Parallel()

	t.Run("plain message", func(t *testing.T) {
		t.Parallel()

		line := handle(t, slog.LevelInfo, "Up to date")

		assert.True(t, strings.HasPrefix(line, green+"[INFO] "+reset+"  "), line)
		assert.Contains(t, line, "Up to date")
		// The message column is padded even when nothing follows it.
		assert.Equal(t, levelWidth+2+msgWidth+2, visibleLen(line))
	})

	t.Run("update record", func(t *testing.T) {
		t.Parallel()

		line := handle(t, slog.LevelWarn, "Update available",
			slog.String("image", "nginx"),
			slog.String("current", "1.2.3"),
			slog.String("latest", "1.2.9"),
			slog.String("update_level", "patch"),
			slog.String("path", "docker-compose.yml"),
		)

		// The update level is appended to the message rather than given a column.
		assert.Contains(t, line, "Update available (patch)")
		assert.Contains(t, line, "Image: nginx")
		assert.Contains(t, line, "1.2.3 -> 1.2."+green+"9"+reset)
		assert.Contains(t, line, "Path: docker-compose.yml")
		// Everything up to the path is fixed-width regardless of the escapes.
		before, _, found := strings.Cut(line, "Path: ")
		require.True(t, found)
		assert.Equal(t, levelWidth+2+msgWidth+2+len("Image: ")+imageWidth+2+versionWidth+2, visibleLen(before))
	})

	t.Run("file attribute stands in for path", func(t *testing.T) {
		t.Parallel()

		line := handle(t, slog.LevelError, "Broken", slog.String("file", "compose.yaml"))

		assert.Contains(t, line, red+"[ERROR]"+reset)
		assert.Contains(t, line, "Path: compose.yaml")
	})

	t.Run("unknown attributes are appended as key=value", func(t *testing.T) {
		t.Parallel()

		line := handle(t, slog.LevelDebug, "Fetching", slog.String("registry", "docker.io"))

		assert.Contains(t, line, "registry=docker.io")
	})

	t.Run("a lone version attribute is not rendered", func(t *testing.T) {
		t.Parallel()

		// Only current+latest together form the version column; a half-filled
		// pair would otherwise print a dangling arrow.
		line := handle(t, slog.LevelInfo, "Partial", slog.String("current", "1.2.3"))

		assert.NotContains(t, line, "->")
	})
}
