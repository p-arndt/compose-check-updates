package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/p-arndt/compose-check-updates/internal"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

type scanStartedMsg struct{ events <-chan scanner.Event }

// The floating-tag scan is a second, much smaller run with its own channel: its
// events may not touch the file counters the first scan owns.
type floatingStartedMsg struct{ events <-chan scanner.Event }
type floatingEventMsg struct{ ev scanner.Event }
type floatingDoneMsg struct{}
type scanEventMsg struct{ ev scanner.Event }

// recheckDoneMsg carries the answer to a single image being looked at again
// after its settings changed. The row key travels with it because the list may
// have been re-sorted, filtered or folded in the meantime.
type recheckDoneMsg struct {
	key string
	ev  scanner.Event
	err error
}
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

// startFloatingScan resolves the digests behind the bare floating tags, which the
// ordinary scan skipped because it was not asked for them.
func (m Model) startFloatingScan() tea.Msg {
	events, err := scanner.ScanPins(m.ctx, m.opts)
	if err != nil {
		// Not fatal: the list the user already has is unaffected, so this is one
		// more entry for the issues pane rather than a reason to quit.
		return floatingEventMsg{ev: scanner.Event{Kind: scanner.EventError, Err: err}}
	}
	return floatingStartedMsg{events: events}
}

// waitForFloatingEvent is waitForEvent for the second channel. Two channels
// rather than one, so a floating-tag event can never be mistaken for scan
// progress.
func waitForFloatingEvent(events <-chan scanner.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return floatingDoneMsg{}
		}
		return floatingEventMsg{ev: ev}
	}
}

// waitForEvent reads exactly one event and re-arms itself from Update. Draining
// the channel in a goroutine instead would only show rows once the scan is over.
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

// recheckImage asks for one image to be resolved again, off the UI thread. It is
// what a changed versioning scheme costs: the requests of a single repository,
// rather than the whole scan the setting used to need.
func recheckImage(opts scanner.Options, r Row) tea.Cmd {
	key := rowKey(r)
	target := r.Update
	return func() tea.Msg {
		ev, err := scanner.CheckImage(opts, target)
		return recheckDoneMsg{key: key, ev: ev, err: err}
	}
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
	case row.Update.HasNewVersion(m.opts.Major, m.opts.Minor, m.opts.Patch):
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
		m.rows = append(m.rows[:i], m.rows[i+1:]...)
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
// which pass -1 for the whole list. It is collapse-blind — folding is display
// only, so both keys act on every row the filter keeps under the node — and the
// status line names the scope, since rows may change off screen. An empty or
// fully filtered list has no node to scope to and falls back to a full sweep.
//
// The two directions are deliberately asymmetric about the filter: selecting only
// adds rows the filter shows, while deselecting sweeps the scope regardless, so
// `n` can never leave a selected row that no header reports.
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
func (m *Model) cycleTarget() { m.setTargetAnnounced(m.target.Next()) }

// setTargetAnnounced is setTarget plus the status line, so every way in leaves
// the same trace.
func (m *Model) setTargetAnnounced(t Target) {
	m.setTarget(t)
	m.setStatus(StatusInfo, fmt.Sprintf("target level: %s", t.Label()))
}

// Nothing to browse is a no-op with an explanation rather than an empty pane.
// toggleFloating lists or hides the floating-tag rows, fetching their digests the
// first time they are asked for: the scan only resolved them when the setting was
// already on, and a run that was told not to pin must not spend the requests
// anyway. Returns the command that does the fetching, or nil when there is
// nothing left to fetch.
func (m *Model) toggleFloating() tea.Cmd {
	m.showFloating = !m.showFloating

	// Hidden rows may not stay selected: `A` would then write a digest into a
	// line the user cannot see, and the apply count would name rows no header
	// reports.
	if !m.showFloating {
		for i := range m.rows {
			if m.rows[i].Level == internal.LevelPin {
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
		if r.Level == internal.LevelPin {
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
