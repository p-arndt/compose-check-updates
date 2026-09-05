package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// The sidebar is where a single image is decided: which release it moves to, and
// whether that choice is remembered past this run. It follows the list rather
// than being opened, so which image is meant is never in question.

// sidebarMinTotal is the terminal width below which the two columns stop fitting
// side by side, leaving the list too cramped to read a path in.
const sidebarMinTotal = 96

// sidebarMinStacked is the width below which even a full-width panel has no room
// to say anything.
const sidebarMinStacked = 34

// sidebarPlacement is where the sidebar goes in the frame — never whether it
// exists: the cap and the per-image target have no keys of their own, so
// dropping it would put them out of reach. A narrow terminal stacks it below.
type sidebarPlacement int

const (
	sidebarNowhere sidebarPlacement = iota // no room for it at all
	sidebarStacked                         // a full-width panel below the list
	sidebarBeside                          // the right-hand column
)

// placeSidebar decides the layout for a terminal width.
func placeSidebar(total int) sidebarPlacement {
	switch {
	case total >= sidebarMinTotal:
		return sidebarBeside
	case clampWidth(total) >= sidebarMinStacked:
		return sidebarStacked
	default:
		return sidebarNowhere
	}
}

// sidebarPlacement is placeSidebar for the current frame.
func (m Model) sidebarPlacement() sidebarPlacement { return placeSidebar(m.width) }

// sidebarAvailable reports whether there is a sidebar for the focus to move into.
func (m Model) sidebarAvailable() bool { return m.sidebarPlacement() != sidebarNowhere }

// stackedSidebarHeight is how many rows the stacked panel takes off the list,
// its border included. Only the fields go in it: the path and the hint are
// already covered by the list and the footer.
func (m Model) stackedSidebarHeight() int {
	if m.sidebarPlacement() != sidebarStacked {
		return 0
	}
	return m.stackedSidebarFields() + 2
}

// stackedSidebarFields is the number of lines inside that panel: the image, its
// target and its cap, plus the scope once there is a cap to put somewhere and the
// versioning scheme on a row that could not be read.
func (m Model) stackedSidebarFields() int {
	r := m.currentRow()
	if r == nil {
		return 1
	}

	n := 3
	if m.fieldVisible(fieldVersioning) {
		n++
	}
	if r.Pin != "" {
		n++
	}
	return n
}

// stackedSidebar renders the panel that goes below the list.
func (m Model) stackedSidebar() []string {
	inner := clampWidth(m.width) - boxChrome
	if inner < 1 {
		inner = 1
	}
	fields := m.stackedSidebarFields()
	return m.theme.Box(m.sidebarLines(inner, fields), inner, fields, m.focus == focusSide)
}

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
	fieldTarget     sideField = iota // the release this run would apply
	fieldVersioning                  // how this image's tags are read, on the rows where reading them failed
	fieldCap                         // the ceiling to remember, as a level of its own
	fieldScope                       // which config file remembers it
	sideFieldCount
)

// focusArea is which half of the frame the keyboard is talking to. The list keeps
// it until deliberately handed over, so every reflex key keeps its meaning.
type focusArea int

const (
	focusList focusArea = iota
	focusBar
	focusSide
)

// capChoicesFor are the values the cap field steps through for one image. The cap
// names a level of its own: the target is what this run does, the cap is what
// every future run may not exceed, so levels the image has no release for are
// still offered. Major is the exception — it appears only when a global cap
// exists, where it is how a project waives that ceiling.
func (m Model) capChoicesFor(image string) []policy.Level {
	choices := []policy.Level{"", policy.LevelPatch, policy.LevelMinor}
	if m.capInScope(pinGlobal, image) != "" {
		choices = append(choices, policy.LevelMajor)
	}
	return choices
}

// scopeChoices are the files a cap can be remembered in, in the order the field
// steps through them.
var scopeChoices = []pinScope{pinProject, pinGlobal}

// versioningChoices are the values the versioning field steps through. The empty
// one comes first because it is where every image starts: it is not a third
// scheme but the absence of a preference, i.e. the run's default.
var versioningChoices = []policy.Versioning{"", policy.VersioningSemver, policy.VersioningLoose}

// sidebarLines renders the right column for the row under the cursor, within the
// height it is given. Added in priority order, not top to bottom: dropping a
// field hides a setting, dropping the path only repeats what the list shows.
func (m Model) sidebarLines(width, height int) []string {
	r := m.currentRow()
	if r == nil {
		return []string{m.theme.dim().Render(fit("no image selected", width))}
	}

	u := r.Update

	// Everything here has to survive: the image, then what can be changed about it.
	fields := []string{
		m.theme.sideTitle(u.ImageName, width),
		m.theme.sideValue("target", m.targetValue(r, width), m.focused(fieldTarget), width),
	}
	if m.fieldVisible(fieldVersioning) {
		// Above the cap, because on this row it is the only field that can change
		// anything: an image ccu could not read has no target and no cap to reach.
		fields = append(fields, m.theme.sideValue("versioning", m.versioningValue(r), m.focused(fieldVersioning), width))
	}
	fields = append(fields, m.theme.sideValue("cap", m.capValue(r), m.focused(fieldCap), width))
	if r.Pin != "" {
		// The scope only exists once there is a cap to put somewhere.
		fields = append(fields, m.theme.sideValue("save to", m.scopeValue(r, width), m.focused(fieldScope), width))
	}

	if height <= len(fields) {
		return fields[:max(height, 1)]
	}

	// Room to spare, so the context and the spacing go back in, path first: it is
	// what tells two identically named services apart.
	out := []string{fields[0]}
	rest := fields[1:]

	if height >= len(fields)+1 {
		out = append(out, m.theme.dim().Render(fit(shortPath(u.FilePath)+" · now "+u.CurrentTag, width)))
	}
	if height >= len(fields)+2 {
		out = append(out, "")
	}
	out = append(out, rest...)

	// Last of the extras, and only when there is a line to spare: it is the one
	// thing here that changes nothing about the run, it only says where to read
	// what this release changed.
	if link := notesLink(u); link != "" && height >= len(out)+2 {
		out = append(out, "", m.theme.dim().Render(fitLink(link, width)))
	}

	// Only while the list still holds the keyboard; once the sidebar has it, the
	// footer names the same keys.
	if m.focus == focusList && height >= len(out)+2 {
		out = append(out, "", m.theme.dim().Render(fit("→ to change", width)))
	}

	return out
}

// notesLink is where the user can read what this release changed: the release
// page when the image's forge has one, the repository itself otherwise. Nothing
// opens it — the sidebar only names it, so it can be copied out.
func notesLink(u check.Update) string {
	if release := u.ReleaseURL(); release != "" {
		return release
	}
	return u.SourceURL
}

// fitLink renders a link into the width the panel has. The scheme goes first —
// it is the same on every link ccu reports and costs eight of under forty
// columns. What will still not fit falls back to the repository itself: a cut
// URL is no URL at all, while the repository is one, and its releases are a
// click away from there.
func fitLink(link string, width int) string {
	short := strings.TrimPrefix(strings.TrimPrefix(link, "https://"), "http://")
	if lipgloss.Width(short) <= width {
		return short
	}

	if repo := repoOnly(short); lipgloss.Width(repo) < lipgloss.Width(short) && lipgloss.Width(repo) <= width {
		return repo
	}
	return fit(short, width)
}

// repoOnly keeps host, owner and repository and drops the path a release link
// adds to them.
func repoOnly(link string) string {
	parts := strings.Split(link, "/")
	if len(parts) <= 3 {
		return link
	}
	return strings.Join(parts[:3], "/")
}

// focused reports whether f is the field the arrow keys would change.
func (m Model) focused(f sideField) bool {
	return m.focus == focusSide && m.sideField == f
}

// versioningValue is how this image's tags are read. An image that was never
// given a scheme shows the run's default beside the word, since "default" on its
// own says nothing about what would change if it were stepped.
func (m Model) versioningValue(r *Row) string {
	focused := m.focused(fieldVersioning)

	name := m.versioningFor(r.Update.ImageName)
	if name == "" {
		return m.theme.sideText("default", focused) + m.theme.dim().Render("  "+string(m.defaultVersioning()))
	}
	return m.theme.sideText(string(name), focused)
}

// targetValue is the release this row would move to, named by its level as well
// as its tag: the level is the thing being chosen, the tag only the consequence.
func (m Model) targetValue(r *Row, width int) string {
	focused := m.focused(fieldTarget)
	if r.NoTarget || len(r.Update.AvailableTargets()) == 0 {
		return m.theme.sideText("—", focused)
	}
	// The same badge the row carries in the list, so field and row read as one.
	value := m.theme.BadgeTight(policy.Level(targetLabel(r.Target))) + " " + m.theme.sideText(r.Update.LatestTag, focused)

	// How long the release has been out, dimmed: it is context for the choice,
	// not part of it. Dropped rather than truncated when the column is too
	// narrow, the same way the scope path is — a cut line eats the chevron.
	age := r.Update.Age()
	if age == "" {
		return value
	}
	const chrome = 8 + 4 // what sideValue puts around the value
	if lipgloss.Width(value)+lipgloss.Width("  "+age)+chrome > width {
		return value
	}
	return value + m.theme.dim().Render("  "+age)
}

// capValue is the ceiling this image may never move past, phrased as a rule
// rather than as a version number.
func (m Model) capValue(r *Row) string {
	focused := m.focused(fieldCap)
	if r.Pin == "" {
		return m.theme.sideText("off", focused)
	}

	value := m.theme.BadgeTight(r.Pin)

	// Spelled out because "major" only means something here: it lifts a ceiling
	// the global file set.
	if r.Pin == policy.LevelMajor {
		value += m.theme.dim().Render("  lifts the global cap")
	}
	return value
}

// scopeValue names the file the cap is remembered in, with the path beside it:
// "project" and "global" only mean something once the file is visible. Dropped
// rather than truncated when too narrow: a cut line eats the closing chevron.
func (m Model) scopeValue(r *Row, width int) string {
	focused := m.focused(fieldScope)

	name, path := "project", "  .ccu.yaml"
	if m.pinScopeOf(r.Update.ImageName) == pinGlobal {
		name, path = "global", "  ~/.config/ccu"
	}

	// What sideValue puts around the value, and therefore what it may not use.
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

func scopeLabel(s pinScope) string {
	if s == pinGlobal {
		return "global"
	}
	return "project"
}

// shortPath trims a compose file path to its last two segments — the directory
// and the file name are what identify a stack.
func shortPath(p string) string {
	parts := strings.Split(strings.ReplaceAll(p, "\\", "/"), "/")
	if len(parts) <= 2 {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// joinColumns places the two boxes side by side, drawn to the same height so
// their borders close level with each other.
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

// sidebarGutter is the single blank column between the two boxes; their own
// borders do the separating.
const sidebarGutter = 1
