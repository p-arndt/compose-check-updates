package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// helpGutter is the blank column between the two halves of the help dialog.
const helpGutter = 3

// helpMinColumn is the narrowest a column may get before another one stops
// being worth it: below this the descriptions truncate faster than the extra
// column saves lines.
const helpMinColumn = 28

// helpSection draws one group: a heading with a rule running out to the column
// edge, so the groups read as groups, then its entries in two aligned columns.
func (t Theme) helpSection(s HelpSection, keyW, width int) []string {
	head := t.accent().Render(s.Title)
	if n := width - len([]rune(s.Title)) - 1; n > 0 {
		head += " " + t.dim().Render(strings.Repeat("─", n))
	}

	keyStyle := lipgloss.NewStyle().Foreground(t.Major).Bold(true)
	lines := []string{fit(head, width)}
	for _, e := range s.Entries {
		k := keyStyle.Render(padRight(truncatePlain(e.Keys, keyW), keyW))
		desc := lipgloss.NewStyle().Foreground(t.Text).
			Render(truncatePlain(e.Desc, max(width-keyW-1, 0)))
		lines = append(lines, fit(k+" "+desc, width))
	}
	return lines
}

// helpKeyWidth is the key column for a set of sections: as wide as the widest
// key it has to hold, capped so one long chord cannot push every description
// off the right edge.
func helpKeyWidth(sections []HelpSection, width int) int {
	w := 0
	for _, s := range sections {
		for _, e := range s.Entries {
			w = max(w, len([]rune(e.Keys)))
		}
	}
	return min(w, max(width/2, 1))
}

// helpColumn stacks sections, with a blank line between them unless the dialog
// is too short to afford one.
func (t Theme) helpColumn(sections []HelpSection, width int, spaced bool) []string {
	keyW := helpKeyWidth(sections, width)
	var out []string
	for i, s := range sections {
		if i > 0 && spaced {
			out = append(out, "")
		}
		out = append(out, t.helpSection(s, keyW, width)...)
	}
	return out
}

// helpDistribute splits the sections across n columns, keeping their order and
// making the tallest column as short as it can be. Splitting by line count
// rather than by section count matters because the groups differ wildly in size
// and the dialog is as tall as its tallest column.
func helpDistribute(sections []HelpSection, n int) [][]HelpSection {
	n = min(max(n, 1), len(sections))

	height := func(i, j int) int {
		h := 0
		for ; i < j; i++ {
			h += len(sections[i].Entries) + 2
		}
		return h
	}

	// Exhaustive over the split points: affordable at this size, and exact where
	// a greedy pass overfills the first columns.
	type state struct{ from, cols int }
	tallest, cutAt := map[state]int{}, map[state]int{}
	var best func(from, cols int) int
	best = func(from, cols int) int {
		if cols == 1 {
			return height(from, len(sections))
		}
		if h, ok := tallest[state{from, cols}]; ok {
			return h
		}
		h, cut := -1, from+1
		for j := from + 1; j <= len(sections)-(cols-1); j++ {
			if v := max(height(from, j), best(j, cols-1)); h < 0 || v < h {
				h, cut = v, j
			}
		}
		tallest[state{from, cols}], cutAt[state{from, cols}] = h, cut
		return h
	}
	best(0, n)

	out := make([][]HelpSection, 0, n)
	from := 0
	for cols := n; cols > 1; cols-- {
		cut := cutAt[state{from, cols}]
		out = append(out, sections[from:cut])
		from = cut
	}
	return append(out, sections[from:])
}

// helpHeight is how many lines a set of columns needs: the tallest of them. The
// blank line between groups is optional, so it is answerable both ways.
func helpHeight(cols [][]HelpSection, spaced bool) int {
	gap := 0
	if spaced {
		gap = 1
	}
	h := 0
	for _, c := range cols {
		n := 0
		for _, s := range c {
			n += len(s.Entries) + 1 + gap
		}
		h = max(h, n-gap) // nothing follows the last section
	}
	return h
}

// HelpPanel lays the sections out for the `?` dialog, taking the roomiest layout
// that fits: fewest columns first, and the blank line between groups given up
// only once columns have run out. Nothing may fall off the bottom.
func (t Theme) HelpPanel(sections []HelpSection, width, maxHeight int) []string {
	if len(sections) == 0 {
		return nil
	}

	widest := min(max((width+helpGutter)/(helpMinColumn+helpGutter), 1), len(sections))
	cols, spaced := helpDistribute(sections, widest), false
	for _, try := range []bool{true, false} {
		fits := false
		for n := 1; n <= widest; n++ {
			cols, spaced = helpDistribute(sections, n), try
			if helpHeight(cols, try) <= maxHeight {
				fits = true
				break
			}
		}
		if fits {
			break
		}
	}

	if len(cols) == 1 {
		return t.helpColumn(cols[0], width, spaced)
	}

	colW := (width - helpGutter*(len(cols)-1)) / len(cols)
	rendered := make([][]string, len(cols))
	height := 0
	for i, c := range cols {
		rendered[i] = t.helpColumn(c, colW, spaced)
		height = max(height, len(rendered[i]))
	}

	gap := strings.Repeat(" ", helpGutter)
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		parts := make([]string, 0, len(rendered))
		for _, c := range rendered {
			line := ""
			if i < len(c) {
				line = c[i]
			}
			parts = append(parts, padDisplay(line, colW))
		}
		out = append(out, strings.TrimRight(strings.Join(parts, gap), " "))
	}
	return out
}

// Help is the key hint footer. Hints are dropped from the right as the terminal
// narrows rather than truncated mid-word.
func (t Theme) Help(bindings []key.Binding, width int) string {
	w := clampWidth(width)

	// Derived from the bindings themselves, so a rebound key cannot leave the
	// footer advertising one that no longer does anything.
	var hints [][2]string
	for _, b := range bindings {
		h := b.Help()
		if h.Key == "" {
			continue
		}
		hints = append(hints, [2]string{h.Key, h.Desc})
	}

	keyStyle := t.accent()
	descStyle := t.dim()
	sep := descStyle.Render("  ")
	render := func(h [2]string) string { return keyStyle.Render(h[0]) + descStyle.Render(" "+h[1]) }
	cost := func(h [2]string) int { return utf8.RuneCountInString(h[0]) + 1 + utf8.RuneCountInString(h[1]) }

	if len(hints) == 0 {
		return ""
	}

	// The caller puts the way out last and it is budgeted first, so a narrow
	// terminal drops middle hints rather than the key that says how to leave.
	last := hints[len(hints)-1]
	reserved := cost(last)

	var parts []string
	used := reserved
	for _, h := range hints[:len(hints)-1] {
		c := cost(h) + 2
		if used+c > w {
			break
		}
		used += c
		parts = append(parts, render(h))
	}
	parts = append(parts, render(last))
	return fit(strings.Join(parts, sep), w)
}
