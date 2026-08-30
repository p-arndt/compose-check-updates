package tui

import (
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/policy"

	"github.com/charmbracelet/lipgloss"
)

// minWidth is the narrowest layout the renderers will attempt. Terminal width is
// legitimately 0 before the first WindowSizeMsg, so every entry point clamps
// rather than guarding each arithmetic step against negative budgets.
const minWidth = 20

// Palette. The values are 256-colour indices rather than lipgloss.AdaptiveColor
// because Theme's fields are plain lipgloss.Color; these particular shades were
// picked to stay legible on both light and dark backgrounds.
func DefaultTheme() Theme {
	return Theme{
		Major:  lipgloss.Color("203"), // soft red — louder shades vibrate on light terminals
		Minor:  lipgloss.Color("179"), // amber, readable on white unlike bright yellow
		Patch:  lipgloss.Color("71"),  // muted green
		Digest: lipgloss.Color("141"), // violet — deliberately off the blue axis so the badge cannot be read as chrome
		Pin:    lipgloss.Color("109"), // desaturated teal — a pin changes no version, so it must not read as loud as one that does
		// Orange: close enough to the warning amber to read as one, far enough from
		// the minor badge that the two are not mistaken for the same thing.
		Unreadable: lipgloss.Color("173"),
		Text:       lipgloss.Color("252"),
		Dim:        lipgloss.Color("244"), // mid grey: the one value that survives both polarities
		Accent:     lipgloss.Color("75"),  // light blue: carries the dark title text with plenty of contrast
		Success:    lipgloss.Color("71"),
		Warn:       lipgloss.Color("179"),
		Error:      lipgloss.Color("203"),
		Highlight:  lipgloss.Color("24"), // dark desaturated blue: reads as the cursor row, never as the title bar
	}
}

// LevelColor maps an update level to its colour, falling back to the dim grey
// used for undetermined updates.
func (t Theme) LevelColor(level policy.Level) lipgloss.Color {
	switch policy.Level(strings.ToLower(level.String())) {
	case policy.LevelMajor:
		return t.Major
	case policy.LevelMinor:
		return t.Minor
	case policy.LevelPatch:
		return t.Patch
	case policy.LevelDigest:
		return t.Digest
	case policy.LevelPin:
		return t.Pin
	case policy.LevelUnreadable:
		return t.Unreadable
	default:
		return t.Dim
	}
}

// badgeWidth is fixed so the columns after the badge line up regardless of
// which level a row carries.
const badgeWidth = 8

// badgeLabels holds the word a level wears on its chip where its own name does
// not fit: truncating "unreadable" to the chip leaves "UNREAD…", which reads as
// a level of its own rather than as the one it is short for.
var badgeLabels = map[policy.Level]string{policy.LevelUnreadable: "UNREAD"}

// badgeLabel is the chip text for a level, in the case the chip is drawn in.
func badgeLabel(level policy.Level) string {
	name := strings.TrimSpace(level.String())
	if l, ok := badgeLabels[policy.Level(strings.ToLower(name))]; ok {
		return l
	}
	return strings.ToUpper(name)
}

// Badge renders the level tag as a fixed-width chip.
func (t Theme) Badge(level policy.Level) string {
	label := badgeLabel(level)
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

// BadgeTight is the same chip without the fixed width. Nothing lines up after a
// badge in the sidebar, where the padding would only open a gap.
func (t Theme) BadgeTight(level policy.Level) string {
	label := badgeLabel(level)
	if label == "" {
		label = "-"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("235")).
		Background(t.LevelColor(level)).
		Bold(true).
		Render(" " + label + " ")
}

// VersionDelta renders "current → latest" with only the version segments that
// actually changed carrying the level colour, so the eye lands on the part of
// the number that moved (the ncu trick the CLI logger uses).
func (t Theme) VersionDelta(current, latest string, level policy.Level) string {
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

// padDisplay pads to a width the terminal will actually show. padRight counts
// runes, which is wrong for styled text: an escape sequence is several runes and
// zero columns wide, so a coloured line comes out short.
func padDisplay(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
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

// The sidebar's own styles, deliberately quieter than the list's: the list is
// what the eye scans.

func (t Theme) dim() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Dim)
}

func (t Theme) rule() string {
	return t.dim().Render(" │ ")
}

// sideTitle names the image the column is about. It is the only bold line in
// the sidebar, because everything below it is qualified by this one.
func (t Theme) sideTitle(image string, width int) string {
	return lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(fit(image, width))
}

// sideField is a read-only fact: a dim label and its value.
func (t Theme) sideField(label, value string, width int) string {
	l := t.dim().Render(padRight(label, 6))
	return fit(l+lipgloss.NewStyle().Foreground(t.Text).Render(value), width)
}

// boxChrome is what a box costs a caller in width: two border columns and the
// one column of padding inside each of them, so text never touches the frame.
const boxChrome = 4

// Box frames a block of lines. The frame is the only thing saying which half the
// keyboard is talking to, so the focused box changes colour *and* weight: colour
// alone is invisible on a terminal told not to use any. Both borders are one cell
// wide, so switching between them moves nothing.
//
// The returned lines are innerH+2 of them, each innerW+boxChrome wide, so a
// caller can place two boxes side by side without measuring anything itself.
func (t Theme) Box(content []string, innerW, innerH int, focused bool) []string {
	colour, border := t.Dim, lipgloss.RoundedBorder()
	if focused {
		colour, border = t.Accent, lipgloss.ThickBorder()
	}

	body := make([]string, innerH)
	for i := range body {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		body[i] = padDisplay(fit(line, innerW), innerW)
	}

	rendered := lipgloss.NewStyle().
		Border(border).
		BorderForeground(colour).
		Padding(0, 1).
		Render(strings.Join(body, "\n"))

	return strings.Split(rendered, "\n")
}

// sideValue is one editable field: a label, and its value between chevrons. The
// chevrons say the value steps sideways, and only light up on the field the arrow
// keys would change. The value arrives already styled so a field can put a level
// badge where its value goes; restyling here would flatten it back into text.
func (t Theme) sideValue(label, value string, focused bool, width int) string {
	name := t.dim().Render(padRight(label, 8))
	chevron := t.dim()
	if focused {
		name = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(padRight(label, 8))
		chevron = lipgloss.NewStyle().Foreground(t.Accent)
	}
	return fit(name+chevron.Render("‹ ")+value+chevron.Render(" ›"), width)
}

// sideText styles a field value that is plain words rather than a badge.
func (t Theme) sideText(s string, focused bool) string {
	style := lipgloss.NewStyle().Foreground(t.Text)
	if focused {
		style = style.Bold(true)
	}
	return style.Render(s)
}
