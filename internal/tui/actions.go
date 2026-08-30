package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

func (m Model) handleApply() (tea.Model, tea.Cmd) {
	cmd := m.beginApply(m.selectedRows())
	if cmd == nil {
		m.setStatus(StatusWarn, "nothing selected — press space to select updates")
		return m, nil
	}
	m.cancel() // a still-running scan would keep appending rows mid-apply
	return m, cmd
}

// handleApplyRow writes just the row under the cursor, reading and setting no
// selection, so it never disturbs one built up for A.
func (m Model) handleApplyRow() (tea.Model, tea.Cmd) {
	r := m.currentRow()
	if r == nil {
		m.setStatus(StatusWarn, "no image under the cursor — press u on an update row")
		return m, nil
	}
	switch {
	case r.State == RowApplied:
		m.setStatus(StatusInfo, "this update has already been applied")
		return m, nil
	case r.Update.IsUnreadable():
		// Update() would refuse this row anyway; saying so here keeps the reason
		// in front of the user instead of turning it into a failed apply.
		m.setStatus(StatusWarn, r.Update.UnreadableMessage)
		return m, nil
	case r.NoTarget:
		m.setStatus(StatusWarn, fmt.Sprintf("no %s release for this image — press T to retarget it", targetLabel(r.Target)))
		return m, nil
	}

	cmd := m.beginApply([]Row{*r})
	m.cancel()
	return m, cmd
}

// toggleCurrent is space/enter: on a header it folds that node, on a row it flips
// the selection. A row with nothing at the current target has no tag to write and
// cannot be selected. Neither key ever writes — that is what A and u are for.
func (m *Model) toggleCurrent() {
	if e, ok := m.currentEntry(); ok && e.kind == entryHeader {
		m.toggleGroup(e.path)
		return
	}
	if r := m.currentRow(); r != nil && r.Actionable() {
		r.Selected = !r.Selected
	}
}

// cycleFilter steps the display filter forward.
func (m *Model) cycleFilter() { m.setFilter(m.filter.Next()) }

// setFilter is the same move with the value named rather than stepped.
func (m *Model) setFilter(f Filter) {
	m.filter = f
	m.rebuild(m.cursorKey())
	m.syncScroll()
}

// cycleTarget steps the level every row is pointed at, and says so — a change
// this wide has to be announced or it reads as the list re-sorting itself.
func (m *Model) cycleTarget() { m.setTargetAnnounced(nextTarget(m.target)) }

// setTargetAnnounced is setTarget plus the status line, so every way in leaves
// the same trace.
func (m *Model) setTargetAnnounced(t Target) {
	m.setTarget(t)
	m.setStatus(StatusInfo, fmt.Sprintf("target level: %s", targetLabel(t)))
}

// Nothing to browse is a no-op with an explanation rather than an empty pane.
// toggleFloating lists or hides the floating-tag rows, fetching their digests
// the first time they are asked for — a run that was told not to pin must not
// spend those requests unasked. Returns nil when there is nothing to fetch.
func (m *Model) toggleFloating() tea.Cmd {
	m.showFloating = !m.showFloating

	// Hidden rows may not stay selected: `A` would then write a digest into a
	// line the user cannot see, and the apply count would name rows no header
	// reports.
	if !m.showFloating {
		for i := range m.rows {
			if m.rows[i].Level == policy.LevelPin {
				m.rows[i].Selected = false
			}
		}
	}

	m.rebuild(m.cursorKey())
	m.syncScroll()

	if m.showFloating && !m.floatingResolved {
		m.setStatus(StatusInfo, "resolving what the floating tags point at…")
		return m.startFloatingScan
	}

	m.setStatus(StatusInfo, m.floatingSummary())
	return nil
}

// floatingSummary is what the status line says about the switch, counting the
// rows rather than the images so it cannot disagree with the list.
func (m Model) floatingSummary() string {
	n := 0
	for _, r := range m.rows {
		if r.Level == policy.LevelPin {
			n++
		}
	}

	switch {
	case n == 0:
		return "no floating tags found"
	case m.showFloating:
		return fmt.Sprintf("%d floating tag(s) listed, applying one writes the digest it resolves to", n)
	default:
		return fmt.Sprintf("%d floating tag(s) hidden", n)
	}
}

// openIssues shows the pane listing every skipped image and unreadable file.
func (m *Model) openIssues() {
	if len(m.scanErrs) == 0 {
		m.setStatus(StatusInfo, "no issues were logged during the scan")
		return
	}
	m.showIssues = true
	m.issueCursor = 0
	m.issueOffset = 0
	m.syncIssueScroll()
}

// toggleHelp opens or closes the help dialog. The scroll sync keeps the list's
// window valid across the height the dialog takes from it.
func (m *Model) toggleHelp() {
	m.showHelp = !m.showHelp
	m.syncScroll()
}
