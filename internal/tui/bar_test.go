package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// barModel is a browsing model wide enough for two columns, with one row that
// has a release at every level and the cursor parked on it.
func barModel(t *testing.T) Model {
	t.Helper()
	m, _ := sidebarModel(t)
	return m
}

// tabMsg steps along the bar once the keyboard is on it.
func tabMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }

// shiftTabMsg steps back along the bar.
func shiftTabMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyShiftTab} }

// focusStop walks the focus onto the named bar stop with `m`; tab is no use
// here, since on a row it opens the detail column.
func focusStop(t *testing.T, m Model, label string) Model {
	t.Helper()
	for i := 0; i < 12; i++ {
		m = feed(t, m, keyMsg("m"))
		require.Equal(t, focusBar, m.focus, "m ran off the end of the bar")
		if strings.HasPrefix(m.barStops()[m.barStop].label, label) {
			return m
		}
	}
	require.Failf(t, "no such bar stop", "%q", label)
	return m
}

// The bar is one line, inside its width, whatever the terminal does: a second
// line would silently take one from the list.
func TestBarLineStaysOnOneLineWithinItsWidth(t *testing.T) {
	withColor(t)
	m := barModel(t)

	for _, w := range []int{-100, -1, 0, 1, 2, 7, 19, 20, 40, 200} {
		for _, focus := range []focusArea{focusList, focusBar} {
			m.focus = focus
			for stop := range m.barStops() {
				m.barStop = stop
				out := m.barLine(w)
				assert.NotContains(t, out, "\n", "width %d", w)
				assert.LessOrEqual(t, lipgloss.Width(out), clampWidth(w), "width %d", w)
			}
		}
	}
}

func TestBarLineHasNoTrailingWhitespace(t *testing.T) {
	m := barModel(t)
	out := plain(m.barLine(200))
	assert.Equal(t, out, strings.TrimRight(out, " "))
}

// The focused stop has to actually look different: a background cut short by a
// nested style reset still "renders", it just does not read as focus.
func TestBarMarksTheFocusedStop(t *testing.T) {
	withColor(t)
	m := barModel(t)

	m.focus = focusList
	unfocused := m.barStopText(m.barStops()[0], false)
	focused := m.barStopText(m.barStops()[0], true)

	require.NotEqual(t, unfocused, focused, "focus must change how a stop is drawn")
	assert.Equal(t, plain(unfocused), plain(focused), "and must not change what it says")

	// The styling has to survive the whole stop rather than stopping at the
	// first segment boundary: every visible piece carries it.
	assert.Contains(t, focused, "\x1b[")
	for _, piece := range []string{"show", "all"} {
		i := strings.Index(plain(focused), piece)
		require.GreaterOrEqual(t, i, 0)
	}
	assert.GreaterOrEqual(t, strings.Count(focused, "4m"), 2,
		"the underline is re-applied per segment, not left to bleed across a reset")
}

// The stops never come and go. A bar that dropped its issues button when there
// was nothing to report would move every stop after it under the user's cursor.
func TestBarStopsAreStableButDisabled(t *testing.T) {
	m := barModel(t)
	require.Empty(t, m.scanErrs)

	labels := func(m Model) []string {
		var out []string
		for _, s := range m.barStops() {
			out = append(out, strings.Split(s.label, " ")[0])
		}
		return out
	}
	assert.Equal(t, []string{"show", "target", "issues", "apply"}, labels(m))

	for _, s := range m.barStops() {
		if strings.HasPrefix(s.label, "issues") || strings.HasPrefix(s.label, "apply") {
			assert.True(t, s.off, "%q has nothing to do yet", s.label)
		}
	}

	m.currentRow().Selected = true
	for _, s := range m.barStops() {
		if strings.HasPrefix(s.label, "apply") {
			assert.False(t, s.off)
			assert.Equal(t, "apply 1", s.label)
		}
	}
}

func TestBarChangesTheFilter(t *testing.T) {
	m := focusStop(t, barModel(t), "show")
	require.Equal(t, FilterAll, m.filter)

	m = feed(t, m, keyMsg(" "))
	assert.Equal(t, FilterMajor, m.filter, "space acts on whatever has the focus")

	m = feed(t, m, keyMsg("-"), keyMsg("-"))
	assert.Equal(t, FilterDigest, m.filter, "stepping back past the first value wraps round")
}

func TestBarChangesTheTarget(t *testing.T) {
	m := focusStop(t, barModel(t), "target")
	require.Equal(t, TargetMajor, m.target, "major is the default")

	// The cycle is patch, minor, major — so forward from major wraps round to
	// the most conservative choice rather than to a bigger jump.
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, TargetPatch, m.target)
	assert.Equal(t, "2.9.4", m.currentRow().Update.LatestTag, "the rows follow the bar")

	m = feed(t, m, keyMsg("-"))
	assert.Equal(t, TargetMajor, m.target)
	assert.Equal(t, "3.7.8", m.currentRow().Update.LatestTag)
}

// ←/→ move along the bar without changing any value.
func TestBarArrowsMoveBetweenStopsWithoutChangingAnything(t *testing.T) {
	m := focusStop(t, barModel(t), "show")
	filter, target := m.filter, m.target

	m = feed(t, m, keyMsg("right"))
	assert.Equal(t, 1, m.barStop)
	m = feed(t, m, keyMsg("left"))
	assert.Equal(t, 0, m.barStop)

	assert.Equal(t, filter, m.filter, "moving must not change a value")
	assert.Equal(t, target, m.target)

	// Off the left edge it wraps rather than falling out: the way out is ↓ and
	// esc, and a third one would only blur what each means.
	m = feed(t, m, keyMsg("left"))
	assert.Equal(t, focusBar, m.focus)
	assert.Equal(t, len(m.barStops())-1, m.barStop)
}

// enter presses a button. The bar never claims space, which belongs to the list.
func TestBarPressesIssues(t *testing.T) {
	m := barModel(t)
	m = feed(t, m, issueEvent("a/compose.yml", "broken yaml"))
	m = focusStop(t, m, "issues")

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.True(t, m.showIssues)
	assert.Equal(t, focusList, m.focus, "the pane replaces the list, so the bar gives the keyboard back")
}

func TestBarIssuesButtonDoesNothingWithoutIssues(t *testing.T) {
	m := focusStop(t, barModel(t), "issues")
	require.Empty(t, m.scanErrs)

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.showIssues)
}

func TestBarPressesApply(t *testing.T) {
	m := barModel(t)
	m.currentRow().Selected = true
	m = focusStop(t, m, "apply")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	assert.NotNil(t, cmd, "pressing apply starts the apply")
	assert.Equal(t, focusList, m.focus)
}

func TestBarApplyButtonDoesNothingWithoutASelection(t *testing.T) {
	m := focusStop(t, barModel(t), "apply")
	require.Zero(t, m.selectedCount())

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd)
}

// The bar claims the arrows, tab and the acting keys, and nothing else: anything
// else reaches the list, so tabbing across is never a mode you are stuck in.
func TestBarPassesUnclaimedKeysToTheList(t *testing.T) {
	m := focusStop(t, barModel(t), "show")
	require.Empty(t, m.collapsed)

	m = feed(t, m, keyMsg("C"))
	assert.NotEmpty(t, m.collapsed, "collapse-all still reaches the list")
	assert.Equal(t, focusBar, m.focus, "and the keyboard stays where it was")
}

// space acts on whatever has the focus. On the bar that is the stop, so it must
// not also select the row that happens to be under the list cursor.
func TestBarSpaceActsOnTheStopNotOnTheRow(t *testing.T) {
	m := focusStop(t, barModel(t), "show")
	selected := m.currentRow().Selected

	m = feed(t, m, keyMsg(" "))
	assert.Equal(t, selected, m.currentRow().Selected, "the row is not what has the focus")
	assert.NotEqual(t, FilterAll, m.filter, "the stop is")
}

// Every stop names the key that does the same thing, and it has to be the one
// the map actually binds.
func TestBarHintsMatchTheKeyMap(t *testing.T) {
	m := barModel(t)
	k := DefaultKeyMap()

	want := map[string]string{
		"show":   k.Filter.Help().Key,
		"target": k.Target.Help().Key,
		"issues": k.Issues.Help().Key,
		"apply":  k.Apply.Help().Key,
	}
	for _, s := range m.barStops() {
		name := strings.Split(s.label, " ")[0]
		hint, ok := want[name]
		require.True(t, ok, "unexpected stop %q", s.label)
		assert.Equal(t, hint, s.hint, "stop %q names the wrong key", name)
	}
}

func TestBarFooterHints(t *testing.T) {
	m := focusStop(t, barModel(t), "show")
	assert.Equal(t, m.keys.BarHints(), m.hintBindings())

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, m.keys.BrowseHints(), m.hintBindings())
}

// The bar is drawn at every width, including one too narrow for the detail
// column. That is the point of putting the list-wide controls there.
func TestBarIsDrawnWhenTheColumnIsNot(t *testing.T) {
	m := barModel(t)
	m.width = sidebarMinTotal - 1
	require.Zero(t, sidebarWidth(m.width))

	assert.Contains(t, plain(m.barLine(m.width)), "show")
}

// The way out has to be one keypress, not a lap round the remaining stops.
func TestBarLeavesOnEsc(t *testing.T) {
	m := focusStop(t, barModel(t), "show")
	require.Equal(t, focusBar, m.focus)
	cursor := m.cursor

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, focusList, m.focus)
	assert.Equal(t, cursor, m.cursor, "leaving is not moving: the place in the list is kept")
}

// The vertical arrows go back down into the list. Letting them fall through
// scrolled the list while the keyboard said it was on the bar.
func TestBarVerticalArrowsReturnToTheList(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyUp}} {
		m := focusStop(t, barModel(t), "show")
		cursor := m.cursor

		m = feed(t, m, k)
		assert.Equal(t, focusList, m.focus)
		assert.Equal(t, cursor, m.cursor, "leaving is not moving: the place in the list is kept")
	}
}

// The footer has to name the way out, or the way out may as well not exist.
func TestBarHintsLeadWithTheWayOut(t *testing.T) {
	k := DefaultKeyMap()
	hints := k.BarHints()
	require.NotEmpty(t, hints)
	assert.Equal(t, k.IssuesClose.Help().Key, hints[0].Help().Key, "esc comes first")
}

// `m` reaches the bar whatever the cursor is on, the detail column included.
func TestBarIsReachableWithMFromAnywhere(t *testing.T) {
	m := barModel(t)
	require.NotNil(t, m.currentRow(), "this test starts on an image")

	m = feed(t, m, keyMsg("m"))
	assert.Equal(t, focusBar, m.focus)
	assert.Equal(t, 0, m.barStop)

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEsc}, keyMsg("tab"))
	require.Equal(t, focusSide, m.focus, "tab on an image opens the column")

	m = feed(t, m, keyMsg("m"))
	assert.Equal(t, focusBar, m.focus, "and m gets to the bar from there too")
}

// tab on an image opens the detail column and comes straight back; the bar is
// not on the way.
func TestTabOnAnImageOpensTheColumnAndBack(t *testing.T) {
	m := barModel(t)

	m = feed(t, m, keyMsg("tab"))
	require.Equal(t, focusSide, m.focus)

	m = feed(t, m, keyMsg("tab"))
	assert.Equal(t, focusList, m.focus)
}

// `m` walks the bar: onto it, one stop further each press, wrapping round.
func TestMStepsAlongTheBar(t *testing.T) {
	m := barModel(t)

	for i := range m.barStops() {
		m = feed(t, m, keyMsg("m"))
		require.Equal(t, focusBar, m.focus, "stop %d", i)
		assert.Equal(t, i, m.barStop)
	}

	m = feed(t, m, keyMsg("m"))
	assert.Equal(t, focusBar, m.focus)
	assert.Equal(t, 0, m.barStop, "past the last stop it wraps")
}

// On a header there is no image to describe, so tab is free to walk the bar.
func TestTabStepsAlongTheBarOnAHeader(t *testing.T) {
	m := barModel(t)
	m.cursor = 0
	require.Nil(t, m.currentRow(), "this test needs the cursor on a header")

	for i := range m.barStops() {
		m = feed(t, m, tabMsg())
		require.Equal(t, focusBar, m.focus, "stop %d", i)
		assert.Equal(t, i, m.barStop)
	}
}

func TestShiftTabStepsBackAlongTheBar(t *testing.T) {
	m := focusStop(t, barModel(t), "target")
	require.Equal(t, 1, m.barStop)

	m = feed(t, m, shiftTabMsg())
	assert.Equal(t, 0, m.barStop)

	m = feed(t, m, shiftTabMsg())
	assert.Equal(t, len(m.barStops())-1, m.barStop, "back past the first stop wraps")
}

// tab asks what the cursor is on, not where the focus is. Arrowing out of the
// bar onto an image and tabbing has to open that image's details.
func TestTabGoesToDetailsAfterLeavingTheBarOntoAnImage(t *testing.T) {
	m := barModel(t)
	m.cursor = 0
	require.Nil(t, m.currentRow(), "this test starts on a header")

	m = feed(t, m, tabMsg())
	require.Equal(t, focusBar, m.focus, "no image under the cursor, so tab goes to the bar")

	// Down leaves the bar; down again moves onto the image below the header.
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, focusList, m.focus)
	require.NotNil(t, m.currentRow(), "the cursor is on an image now")

	m = feed(t, m, tabMsg())
	assert.Equal(t, focusSide, m.focus, "tab follows the cursor, not the focus")
}

// Up at the top of the list carries on into the bar drawn directly above it.
func TestUpAtTheTopOfTheListEntersTheBar(t *testing.T) {
	m := barModel(t)
	m.cursor = 0
	require.Equal(t, focusList, m.focus)

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, focusBar, m.focus)
	assert.Equal(t, 0, m.barStop)

	// And straight back down again: one gesture, both directions.
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, focusList, m.focus)
	assert.Equal(t, 0, m.cursor, "the place in the list is kept")
}

// Anywhere else up still just moves the cursor.
func TestUpBelowTheTopStillMovesTheCursor(t *testing.T) {
	m := barModel(t)
	require.NotZero(t, m.cursor, "this test needs the cursor off the top")

	before := m.cursor
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, focusList, m.focus)
	assert.Equal(t, before-1, m.cursor)
}
