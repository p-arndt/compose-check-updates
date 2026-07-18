package tui

import (
	"context"
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/padi2312/compose-check-updates/internal"
	"github.com/padi2312/compose-check-updates/internal/scanner"
)

// phase is the stage of the session. The UI is one program that walks forward
// through these; there is no way back, which keeps the key handling per phase
// small enough to read in one screen.
type phase int

const (
	phaseScanning phase = iota
	phaseBrowsing
	phaseApplying
	phaseRestartPrompt
	phaseRestarting
	phaseDone
)

// applyConcurrency bounds how many Update() calls are in flight. They serialise
// on a mutex inside internal anyway, so a high number would only pile up
// goroutines waiting for the lock.
const applyConcurrency = 4

type Model struct {
	opts  scanner.Options
	theme Theme
	keys  KeyMap

	phase   phase
	spinner spinner.Model

	// ctx is cancelled on quit so a scan still hitting registries stops instead
	// of writing into a channel nobody reads any more.
	ctx    context.Context
	cancel context.CancelFunc
	events <-chan scanner.Event

	rows    []Row
	visible []int // indices into rows that pass the current filter
	cursor  int   // index into visible
	offset  int   // first display line rendered, for scrolling
	filter  Filter
	// target is the level every row is pointed at unless the user has moved that
	// row individually. Filter hides rows; target changes what gets written.
	target Target

	// logs captures slog output for the lifetime of the program. The scan logs
	// from many goroutines and the default handler writes to the terminal, which
	// would paint over the alt screen; see run.go.
	logs *logCapture

	total   int // compose files discovered
	checked int // compose files finished, successfully or not

	scanErrs []error

	showDetail bool
	showHelp   bool

	width  int
	height int

	statusKind StatusKind
	statusText string

	// applyQueue holds row keys not yet started; applyActive counts in-flight
	// Update() calls. Together they give bounded concurrency without a
	// semaphore that would have to live outside the update loop.
	applyQueue  []string
	applyActive int

	// restartTargets is filled when the user answers yes to the restart prompt
	// and is consumed by Run after the alt screen is gone — docker writes
	// straight to stdout and would otherwise paint over the UI.
	restartTargets []internal.UpdateInfo

	err error
}

func NewModel(opts scanner.Options) Model {
	ctx, cancel := context.WithCancel(context.Background())

	theme := DefaultTheme()
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return Model{
		opts:    opts,
		theme:   theme,
		keys:    DefaultKeyMap(),
		phase:   phaseScanning,
		spinner: sp,
		ctx:     ctx,
		cancel:  cancel,
		filter:  FilterAll,
		// Major preserves the behaviour of every earlier release: the highest
		// available version is what a fresh session offers.
		target: TargetMajor,
		width:  80,
		height: 24,
	}
}

// WithLogCapture attaches the handler whose records the status line surfaces.
func (m Model) WithLogCapture(c *logCapture) Model {
	m.logs = c
	return m
}

// rowKey identifies a row across re-sorts and across the goroutines that apply
// updates. A compose file cannot pin the same image reference twice, so the
// file plus the full reference is unique.
func rowKey(r Row) string {
	return r.Update.FilePath + "\x00" + r.Update.FullImageName + "\x00" + r.Update.CurrentTag
}

func (m *Model) addRow(r Row) {
	key := m.cursorKey()

	// Rows keep arriving after the user has changed the global target, so a new
	// one is pointed at it immediately rather than showing the scanner's default.
	r.Target = m.target
	m.retarget(&r, m.target)

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

// cursorKey is the identity of the row under the cursor, or "" when the list is
// empty.
func (m Model) cursorKey() string {
	r := m.currentRow()
	if r == nil {
		return ""
	}
	return rowKey(*r)
}

// rebuild recomputes the visible set and puts the cursor back on the row it was
// on before, so inserting or filtering never moves the selection to a different
// image under the user's hands.
func (m *Model) rebuild(keepKey string) {
	m.visible = m.visible[:0]
	for i, r := range m.rows {
		if m.filter.Matches(r.Level) {
			m.visible = append(m.visible, i)
		}
	}

	if keepKey != "" {
		for vi, ri := range m.visible {
			if rowKey(m.rows[ri]) == keepKey {
				m.cursor = vi
				m.clampCursor()
				return
			}
		}
	}
	m.clampCursor()
}

func (m *Model) clampCursor() {
	if len(m.visible) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
}

func (m Model) currentRow() *Row {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return &m.rows[m.visible[m.cursor]]
}

func (m *Model) rowByKey(key string) *Row {
	for i := range m.rows {
		if rowKey(m.rows[i]) == key {
			return &m.rows[i]
		}
	}
	return nil
}

func (m *Model) moveCursor(delta int) {
	if len(m.visible) == 0 {
		return
	}
	m.cursor += delta
	m.clampCursor()
	m.syncScroll()
}

// retarget points one row at its tag for the given level. Rows that have no
// release at that level are marked NoTarget and deselected rather than quietly
// keeping the higher tag they were showing — applying a version the user did
// not pick is the one outcome this feature must never produce.
//
// Rows with no levels at all (a digest move, or a tag semver cannot read) are
// left untouched: there is nothing to choose between for them.
func (m *Model) retarget(r *Row, target Target) {
	if r.State != RowPending || len(r.Update.AvailableTargets()) == 0 {
		return
	}

	r.Target = target

	// SelectTarget reports whether the tag *changed*, which is false both when
	// there is nothing at this level and when the row already sits on it — so
	// availability is decided by TagForTarget, not by that bool.
	if r.Update.TagForTarget(string(target)) == "" {
		r.NoTarget = true
		r.Selected = false
		r.Level = ""
		return
	}

	r.Update.SelectTarget(string(target))
	r.NoTarget = false
	r.Level = r.Update.UpdateLevel()
}

// setTarget re-points every row and rebuilds the view, because a row that lost
// its update also lost the level the filter matches on.
func (m *Model) setTarget(target Target) {
	m.target = target
	for i := range m.rows {
		m.retarget(&m.rows[i], target)
	}
	m.rebuild(m.cursorKey())
	m.syncScroll()
}

// cycleRowTarget moves the highlighted row to its next available level, staying
// inside AvailableTargets so the cycle only ever offers versions that exist.
// delta is +1 or -1.
func (m *Model) cycleRowTarget(delta int) {
	r := m.currentRow()
	if r == nil || r.State != RowPending {
		return
	}

	avail := r.Update.AvailableTargets()
	if len(avail) == 0 {
		m.setStatus(StatusWarn, "no alternative versions for this image")
		return
	}

	// Match on the level of the tag actually selected rather than the requested
	// one: TagForTarget degrades downwards, so a row asked for "major" can be
	// sitting on its patch release, and the cycle must continue from there.
	i := -1
	for j, t := range avail {
		if t == r.Level || Target(t) == r.Target {
			i = j
			break
		}
	}
	switch {
	case i < 0 && delta < 0:
		i = len(avail) - 1
	case i < 0:
		i = 0
	default:
		i = ((i+delta)%len(avail) + len(avail)) % len(avail)
	}

	key := rowKey(*r)
	m.retarget(r, Target(avail[i]))
	m.setStatus(StatusInfo, fmt.Sprintf("%s → %s (%s)", r.Update.ImageName, r.Update.LatestTag, r.Target.Label()))
	m.rebuild(key)
	m.syncScroll()
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

func (m *Model) setStatus(kind StatusKind, text string) {
	m.statusKind = kind
	m.statusText = text
}

// displayIndex maps a visible-row index to its line in the rendered list, which
// also contains one header line per compose file.
func (m Model) displayIndex(vi int) int {
	headers := 0
	last := ""
	for i := 0; i <= vi && i < len(m.visible); i++ {
		p := m.rows[m.visible[i]].FilePath()
		if p != last {
			headers++
			last = p
		}
	}
	return vi + headers
}

func (m Model) displayCount() int {
	if len(m.visible) == 0 {
		return 0
	}
	return m.displayIndex(len(m.visible)-1) + 1
}

// syncScroll nudges the window just far enough to keep the cursor on screen,
// rather than recentring, so paging feels like a terminal pager.
func (m *Model) syncScroll() {
	h := m.listHeight()
	total := m.displayCount()

	if total <= h {
		m.offset = 0
		return
	}

	// The file header above the cursor row should stay visible together with it.
	ci := m.displayIndex(m.cursor)
	top := ci
	if m.startsGroup(m.cursor) {
		top = ci - 1
	}

	if top < m.offset {
		m.offset = top
	}
	if ci >= m.offset+h {
		m.offset = ci - h + 1
	}
	if m.offset > total-h {
		m.offset = total - h
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// startsGroup reports whether the visible row at vi is the first of its compose
// file, i.e. whether a file header is drawn directly above it.
func (m Model) startsGroup(vi int) bool {
	if vi <= 0 || vi >= len(m.visible) {
		return vi == 0 && len(m.visible) > 0
	}
	return m.rows[m.visible[vi]].FilePath() != m.rows[m.visible[vi-1]].FilePath()
}
