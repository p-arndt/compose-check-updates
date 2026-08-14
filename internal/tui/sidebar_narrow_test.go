package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The whole point of the stacked layout: at a width with no room for a second
// column the fields are still on screen, so the settings behind them are still
// reachable.
func TestStackedSidebarIsDrawnBelowTheList(t *testing.T) {
	m, _ := sidebarModel(t)
	m.width, m.height = sidebarMinTotal-1, 30
	require.Equal(t, sidebarStacked, m.sidebarPlacement())

	view := plainText(m.View())
	assert.Contains(t, view, "target", "the target field must survive the narrow layout")
	assert.Contains(t, view, "cap", "the cap field must survive the narrow layout")
}

// Below the stacked width there is genuinely nothing to draw, and the frame
// must not try.
func TestNoSidebarAtAllWhenThereIsNoRoom(t *testing.T) {
	m, _ := sidebarModel(t)
	m.width, m.height = sidebarMinStacked-1, 30
	require.Equal(t, sidebarNowhere, m.sidebarPlacement())

	assert.Zero(t, m.stackedSidebarHeight())
}

// The panel comes out of the list's rows rather than out of the terminal, or
// the frame would run past the last line and the whole UI would shake.
func TestStackedSidebarKeepsTheFrameHeight(t *testing.T) {
	for _, width := range []int{sidebarMinStacked, 60, sidebarMinTotal - 1, sidebarMinTotal, 140} {
		m, _ := sidebarModel(t)
		m.width, m.height = width, 30

		lines := strings.Split(m.View(), "\n")
		assert.Len(t, lines, 30, "width %d", width)
	}
}

// A cap set from the stacked panel has to reach the same place it would from
// the column: the layout decides where the fields are drawn, nothing else.
func TestCapIsSettableFromTheStackedSidebar(t *testing.T) {
	m, _ := sidebarModel(t)
	m.width, m.height = sidebarMinTotal-1, 30
	require.Equal(t, sidebarStacked, m.sidebarPlacement())

	m = feed(t, m, keyMsg("tab"))
	require.Equal(t, focusSide, m.focus)

	before := m.currentRow().Pin
	m.sideField = fieldCap
	m.cycleSideValue(1)
	assert.NotEqual(t, before, m.currentRow().Pin, "the cap has to change from here too")
}

// A short terminal is the case the stacked panel is most likely to meet, and it
// must not eat the list entirely or push the footer off screen.
func TestStackedSidebarOnAShortTerminal(t *testing.T) {
	m, _ := sidebarModel(t)
	m.width, m.height = 70, minViewHeight

	lines := strings.Split(m.View(), "\n")
	assert.Len(t, lines, minViewHeight)
	assert.GreaterOrEqual(t, m.listHeight(), 1, "the list never goes below one row")
}
