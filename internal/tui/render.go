package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// None of the renderers below append a trailing newline: the model joins the
// panes, so emitting one here would silently double-space the view.

// Detail is the pane under the list describing the highlighted row. Digest
// lines are omitted entirely when the image is not digest-pinned, so an
// ordinary tag update does not show two empty fields.
func (t Theme) Detail(u check.Update, level policy.Level, width int) string {
	w := clampWidth(width)

	type field struct{ label, value string }
	fields := []field{
		{"image", u.FullImageName},
		{"name", u.ImageName},
	}
	// A pin keeps its tag, so there is no delta to show: "latest → latest" is the
	// no-op line the list column already goes out of its way to avoid, and the
	// digest fields below say what actually changes.
	if level == policy.LevelPin {
		fields = append(fields, field{"tag", u.CurrentTag})
	} else if u.CurrentTag != "" || u.LatestTag != "" {
		fields = append(fields, field{"version", plainDelta(u.CurrentTag, u.LatestTag)})
	}
	if u.CurrentDigest != "" {
		fields = append(fields, field{"digest", shortDigest(u.CurrentDigest)})
	}
	if u.LatestDigest != "" {
		fields = append(fields, field{"new digest", shortDigest(u.LatestDigest)})
	}
	// Both halves of it: the reason is what a bug report quotes, the sentence is
	// what tells the user what to do next.
	if u.IsUnreadable() {
		fields = append(fields,
			field{"unreadable", u.UnreadableReason},
			field{"why", u.UnreadableMessage},
		)
	}
	fields = append(fields,
		field{"file", u.FilePath},
		field{"line", strings.TrimSpace(u.RawLine)},
	)

	const labelWidth = 11
	labelStyle := t.dim()
	valueStyle := lipgloss.NewStyle().Foreground(t.Text)
	valueBudget := max(w-labelWidth, 1)

	lines := make([]string, 0, len(fields)+1)
	lines = append(lines, t.Badge(level)+" "+
		lipgloss.NewStyle().Foreground(t.LevelColor(level)).Bold(true).
			Render(truncatePlain(u.ImageName, max(w-badgeWidth-1, 1))))
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		value := f.value
		if f.label == "version" {
			// Re-render through VersionDelta so the detail pane highlights the
			// same segments the list does.
			styled := t.VersionDelta(u.CurrentTag, u.LatestTag, level)
			if lipgloss.Width(styled) <= valueBudget {
				lines = append(lines, labelStyle.Render(padRight(f.label, labelWidth))+styled)
				continue
			}
		}
		lines = append(lines, labelStyle.Render(padRight(f.label, labelWidth))+
			valueStyle.Render(truncatePlain(value, valueBudget)))
	}

	for i, l := range lines {
		lines[i] = fit(l, w)
	}
	return strings.Join(lines, "\n")
}

// IssueEntry renders one captured scan issue: the message, then one line per
// attribute, so an ellipsis never eats the image and file it is about. Returns a
// slice rather than a joined string because the pane scrolls by line.
func (t Theme) IssueEntry(index int, msg string, attrs []string, cursor bool, width int) []string {
	w := clampWidth(width)

	marker := "  "
	if cursor {
		marker = "▸ "
	}
	const attrIndent = "    "

	msgStyle := lipgloss.NewStyle().Foreground(t.Error)
	if cursor {
		msgStyle = msgStyle.Bold(true)
	}
	markStyle := t.accent()
	attrStyle := t.dim()

	var out []string
	for i, l := range wrapPlain(fmt.Sprintf("%d. %s", index, msg), w-utf8.RuneCountInString(marker)) {
		prefix := marker
		if i > 0 {
			prefix = "  "
		}
		out = append(out, fit(markStyle.Render(prefix)+msgStyle.Render(l), w))
	}
	for _, a := range attrs {
		for _, l := range wrapPlain(a, w-len(attrIndent)) {
			out = append(out, fit(attrIndent+attrStyle.Render(l), w))
		}
	}

	if cursor {
		bg := t.highlight()
		for i := range out {
			out[i] = bg.Render(out[i])
		}
	}
	return out
}

// wrapPlain breaks unstyled text into lines of at most w runes, preferring word
// boundaries and cutting words that are longer than the whole width. Apply it
// before styling, for the same reason truncatePlain must be.
func wrapPlain(s string, w int) []string {
	w = max(w, 1)

	var out []string
	line := ""
	flush := func() {
		out = append(out, line)
		line = ""
	}
	for word := range strings.FieldsSeq(s) {
		switch {
		case line == "":
			line = word
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= w:
			line += " " + word
		default:
			flush()
			line = word
		}
		// A word wider than the pane still has to go somewhere.
		for utf8.RuneCountInString(line) > w {
			r := []rune(line)
			out = append(out, string(r[:w]))
			line = string(r[w:])
		}
	}
	if line != "" || len(out) == 0 {
		flush()
	}
	return out
}

// Status renders a one-line message; the symbol carries the meaning when the
// terminal has no colour.
func (t Theme) Status(kind StatusKind, text string) string {
	symbol, colour := "•", t.Dim
	switch kind {
	case StatusSuccess:
		symbol, colour = "✓", t.Success
	case StatusWarn:
		symbol, colour = "!", t.Warn
	case StatusError:
		symbol, colour = "✗", t.Error
	}
	return lipgloss.NewStyle().Foreground(colour).Render(symbol + " " + text)
}

// Empty centres a placeholder for the "nothing found" and "filter matches
// nothing" states.
func (t Theme) Empty(text string, width int) string {
	w := clampWidth(width)
	plain := truncatePlain(text, w)
	body := t.dim().Italic(true).Render(plain)
	// Left padding only — lipgloss.PlaceHorizontal would also pad the right,
	// leaving trailing blanks on every empty-state line.
	return fit(strings.Repeat(" ", max((w-utf8.RuneCountInString(plain))/2, 0))+body, w)
}
