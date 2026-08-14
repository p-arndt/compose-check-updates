package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The bar is where the decisions that are not about any one row live: what the
// list shows, what level every row is pointed at, and the two things you do to
// the run as a whole. They used to be keys and nothing else — f, t, i, A — which
// is fine once you know them and invisible until you do.
//
// It is on screen from the first frame. That is the whole point: there is
// nothing to open, so there is no key standing between a user and finding out
// what this program can do. Every stop still names its key, because the bar is
// meant to teach the shortcut, not to replace it.
//
// It follows the same rule as everything else here: the arrows move, space and
// enter act on the stop under the cursor. ←/→ walk the stops, ↑/↓ go back down
// into the list, space and enter step a value or press a button. Every key it
// does not claim falls straight through to the list.

// barKind is what a stop does when it is acted on: a value steps, a button
// fires.
type barKind int

const (
	barValue barKind = iota
	barButton
)

// barAct is which button. Values carry no action: what they change is decided
// by their field.
type barAct int

const (
	barActNone barAct = iota
	barActIssues
	barActApply
)

// barField is which value a stop steps.
type barField int

const (
	barFieldShow barField = iota
	barFieldTarget
)

// barStop is one station on the bar.
type barStop struct {
	kind  barKind
	field barField
	act   barAct
	label string
	value string // values only
	hint  string // the key that does the same thing
	off   bool   // shown, but pressing it would do nothing
}

// barStops is the bar as it stands. Everything that draws it or acts on it goes
// through here, so what is on screen and what tab walks cannot disagree.
//
// The stops never come and go: a bar that dropped its issues button when there
// was nothing to report would move every stop after it, and a row of controls
// that shifts under the cursor is worse than one with a greyed-out entry.
func (m Model) barStops() []barStop {
	n := m.selectedCount()
	return []barStop{
		{
			kind: barValue, field: barFieldShow,
			label: "show", value: m.filter.Label(), hint: hintFor(m.keys.Filter),
		},
		{
			kind: barValue, field: barFieldTarget,
			label: "target", value: m.target.Label(), hint: hintFor(m.keys.Target),
		},
		{
			kind: barButton, act: barActIssues,
			label: fmt.Sprintf("issues %d", len(m.scanErrs)), hint: hintFor(m.keys.Issues),
			off: len(m.scanErrs) == 0,
		},
		{
			kind: barButton, act: barActApply,
			label: fmt.Sprintf("apply %d", n), hint: hintFor(m.keys.Apply),
			off: n == 0,
		},
	}
}

// hintFor is a binding's own help key, so the bar can never name a key the map
// does not bind.
func hintFor(b key.Binding) string { return b.Help().Key }

// --- focus --------------------------------------------------------------

// tab answers one question, and it is always the same one: what is the cursor
// on? On an image it opens the detail column, because that is what describes
// the image. On anything else — a directory or file header — there is nothing
// to describe, so it goes up to the bar. tab in the column comes back to the
// list.
//
// It deliberately asks the cursor rather than the focus. Asking the focus meant
// that arrowing onto an image while the bar had the keyboard left tab stepping
// along the bar, when the thing under the cursor had plainly become a row with
// details to show.
//
// Stepping along the bar is `m`'s job for the same reason: it is the key that
// means "the bar", so pressing it again means "the next thing on it". That
// works from a row, where tab is busy answering about the row.

// advanceFocus is what tab does, from wherever the keyboard currently is.
func (m *Model) advanceFocus() {
	if m.focus == focusSide {
		m.focus = focusList
		return
	}
	if m.currentRow() != nil && sidebarWidth(m.width) > 0 {
		m.focus = focusSide
		return
	}
	m.stepBarFocus()
}

// openBar is what `m` does: onto the bar, then one stop further each time, and
// back to the list once it runs out. It reaches the bar from the list and from
// the detail column alike.
func (m *Model) openBar() { m.stepBarFocus() }

// stepBarFocus enters the bar, or moves to the next stop when it is already
// there. Entering is all tab and `m` have to do now: once the keyboard is on
// the bar, ←/→ are what move along it.
func (m *Model) stepBarFocus() {
	if m.focus != focusBar {
		m.enterBar(0)
		return
	}
	m.stepStop(1)
}

// retreatFocus is shift+tab: backwards along the bar, so a stop overshot costs
// one keypress rather than a lap. Anywhere else it is the way back to the list.
func (m *Model) retreatFocus() {
	if m.focus != focusBar {
		m.focus = focusList
		return
	}
	if m.barStop > 0 {
		m.barStop--
		return
	}
	m.focus = focusList
}

// enterBar puts the keyboard on a stop, or leaves it with the list when there
// is no bar to enter.
func (m *Model) enterBar(stop int) {
	stops := m.barStops()
	if len(stops) == 0 || stop < 0 || stop >= len(stops) {
		m.focus = focusList
		return
	}
	m.focus, m.barStop = focusBar, stop
}

// --- keys ---------------------------------------------------------------

// handleBarKey reads the keys the bar claims while it has the focus.
//
// It claims both arrows now. Horizontal ones move between stops; vertical ones
// go back down into the list, which is where they point. Letting them fall
// through was the confusing part: the keyboard said it was on the bar while the
// list scrolled underneath, so the cursor ended up somewhere the user had not
// watched it go.
//
// Everything it does not claim still reaches the list.
func (m Model) handleBarKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Focus), key.Matches(msg, m.keys.BarNext):
		m.stepStop(1)
		return true, m, nil
	case key.Matches(msg, m.keys.FocusPrev), key.Matches(msg, m.keys.BarPrev):
		m.stepStop(-1)
		return true, m, nil
	case key.Matches(msg, m.keys.IssuesClose),
		key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down):
		m.focus = focusList
		return true, m, nil
	case key.Matches(msg, m.keys.ValueNext):
		model, cmd := m.pressBar(1)
		return true, model, cmd
	case key.Matches(msg, m.keys.ValuePrev):
		model, cmd := m.pressBar(-1)
		return true, model, cmd
	}
	return false, m, nil
}

// stepStop moves along the bar, wrapping at both ends. Wrapping rather than
// falling off is what makes ←/→ read as movement within one strip: the way out
// is ↓ and esc, and having three ways out would only blur what each one means.
func (m *Model) stepStop(delta int) {
	stops := m.barStops()
	if len(stops) == 0 {
		m.focus = focusList
		return
	}
	m.barStop = stepIndex(len(stops), m.barStop, delta)
}

// pressBar acts on the stop under the cursor: a value steps by delta, a button
// is pressed. Stepping a button backwards is meaningless, so only forward
// presses it.
func (m Model) pressBar(delta int) (tea.Model, tea.Cmd) {
	stops := m.barStops()
	if m.barStop < 0 || m.barStop >= len(stops) {
		return m, nil
	}
	s := stops[m.barStop]

	if s.kind == barValue {
		switch s.field {
		case barFieldShow:
			m.setFilter(stepFilter(m.filter, delta))
		case barFieldTarget:
			m.setTargetAnnounced(stepTarget(m.target, delta))
		}
		return m, nil
	}

	if s.off || delta < 0 {
		return m, nil
	}
	switch s.act {
	case barActIssues:
		m.openIssues()
		m.focus = focusList // the pane replaces the list, so the bar gives the keyboard back
	case barActApply:
		m.focus = focusList
		return m.handleApply()
	}
	return m, nil
}

// stepFilter and stepTarget are the cycles ←/→ walk. Filter.Next and
// Target.Next only go forward; a value has to step back as well.
func stepFilter(f Filter, delta int) Filter {
	all := []Filter{FilterAll, FilterMajor, FilterMinor, FilterPatch, FilterDigest}
	return all[stepIndex(len(all), int(f), delta)]
}

func stepTarget(t Target, delta int) Target {
	cur := 0
	for i, c := range targetOrder {
		if c == t {
			cur = i
		}
	}
	return targetOrder[stepIndex(len(targetOrder), cur, delta)]
}

func stepIndex(n, cur, delta int) int { return ((cur+delta)%n + n) % n }

// --- rendering ----------------------------------------------------------

// barGap is the space between two stops. Wide enough that the chevrons of one
// value do not read as belonging to the next.
const barGap = "   "

// barLine renders the bar into one line. One line, always: it sits in the fixed
// block above the pane, and a block whose height moved would take the list's
// height with it.
func (m Model) barLine(width int) string {
	stops := m.barStops()
	parts := make([]string, 0, len(stops))
	for i, s := range stops {
		parts = append(parts, m.barStopText(s, m.focus == focusBar && i == m.barStop))
	}

	line := strings.Join(parts, barGap)

	// The hint for the focused stop, if what is left of the line can hold it.
	if m.focus == focusBar && m.barStop < len(stops) {
		if h := stops[m.barStop].hint; h != "" {
			tail := m.theme.dim().Render(barGap + "(" + h + ")")
			if lipgloss.Width(line)+lipgloss.Width(tail) <= width {
				line += tail
			}
		}
	}
	return fit(" "+line, width)
}

// barStopText renders one stop.
//
// The focus is carried by colour, weight and an underline rather than by a
// background. A background does not survive here: every segment of a stop is
// styled on its own — dim brackets, a bright value — and each of those emits a
// reset that ends the background the moment it closes, leaving the highlight
// smeared across the first bracket and nothing else. Merging the focus into
// each segment's own style is the only composition that holds, and an
// underlined accent reads as selected on a light terminal as well as a dark
// one, which a hardcoded highlight colour does not.
func (m Model) barStopText(s barStop, focused bool) string {
	edge := m.theme.dim()
	label := m.theme.dim()
	value := lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true)

	if focused {
		edge = lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Underline(true)
		label = lipgloss.NewStyle().Foreground(m.theme.Accent).Underline(true)
		value = value.Foreground(m.theme.Accent).Underline(true)
	}
	if s.off {
		// A stop that would do nothing stays dim even with the keyboard on it,
		// or focus would read as "this is ready to press".
		value = m.theme.dim()
		if focused {
			value = value.Underline(true)
		}
	}

	if s.kind == barValue {
		return label.Render(s.label+" ") + edge.Render("‹ ") + value.Render(s.value) + edge.Render(" ›")
	}
	return edge.Render("[ ") + value.Render(s.label) + edge.Render(" ]")
}
