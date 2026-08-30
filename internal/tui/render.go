package tui

import (
	"fmt"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// None of the renderers below append a trailing newline: the model joins the
// panes, so emitting one here would silently double-space the view.

// Title renders the top bar spanning the full width.
func (t Theme) Title(width int) string {
	w := clampWidth(width)
	name := truncatePlain(" compose-check-updates ", w)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("235")).
		Background(t.Accent).
		Bold(true).
		Width(w).
		Render(name)
}

// FileHeader groups rows under their compose file. The path is truncated from
// the left because the tail (the file name) identifies it, not the mount point.
func (t Theme) FileHeader(path string, shown, total, width int) string {
	w := clampWidth(width)
	count := fmt.Sprintf(" (%d of %d)", shown, total)

	budget := w - len([]rune(count))
	if budget < 4 {
		budget = 4
	}
	line := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(truncateLeft(path, budget)) +
		lipgloss.NewStyle().Foreground(t.Dim).Render(count)
	return fit(line, w)
}

// truncateLeft drops leading runes, keeping the informative end of a path.
func truncateLeft(s string, w int) string {
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
	return "…" + string(r[len(r)-(w-1):])
}

// Row layout: "▸ [x] BADGE    image  1.2.3 → 1.2.9". The fixed columns are
// budgeted first and the image name absorbs whatever is left, so a long
// registry reference truncates instead of wrapping the list out of alignment.
const rowFixed = 2 + 3 + 1 + badgeWidth + 1 // marker + checkbox + gap + badge + gap

// RowLine renders one update as a single line no wider than width.
func (t Theme) RowLine(r Row, cursor bool, width int) string {
	w := clampWidth(width)

	marker := "  "
	if cursor {
		marker = "▸ "
	}

	box := "[ ]"
	if r.Selected {
		box = "[x]"
	}
	if r.NoTarget {
		box = "[-]" // not selectable: no release at the current target
	}
	if r.Update.IsUnreadable() {
		box = "[!]" // not selectable either, but for a reason the user can act on
	}
	switch r.State {
	case RowApplied:
		box = " ✓ "
	case RowFailed:
		box = " ✗ "
	}

	// Failed rows replace the version delta with the reason: the only place the
	// user can see why an apply did not take.
	tailPlain := rowTailPlain(r)
	if r.State == RowFailed && r.Err != nil {
		tailPlain = r.Err.Error()
	}

	namePlain := r.Update.FullImageName
	if namePlain == "" {
		namePlain = r.Update.ImageName
	}

	remaining := w - rowFixed
	if remaining < 1 {
		remaining = 1
	}
	nameBudget, tailBudget := remaining, 0
	if remaining >= 12 && tailPlain != "" {
		tailBudget = min(len([]rune(tailPlain)), remaining-8)
		nameBudget = remaining - tailBudget - 1
	}

	nameStyle := lipgloss.NewStyle().Foreground(t.Text)
	boxStyle := lipgloss.NewStyle().Foreground(t.Accent)
	if r.Update.IsUnreadable() {
		// Kept legible rather than dimmed away with the rows that merely have
		// nothing at this target: this one is asking to be dealt with.
		nameStyle = lipgloss.NewStyle().Foreground(t.Text)
		boxStyle = lipgloss.NewStyle().Foreground(t.Unreadable)
	}
	if r.NoTarget {
		// Nothing to apply at the current target, so the row reads as unavailable
		// rather than as an update the user forgot to tick.
		nameStyle = lipgloss.NewStyle().Foreground(t.Dim)
		boxStyle = lipgloss.NewStyle().Foreground(t.Dim)
	}
	switch r.State {
	case RowApplied:
		nameStyle = lipgloss.NewStyle().Foreground(t.Dim).Strikethrough(true)
		boxStyle = lipgloss.NewStyle().Foreground(t.Success)
	case RowFailed:
		nameStyle = lipgloss.NewStyle().Foreground(t.Dim)
		boxStyle = lipgloss.NewStyle().Foreground(t.Error)
	}

	name := truncatePlain(namePlain, nameBudget)
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(marker))
	b.WriteString(boxStyle.Render(box))
	b.WriteByte(' ')
	b.WriteString(t.Badge(r.Level))
	b.WriteByte(' ')
	if tailBudget > 0 {
		b.WriteString(nameStyle.Render(padRight(name, nameBudget)))
		b.WriteByte(' ')
		b.WriteString(t.rowTail(r, tailPlain, tailBudget))
	} else {
		b.WriteString(nameStyle.Render(name))
	}

	line := fit(b.String(), w)
	if cursor {
		// Background only — padding to the full width would emit trailing
		// spaces that show up as stray blanks when colour is unavailable.
		line = lipgloss.NewStyle().Background(t.Highlight).Render(line)
	}
	return line
}

// rowTailPlain is the unstyled right-hand column, used for width budgeting and
// as the single definition of what that column says.
func rowTailPlain(r Row) string {
	// The reason rather than the sentence: this column is one line wide, and the
	// sentence is what the detail pane and the sidebar are for.
	if r.Update.IsUnreadable() {
		return "unreadable · " + r.Update.UnreadableReason + pinMarker(r)
	}
	if r.NoTarget {
		return "no " + targetLabel(r.Target) + " update" + pinMarker(r)
	}
	s := plainDelta(rowDelta(r))
	if n := r.otherTargets(); n > 0 {
		// Without this, a row pointing at 2.9.4 looks like the only version `T`
		// could offer.
		s += fmt.Sprintf(" (+%d)", n)
	}
	return s + pinMarker(r)
}

// pinMarker is how a saved cap shows on the row. It spells the level out: which
// level was saved is the whole point of the pin.
func pinMarker(r Row) string {
	if r.Pin == "" {
		return ""
	}
	return fmt.Sprintf(" [pin %s]", r.Pin)
}

// rowTail is the right-hand column: the version delta, or the error on a row
// whose apply failed.
func (t Theme) rowTail(r Row, tailPlain string, budget int) string {
	if r.State == RowFailed && r.Err != nil {
		return lipgloss.NewStyle().Foreground(t.Error).Render(truncatePlain(tailPlain, budget))
	}
	if r.Update.IsUnreadable() {
		return lipgloss.NewStyle().Foreground(t.Unreadable).Render(truncatePlain(tailPlain, budget))
	}
	if r.NoTarget {
		return lipgloss.NewStyle().Foreground(t.Dim).Italic(true).Render(truncatePlain(tailPlain, budget))
	}

	current, latest := rowDelta(r)
	full := t.VersionDelta(current, latest, r.Level)
	if n := r.otherTargets(); n > 0 {
		full += lipgloss.NewStyle().Foreground(t.Dim).Render(fmt.Sprintf(" (+%d)", n))
	}
	if p := pinMarker(r); p != "" {
		full += lipgloss.NewStyle().Foreground(t.Accent).Render(p)
	}
	if lipgloss.Width(full) <= budget {
		return full
	}
	// Too narrow for both versions: show where we are going, not where we were.
	return lipgloss.NewStyle().
		Foreground(t.LevelColor(r.Level)).
		Render(truncatePlain(latest, budget))
}

// rowDelta is what the version column compares. A pin keeps its tag — that is
// what makes it a pin — so the digest it resolved to stands in for the new
// version; without it the column would read "latest → latest" and say nothing.
func rowDelta(r Row) (current, latest string) {
	if r.Level == policy.LevelPin && r.Update.LatestDigest != "" {
		return r.Update.CurrentTag, "@" + shortDigest(r.Update.LatestDigest)
	}
	return r.Update.CurrentTag, r.Update.LatestTag
}

// plainDelta is the unstyled form of VersionDelta, used for width budgeting.
func plainDelta(current, latest string) string {
	switch {
	case latest == "":
		return current
	case current == "":
		return latest
	default:
		return current + " → " + latest
	}
}

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
	markStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	attrStyle := lipgloss.NewStyle().Foreground(t.Dim)

	var out []string
	for i, l := range wrapPlain(fmt.Sprintf("%d. %s", index, msg), w-len([]rune(marker))) {
		prefix := marker
		if i > 0 {
			prefix = "  "
		}
		out = append(out, fit(markStyle.Render(prefix)+msgStyle.Render(l), w))
	}
	for _, a := range attrs {
		for _, l := range wrapPlain(a, w-len([]rune(attrIndent))) {
			out = append(out, fit(attrIndent+attrStyle.Render(l), w))
		}
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

	keyStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(t.Dim)
	sep := descStyle.Render("  ")
	render := func(h [2]string) string { return keyStyle.Render(h[0]) + descStyle.Render(" "+h[1]) }
	cost := func(h [2]string) int { return len([]rune(h[0])) + 1 + len([]rune(h[1])) }

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

// helpGutter is the blank column between the two halves of the help dialog.
const helpGutter = 3

// helpMinColumn is the narrowest a column may get before another one stops
// being worth it: below this the descriptions truncate faster than the extra
// column saves lines.
const helpMinColumn = 28

// helpSection draws one group: a heading with a rule running out to the column
// edge, so the groups read as groups, then its entries in two aligned columns.
func (t Theme) helpSection(s HelpSection, keyW, width int) []string {
	head := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(s.Title)
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
