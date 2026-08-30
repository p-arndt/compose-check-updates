// Package tui implements the interactive terminal UI shown by `ccu -i`.
//
// It consumes the streaming scanner, so rows appear while registries are still
// being queried.
package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// Filter is the set of update levels the list is currently showing. It is a
// display filter only — the scanner always resolves every level, so widening
// it never requires re-running the scan.
type Filter int

const (
	FilterAll Filter = iota
	FilterMajor
	FilterMinor
	FilterPatch
	FilterDigest
)

// Next cycles to the following filter, wrapping around at the end.
func (f Filter) Next() Filter {
	if f == FilterDigest {
		return FilterAll
	}
	return f + 1
}

// Label is the human-readable name used in the legend and footer.
func (f Filter) Label() string {
	switch f {
	case FilterMajor:
		return "major"
	case FilterMinor:
		return "minor"
	case FilterPatch:
		return "patch"
	case FilterDigest:
		return "digest"
	default:
		return "all"
	}
}

// Matches reports whether an update of the given level (as returned by
// check.Update.UpdateLevel) should be shown under this filter.
func (f Filter) Matches(level policy.Level) bool {
	if f == FilterAll {
		return true
	}
	return f.Label() == level.String()
}

// Target is the update level a row's tag is pointed at. Filter only decides what
// is *shown*; Target decides what the apply keys write into the compose file.
type Target = policy.Level

const (
	TargetPatch = policy.LevelPatch
	TargetMinor = policy.LevelMinor
	TargetMajor = policy.LevelMajor
)

// targetOrder is the cycle order, lowest-risk first. Major is last so the
// default wraps round to the most conservative choice on the first press.
var targetOrder = []Target{TargetPatch, TargetMinor, TargetMajor}

// nextTarget cycles to the following target, wrapping around at the end.
func nextTarget(t Target) Target {
	for i, c := range targetOrder {
		if c == t {
			return targetOrder[(i+1)%len(targetOrder)]
		}
	}
	return TargetMajor
}

// targetLabel is the human-readable name used in the legend and footer.
func targetLabel(t Target) string {
	if t == "" {
		return TargetMajor.String()
	}
	return t.String()
}

// RowState is what has happened to a row so far. Rows start Pending, become
// Applied or Failed once the user commits the selection.
type RowState int

const (
	RowPending RowState = iota
	RowApplied
	RowFailed
)

// Row is one image with an available update, as displayed in the list. Rows are
// flattened across compose files; FilePath groups them under file headers.
type Row struct {
	Update   check.Update
	Level    policy.Level // of the SELECTED tag
	Selected bool
	State    RowState
	Err      error // set when State is RowFailed

	// Target is the level this row's LatestTag currently points at. NoTarget means
	// the image has no release at that level: the row keeps its old LatestTag
	// internally but must be treated as having no update.
	Target   Target
	NoTarget bool

	// Pin is the cap recorded for this image, or "" when there is none. Carried on
	// the row rather than looked up while rendering, so the list line stays a pure
	// function of the row.
	Pin policy.Level
}

// FilePath is the compose file this row's image lives in.
func (r Row) FilePath() string { return r.Update.FilePath }

// Actionable reports whether the row may be selected and applied. An image ccu
// could not read has no tag to write, so it is listed but never ticked: `A` must
// not be able to sweep it up along with the rows that do have a target.
func (r Row) Actionable() bool {
	return !r.NoTarget && r.State == RowPending && !r.Update.IsUnreadable()
}

// otherTargets counts the alternative levels this image could be pointed at, so
// the list can hint that `T` would do something here.
func (r Row) otherTargets() int {
	n := len(r.Update.AvailableTargets())
	if n <= 1 {
		return 0
	}
	if r.NoTarget {
		return n
	}
	return n - 1
}

// pinScope is the config file a cap is written to. The user picks one per pin:
// capping an image in one project says something different from capping it
// everywhere.
type pinScope int

const (
	pinProject pinScope = iota
	pinGlobal
)

// Label is the word the prompt and the confirmation use for the scope.
func (s pinScope) Label() string {
	if s == pinGlobal {
		return "global"
	}
	return "project"
}

// entryKind distinguishes the two things a list line, and therefore the cursor,
// can be.
type entryKind int

const (
	entryHeader entryKind = iota
	entryRow
)

// entry is one rendered line of the list. Headers are entries in their own right
// so the cursor can land on a level of the tree and fold it, which also keeps the
// rendered line index and the cursor index the same number.
type entry struct {
	kind entryKind
	// path is the node key for a header — any prefix of the tree, not only a
	// file — and the compose file path for a row. A row keeps the raw path the
	// scanner reported so it still matches its rowKey and its Row.
	path string
	row  int // index into Model.rows; -1 on a header
	node int // index into Model.nodes; -1 on a row
}

// StatusKind classifies a transient status line rendered under the title.
type StatusKind int

const (
	StatusInfo StatusKind = iota
	StatusSuccess
	StatusWarn
	StatusError
)

// Theme collects every colour the UI uses, so the palette lives in one place.
type Theme struct {
	Major  lipgloss.Color
	Minor  lipgloss.Color
	Patch  lipgloss.Color
	Digest lipgloss.Color
	Pin    lipgloss.Color
	// Unreadable is the colour of a row nothing could be resolved for. It is a
	// warning, not a level: the row is on screen to be fixed, not to be applied.
	Unreadable lipgloss.Color
	Text       lipgloss.Color
	Dim        lipgloss.Color
	Accent     lipgloss.Color
	Success    lipgloss.Color
	Warn       lipgloss.Color
	Error      lipgloss.Color
	Highlight  lipgloss.Color // background of the cursor row
}
