package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// GroupInfo is everything a collapsible header shows, at any depth of the tree.
// Selected counts every row beneath the node, hidden ones included: collapsing
// must never make a pending selection invisible.
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

// groupInfo gathers the counts for one tree node's header. Directories aggregate
// over their subtree and files over their own rows — the same walk either way.
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

	// From m.rows rather than subtreeRows, which already honours the filter: Total
	// has to see the hidden rows so the header can say "2 of 7 updates".
	files := make(map[string]struct{})
	for _, f := range m.subtreeFiles(nodeIdx) {
		files[f] = struct{}{}
	}
	for _, r := range m.rows {
		if _, ok := files[r.FilePath()]; !ok {
			continue
		}
		// Both counts go through the same predicates the list itself uses, so the
		// header cannot claim rows the filter — or the floating switch — removed.
		if !m.rowEligible(r) {
			continue
		}
		g.Total++
		if m.rowVisible(r) {
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
// with ▾ when the group is expanded. Truncated from the left, because the tail
// identifies the node.
func (t Theme) GroupHeader(g GroupInfo, width int) string {
	w := clampWidth(width)

	arrow := "▾"
	if g.Collapsed {
		arrow = "▸"
	}

	indent := strings.Repeat("  ", max(g.Depth, 0))
	count := fmt.Sprintf("  (%s, %d selected)", groupCountText(g.Shown, g.Total), g.Selected)

	// A trailing slash marks a directory. The colour below says the same, but this
	// is the cue that survives a monochrome terminal or a copied-out screenshot.
	label := g.Label
	if g.IsDir {
		label += "/"
	}

	// Directories carry the plain text colour and files the accent, so the two are
	// distinguishable without relying on the indent. Both stay bold: they are headers.
	labelStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	if g.IsDir {
		labelStyle = lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	}
	// The arrow occupies the slot a row uses for its cursor marker, so the cursor
	// is carried by the underline and the highlight background instead.
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
