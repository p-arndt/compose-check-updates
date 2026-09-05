package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

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

	budget := max(w-utf8.RuneCountInString(count), 4)
	return fit(t.accent().Render(truncateLeft(path, budget))+t.dim().Render(count), w)
}

// truncateLeft drops leading runes, keeping the informative end of a path.
func truncateLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	r := []rune(s)
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
	// user can see why an apply did not take. The alternative-level count is
	// taken once here: both halves of the tail need it and it walks the tags.
	other := r.otherTargets()
	tailPlain := rowTailPlain(r, other)
	if r.State == RowFailed && r.Err != nil {
		tailPlain = r.Err.Error()
	}

	namePlain := r.Update.FullImageName
	if namePlain == "" {
		namePlain = r.Update.ImageName
	}

	remaining := max(w-rowFixed, 1)
	nameBudget, tailBudget := remaining, 0
	if remaining >= 12 && tailPlain != "" {
		tailBudget = min(utf8.RuneCountInString(tailPlain), remaining-8)
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
		nameStyle = t.dim()
		boxStyle = t.dim()
	}
	switch r.State {
	case RowApplied:
		nameStyle = t.dim().Strikethrough(true)
		boxStyle = lipgloss.NewStyle().Foreground(t.Success)
	case RowFailed:
		nameStyle = t.dim()
		boxStyle = lipgloss.NewStyle().Foreground(t.Error)
	}

	name := truncatePlain(namePlain, nameBudget)
	var b strings.Builder
	b.WriteString(t.accent().Render(marker))
	b.WriteString(boxStyle.Render(box))
	b.WriteByte(' ')
	b.WriteString(t.Badge(r.Level))
	b.WriteByte(' ')
	if tailBudget > 0 {
		b.WriteString(nameStyle.Render(padRight(name, nameBudget)))
		b.WriteByte(' ')
		b.WriteString(t.rowTail(r, tailPlain, other, tailBudget))
	} else {
		b.WriteString(nameStyle.Render(name))
	}

	line := fit(b.String(), w)
	if cursor {
		// Background only — padding to the full width would emit trailing
		// spaces that show up as stray blanks when colour is unavailable.
		line = t.highlight().Render(line)
	}
	return line
}

// rowTailPlain is the unstyled right-hand column, used for width budgeting and
// as the single definition of what that column says.
func rowTailPlain(r Row, other int) string {
	// The reason rather than the sentence: this column is one line wide, and the
	// sentence is what the detail pane and the sidebar are for.
	if r.Update.IsUnreadable() {
		return "unreadable · " + r.Update.UnreadableReason + pinMarker(r)
	}
	if r.NoTarget {
		return "no " + targetLabel(r.Target) + " update" + pinMarker(r)
	}
	s := plainDelta(rowDelta(r))
	if other > 0 {
		// Without this, a row pointing at 2.9.4 looks like the only version `T`
		// could offer.
		s += fmt.Sprintf(" (+%d)", other)
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
func (t Theme) rowTail(r Row, tailPlain string, other, budget int) string {
	if r.State == RowFailed && r.Err != nil {
		return lipgloss.NewStyle().Foreground(t.Error).Render(truncatePlain(tailPlain, budget))
	}
	if r.Update.IsUnreadable() {
		return lipgloss.NewStyle().Foreground(t.Unreadable).Render(truncatePlain(tailPlain, budget))
	}
	if r.NoTarget {
		return t.dim().Italic(true).Render(truncatePlain(tailPlain, budget))
	}

	current, latest := rowDelta(r)
	full := t.VersionDelta(current, latest, r.Level)
	if other > 0 {
		full += t.dim().Render(fmt.Sprintf(" (+%d)", other))
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
