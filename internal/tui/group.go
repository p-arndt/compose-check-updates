package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// GroupInfo is everything a collapsible header shows, at any depth of the
// directory tree. Selected is counted over every row beneath the node,
// including ones this header is currently hiding: collapsing must never make a
// pending selection invisible, or the user would apply more than they can see.
type GroupInfo struct {
	Path      string // the node key — full path prefix, used for identity only
	Label     string // what the header prints: the node's compressed segment(s)
	Depth     int    // 0 for a root; each level indents by two spaces
	IsDir     bool   // false when the node is a compose file owning rows directly
	Shown     int    // rows passing the current filter — what the group holds
	Total     int    // rows beneath the node regardless of the filter
	Selected  int
	Collapsed bool
	Cursor    bool
}

// groupInfo gathers the counts for one tree node's header. Directories
// aggregate over their whole subtree and files over their own rows, which is
// the same walk either way: a file node's subtree is just itself.
func (m Model) groupInfo(nodeIdx int, cursor bool) GroupInfo {
	if nodeIdx < 0 || nodeIdx >= len(m.nodes) {
		return GroupInfo{Cursor: cursor}
	}
	n := m.nodes[nodeIdx]
	g := GroupInfo{
		Path:      n.key,
		Label:     n.label,
		Depth:     n.depth,
		IsDir:     !n.isFile,
		Collapsed: m.collapsed[n.key],
		Cursor:    cursor,
	}

	// The counts come from m.rows filtered by the subtree's files rather than
	// from subtreeRows: that helper already honours the filter, and Total has to
	// see the rows the filter hides so the header can say "2 of 7 updates".
	files := make(map[string]struct{})
	for _, f := range m.subtreeFiles(nodeIdx) {
		files[f] = struct{}{}
	}
	for _, r := range m.rows {
		if _, ok := files[r.FilePath()]; !ok {
			continue
		}
		g.Total++
		if m.filter.Matches(r.Level) {
			g.Shown++
		}
		if r.Selected {
			g.Selected++
		}
	}
	return g
}

// GroupHeader renders one tree node as a single line, indented two spaces per
// level so the nesting is readable without box drawing, e.g.
//
//	▾ tests/                        (7 updates, 1 selected)
//	  ▸ folder1/compose.yaml        (2 updates, 1 selected)
//
// with ▾ when the group is expanded. Like every renderer it emits no trailing
// newline and never exceeds width. The label is truncated from the left because
// the tail identifies the node, not the segments leading up to it.
func (t Theme) GroupHeader(g GroupInfo, width int) string {
	w := clampWidth(width)

	arrow := "▾"
	if g.Collapsed {
		arrow = "▸"
	}

	indent := strings.Repeat("  ", max(g.Depth, 0))
	count := fmt.Sprintf("  (%s, %d selected)", groupCountText(g.Shown, g.Total), g.Selected)

	// A directory is spelled with a trailing slash. The colour below says the same
	// thing, but this is the only cue that survives a monochrome terminal or a
	// copied-out screenshot, and telling a directory from a compose file is the
	// whole point of the two being different rows.
	label := g.Label
	if g.IsDir {
		label += "/"
	}

	// Directories carry the plain text colour and files the accent, so the eye
	// can tell a container apart from the compose file that actually owns rows
	// without relying on the indent alone. Both stay bold: they are headers.
	labelStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	if g.IsDir {
		labelStyle = lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	}
	// The arrow occupies the slot a row uses for its cursor marker, so the
	// cursor is carried by the underline (visible without colour) and the
	// highlight background instead of a second marker glyph.
	if g.Cursor {
		labelStyle = labelStyle.Underline(true)
	}

	// The indent eats into the label's budget rather than the count's, so a deep
	// node loses characters off its front instead of pushing the line past width.
	budget := w - len([]rune(indent)) - 2 - len([]rune(count))
	if budget < 4 {
		budget = 4
	}

	line := fit(indent+labelStyle.Render(arrow+" "+truncateLeft(label, budget))+
		lipgloss.NewStyle().Foreground(t.Dim).Render(count), w)
	if g.Cursor {
		line = lipgloss.NewStyle().Background(t.Highlight).Render(line)
	}
	return line
}

// groupCountText spells out the update count, mentioning the filtered-away rows
// only when there are any — the common case reads "3 updates", not "3 of 3".
func groupCountText(shown, total int) string {
	if shown != total {
		return fmt.Sprintf("%d of %d updates", shown, total)
	}
	if total == 1 {
		return "1 update"
	}
	return fmt.Sprintf("%d updates", total)
}
