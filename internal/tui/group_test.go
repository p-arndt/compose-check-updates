package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainText strips styling so a header can be asserted on as text. It is
// deliberately independent of render_test.go's helper: these tests must not
// break when the palette moves.
var groupAnsi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plainText(s string) string { return groupAnsi.ReplaceAllString(s, "") }

func homeKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyHome} }
func endKey() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyEnd} }

// entryPaths flattens the rendered/navigable list into "path" for a header and
// "path/image" for a row, which is what the collapse tests reason about.
func entryPaths(m Model) []string {
	var out []string
	for _, e := range m.entries {
		if e.kind == entryHeader {
			out = append(out, e.path)
			continue
		}
		out = append(out, e.path+"/"+m.rows[e.row].Update.ImageName)
	}
	return out
}

// cursorOnHeader reports whether the cursor sits on a group header.
func cursorOnHeader(m Model) bool {
	e, ok := m.currentEntry()
	return ok && e.kind == entryHeader
}

// twoGroups is the fixture most collapse tests use: two files, two images each.
func twoGroups(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	return feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
		updateEvent("b/compose.yml", "redis", "7.0", "7.2", "minor"),
		updateEvent("b/compose.yml", "traefik", "2.9", "3.0", "major"),
	)
}

func TestHeadersAreEntriesAndCollapseHidesOnlyRows(t *testing.T) {
	m := twoGroups(t)
	require.Equal(t, []string{
		"a/compose.yml", "a/compose.yml/caddy", "a/compose.yml/postgres",
		"b/compose.yml", "b/compose.yml/redis", "b/compose.yml/traefik",
	}, entryPaths(m))

	m = feed(t, m, keyMsg("z")) // cursor starts on a/compose.yml's header
	assert.Equal(t, []string{
		"a/compose.yml",
		"b/compose.yml", "b/compose.yml/redis", "b/compose.yml/traefik",
	}, entryPaths(m))

	// The rows are hidden, not dropped: the filtered set is untouched.
	assert.Len(t, m.visible, 4)
	assert.Len(t, m.rows, 4)
}

// space and enter are the same key as far as the list is concerned, so the same
// table drives both rather than a near-copy of this test drifting out of sync.
func TestSpaceOnHeaderFoldsAndOnRowSelects(t *testing.T) {
	for _, k := range []string{" ", "enter"} {
		t.Run(k, func(t *testing.T) {
			m := twoGroups(t)

			m = feed(t, m, keyMsg(k)) // on the header: folds
			assert.True(t, m.collapsed["a/compose.yml"])
			assert.Equal(t, 0, m.selectedCount(), "toggling a header must not select anything")

			m = feed(t, m, keyMsg(k)) // unfold again
			require.False(t, m.collapsed["a/compose.yml"])

			m = feed(t, m, keyMsg("j"), keyMsg(k)) // on a row: selects
			assert.Equal(t, 1, m.selectedCount())
			assert.False(t, m.collapsed["a/compose.yml"])
		})
	}
}

func TestCollapseStateSurvivesNewRowsAndResorts(t *testing.T) {
	m := twoGroups(t)
	m = feed(t, m, keyMsg("z")) // fold a/compose.yml
	require.True(t, m.collapsed["a/compose.yml"])

	// A row streaming into the folded group mid-scan must not re-expand it.
	m = feed(t, m, updateEvent("a/compose.yml", "alpine", "3.18", "3.19", "minor"))
	assert.True(t, m.collapsed["a/compose.yml"])
	assert.Equal(t, []string{
		"a/compose.yml",
		"b/compose.yml", "b/compose.yml/redis", "b/compose.yml/traefik",
	}, entryPaths(m))

	// Neither must a re-sort caused by a row sorting above everything.
	m = feed(t, m, updateEvent("0/compose.yml", "nginx", "1.24", "1.25", "minor"))
	assert.True(t, m.collapsed["a/compose.yml"])

	// Nor a filter change, which rebuilds the whole list.
	m = feed(t, m, keyMsg("f"))
	assert.True(t, m.collapsed["a/compose.yml"])
}

func TestCollapsingMovesTheCursorToTheHeader(t *testing.T) {
	m := twoGroups(t)
	m = feed(t, m, keyMsg("j"), keyMsg("j")) // onto a/postgres, inside the group
	require.Equal(t, "postgres", m.currentRow().Update.ImageName)

	m = feed(t, m, keyMsg("z"))
	assert.True(t, cursorOnHeader(m))
	assert.Equal(t, "a/compose.yml", m.cursorGroup())
	assert.Equal(t, 0, m.cursor)
	assert.Nil(t, m.currentRow(), "the cursor must not point into a hidden group")

	// Expanding leaves the cursor on the header rather than jumping the viewport.
	m = feed(t, m, keyMsg("z"))
	assert.True(t, cursorOnHeader(m))
	assert.Equal(t, 0, m.cursor)
}

func TestNavigationSkipsHiddenRows(t *testing.T) {
	m := twoGroups(t)
	m = feed(t, m, keyMsg("z")) // fold the first group
	require.True(t, cursorOnHeader(m))

	// Down from a collapsed header lands on the NEXT group's header.
	m = feed(t, m, keyMsg("j"))
	require.True(t, cursorOnHeader(m))
	assert.Equal(t, "b/compose.yml", m.cursorGroup())

	// And back up again, never into the folded interior.
	m = feed(t, m, keyMsg("k"))
	assert.Equal(t, "a/compose.yml", m.cursorGroup())
	assert.True(t, cursorOnHeader(m))
}

func TestCollapseAllAndExpandAllKeepTheCursorInRange(t *testing.T) {
	m := twoGroups(t)
	m = feed(t, m, endKey()) // last entry
	require.Equal(t, len(m.entries)-1, m.cursor)

	m = feed(t, m, keyMsg("C"))
	assert.Equal(t, []string{"a/compose.yml", "b/compose.yml"}, entryPaths(m))
	assert.True(t, m.cursor >= 0 && m.cursor < len(m.entries))
	assert.True(t, cursorOnHeader(m))
	assert.Equal(t, "b/compose.yml", m.cursorGroup(), "the cursor stays on the group it was in")

	m = feed(t, m, keyMsg("E"))
	assert.Len(t, m.entries, 6)
	assert.True(t, m.cursor >= 0 && m.cursor < len(m.entries))

	// Every navigation key must stay in range with everything folded.
	m = feed(t, m, keyMsg("C"))
	for _, k := range []string{"j", "j", "j", "k", "k", "k"} {
		m = feed(t, m, keyMsg(k))
		require.True(t, m.cursor >= 0 && m.cursor < len(m.entries))
	}
	m = feed(t, m, homeKey())
	assert.Equal(t, 0, m.cursor)
	m = feed(t, m, endKey())
	assert.Equal(t, len(m.entries)-1, m.cursor)
}

// THE correctness requirement: collapsing is a display operation. A selection
// made before folding must still be applied.
func TestSelectionsInsideACollapsedGroupStillApply(t *testing.T) {
	m := twoGroups(t)
	m = feed(t, m, keyMsg("j"), keyMsg(" ")) // select a/caddy
	require.Equal(t, 1, m.selectedCount())

	m = feed(t, m, keyMsg("z")) // fold the group holding it
	require.True(t, m.collapsed["a/compose.yml"])

	// Hidden, but still counted and still actionable.
	assert.Equal(t, 1, m.selectedCount())
	require.Len(t, m.selectedRows(), 1)
	assert.Equal(t, "caddy", m.selectedRows()[0].Update.ImageName)
	assert.True(t, m.selectedRows()[0].Actionable())

	// And still applied: A queues it even though its group is folded away.
	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)
	require.NotNil(t, cmd, "a folded selection must not turn A into a no-op")
	assert.Equal(t, phaseApplying, m.phase)
	assert.Equal(t, 1, m.applyActive)

	m = feed(t, m, applyResultMsg{key: rowKey(*rowFor(t, m, "caddy"))})
	assert.Equal(t, RowApplied, rowFor(t, m, "caddy").State)
	require.Len(t, m.affectedFiles(), 1)
	assert.Equal(t, "a/compose.yml", m.affectedFiles()[0].FilePath)
}

// `a` is deliberately collapse-blind — it selects every row the filter shows,
// folded or not — and the header's counts are what make that visible.
func TestSelectAllIgnoresCollapseAndHeaderReportsIt(t *testing.T) {
	m := twoGroups(t)
	m = feed(t, m, keyMsg("z")) // fold a/compose.yml
	m = feed(t, m, keyMsg("a"))

	assert.Equal(t, 4, m.selectedCount(), "folding must not shrink what `a` selects")
	assert.Len(t, m.selectedRows(), 4)

	g := m.groupInfo("a/compose.yml", false)
	assert.Equal(t, 2, g.Selected)
	assert.Contains(t, plainText(m.theme.GroupHeader(g, 80)), "(2 updates, 2 selected)")

	// `n` clears everything, folded groups included.
	m = feed(t, m, keyMsg("n"))
	assert.Equal(t, 0, m.selectedCount())
}

func TestGroupHeaderRendersCountsAndArrow(t *testing.T) {
	th := DefaultTheme()

	expanded := plainText(th.GroupHeader(GroupInfo{
		Path: "tests/folder1/compose.yaml", Shown: 2, Total: 2, Selected: 1,
	}, 80))
	assert.Equal(t, "▾ tests/folder1/compose.yaml  (2 updates, 1 selected)", expanded)

	collapsed := plainText(th.GroupHeader(GroupInfo{
		Path: "tests/folder1/compose.yaml", Shown: 2, Total: 2, Selected: 1, Collapsed: true,
	}, 80))
	assert.Equal(t, "▸ tests/folder1/compose.yaml  (2 updates, 1 selected)", collapsed)

	// Rows the filter hides are still accounted for, so the header never claims
	// the file has fewer updates than it does.
	filtered := plainText(th.GroupHeader(GroupInfo{
		Path: "a/compose.yml", Shown: 1, Total: 5, Selected: 3, Collapsed: true,
	}, 80))
	assert.Contains(t, filtered, "(1 of 5 updates, 3 selected)")

	single := plainText(th.GroupHeader(GroupInfo{Path: "a.yml", Shown: 1, Total: 1}, 80))
	assert.Contains(t, single, "(1 update, 0 selected)")
}

func TestGroupHeaderNeverExceedsWidthAndSurvivesTinyTerminals(t *testing.T) {
	th := DefaultTheme()
	g := GroupInfo{Path: "some/deeply/nested/path/docker-compose.yml", Shown: 3, Total: 5, Selected: 2}

	for _, w := range []int{0, 1, 5, 20, 21, 40, 120} {
		for _, cursor := range []bool{false, true} {
			g.Cursor = cursor
			out := th.GroupHeader(g, w)
			assert.NotContains(t, out, "\n", "headers are one line")
			assert.LessOrEqual(t, lipgloss.Width(out), clampWidth(w), "width %d", w)
		}
	}
}

func TestGroupInfoCountsSelectionsHiddenByTheFilter(t *testing.T) {
	m := twoGroups(t)
	m = feed(t, m, keyMsg("a")) // select everything
	m = feed(t, m, keyMsg("f")) // filter=major: a/compose.yml shows only postgres

	g := m.groupInfo("a/compose.yml", false)
	assert.Equal(t, 1, g.Shown)
	assert.Equal(t, 2, g.Total)
	assert.Equal(t, 2, g.Selected, "a selection the filter hides must still be reported")
}

func TestDisplayIndexAndWindowStayConsistentWhenCollapsed(t *testing.T) {
	m := twoGroups(t)
	m.width, m.height = 80, 24

	// Expanded: entry i is display line i, and every visible row maps to one.
	for vi := range m.visible {
		di := m.displayIndex(vi)
		require.GreaterOrEqual(t, di, 0)
		require.Equal(t, entryRow, m.entries[di].kind)
		require.Equal(t, m.visible[vi], m.entries[di].row)
	}
	require.Equal(t, len(m.entries), m.displayCount())

	m = feed(t, m, keyMsg("z")) // fold a/compose.yml
	assert.Equal(t, 4, m.displayCount())
	// Its rows are no longer rendered at all.
	assert.Equal(t, -1, m.displayIndex(0))
	assert.Equal(t, -1, m.displayIndex(1))
	// The survivors still map onto the exact lines the window draws.
	lines := strings.Split(m.listView(), "\n")
	require.Len(t, lines, 4)
	for vi := 2; vi < len(m.visible); vi++ {
		di := m.displayIndex(vi)
		require.GreaterOrEqual(t, di, 0)
		assert.Contains(t, plainText(lines[di-m.offset]), m.rows[m.visible[vi]].Update.ImageName)
	}
}

func TestScrollingKeepsTheCursorVisibleWithGroupsCollapsed(t *testing.T) {
	m := newTestModel()
	for _, p := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		m = feed(t, m,
			updateEvent(p+"/compose.yml", "caddy", "2.7", "2.8", "minor"),
			updateEvent(p+"/compose.yml", "redis", "7.0", "7.2", "minor"),
		)
	}
	m.width, m.height = 80, 12 // a list window far shorter than 21 entries
	m.syncScroll()

	h := m.listHeight()
	require.Less(t, h, len(m.entries))

	assertCursorVisible := func(m Model) {
		t.Helper()
		require.GreaterOrEqual(t, m.cursor, m.offset)
		require.Less(t, m.cursor, m.offset+m.listHeight())
		require.LessOrEqual(t, len(strings.Split(m.listView(), "\n")), m.listHeight())
	}

	for i := 0; i < len(m.entries)+3; i++ {
		m = feed(t, m, keyMsg("j"))
		assertCursorVisible(m)
	}
	// Folding under a scrolled window must not strand the offset past the end.
	m = feed(t, m, keyMsg("C"))
	assertCursorVisible(m)
	assert.LessOrEqual(t, m.offset, max(len(m.entries)-m.listHeight(), 0))

	for i := 0; i < len(m.entries)+3; i++ {
		m = feed(t, m, keyMsg("k"))
		assertCursorVisible(m)
	}
	m = feed(t, m, keyMsg("E"))
	assertCursorVisible(m)
}

func TestCollapseOnDegenerateLists(t *testing.T) {
	// Empty list: every fold key must be a safe no-op.
	m := newTestModel()
	m = feed(t, m, keyMsg("z"), keyMsg("C"), keyMsg("E"), keyMsg(" "))
	assert.Empty(t, m.entries)
	assert.Equal(t, 0, m.cursor)
	assert.Nil(t, m.currentRow())
	assert.Equal(t, "", m.cursorGroup())

	// A single group: folding leaves exactly its header behind.
	m = newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m = feed(t, m, keyMsg("z"))
	assert.Equal(t, []string{"a/compose.yml"}, entryPaths(m))
	assert.Equal(t, 0, m.cursor)
	assert.NotEmpty(t, m.listView())

	// Folding a group the filter has emptied out leaves nothing to point at.
	m = feed(t, m, keyMsg("f")) // filter=major, this minor row drops out
	assert.Empty(t, m.entries)
	assert.Equal(t, 0, m.cursor)
	m = feed(t, m, keyMsg("z"), keyMsg("j"), keyMsg("k"))
	assert.Equal(t, 0, m.cursor)
}
