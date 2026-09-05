package tui

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, m.startScan}
	if m.logs != nil {
		cmds = append(cmds, pollLogs())
	}
	return tea.Batch(cmds...)
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

	case floatingStartedMsg:
		m.floatingEvents = msg.events
		return m, waitForFloatingEvent(msg.events)

	case floatingEventMsg:
		switch msg.ev.Kind {
		case scanner.EventUpdate:
			m.addRow(Row{Update: msg.ev.Update, Level: msg.ev.Level})
			m.syncScroll()
		case scanner.EventError:
			m.scanErrs = append(m.scanErrs, msg.ev.Err)
		}
		return m, waitForFloatingEvent(m.floatingEvents)

	case floatingDoneMsg:
		m.floatingResolved = true
		m.drainLogs()
		m.rebuild(m.cursorKey())
		m.syncScroll()
		m.setStatus(StatusInfo, m.floatingSummary())
		return m, nil

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

	case recheckDoneMsg:
		m.applyRecheck(msg)
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

// applyRecheck folds the answer back into the row it was asked for. A row that
// now resolves to nothing at all is dropped: it was only ever on screen because
// ccu could not read it, and leaving it there would claim it still cannot.
func (m *Model) applyRecheck(msg recheckDoneMsg) {
	row := m.rowByKey(msg.key)
	if row == nil {
		return
	}

	if msg.err != nil {
		m.setStatus(StatusError, fmt.Sprintf("could not re-check %s: %v", row.Update.ImageName, msg.err))
		return
	}

	info := msg.ev.Update
	image := info.ImageName

	// Selection and applied state belong to the row, not to what the registry just
	// said, so only the resolved half is replaced.
	row.Update = info
	row.Level = msg.ev.Level
	m.retarget(row, m.target)
	m.refreshPins()

	switch {
	case info.IsUnreadable():
		m.setStatus(StatusWarn, info.UnreadableMessage)
	case row.Update.HasNewVersion():
		m.setStatus(StatusSuccess, fmt.Sprintf("%s → %s", image, row.Update.LatestTag))
	default:
		m.dropRow(msg.key)
		m.setStatus(StatusSuccess, fmt.Sprintf("%s reads fine now and is up to date", image))
		return
	}

	m.rebuild(m.cursorKey())
	m.syncScroll()
}

// dropRow removes a row and rebuilds around it, keeping the cursor on whatever
// survives nearest to where it was.
func (m *Model) dropRow(key string) {
	for i := range m.rows {
		if rowKey(m.rows[i]) != key {
			continue
		}
		keep := m.cursorKey()
		if keep == key {
			// The line under the cursor is the one going away, so the cursor is
			// handed to its file header rather than to whatever slides up into it.
			keep = headerKeyPrefix + m.rows[i].FilePath()
		}
		// slices.Delete zeroes the vacated tail, so the dropped row is not kept
		// alive by the backing array the way the append form left it.
		m.rows = slices.Delete(m.rows, i, i+1)
		m.rebuild(keep)
		m.syncScroll()
		return
	}
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
