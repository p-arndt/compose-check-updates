package tui

import (
	"context"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/p-arndt/compose-check-updates/internal/config"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

// phase is the stage of the session. It only ever walks forward, which keeps
// the per-phase key handling small.
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
	// floatingEvents is the second, much smaller scan: the digests behind the
	// floating tags, fetched only once the user asks to see them.
	floatingEvents <-chan scanner.Event

	rows    []Row
	visible []int   // indices into rows that pass the current filter
	entries []entry // the rendered lines: tree headers plus the rows they show
	cursor  int     // index into entries — headers are navigable too
	offset  int     // first display line rendered, for scrolling
	filter  Filter
	// showFloating is whether the floating-tag rows — a "latest" offered the
	// digest it resolves to — are listed. Named for the tag, not for the pin, so
	// it cannot be confused with Row.Pin, which is the saved cap.
	showFloating bool
	// floatingResolved records that the digests behind the floating tags have been
	// fetched, which the ordinary scan only does when the setting was on. Listing
	// them otherwise has to go and get them first, once.
	floatingResolved bool
	// nodes is the directory tree the headers are drawn from, rebuilt alongside
	// entries because a filter change can remove whole directories from it.
	nodes []node
	// nodeByKey and nodeByFile index nodes for the two lookups the list does on
	// every keystroke: by node key (headers) and by raw compose file path (rows,
	// which carry the scanner's path rather than the normalised key).
	nodeByKey  map[string]int
	nodeByFile map[string]int
	// collapsed folds a level of the tree away, keyed by node key rather than by
	// index so it survives the re-sorts a streaming scan causes. Display-only: a
	// folded row keeps its selection and is still applied.
	collapsed map[string]bool
	// target is the level every row is pointed at unless the user has moved that
	// row individually. Filter hides rows; target changes what gets written.
	target Target

	// pins are the caps already recorded on disk, one Config per scope rather
	// than merged: a pin is toggled inside the scope the user picked, so clearing
	// a project cap must not be blocked by a global file saying the same thing.
	pins map[pinScope]config.Config

	// setCap writes a pin; a field rather than a direct config call so a test can
	// observe writes. An empty level means "remove the cap for this image".
	setCap func(scope pinScope, image string, max policy.Level) error

	// setVersioning writes the scheme an image's tags are read under, for the same
	// reason and in the same shape as setCap. An empty scheme means "let this
	// image take the run's default again".
	setVersioning func(scope pinScope, image string, versioning policy.Versioning) error

	// focus is which half of the frame the keyboard is talking to. The list holds
	// it by default, so every reflex key keeps its old meaning.
	focus focusArea

	// barStop is which station on the bar has the keyboard, meaningful only while
	// focus is focusBar.
	barStop int

	// sideField is the sidebar's own cursor, meaningful only while focus is
	// focusSide.
	sideField sideField

	// logs captures slog output for the lifetime of the program. The scan logs
	// from many goroutines and the default handler writes to the terminal, which
	// would paint over the alt screen; see run.go.
	logs *logCapture

	total   int // compose files discovered
	checked int // compose files finished, successfully or not

	scanErrs []error

	showDetail bool
	showHelp   bool

	// The issues pane browses scanErrs in full. It keeps its own cursor, so
	// returning to the list lands where the user left it.
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
	restartTargets []check.Update

	err error
}

func NewModel(opts scanner.Options) Model {
	ctx, cancel := context.WithCancel(context.Background())

	theme := DefaultTheme()
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return Model{
		opts:          opts,
		setCap:        writeCap(opts.Root),
		setVersioning: writeVersioning(opts.Root),
		pins:          make(map[pinScope]config.Config),
		theme:         theme,
		keys:          DefaultKeyMap(),
		phase:         phaseScanning,
		spinner:       sp,
		ctx:           ctx,
		cancel:        cancel,
		filter:        FilterAll,
		// The scan resolved them only if it was asked to, which is the same
		// condition under which they are listed to begin with.
		showFloating:     opts.Policies.PinFloating,
		floatingResolved: opts.Policies.PinFloating,
		collapsed:        make(map[string]bool),
		// The highest available version is what a fresh session offers.
		target: TargetMajor,
		width:  80,
		height: 24,
	}
}

// WithPins attaches the caps already on disk, so the list can mark a pinned
// image and `p` can tell setting one from clearing it.
func (m Model) WithPins(project, global config.Config) Model {
	m.pins = map[pinScope]config.Config{pinProject: project, pinGlobal: global}
	m.refreshPins()
	return m
}

// WithLogCapture attaches the handler whose records the status line surfaces.
func (m Model) WithLogCapture(c *logCapture) Model {
	m.logs = c
	return m
}

// headerKeyPrefix marks a tree header's cursor identity. A rowKey always starts
// with a file path, so this byte cannot collide with one.
const headerKeyPrefix = "\x01"

func (m *Model) setStatus(kind StatusKind, text string) {
	m.statusKind = kind
	m.statusText = text
}
