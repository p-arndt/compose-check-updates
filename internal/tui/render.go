package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"

	"github.com/p-arndt/compose-check-updates/internal"
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
	switch r.State {
	case RowApplied:
		box = " ✓ "
	case RowFailed:
		box = " ✗ "
	}

	// Failed rows replace the version delta with the reason, which is the only
	// place the user can see why an apply did not take.
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
	if r.NoTarget {
		// Nothing to apply at the current target: dim the whole row so it reads
		// as unavailable rather than as an update the user forgot to tick.
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
	if r.NoTarget {
		return "no " + r.Target.Label() + " update"
	}
	s := plainDelta(r.Update.CurrentTag, r.Update.LatestTag)
	if n := r.otherTargets(); n > 0 {
		// The user's way of discovering that `T` would offer this image another
		// version — without it, a row pointing at 2.9.4 looks like the only option.
		s += fmt.Sprintf(" (+%d)", n)
	}
	return s
}

// rowTail is the right-hand column: the version delta, or the error on a row
// whose apply failed.
func (t Theme) rowTail(r Row, tailPlain string, budget int) string {
	if r.State == RowFailed && r.Err != nil {
		return lipgloss.NewStyle().Foreground(t.Error).Render(truncatePlain(tailPlain, budget))
	}
	if r.NoTarget {
		return lipgloss.NewStyle().Foreground(t.Dim).Italic(true).Render(truncatePlain(tailPlain, budget))
	}

	full := t.VersionDelta(r.Update.CurrentTag, r.Update.LatestTag, r.Level)
	if n := r.otherTargets(); n > 0 {
		full += lipgloss.NewStyle().Foreground(t.Dim).Render(fmt.Sprintf(" (+%d)", n))
	}
	if lipgloss.Width(full) <= budget {
		return full
	}
	// Too narrow for both versions: show where we are going, not where we were.
	return lipgloss.NewStyle().
		Foreground(t.LevelColor(r.Level)).
		Render(truncatePlain(r.Update.LatestTag, budget))
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
func (t Theme) Detail(u internal.UpdateInfo, level string, width int) string {
	w := clampWidth(width)

	type field struct{ label, value string }
	fields := []field{
		{"image", u.FullImageName},
		{"name", u.ImageName},
	}
	if u.CurrentTag != "" || u.LatestTag != "" {
		fields = append(fields, field{"version", plainDelta(u.CurrentTag, u.LatestTag)})
	}
	if u.CurrentDigest != "" {
		fields = append(fields, field{"digest", shortDigest(u.CurrentDigest)})
	}
	if u.LatestDigest != "" {
		fields = append(fields, field{"new digest", shortDigest(u.LatestDigest)})
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

// IssueEntry renders one captured scan issue as the lines it needs: the message
// first, then one line per attribute, so the image and file a warning is about
// are readable instead of being the part an ellipsis eats. Returns a slice
// rather than a joined string because the pane scrolls by line.
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

// Legend is the colour key and the state line for the two independent settings
// the list has. They are labelled rather than merely styled because they are
// easy to confuse: "show" only hides rows, "target" decides which version
// the apply keys actually write.
func (t Theme) Legend(active Filter, target Target, width int) string {
	w := clampWidth(width)
	dim := lipgloss.NewStyle().Foreground(t.Dim)

	filters := []Filter{FilterAll, FilterMajor, FilterMinor, FilterPatch, FilterDigest}
	parts := make([]string, 0, len(filters))
	for _, f := range filters {
		label := f.Label()
		style := lipgloss.NewStyle().Foreground(t.LevelColor(label))
		if f == active {
			style = style.Bold(true).Underline(true)
			label = "[" + label + "]"
		}
		parts = append(parts, style.Render(label))
	}

	line := dim.Render("show ") + strings.Join(parts, dim.Render(" · ")) +
		dim.Render("  │  target ") +
		lipgloss.NewStyle().Foreground(t.LevelColor(target.Label())).Bold(true).
			Render("["+target.Label()+"]")
	return fit(line, w)
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

	// The hints are derived from the bindings themselves rather than written out
	// here, so a rebound or removed key cannot leave the footer advertising a
	// key that no longer does anything.
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

	// The caller puts the way out last, so the final hint is budgeted first and
	// the rest fill what remains: a narrow terminal drops middle hints rather
	// than the one key telling the user how to leave.
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
