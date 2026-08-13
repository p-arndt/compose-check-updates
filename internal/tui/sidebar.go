package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/p-arndt/compose-check-updates/internal/config"
)

// The sidebar is the one place a single image is decided: which release it
// moves to, and whether that choice is remembered past this run. It replaces
// the keys those two questions used to have — a key each for stepping the
// target, another for pinning, another for the scope — because a decision with
// three visible fields needs no keys of its own beyond a cursor.
//
// It follows the list rather than being opened: the row under the cursor is
// what it describes, so there is never a question of which image is meant.

// sidebarMinTotal is the terminal width below which the frame stays a single
// column. Two columns narrower than this leave the list too cramped to read a
// path in, and the list is the half that cannot be given up.
const sidebarMinTotal = 96

// sidebarWidth is how much of the frame the right column takes. Wide enough for
// a tag and its label, and capped so a very wide terminal spends the extra room
// on the list, where long paths live.
func sidebarWidth(total int) int {
	if total < sidebarMinTotal {
		return 0
	}
	w := total / 3
	if w > 38 {
		w = 38
	}
	return w
}

// sideField is one of the questions the sidebar asks. The order is the order
// they are rendered and cycled in.
type sideField int

const (
	fieldTarget sideField = iota
	fieldPin
	sideFieldCount
)

// focusArea is which half of the frame the keyboard is talking to. The list
// keeps the focus until it is deliberately handed over, so every reflex key
// still means what it meant before the sidebar existed.
type focusArea int

const (
	focusList focusArea = iota
	focusSide
)

// pinChoices are the values the Merken field cycles through, in the order shown.
// The empty Level is "not remembered", which is what an image starts as.
var pinChoices = []struct {
	scope pinScope
	level bool // whether this choice records a cap at all
	label string
}{
	{label: "no"},
	{scope: pinProject, level: true, label: "project"},
	{scope: pinGlobal, level: true, label: "global"},
}

// sidebarLines renders the right column for the row under the cursor, padded to
// exactly height lines so the two columns stay in step.
func (m Model) sidebarLines(width, height int) []string {
	out := make([]string, 0, height)

	r := m.currentRow()
	if r == nil {
		out = append(out, m.theme.dim().Render("no image selected"))
		return padLines(out, width, height)
	}

	u := r.Update
	out = append(out, m.theme.sideTitle(u.ImageName, width))
	out = append(out, "")
	out = append(out, m.theme.sideField("file", shortPath(u.FilePath), width))
	out = append(out, m.theme.sideField("now", u.CurrentTag, width))
	out = append(out, "")

	out = append(out, m.theme.sideHeading("target", m.focus == focusSide && m.sideField == fieldTarget))
	for _, line := range m.targetChoices(r, width) {
		out = append(out, line)
	}
	out = append(out, "")

	out = append(out, m.theme.sideHeading("remember", m.focus == focusSide && m.sideField == fieldPin))
	for _, line := range m.pinChoiceLines(r, width) {
		out = append(out, line)
	}

	return padLines(out, width, height)
}

// targetChoices renders one line per level this image actually has a release
// for, marking the one currently selected. A level the image does not publish
// is left out rather than shown as unavailable: the list is short enough that
// an absent line is read as "not offered", and a greyed one invites pressing it.
func (m Model) targetChoices(r *Row, width int) []string {
	avail := r.Update.AvailableTargets()
	if len(avail) == 0 {
		return []string{m.theme.dim().Render("  no other releases")}
	}

	lines := make([]string, 0, len(avail))
	for _, level := range avail {
		tag := r.Update.TagForTarget(level)
		selected := !r.NoTarget && string(r.Target) == level
		lines = append(lines, m.theme.sideChoice(level, tag, selected, width))
	}
	return lines
}

// pinChoiceLines renders the remember field. The scope's file is named next to
// it, because "project" and "global" only mean something once you can see which
// file each would write.
func (m Model) pinChoiceLines(r *Row, width int) []string {
	current := m.pinScopeOf(r.Update.ImageName)

	lines := make([]string, 0, len(pinChoices))
	for _, c := range pinChoices {
		selected := c.level == (r.Pin != "") && (!c.level || c.scope == current)
		hint := ""
		switch c.scope {
		case pinProject:
			hint = ".ccu.yaml"
		case pinGlobal:
			hint = "~/.config/ccu"
		}
		lines = append(lines, m.theme.sideChoice(c.label, hint, selected, width))
	}
	return lines
}

// pinScopeOf reports which scope holds a cap for this image, so the remember
// field can show which of the two files the value came from. Project wins, the
// same way it wins when the two are merged.
func (m Model) pinScopeOf(image string) pinScope {
	if m.pins[pinProject].MaxLevel(image) != "" {
		return pinProject
	}
	return pinGlobal
}

// cycleSideValue changes the focused field by delta. It is the only way the
// sidebar writes anything, so both questions go through one place.
func (m *Model) cycleSideValue(delta int) {
	r := m.currentRow()
	if r == nil {
		return
	}

	switch m.sideField {
	case fieldTarget:
		m.cycleRowTarget(delta)
	case fieldPin:
		m.cyclePin(r, delta)
	}
}

// cyclePin steps the remember field and writes the result immediately. Writing
// on the keypress rather than on leaving the pane is what makes the field honest:
// what it shows is what is on disk, with no unsaved state to lose.
func (m *Model) cyclePin(r *Row, delta int) {
	image := r.Update.ImageName
	cur := 0
	if r.Pin != "" {
		cur = 1
		if m.pinScopeOf(image) == pinGlobal {
			cur = 2
		}
	}

	next := ((cur+delta)%len(pinChoices) + len(pinChoices)) % len(pinChoices)
	choice := pinChoices[next]

	// Leaving a scope means clearing it, or the image would end up capped in two
	// files at once and the field could no longer say which one it is showing.
	if r.Pin != "" {
		if err := m.setCap(m.pinScopeOf(image), image, ""); err != nil {
			m.setStatus(StatusError, fmt.Sprintf("could not update %s: %v", image, err))
			return
		}
	}

	if !choice.level {
		m.applyPin(image, "", 0)
		m.setStatus(StatusSuccess, fmt.Sprintf("%s no longer capped", image))
		return
	}

	level := config.Level(r.Target.Label())
	if err := m.setCap(choice.scope, image, level); err != nil {
		m.setStatus(StatusError, fmt.Sprintf("could not save %s: %v", image, err))
		return
	}
	m.applyPin(image, level, choice.scope)
	m.setStatus(StatusSuccess, fmt.Sprintf("%s capped at %s (%s)", image, level, choice.label))
}

// applyPin records the new cap in the in-memory layers and restamps the rows, so
// the marker and the field agree with what was just written without re-reading
// the file.
func (m *Model) applyPin(image string, level config.Level, scope pinScope) {
	for s := range m.pins {
		cfg := m.pins[s]
		if cfg.Images != nil {
			delete(cfg.Images, image)
		}
		m.pins[s] = cfg
	}

	if level != "" {
		cfg := m.pins[scope]
		if cfg.Images == nil {
			cfg.Images = map[string]config.ImagePolicy{}
		}
		cfg.Images[image] = config.ImagePolicy{Max: level}
		m.pins[scope] = cfg
	}

	m.refreshPins()
}

// shortPath trims a compose file path to its last two segments. The sidebar has
// no room for an absolute path, and the two segments that identify a stack are
// the directory and the file name.
func shortPath(p string) string {
	parts := strings.Split(strings.ReplaceAll(p, "\\", "/"), "/")
	if len(parts) <= 2 {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// padLines fits every line to width and pads the block out to height, so the
// sidebar can be joined to the list line by line without either drifting.
func padLines(lines []string, width, height int) []string {
	out := make([]string, 0, height)
	for _, l := range lines {
		if len(out) == height {
			break
		}
		out = append(out, padRight(fit(l, width), width))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return out
}

// joinColumns glues the list and the sidebar together with a vertical rule.
// Both blocks are already exactly height lines, so this only has to concatenate.
func (m Model) joinColumns(left []string, right []string, leftWidth int) string {
	rule := m.theme.rule()
	out := make([]string, 0, len(left))
	for i := range left {
		l := padRight(fit(left[i], leftWidth), leftWidth)
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, l+rule+r)
	}
	return strings.Join(out, "\n")
}

// sidebarGutter is the width the vertical rule and its padding occupy.
const sidebarGutter = 3

var _ = lipgloss.Width
