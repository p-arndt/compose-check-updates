package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// GroupInfo is everything a collapsible file header shows. Selected is counted
// over every row of the file, including ones this header is currently hiding:
// collapsing must never make a pending selection invisible, or the user would
// apply more than they can see.
type GroupInfo struct {
	Path      string
	Shown     int // rows passing the current filter — what the group holds
	Total     int // rows in the file regardless of the filter
	Selected  int
	Collapsed bool
	Cursor    bool
}

// groupInfo gathers the counts for one compose file's header.
func (m Model) groupInfo(path string, cursor bool) GroupInfo {
	g := GroupInfo{Path: path, Collapsed: m.collapsed[path], Cursor: cursor}
	for _, r := range m.rows {
		if r.FilePath() != path {
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

// GroupHeader renders one file group as a single line, e.g.
//
//	▸ tests/folder1/compose.yaml  (2 updates, 1 selected)
//
// with ▾ when the group is expanded. Like every renderer it emits no trailing
// newline and never exceeds width. The path is truncated from the left because
// the tail (the file name) identifies it, not the mount point.
func (t Theme) GroupHeader(g GroupInfo, width int) string {
	w := clampWidth(width)

	arrow := "▾"
	if g.Collapsed {
		arrow = "▸"
	}

	count := fmt.Sprintf("  (%s, %d selected)", groupCountText(g.Shown, g.Total), g.Selected)

	// The arrow occupies the slot a row uses for its cursor marker, so the
	// cursor is carried by the underline (visible without colour) and the
	// highlight background instead of a second marker glyph.
	pathStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	if g.Cursor {
		pathStyle = pathStyle.Underline(true)
	}

	budget := w - 2 - len([]rune(count))
	if budget < 4 {
		budget = 4
	}

	line := fit(pathStyle.Render(arrow+" "+truncateLeft(g.Path, budget))+
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
