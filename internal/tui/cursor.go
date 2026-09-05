package tui

func (m *Model) clampCursor() {
	if len(m.entries) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	m.cursor = min(max(m.cursor, 0), len(m.entries)-1)
}

// currentEntry is the list line under the cursor.
func (m Model) currentEntry() (entry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return entry{}, false
	}
	return m.entries[m.cursor], true
}

// currentRow is the highlighted row, or nil when the cursor sits on a file
// header — which is what makes every per-row key a no-op there.
func (m Model) currentRow() *Row {
	e, ok := m.currentEntry()
	if !ok || e.kind != entryRow {
		return nil
	}
	return &m.rows[e.row]
}

// cursorGroup is the key of the node the cursor is in, whether it sits on that
// node's header or on a row inside the file it stands for.
func (m Model) cursorGroup() string {
	n := m.cursorNode()
	if n < 0 {
		return ""
	}
	return m.nodes[n].key
}

// toggleGroup folds or unfolds one node of the tree, at any depth. Rebuilding
// on the current cursor key is what moves the cursor up onto the header when
// the row it was sitting on has just been folded away.
func (m *Model) toggleGroup(key string) {
	if key == "" {
		return
	}
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}
	m.collapsed[key] = !m.collapsed[key]
	m.rebuild(m.cursorKey())
	m.syncScroll()
}

// setAllCollapsed folds or unfolds every node at every depth at once, directory
// levels included, so collapsing all leaves just the roots.
func (m *Model) setAllCollapsed(v bool) {
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}
	for _, n := range m.nodes {
		m.collapsed[n.key] = v
	}
	m.rebuild(m.cursorKey())
	m.syncScroll()
}

func (m *Model) moveCursor(delta int) {
	if len(m.entries) == 0 {
		return
	}
	m.cursor += delta
	m.clampCursor()
	m.syncScroll()
}

// displayIndex maps a visible-row index to its line in the rendered list, or -1
// when the row's group is collapsed and it is not on screen at all.
func (m Model) displayIndex(vi int) int {
	if vi < 0 || vi >= len(m.visible) {
		return -1
	}
	ri := m.visible[vi]
	for i, e := range m.entries {
		if e.kind == entryRow && e.row == ri {
			return i
		}
	}
	return -1
}

// displayCount is how many lines the list renders. Since headers became entries
// this is simply their count — no header arithmetic on top of the row count.
func (m Model) displayCount() int { return len(m.entries) }

// syncScroll nudges the window just far enough to keep the cursor on screen,
// rather than recentring, so paging feels like a terminal pager.
func (m *Model) syncScroll() {
	h := m.listHeight()
	total := len(m.entries)

	if total <= h {
		m.offset = 0
		return
	}

	ci := min(max(m.cursor, 0), total-1)

	// The file header above the cursor row should stay visible together with it,
	// so a row never appears detached from the file it belongs to.
	top := ci
	if ci > 0 && m.entries[ci].kind == entryRow && m.entries[ci-1].kind == entryHeader {
		top = ci - 1
	}

	if top < m.offset {
		m.offset = top
	}
	if ci >= m.offset+h {
		m.offset = ci - h + 1
	}
	m.offset = max(min(m.offset, total-h), 0)
}

// moveIssueCursor walks the issues pane by whole issues, not by wrapped lines.
func (m *Model) moveIssueCursor(delta int) {
	m.issueCursor += delta
	m.syncIssueScroll() // clamps the cursor as well
}

// syncIssueScroll keeps the highlighted issue on screen, pinning its first line
// to the top when the entry alone is taller than the pane.
func (m *Model) syncIssueScroll() {
	if len(m.scanErrs) == 0 {
		m.issueCursor, m.issueOffset = 0, 0
		return
	}
	m.issueCursor = min(max(m.issueCursor, 0), len(m.scanErrs)-1)

	starts, total := m.issueOffsets()
	h := m.listHeight()
	if total <= h {
		m.issueOffset = 0
		return
	}

	top := starts[m.issueCursor]
	bottom := total
	if m.issueCursor+1 < len(starts) {
		bottom = starts[m.issueCursor+1]
	}

	if top < m.issueOffset {
		m.issueOffset = top
	}
	if bottom > m.issueOffset+h {
		m.issueOffset = bottom - h
	}
	m.issueOffset = max(min(m.issueOffset, top, total-h), 0)
}
