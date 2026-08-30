package tui

import (
	"sort"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// rowKey identifies a row across re-sorts and across the goroutines that apply
// updates. A compose file cannot pin the same image reference twice, so the
// file plus the full reference is unique.
func rowKey(r Row) string {
	return r.Update.FilePath + "\x00" + r.Update.FullImageName + "\x00" + r.Update.CurrentTag
}

func (m *Model) addRow(r Row) {
	// One Dockerfile can be built by several compose files — a `compose.yaml`
	// and its `compose.yml` override in the same directory, say — and each of
	// them reports it. Two rows would then share a rowKey, so rowByKey would
	// resolve both to the first: applying would write it twice and leave its
	// twin pending forever. The services of the row dropped are kept, since it
	// is the same update either way.
	if existing := m.rowByKey(rowKey(r)); existing != nil {
		for _, s := range r.Update.Services {
			existing.Update.Services = check.AppendService(existing.Update.Services, s)
		}
		return
	}

	key := m.cursorKey()

	// Rows keep arriving after the user has changed the global target, so a new
	// one is pointed at it immediately rather than showing the scanner's default.
	r.Target = m.target
	m.retarget(&r, m.target)
	r.Pin = m.capFor(r.Update.ImageName)

	m.rows = append(m.rows, r)
	// Stable ordering by file then image means a row arriving mid-scan lands in
	// its final position immediately, so nothing below it ever shifts twice.
	sort.SliceStable(m.rows, func(i, j int) bool {
		a, b := m.rows[i], m.rows[j]
		if a.Update.FilePath != b.Update.FilePath {
			return a.Update.FilePath < b.Update.FilePath
		}
		if a.Update.ImageName != b.Update.ImageName {
			return a.Update.ImageName < b.Update.ImageName
		}
		return a.Update.CurrentTag < b.Update.CurrentTag
	})

	m.rebuild(key)
}

// entryKey is the identity of one list line across re-sorts: its node key for a
// header, its row key for a row.
func (m Model) entryKey(e entry) string {
	if e.kind == entryHeader {
		return headerKeyPrefix + e.path
	}
	return rowKey(m.rows[e.row])
}

// keyGroup is the path an entry key belongs to: the node key for a header, the
// compose file for a row. It lets rebuild fall back to a header when the line
// the cursor was on has been folded away.
func keyGroup(key string) string {
	if strings.HasPrefix(key, headerKeyPrefix) {
		return key[len(headerKeyPrefix):]
	}
	path, _, _ := strings.Cut(key, "\x00")
	return path
}

// cursorKey is the identity of the entry under the cursor, or "" when the list
// is empty.
func (m Model) cursorKey() string {
	e, ok := m.currentEntry()
	if !ok {
		return ""
	}
	return m.entryKey(e)
}

// rowEligible reports whether a row is part of what the list is about at all,
// the level filter aside. A floating-tag pin is not: it is an offer to write down
// what "latest" resolves to, and until the user asks for those it may not be
// counted either — a header reading "1 of 2 updates" would send them to `f`,
// which cannot reveal it.
func (m Model) rowEligible(r Row) bool {
	if r.Level == policy.LevelPin {
		return m.showFloating
	}
	return true
}

// rowVisible is rowEligible plus the level filter, and is the single definition
// of what the list shows: every counter reads it, so a header can never disagree
// with the lines under it. The filter — which only speaks about versions — has
// nothing to say about a pin either way.
func (m Model) rowVisible(r Row) bool {
	if !m.rowEligible(r) {
		return false
	}
	if r.Level == policy.LevelPin {
		return true
	}
	// An unreadable image has no level for the filter to speak about, and hiding
	// it under every filter but "all" would put it out of reach of the very field
	// that fixes it.
	if r.Level == policy.LevelUnreadable {
		return true
	}
	return m.filter.Matches(r.Level)
}

// eligibleCount is how many rows the list is about, for the readouts that would
// otherwise say len(m.rows) and count the ones nobody can see.
func (m Model) eligibleCount() int {
	n := 0
	for _, r := range m.rows {
		if m.rowEligible(r) {
			n++
		}
	}
	return n
}

// hiddenFloatingCount is how many rows the "floating" switch is currently
// keeping out of the list, so an empty list can name the key that fills it.
func (m Model) hiddenFloatingCount() int {
	if m.showFloating {
		return 0
	}
	n := 0
	for _, r := range m.rows {
		if r.Level == policy.LevelPin {
			n++
		}
	}
	return n
}

// rebuild recomputes the visible set and the rendered entries, then restores the
// cursor, so inserting or filtering never moves it to a different image.
func (m *Model) rebuild(keepKey string) {
	m.visible = m.visible[:0]
	for i, r := range m.rows {
		if m.rowVisible(r) {
			m.visible = append(m.visible, i)
		}
	}

	// The tree is derived purely from the visible rows, so a filter that empties a
	// directory removes its headers too instead of leaving empty folds.
	paths := make([]string, 0, len(m.visible))
	seen := make(map[string]bool, len(m.visible))
	for _, ri := range m.visible {
		if p := m.rows[ri].FilePath(); !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	m.nodes, m.nodeByKey, m.nodeByFile = buildTree(paths)

	rowsByNode := make(map[int][]int, len(paths))
	for _, ri := range m.visible {
		if n := m.fileNode(m.rows[ri].FilePath()); n >= 0 {
			rowsByNode[n] = append(rowsByNode[n], ri)
		}
	}

	// One header per node in depth-first order, then a file node's rows unless it
	// is folded. A collapsed node keeps its own header, so nothing vanishes
	// silently. Parents precede children, so hiding propagates in one pass.
	m.entries = m.entries[:0]
	hidden := make([]bool, len(m.nodes))
	for i, n := range m.nodes {
		if p := n.parent; p >= 0 && (hidden[p] || m.collapsed[m.nodes[p].key]) {
			hidden[i] = true
			continue
		}
		m.entries = append(m.entries, entry{kind: entryHeader, path: n.key, row: -1, node: i})
		if n.isFile && !m.collapsed[n.key] {
			for _, ri := range rowsByNode[i] {
				m.entries = append(m.entries, entry{kind: entryRow, path: m.rows[ri].FilePath(), row: ri, node: -1})
			}
		}
	}

	if keepKey != "" {
		for i, e := range m.entries {
			if m.entryKey(e) == keepKey {
				m.cursor = i
				m.clampCursor()
				return
			}
		}
		// The entry is gone — folded away or filtered out. The nearest surviving
		// header keeps the cursor where the user was looking.
		if i := m.ancestorHeader(keyGroup(keepKey)); i >= 0 {
			m.cursor = i
		}
	}
	m.clampCursor()
}

// ancestorHeader is the entry index of the deepest header still drawn for a
// path: the node itself when it survived, otherwise its closest ancestor. The
// prefix search covers the case a filter change produces, where the node is gone.
func (m Model) ancestorHeader(path string) int {
	if path == "" {
		return -1
	}

	start, ok := m.nodeByFile[path]
	if !ok {
		start, ok = m.nodeByKey[path]
	}
	if !ok {
		start = -1
		key := strings.Join(pathSegments(path), "/")
		for i, n := range m.nodes {
			if n.key != key && !strings.HasPrefix(key, n.key+"/") {
				continue
			}
			// Longest match wins: it is the deepest surviving ancestor, and so
			// the smallest jump away from where the user was.
			if start < 0 || len(n.key) > len(m.nodes[start].key) {
				start = i
			}
		}
	}

	for i := start; i >= 0; i = m.nodes[i].parent {
		if e := m.headerIndex(i); e >= 0 {
			return e
		}
	}
	return -1
}

func (m *Model) rowByKey(key string) *Row {
	for i := range m.rows {
		if rowKey(m.rows[i]) == key {
			return &m.rows[i]
		}
	}
	return nil
}

func (m Model) selectedRows() []Row {
	var out []Row
	for _, r := range m.rows {
		if r.Selected && r.Actionable() {
			out = append(out, r)
		}
	}
	return out
}

func (m Model) selectedCount() int {
	n := 0
	for _, r := range m.rows {
		if r.Selected {
			n++
		}
	}
	return n
}
