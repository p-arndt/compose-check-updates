package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.phase != phaseScanning && m.phase != phaseBrowsing {
		return m, nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		delta = -1
	case tea.MouseButtonWheelDown:
		delta = 1
	}
	if delta == 0 {
		return m, nil
	}
	// The wheel scrolls whichever pane is on screen.
	if m.showIssues {
		m.moveIssueCursor(delta)
	} else {
		m.moveCursor(delta)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.phase == phaseRestartPrompt {
		return m.handleRestartKey(msg)
	}

	// The help dialog covers the pane, so keys acting on hidden rows are inert and
	// esc closes the dialog rather than quitting behind it.
	if m.showHelp {
		if key.Matches(msg, m.keys.Help) || key.Matches(msg, m.keys.IssuesClose) || key.Matches(msg, m.keys.Quit) {
			m.showHelp = false
		}
		return m, nil
	}

	// The issues pane owns the keyboard while it is open, which is also what
	// lets esc mean "back to the list" there and "quit" everywhere else.
	if m.showIssues {
		return m.handleIssuesKey(msg)
	}

	// The bar and the detail column claim only the keys they need and hand the
	// rest back to the list: a user who tabs across, changes a level and then
	// presses `j` means to move down the list.
	if m.focus == focusBar {
		if handled, model, cmd := m.handleBarKey(msg); handled {
			return model, cmd
		}
	}

	if m.focus == focusSide {
		if handled, model, cmd := m.handleSideKey(msg); handled {
			return model, cmd
		}
	}

	if key.Matches(msg, m.keys.Quit) {
		m.cancel()
		m.phase = phaseDone
		return m, tea.Quit
	}

	// Applying is short and touches files; ignore everything but quit so the
	// list cannot be re-sorted out from under the results arriving for it.
	if m.phase != phaseScanning && m.phase != phaseBrowsing {
		return m, nil
	}

	return m.handleListKey(msg)
}

// handleListKey reads the keys of the list itself: every pane that could have
// claimed the key has already declined it by the time handleKey gets here.
func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		// At the top of the list the only thing further up is the bar, and this is
		// the reverse of the ↓ that comes back down from it.
		if m.cursor == 0 {
			m.enterBar(0)
			break
		}
		m.moveCursor(-1)
	case key.Matches(msg, m.keys.Down):
		m.moveCursor(1)
	case key.Matches(msg, m.keys.PageUp):
		m.moveCursor(-m.listHeight())
	case key.Matches(msg, m.keys.PageDown):
		m.moveCursor(m.listHeight())
	case key.Matches(msg, m.keys.Home):
		m.moveCursor(-len(m.entries))
	case key.Matches(msg, m.keys.End):
		m.moveCursor(len(m.entries))
	case key.Matches(msg, m.keys.Toggle):
		m.toggleCurrent()
	case key.Matches(msg, m.keys.Bar):
		m.openBar()
	case key.Matches(msg, m.keys.ToggleGroup):
		m.toggleGroup(m.cursorGroup())
	case key.Matches(msg, m.keys.Collapse):
		m.collapseOrParent()
	case key.Matches(msg, m.keys.Expand):
		// On a header this walks the tree. A row has nothing to expand, so the key
		// means the one thing to the right of it: its detail column.
		if m.currentRow() != nil && m.sidebarAvailable() {
			m.focus = focusSide
			break
		}
		m.expandOrChild()
	case key.Matches(msg, m.keys.CollapseAll):
		m.setAllCollapsed(true)
	case key.Matches(msg, m.keys.ExpandAll):
		m.setAllCollapsed(false)
	case key.Matches(msg, m.keys.SelectAllGlobal):
		m.setScopeSelected(-1, true)
	case key.Matches(msg, m.keys.SelectNoneGlobal):
		m.setScopeSelected(-1, false)
	case key.Matches(msg, m.keys.SelectAll):
		m.setScopeSelected(m.cursorNode(), true)
	case key.Matches(msg, m.keys.SelectNone):
		m.setScopeSelected(m.cursorNode(), false)
	case key.Matches(msg, m.keys.Filter):
		m.cycleFilter()
	case key.Matches(msg, m.keys.Target):
		m.cycleTarget()
	case key.Matches(msg, m.keys.Floating):
		cmd := m.toggleFloating()
		return m, cmd
	case key.Matches(msg, m.keys.Focus):
		m.advanceFocus()
	case key.Matches(msg, m.keys.FocusPrev):
		m.retreatFocus()
	case key.Matches(msg, m.keys.Issues):
		m.openIssues()
	case key.Matches(msg, m.keys.Help):
		m.toggleHelp()
	case key.Matches(msg, m.keys.Apply):
		return m.handleApply()
	case key.Matches(msg, m.keys.ApplyRow):
		return m.handleApplyRow()
	}
	return m, nil
}

// setScopeSelected drives a/n, which pass the cursor's node, and ctrl+a/ctrl+n,
// which pass -1 for the whole list. Collapse-blind, since folding is display
// only, and asymmetric about the filter: selecting only adds rows the filter
// shows, while deselecting sweeps the scope regardless — so `n` can never leave
// a selected row that no header reports.
func (m *Model) setScopeSelected(node int, v bool) {
	verb := "deselected"
	if v {
		verb = "selected"
	}

	scope := ""
	idxs := m.visible
	if node >= 0 {
		scope = m.nodes[node].label
		idxs = m.subtreeRows(node)
	}
	if !v {
		idxs = m.scopeRowsUnfiltered(node)
	}

	n := 0
	for _, ri := range idxs {
		r := &m.rows[ri]
		// Only actionable rows can be selected, but anything already selected can be
		// cleared, or a row that lost its target would stay stuck on.
		if (v && !r.Actionable()) || r.Selected == v {
			continue
		}
		r.Selected = v
		n++
	}

	if scope == "" {
		m.setStatus(StatusInfo, fmt.Sprintf("%s %d update(s)", verb, n))
		return
	}
	m.setStatus(StatusInfo, fmt.Sprintf("%s %d update(s) under %s", verb, n, scope))
}

// scopeRowsUnfiltered is subtreeRows without the filter, so deselection really
// clears a scope. A node index below zero means every row there is.
func (m Model) scopeRowsUnfiltered(node int) []int {
	if node < 0 {
		idxs := make([]int, len(m.rows))
		for i := range m.rows {
			idxs[i] = i
		}
		return idxs
	}

	files := make(map[string]bool, len(m.subtreeFiles(node)))
	for _, p := range m.subtreeFiles(node) {
		files[p] = true
	}

	idxs := make([]int, 0, len(m.rows))
	for i, r := range m.rows {
		if files[r.FilePath()] {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

// handleIssuesKey drives the issues pane. It reads only navigation, the two
// ways out, and quit: every list key would act on a list nobody can see.
func (m Model) handleIssuesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.IssuesClose), key.Matches(msg, m.keys.Issues):
		m.showIssues = false
		m.syncScroll()
	case key.Matches(msg, m.keys.Quit):
		m.cancel()
		m.phase = phaseDone
		return m, tea.Quit
	case key.Matches(msg, m.keys.Up):
		m.moveIssueCursor(-1)
	case key.Matches(msg, m.keys.Down):
		m.moveIssueCursor(1)
	case key.Matches(msg, m.keys.PageUp):
		m.moveIssueCursor(-m.listHeight())
	case key.Matches(msg, m.keys.PageDown):
		m.moveIssueCursor(m.listHeight())
	case key.Matches(msg, m.keys.Home):
		m.moveIssueCursor(-len(m.scanErrs))
	case key.Matches(msg, m.keys.End):
		m.moveIssueCursor(len(m.scanErrs))
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		m.syncIssueScroll()
	}
	return m, nil
}

func (m Model) handleRestartKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Yes):
		m.restartTargets = m.affectedFiles()
		m.phase = phaseRestarting
		// Quitting here hands control back to Run, which runs docker after the
		// alt screen is torn down.
		return m, tea.Quit
	case key.Matches(msg, m.keys.No), key.Matches(msg, m.keys.Quit):
		m.phase = phaseDone
		return m, tea.Quit
	}
	return m, nil
}

// handleSideKey reads the keys the sidebar claims while it has the focus. The
// bool reports whether it consumed the key; anything else falls through to the
// list, so the sidebar never becomes a mode the user is stuck in.
func (m Model) handleSideKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.FocusBack), key.Matches(msg, m.keys.FocusPrev):
		m.focus = focusList
		return true, m, nil
	case key.Matches(msg, m.keys.Bar):
		m.openBar()
		return true, m, nil
	// ←/→ step the field's options rather than closing the column; tab/esc is the
	// way out of every pane anyway.
	case key.Matches(msg, m.keys.SidePrev):
		return true, m, m.cycleSideValue(-1)
	case key.Matches(msg, m.keys.SideNext):
		return true, m, m.cycleSideValue(1)
	case key.Matches(msg, m.keys.Up):
		m.sideField = m.stepField(-1)
		return true, m, nil
	case key.Matches(msg, m.keys.Down):
		m.sideField = m.stepField(1)
		return true, m, nil
	case key.Matches(msg, m.keys.ValueNext):
		return true, m, m.cycleSideValue(1)
	case key.Matches(msg, m.keys.ValuePrev):
		return true, m, m.cycleSideValue(-1)
	}
	return false, m, nil
}

// stepField moves the sidebar cursor by delta, skipping fields that are not on
// screen — the scope has nothing to answer until a cap exists.
func (m Model) stepField(delta int) sideField {
	f := m.sideField
	for i := 0; i < int(sideFieldCount); i++ {
		f = (f + sideField(delta) + sideFieldCount) % sideFieldCount
		if m.fieldVisible(f) {
			return f
		}
	}
	return fieldTarget
}

// fieldVisible reports whether the sidebar currently draws this field.
func (m Model) fieldVisible(f sideField) bool {
	switch f {
	case fieldScope:
		// The scope has nothing to answer until a cap exists.
		r := m.currentRow()
		return r != nil && r.Pin != ""
	case fieldVersioning:
		// Only where reading the tags is the thing that failed. Every other row is
		// proof that the scheme it is being read under works, and a field offering
		// to change it would be an invitation to break a row that is fine.
		r := m.currentRow()
		return r != nil && r.Update.IsUnreadable()
	}
	return true
}
