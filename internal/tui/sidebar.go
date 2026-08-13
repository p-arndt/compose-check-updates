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
	fieldTarget sideField = iota // the release this run would apply
	fieldCap                     // the ceiling to remember, as a level of its own
	fieldScope                   // which config file remembers it
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

// capChoicesFor are the values the cap field steps through for one image. The
// cap names a level in its own right rather than borrowing whatever the target
// field happens to show: the target is what this run does, the cap is what every
// run from now on may not exceed, and reading one off the other made it
// impossible to set a cap without first moving a target the user did not want
// moved.
//
// Levels the image has no release for are still offered — a cap is a policy
// about the future, and an image with no major today may publish one tomorrow.
//
// Major is the exception, and only appears when it would mean something. On its
// own it is the same as no cap at all, since nothing in semver sits above it.
// It earns its place only when a global cap exists: the project file overrides
// the global one, so "major" there is how a project says the global ceiling does
// not apply to it.
func (m Model) capChoicesFor(image string) []config.Level {
	choices := []config.Level{"", config.LevelPatch, config.LevelMinor}
	if m.capInScope(pinGlobal, image) != "" {
		choices = append(choices, config.LevelMajor)
	}
	return choices
}

// scopeChoices are the files a cap can be remembered in, in the order the field
// steps through them.
var scopeChoices = []pinScope{pinProject, pinGlobal}

// sidebarLines renders the right column for the row under the cursor, within
// the height it is given.
//
// The fields come first when space runs out. A short terminal that dropped the
// cap field would leave the setting unreachable, whereas dropping the file path
// only costs a fact the user can see in the list anyway — so the lines are
// added in priority order rather than top to bottom.
func (m Model) sidebarLines(width, height int) []string {
	r := m.currentRow()
	if r == nil {
		return []string{m.theme.dim().Render(fit("no image selected", width))}
	}

	u := r.Update

	// Everything here has to survive, in this order: the image being described,
	// then each thing that can be changed about it.
	fields := []string{
		m.theme.sideTitle(u.ImageName, width),
		m.theme.sideValue("target", m.targetValue(r), m.focused(fieldTarget), width),
		m.theme.sideValue("cap", m.capValue(r), m.focused(fieldCap), width),
	}
	if r.Pin != "" {
		// The scope only exists once there is a cap to put somewhere. Showing it
		// permanently would ask the user to answer a question with no subject.
		fields = append(fields, m.theme.sideValue("save to", m.scopeValue(r, width), m.focused(fieldScope), width))
	}

	if height <= len(fields) {
		return fields[:max(height, 1)]
	}

	// Room to spare, so the context and the spacing go back in — the path first,
	// because it is the one that says which of two identically named services
	// this is.
	out := []string{fields[0]}
	rest := fields[1:]

	if height >= len(fields)+1 {
		out = append(out, m.theme.dim().Render(fit(shortPath(u.FilePath)+" · now "+u.CurrentTag, width)))
	}
	if height >= len(fields)+2 {
		out = append(out, "")
	}
	out = append(out, rest...)

	// Only while the list still holds the keyboard: once the sidebar has it the
	// footer names the same keys, and saying it twice on one frame is noise.
	if m.focus == focusList && height >= len(out)+2 {
		out = append(out, "", m.theme.dim().Render(fit("tab to change", width)))
	}

	return out
}

// focused reports whether f is the field the arrow keys would change.
func (m Model) focused(f sideField) bool {
	return m.focus == focusSide && m.sideField == f
}

// targetValue is the release this row would move to, named by its level as well
// as its tag: the level is the thing being chosen, the tag only the consequence.
func (m Model) targetValue(r *Row) string {
	focused := m.focused(fieldTarget)
	if r.NoTarget || len(r.Update.AvailableTargets()) == 0 {
		return m.theme.sideText("—", focused)
	}
	// The same badge the row carries in the list, so the level the field is set
	// to and the level the row shows are recognisably one thing.
	return m.theme.BadgeTight(r.Target.Label()) + " " + m.theme.sideText(r.Update.LatestTag, focused)
}

// capValue is the ceiling this image may never move past, phrased so the line
// reads as the rule it is rather than as a version number.
func (m Model) capValue(r *Row) string {
	focused := m.focused(fieldCap)
	if r.Pin == "" {
		return m.theme.sideText("off", focused)
	}

	value := m.theme.BadgeTight(string(r.Pin))

	// The one case where "major" is not a no-op is worth spelling out, because
	// on its own the word says nothing: it is only there to lift a ceiling the
	// global file set.
	if r.Pin == config.LevelMajor {
		value += m.theme.dim().Render("  lifts the global cap")
	}
	return value
}

// scopeValue names the file the cap is remembered in, with the path beside it:
// "project" and "global" only mean something once you can see which file each
// one is.
// scopeValue names the file the cap is remembered in. The path is a hint rather
// than the answer, so it is dropped when the column is too narrow to hold it —
// a truncated line would eat the closing chevron and make the field look broken
// rather than abbreviated.
func (m Model) scopeValue(r *Row, width int) string {
	focused := m.focused(fieldScope)

	name, path := "project", "  .ccu.yaml"
	if m.pinScopeOf(r.Update.ImageName) == pinGlobal {
		name, path = "global", "  ~/.config/ccu"
	}

	// The label, the chevrons and their spaces: what sideValue puts around the
	// value, and therefore what the value may not use.
	const chrome = 8 + 4
	if lipgloss.Width(name+path)+chrome > width {
		path = ""
	}
	return m.theme.sideText(name, focused) + m.theme.dim().Render(path)
}

// pinScopeOf reports which scope holds a cap for this image, so the cap field
// can say which of the two files the value came from. Project wins, the same
// way it wins when the two layers are merged.
func (m Model) pinScopeOf(image string) pinScope {
	if m.pins[pinProject].MaxLevel(image) != "" {
		return pinProject
	}
	return pinGlobal
}

// cycleSideValue changes the focused field by delta. It is the only way the
// sidebar writes anything, so every question goes through one place.
func (m *Model) cycleSideValue(delta int) {
	r := m.currentRow()
	if r == nil {
		return
	}

	switch m.sideField {
	case fieldTarget:
		m.cycleRowTarget(delta)
	case fieldCap:
		m.cycleCap(r, delta)
	case fieldScope:
		m.cycleScope(r, delta)
	}
}

// cycleCap steps the ceiling and writes it immediately. Writing on the keypress
// rather than on leaving the pane is what makes the field honest: what it shows
// is what is on disk, with no unsaved state to lose.
func (m *Model) cycleCap(r *Row, delta int) {
	image := r.Update.ImageName

	choices := m.capChoicesFor(image)
	cur := 0
	for i, c := range choices {
		if c == r.Pin {
			cur = i
			break
		}
	}
	next := choices[((cur+delta)%len(choices)+len(choices))%len(choices)]

	// A cap already recorded lives in one scope; a new one goes to the project
	// file, which is the narrower of the two and the easier to undo.
	scope := pinProject
	if r.Pin != "" {
		scope = m.pinScopeOf(image)
	}

	if !m.writeCapValue(scope, image, next) {
		return
	}

	if next == "" {
		m.setStatus(StatusSuccess, fmt.Sprintf("%s no longer capped", image))
		return
	}
	m.setStatus(StatusSuccess, fmt.Sprintf("%s capped at %s (%s)", image, next, scopeLabel(scope)))
}

// cycleScope moves an existing cap between the two files. There is nothing to
// move when no cap is set, and the field is not shown then either.
func (m *Model) cycleScope(r *Row, delta int) {
	if r.Pin == "" {
		return
	}

	image := r.Update.ImageName
	from := m.pinScopeOf(image)

	cur := 0
	for i, s := range scopeChoices {
		if s == from {
			cur = i
			break
		}
	}
	to := scopeChoices[((cur+delta)%len(scopeChoices)+len(scopeChoices))%len(scopeChoices)]
	if to == from {
		return
	}

	// Cleared from the file it is leaving before being written to the new one,
	// or the image ends up capped in both and the field can no longer say which
	// one it is showing.
	if err := m.setCap(from, image, ""); err != nil {
		m.setStatus(StatusError, fmt.Sprintf("could not update %s: %v", image, err))
		return
	}
	if !m.writeCapValue(to, image, r.Pin) {
		return
	}
	m.setStatus(StatusSuccess, fmt.Sprintf("%s capped at %s (%s)", image, r.Pin, scopeLabel(to)))
}

// writeCapValue records level in scope — clearing every scope first, so a cap
// can never end up in two files — and reports whether it got that far. A failed
// write leaves the rows alone: the sidebar must not show a cap that is not on
// disk.
func (m *Model) writeCapValue(scope pinScope, image string, level config.Level) bool {
	if level == "" {
		for _, s := range scopeChoices {
			if err := m.setCap(s, image, ""); err != nil {
				m.setStatus(StatusError, fmt.Sprintf("could not update %s: %v", image, err))
				return false
			}
		}
		m.applyPin(image, "", scope)
		return true
	}

	if err := m.setCap(scope, image, level); err != nil {
		m.setStatus(StatusError, fmt.Sprintf("could not save %s: %v", image, err))
		return false
	}
	m.applyPin(image, level, scope)
	return true
}

func scopeLabel(s pinScope) string {
	if s == pinGlobal {
		return "global"
	}
	return "project"
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

// joinColumns places the two boxes side by side. Both are drawn to the same
// height so their borders close level with each other; a shorter box would look
// like the taller one had lost its bottom edge.
func (m Model) joinColumns(left, right []string, leftInner, height int) string {
	inner := height - 2
	if inner < 1 {
		inner = 1
	}

	rightInner := sidebarWidth(m.width) - boxChrome
	if rightInner < 1 {
		rightInner = 1
	}

	lb := m.theme.Box(left, leftInner, inner, m.focus == focusList)
	rb := m.theme.Box(right, rightInner, inner, m.focus == focusSide)

	out := make([]string, 0, height)
	for i := range lb {
		r := ""
		if i < len(rb) {
			r = rb[i]
		}
		out = append(out, lb[i]+" "+r)
	}
	for len(out) < height {
		out = append(out, "")
	}

	return strings.Join(out, "\n")
}

// sidebarGutter is the single blank column between the two boxes. Their own
// borders do the separating, so one space is all that is needed to keep them
// from touching.
const sidebarGutter = 1
