package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/p-arndt/compose-check-updates/internal"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
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
	visible []int   // indices into rows that pass the current filter
	entries []entry // the rendered lines: tree headers plus the rows they show
	cursor  int     // index into entries — headers are navigable too
	offset  int     // first display line rendered, for scrolling
	filter  Filter
	// nodes is the directory tree the headers are drawn from, rebuilt alongside
	// entries because a filter change can remove whole directories from it.
	nodes []node
	// nodeByKey and nodeByFile index nodes for the two lookups the list does on
	// every keystroke: by node key (headers) and by raw compose file path (rows,
	// which carry the scanner's path rather than the normalised key).
	nodeByKey  map[string]int
	nodeByFile map[string]int
	// collapsed folds a level of the tree away. It is keyed by node key — any
	// path prefix, not only a file — rather than by index so it survives the
	// re-sorts a streaming scan causes, and it is display-only: a folded row
	// keeps its selection and is still applied.
	collapsed map[string]bool
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

	// The issues pane browses scanErrs in full. It takes over the middle of the
	// screen and keeps its own cursor, so returning to the list lands the user
	// exactly where they left it.
	showIssues  bool
	issueCursor int
	issueOffset int

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
		opts:      opts,
		theme:     theme,
		keys:      DefaultKeyMap(),
		phase:     phaseScanning,
		spinner:   sp,
		ctx:       ctx,
		cancel:    cancel,
		filter:    FilterAll,
		collapsed: make(map[string]bool),
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

// headerKeyPrefix marks a tree header's cursor identity. A rowKey always starts
// with a file path, so this byte cannot collide with one.
const headerKeyPrefix = "\x01"

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

// rebuild recomputes the visible set and the rendered entries, then puts the
// cursor back on what it was on before, so inserting or filtering never moves
// the selection to a different image under the user's hands.
func (m *Model) rebuild(keepKey string) {
	m.visible = m.visible[:0]
	for i, r := range m.rows {
		if m.filter.Matches(r.Level) {
			m.visible = append(m.visible, i)
		}
	}

	// The tree is derived purely from the visible rows, so a filter that empties
	// a directory removes that directory's headers too rather than leaving the
	// user folds that open onto nothing.
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

	// One header per node in depth-first order, then a file node's rows unless
	// it is folded. A collapsed node keeps its own header — only its subtree
	// goes — so its content is never silently gone from the list. Parents always
	// precede their children, so hiding propagates in this single pass.
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
		// The entry is gone — folded away, or filtered out. Landing on the
		// nearest header still on screen keeps the cursor where the user was
		// looking instead of on whatever row the old index now points at.
		if i := m.ancestorHeader(keyGroup(keepKey)); i >= 0 {
			m.cursor = i
		}
	}
	m.clampCursor()
}

// ancestorHeader is the entry index of the deepest header still drawn for a
// path — the node itself when it survived, otherwise the closest ancestor of
// it. The prefix search is what makes this work for a path whose node is gone
// entirely, which is the case a filter change produces.
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

func (m *Model) clampCursor() {
	if len(m.entries) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
}

// currentEntry is the list line under the cursor.
func (m Model) currentEntry() (entry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return entry{}, false
	}
	return m.entries[m.cursor], true
}

// currentRow is the highlighted row, or nil when the cursor sits on a file
// header — which is what makes every per-row key a no-op there.
func (m Model) currentRow() *Row {
	e, ok := m.currentEntry()
	if !ok || e.kind != entryRow {
		return nil
	}
	return &m.rows[e.row]
}

// cursorGroup is the key of the node the cursor is in, whether it sits on that
// node's header or on a row inside the file it stands for.
func (m Model) cursorGroup() string {
	n := m.cursorNode()
	if n < 0 {
		return ""
	}
	return m.nodes[n].key
}

// toggleGroup folds or unfolds one node of the tree, at any depth. Rebuilding
// on the current cursor key is what moves the cursor up onto the header when
// the row it was sitting on has just been folded away.
func (m *Model) toggleGroup(key string) {
	if key == "" {
		return
	}
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}
	m.collapsed[key] = !m.collapsed[key]
	m.rebuild(m.cursorKey())
	m.syncScroll()
}

// setAllCollapsed folds or unfolds every node at every depth at once. Directory
// levels are included, so collapsing all leaves just the roots — which is the
// whole point of the key on a deep tree.
func (m *Model) setAllCollapsed(v bool) {
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}
	for _, n := range m.nodes {
		m.collapsed[n.key] = v
	}
	m.rebuild(m.cursorKey())
	m.syncScroll()
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
	if len(m.entries) == 0 {
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

// moveIssueCursor walks the issues pane by whole issues, not by wrapped lines,
// so a long entry is never something the user has to scroll through blind.
func (m *Model) moveIssueCursor(delta int) {
	if len(m.scanErrs) == 0 {
		m.issueCursor, m.issueOffset = 0, 0
		return
	}
	m.issueCursor += delta
	if m.issueCursor < 0 {
		m.issueCursor = 0
	}
	if m.issueCursor >= len(m.scanErrs) {
		m.issueCursor = len(m.scanErrs) - 1
	}
	m.syncIssueScroll()
}

// syncIssueScroll keeps the highlighted issue on screen, pinning its first line
// to the top when the entry alone is taller than the pane.
func (m *Model) syncIssueScroll() {
	if len(m.scanErrs) == 0 {
		m.issueCursor, m.issueOffset = 0, 0
		return
	}
	if m.issueCursor < 0 {
		m.issueCursor = 0
	}
	if m.issueCursor >= len(m.scanErrs) {
		m.issueCursor = len(m.scanErrs) - 1
	}

	lines, starts := m.issueLines()
	h := m.listHeight()
	if len(lines) <= h {
		m.issueOffset = 0
		return
	}

	top := starts[m.issueCursor]
	bottom := len(lines)
	if m.issueCursor+1 < len(starts) {
		bottom = starts[m.issueCursor+1]
	}

	if top < m.issueOffset {
		m.issueOffset = top
	}
	if bottom > m.issueOffset+h {
		m.issueOffset = bottom - h
	}
	if m.issueOffset > top {
		m.issueOffset = top
	}
	if m.issueOffset > len(lines)-h {
		m.issueOffset = len(lines) - h
	}
	if m.issueOffset < 0 {
		m.issueOffset = 0
	}
}

// displayIndex maps a visible-row index to its line in the rendered list, or -1
// when the row's group is collapsed and it is not on screen at all.
func (m Model) displayIndex(vi int) int {
	if vi < 0 || vi >= len(m.visible) {
		return -1
	}
	ri := m.visible[vi]
	for i, e := range m.entries {
		if e.kind == entryRow && e.row == ri {
			return i
		}
	}
	return -1
}

// displayCount is how many lines the list renders. Since headers became entries
// this is simply their count — no header arithmetic on top of the row count.
func (m Model) displayCount() int { return len(m.entries) }

// syncScroll nudges the window just far enough to keep the cursor on screen,
// rather than recentring, so paging feels like a terminal pager.
func (m *Model) syncScroll() {
	h := m.listHeight()
	total := len(m.entries)

	if total <= h {
		m.offset = 0
		return
	}

	ci := m.cursor
	if ci < 0 {
		ci = 0
	}
	if ci >= total {
		ci = total - 1
	}

	// The file header above the cursor row should stay visible together with it,
	// so a row never appears detached from the file it belongs to.
	top := ci
	if ci > 0 && m.entries[ci].kind == entryRow && m.entries[ci-1].kind == entryHeader {
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
