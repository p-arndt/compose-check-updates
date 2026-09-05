package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// None of the renderers below append a trailing newline: the model joins the
// panes, so emitting one here would silently double-space the view.

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
