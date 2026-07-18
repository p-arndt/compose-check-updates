package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// minWidth is the narrowest layout the renderers will attempt. Callers hand us
// whatever the terminal reports, including 0 during the first frame before
// Bubble Tea has delivered a WindowSizeMsg, so every entry point clamps first
// rather than guarding each arithmetic step against negative budgets.
const minWidth = 20

// Palette. The values are 256-colour indices rather than lipgloss.AdaptiveColor
// because Theme's fields are plain lipgloss.Color; these particular shades were
// picked to stay legible on both light and dark backgrounds.
func DefaultTheme() Theme {
	return Theme{
		Major:     lipgloss.Color("203"), // soft red — louder shades vibrate on light terminals
		Minor:     lipgloss.Color("179"), // amber, readable on white unlike bright yellow
		Patch:     lipgloss.Color("71"),  // muted green
		Digest:    lipgloss.Color("141"), // violet — deliberately off the blue axis so the badge cannot be read as chrome
		Text:      lipgloss.Color("252"),
		Dim:       lipgloss.Color("244"), // mid grey: the one value that survives both polarities
		Accent:    lipgloss.Color("75"),  // light blue: carries the dark title text with plenty of contrast
		Success:   lipgloss.Color("71"),
		Warn:      lipgloss.Color("179"),
		Error:     lipgloss.Color("203"),
		Highlight: lipgloss.Color("24"), // dark desaturated blue: reads as the cursor row, never as the title bar
	}
}

// LevelColor maps an internal.UpdateInfo.UpdateLevel value to its colour,
// falling back to the dim grey used for undetermined updates.
func (t Theme) LevelColor(level string) lipgloss.Color {
	switch strings.ToLower(level) {
	case "major":
		return t.Major
	case "minor":
		return t.Minor
	case "patch":
		return t.Patch
	case "digest":
		return t.Digest
	default:
		return t.Dim
	}
}

// badgeWidth is fixed so the columns after the badge line up regardless of
// which level a row carries.
const badgeWidth = 8

// Badge renders the level tag as a fixed-width chip.
func (t Theme) Badge(level string) string {
	label := strings.ToUpper(strings.TrimSpace(level))
	if label == "" {
		label = "-"
	}
	label = truncatePlain(label, badgeWidth-2)

	body := " " + padRight(label, badgeWidth-2) + " "
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("235")).
		Background(t.LevelColor(level)).
		Bold(true).
		Render(body)
}

// VersionDelta renders "current → latest" with only the version segments that
// actually changed carrying the level colour, so the eye lands on the part of
// the number that moved (the ncu trick the CLI logger uses).
func (t Theme) VersionDelta(current, latest, level string) string {
	dim := lipgloss.NewStyle().Foreground(t.Dim)
	col := lipgloss.NewStyle().Foreground(t.LevelColor(level)).Bold(true)

	if latest == "" {
		return dim.Render(current)
	}
	if current == "" {
		return col.Render(latest)
	}

	unchanged, changed := splitAtFirstDiff(current, latest)
	right := col.Render(changed)
	if unchanged != "" {
		right = lipgloss.NewStyle().Foreground(t.Text).Render(unchanged) + right
	}
	return dim.Render(current) + dim.Render(" → ") + right
}

// splitAtFirstDiff divides latest into the dot-separated segments it shares
// with current and the remainder that differs. A leading "v" is kept with the
// shared prefix so "v1.2.3 → v1.2.9" only lights up the patch segment.
func splitAtFirstDiff(current, latest string) (unchanged, changed string) {
	prefix := ""
	if strings.HasPrefix(latest, "v") && strings.HasPrefix(current, "v") {
		prefix = "v"
	}
	curParts := strings.Split(strings.TrimPrefix(current, "v"), ".")
	latParts := strings.Split(strings.TrimPrefix(latest, "v"), ".")

	i := 0
	for i < len(curParts) && i < len(latParts) && curParts[i] == latParts[i] {
		i++
	}
	if i == 0 {
		// Nothing in common (or a non-semver tag): colour the whole thing.
		return "", latest
	}
	if i >= len(latParts) {
		// latest is a prefix of current — no differing tail to highlight.
		return "", latest
	}
	return prefix + strings.Join(latParts[:i], ".") + ".", strings.Join(latParts[i:], ".")
}

// clampWidth keeps every renderer inside a sane budget so a zero or negative
// terminal width can never turn into a negative repeat count or slice bound.
func clampWidth(width int) int {
	if width < minWidth {
		return minWidth
	}
	return width
}

// truncatePlain shortens unstyled text to at most w visible runes, marking the
// cut with an ellipsis. Apply it before styling — truncating rendered output
// would slice through escape sequences.
func truncatePlain(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func padRight(s string, w int) string {
	if n := w - len([]rune(s)); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// fit is the last line of defence: it trims already-styled output to width
// without cutting escape sequences, guaranteeing the caller's layout invariant
// even if a segment estimate was off.
func fit(s string, width int) string {
	return lipgloss.NewStyle().MaxWidth(clampWidth(width)).Render(s)
}

// shortDigest keeps the algorithm and enough hex to identify a manifest; full
// digests are 71 characters and would dominate any pane they appear in.
func shortDigest(d string) string {
	if d == "" {
		return ""
	}
	algo, hex, ok := strings.Cut(d, ":")
	if !ok {
		return truncatePlain(d, 14)
	}
	if len(hex) <= 12 {
		return d
	}
	return algo + ":" + hex[:12] + "…"
}
