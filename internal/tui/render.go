package tui

import (
	"fmt"
	"strings"

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
	labelStyle := lipgloss.NewStyle().Foreground(t.Dim)
	valueStyle := lipgloss.NewStyle().Foreground(t.Text)
	valueBudget := w - labelWidth
	if valueBudget < 1 {
		valueBudget = 1
	}

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

// issueMarkerWidth is the width of the cursor marker column. Both markers are
// two runes wide, so an entry's height does not depend on the cursor — which is
// what lets syncIssueScroll size the pane without rendering it.
const issueMarkerWidth = 2

// issueAttrIndent indents an issue's attribute lines under its message.
const issueAttrIndent = "    "

// issuePlainLines wraps one issue exactly as IssueEntry lays it out, before any
// styling: the numbered message, then one line per attribute. IssueEntry styles
// these very lines and the scroll sync merely counts them, so the two cannot
// disagree about how tall an entry is.
func issuePlainLines(index int, msg string, attrs []string, width int) (msgLines, attrLines []string) {
	w := clampWidth(width)

	msgLines = wrapPlain(fmt.Sprintf("%d. %s", index, msg), w-issueMarkerWidth)
	for _, a := range attrs {
		attrLines = append(attrLines, wrapPlain(a, w-len([]rune(issueAttrIndent)))...)
	}
	return msgLines, attrLines
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

	msgStyle := lipgloss.NewStyle().Foreground(t.Error)
	if cursor {
		msgStyle = msgStyle.Bold(true)
	}
	markStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	attrStyle := lipgloss.NewStyle().Foreground(t.Dim)

	msgLines, attrLines := issuePlainLines(index, msg, attrs, width)

	out := make([]string, 0, len(msgLines)+len(attrLines))
	for i, l := range msgLines {
		prefix := marker
		if i > 0 {
			prefix = "  "
		}
		out = append(out, fit(markStyle.Render(prefix)+msgStyle.Render(l), w))
	}
	for _, l := range attrLines {
		out = append(out, fit(issueAttrIndent+attrStyle.Render(l), w))
	}

	if cursor {
		for i := range out {
			out[i] = lipgloss.NewStyle().Background(t.Highlight).Render(out[i])
		}
	}
	return out
}

// wrapPlain breaks unstyled text into lines of at most w runes, preferring word
// boundaries and cutting words that are longer than the whole width. Apply it
// before styling, for the same reason truncatePlain must be.
func wrapPlain(s string, w int) []string {
	if w < 1 {
		w = 1
	}

	var out []string
	line := ""
	flush := func() {
		out = append(out, line)
		line = ""
	}
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= w:
			line += " " + word
		default:
			flush()
			line = word
		}
		// A word wider than the pane still has to go somewhere.
		for len([]rune(line)) > w {
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
	body := lipgloss.NewStyle().Foreground(t.Dim).Italic(true).Render(plain)
	// Left padding only — lipgloss.PlaceHorizontal would also pad the right,
	// leaving trailing blanks on every empty-state line.
	if pad := (w - len([]rune(plain))) / 2; pad > 0 {
		body = strings.Repeat(" ", pad) + body
	}
	return fit(body, w)
}
