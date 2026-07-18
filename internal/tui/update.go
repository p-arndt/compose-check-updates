package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

type scanStartedMsg struct{ events <-chan scanner.Event }
type scanEventMsg struct{ ev scanner.Event }
type scanDoneMsg struct{}
type scanFailedMsg struct{ err error }

// logPollMsg drives the pull of captured slog records into the UI. The handler
// is written to from scan goroutines that know nothing about Bubble Tea, so the
// UI polls it rather than the handler pushing messages into the program.
type logPollMsg struct{}

const logPollInterval = 300 * time.Millisecond

func pollLogs() tea.Cmd {
	return tea.Tick(logPollInterval, func(time.Time) tea.Msg { return logPollMsg{} })
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, m.startScan}
	if m.logs != nil {
		cmds = append(cmds, pollLogs())
	}
	return tea.Batch(cmds...)
}

func (m Model) startScan() tea.Msg {
	events, err := scanner.Scan(m.ctx, m.opts)
	if err != nil {
		return scanFailedMsg{err: err}
	}
	return scanStartedMsg{events: events}
}

// waitForEvent reads exactly one event and re-arms itself from Update. Draining
// the channel in a goroutine instead would mean rows only appear once the scan
// is over, which is the whole thing the streaming scanner exists to avoid.
func waitForEvent(events <-chan scanner.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return scanDoneMsg{}
		}
		return scanEventMsg{ev: ev}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.syncScroll()
		return m, nil

	case spinner.TickMsg:
		// Only the scan phase shows the spinner; stopping the tick elsewhere
		// keeps the program idle instead of redrawing forever.
		if m.phase != phaseScanning {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case scanStartedMsg:
		m.events = msg.events
		return m, waitForEvent(msg.events)

	case scanFailedMsg:
		m.err = msg.err
		m.phase = phaseDone
		return m, tea.Quit

	case scanEventMsg:
		m.handleScanEvent(msg.ev)
		return m, waitForEvent(m.events)

	case logPollMsg:
		m.drainLogs()
		if m.phase == phaseDone {
			return m, nil
		}
		return m, pollLogs()

	case scanDoneMsg:
		// Drain once more here: the last skipped image is usually logged in the
		// same instant the scan finishes, i.e. between two polls.
		m.drainLogs()
		if m.phase == phaseScanning {
			m.phase = phaseBrowsing
			m.setStatus(StatusInfo, fmt.Sprintf("%d update(s) found in %d file(s)", len(m.rows), m.checked))
		}
		return m, nil

	case applyResultMsg:
		cmd := m.handleApplyResult(msg)
		return m, cmd

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// drainLogs folds newly captured warnings and errors into the same list the
// scanner's own failures land in, so "an image was skipped" reaches the user
// through the status line instead of being written over the frame.
func (m *Model) drainLogs() {
	if m.logs == nil {
		return
	}
	for _, rec := range m.logs.drain() {
		m.scanErrs = append(m.scanErrs, rec)
	}
}

func (m *Model) handleScanEvent(ev scanner.Event) {
	switch ev.Kind {
	case scanner.EventDiscovered:
		m.total = ev.Total
	case scanner.EventUpdate:
		m.addRow(Row{Update: ev.Update, Level: ev.Level})
		m.syncScroll()
	case scanner.EventFileDone:
		m.checked++
	case scanner.EventError:
		// A file that errored never reports done, so it is counted here instead
		// or the progress readout would stall short of the total.
		m.checked++
		m.scanErrs = append(m.scanErrs, ev.Err)
	}
}

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

	// The issues pane owns the keyboard while it is open, which is also what
	// lets esc mean "back to the list" there and "quit" everywhere else.
	if m.showIssues {
		return m.handleIssuesKey(msg)
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

	switch {
	case key.Matches(msg, m.keys.Up):
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
		// On a file header space/enter folds the group; on a row it keeps its
		// select meaning. A row with nothing at the current target has no tag to
		// write, so it cannot be selected at all. Neither key ever writes: that is
		// what A and u are for.
		if e, ok := m.currentEntry(); ok && e.kind == entryHeader {
			m.toggleGroup(e.path)
			break
		}
		if r := m.currentRow(); r != nil && r.Actionable() {
			r.Selected = !r.Selected
		}
	case key.Matches(msg, m.keys.ToggleGroup):
		m.toggleGroup(m.cursorGroup())
	case key.Matches(msg, m.keys.CollapseAll):
		m.setAllCollapsed(true)
	case key.Matches(msg, m.keys.ExpandAll):
		m.setAllCollapsed(false)
	case key.Matches(msg, m.keys.SelectAll):
		// Deliberately collapse-blind: folding is a display operation, so `a`
		// selects every row the *filter* shows, folded or not. Each collapsed
		// header reports "(N updates, M selected)", which is what keeps the
		// outcome visible rather than surprising.
		for _, ri := range m.visible {
			if m.rows[ri].Actionable() {
				m.rows[ri].Selected = true
			}
		}
	case key.Matches(msg, m.keys.SelectNone):
		for i := range m.rows {
			m.rows[i].Selected = false
		}
	case key.Matches(msg, m.keys.Filter):
		m.filter = m.filter.Next()
		m.rebuild(m.cursorKey())
		m.syncScroll()
	case key.Matches(msg, m.keys.Target):
		next := m.target.Next()
		m.setTarget(next)
		m.setStatus(StatusInfo, fmt.Sprintf("target level: %s", next.Label()))
	case key.Matches(msg, m.keys.RowNext):
		m.cycleRowTarget(1)
	case key.Matches(msg, m.keys.RowPrev):
		m.cycleRowTarget(-1)
	case key.Matches(msg, m.keys.Detail):
		m.showDetail = !m.showDetail
		m.syncScroll()
	case key.Matches(msg, m.keys.Issues):
		// Nothing to browse is a no-op with an explanation, not an empty pane
		// the user then has to find their way out of.
		if len(m.scanErrs) == 0 {
			m.setStatus(StatusInfo, "no issues were logged during the scan")
			break
		}
		m.showIssues = true
		m.issueCursor = 0
		m.issueOffset = 0
		m.syncIssueScroll()
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		m.syncScroll()
	case key.Matches(msg, m.keys.Apply):
		return m.handleApply()
	case key.Matches(msg, m.keys.ApplyRow):
		return m.handleApplyRow()
	}
	return m, nil
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

func (m Model) handleApply() (tea.Model, tea.Cmd) {
	cmd := m.beginApply(m.selectedRows())
	if cmd == nil {
		m.setStatus(StatusWarn, "nothing selected — press space to select updates")
		return m, nil
	}
	m.cancel() // a still-running scan would keep appending rows mid-apply
	return m, cmd
}

// handleApplyRow writes just the row under the cursor. It reads no selection and
// sets none, so it is the escape hatch for "this one, now" without disturbing a
// selection built up for A.
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
	case r.NoTarget:
		m.setStatus(StatusWarn, fmt.Sprintf("no %s release for this image — press T to retarget it", r.Target.Label()))
		return m, nil
	}

	cmd := m.beginApply([]Row{*r})
	m.cancel()
	return m, cmd
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
