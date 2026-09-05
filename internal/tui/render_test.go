package tui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// ansiEscape strips styling so assertions can talk about what the user sees.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiEscape.ReplaceAllString(s, "") }

// helpBindings is the real key map, so the footer assertions break if a binding
// is added without a help string.
var helpBindings = DefaultKeyMap().Bindings()

// withColor forces a colour profile for the duration of a test. `go test` is
// not attached to a TTY, so lipgloss otherwise strips every escape sequence and
// the colour-placement assertions would be vacuous.
func withColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestVersionDeltaColorsOnlyChangedSegments(t *testing.T) {
	withColor(t)
	th := DefaultTheme()

	out := th.VersionDelta("1.2.3", "1.2.9", "patch")
	assert.Equal(t, "1.2.3 → 1.2.9", plain(out))

	// The shared "1.2." prefix must sit outside the coloured span: styling has
	// to change between it and the differing "9".
	assert.Regexp(t, regexp.MustCompile(`1\.2\.(\x1b\[[0-9;]*m)+9`), out)
	// ...which also means the latest tag is never one contiguous run of text.
	assert.NotContains(t, out, "1.2.9")

	// A major bump shares nothing, so the whole tag is coloured.
	major := th.VersionDelta("1.2.3", "2.0.0", "major")
	assert.Equal(t, "1.2.3 → 2.0.0", plain(major))
}

func TestVersionDeltaPlainForms(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	assert.Equal(t, "v1.2.3 → v1.3.0", plain(th.VersionDelta("v1.2.3", "v1.3.0", "minor")))
	assert.Equal(t, "latest", plain(th.VersionDelta("", "latest", "digest")))
	assert.Equal(t, "1.0.0", plain(th.VersionDelta("1.0.0", "", "")))
}

func TestBadgeHasStableVisibleWidth(t *testing.T) {
	withColor(t)
	th := DefaultTheme()

	for _, level := range []policy.Level{"major", "minor", "patch", "digest", "", "nonsense-level"} {
		assert.Equal(t, badgeWidth, lipgloss.Width(th.Badge(level)), "level %q", level)
		assert.Equal(t, badgeWidth, len([]rune(plain(th.Badge(level)))), "level %q", level)
	}
	assert.Equal(t, " MAJOR  ", plain(th.Badge("major")))
}

func sampleRow() Row {
	return Row{
		Update: check.Update{
			FilePath:      "tests/docker-compose.yml",
			RawLine:       "    image: nginx:1.2.3",
			ImageName:     "nginx",
			FullImageName: "docker.io/library/nginx:1.2.3",
			CurrentTag:    "1.2.3",
			LatestTag:     "1.2.9",
		},
		Level: "patch",
	}
}

func TestRowLineRespectsWidth(t *testing.T) {
	withColor(t)
	th := DefaultTheme()

	long := sampleRow()
	long.Update.FullImageName = "registry.example.internal:5000/a-very/long/namespace/with/an/image:1.2.3"
	long.Update.LatestTag = "10.20.30-rc.1+build.7"

	rows := []Row{sampleRow(), long}
	rows[0].Selected = true

	applied := sampleRow()
	applied.State = RowApplied
	failed := sampleRow()
	failed.State = RowFailed
	failed.Err = errors.New("permission denied writing tests/docker-compose.yml")
	noTarget := targetRow()
	noTarget.Update.PatchTag, noTarget.Update.MinorTag = "", ""
	var m Model
	m.retarget(&noTarget, TargetPatch)
	pinned := targetRow()
	pinned.Pin = policy.LevelMinor
	rows = append(rows, applied, failed, targetRow(), noTarget, pinned)

	widths := []int{-5, 0, 1, 2, 5, 20, 21, 24, 30, 40, 60, 80, 120}
	for _, r := range rows {
		for _, w := range widths {
			for _, cursor := range []bool{false, true} {
				out := th.RowLine(r, cursor, w)
				limit := clampWidth(w)
				assert.LessOrEqual(t, lipgloss.Width(out), limit, "width %d cursor %v", w, cursor)
				assert.NotContains(t, out, "\n", "row must stay on one line")
			}
		}
	}
}

func TestRowLineContent(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()

	r := sampleRow()
	assert.Contains(t, plain(th.RowLine(r, false, 100)), "[ ]")
	r.Selected = true
	assert.Contains(t, plain(th.RowLine(r, false, 100)), "[x]")

	r.State = RowApplied
	assert.Contains(t, plain(th.RowLine(r, false, 100)), "✓")

	r.State = RowFailed
	r.Err = errors.New("boom")
	out := plain(th.RowLine(r, false, 100))
	assert.Contains(t, out, "✗")
	assert.Contains(t, out, "boom")

	// The cursor marker only appears on the highlighted row.
	assert.Contains(t, plain(th.RowLine(sampleRow(), true, 100)), "▸")
	assert.NotContains(t, plain(th.RowLine(sampleRow(), false, 100)), "▸")
}

// targetRow is an image with a release at every level, the case the target
// feature exists for: traefik v2.9.3 with 2.9.4, 2.11.0 and 3.7.8 available.
func targetRow() Row {
	return Row{
		Update: check.Update{
			FilePath:      "tests/docker-compose.yml",
			RawLine:       "    image: traefik:v2.9.3",
			ImageName:     "traefik",
			FullImageName: "traefik:v2.9.3",
			CurrentTag:    "v2.9.3",
			LatestTag:     "3.7.8",
			PatchTag:      "2.9.4",
			MinorTag:      "2.11.0",
			MajorTag:      "3.7.8",
		},
		Level:  "major",
		Target: TargetMajor,
	}
}

func TestRowLineShowsSelectedTargetAndHintsOthers(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()

	r := targetRow()
	out := plain(th.RowLine(r, false, 120))
	assert.Contains(t, out, "MAJOR")
	assert.Contains(t, out, "v2.9.3 → 3.7.8")
	// Two other levels exist for this image, which is the only hint the user has
	// that T would do anything here.
	assert.Contains(t, out, "(+2)")

	// Pointed at its patch release, the badge must follow the SELECTED tag.
	var m Model
	m.retarget(&r, TargetPatch)
	patch := plain(th.RowLine(r, false, 120))
	assert.Contains(t, patch, "PATCH")
	assert.NotContains(t, patch, "MAJOR")
	assert.Contains(t, patch, "v2.9.3 → 2.9.4")
	assert.Contains(t, patch, "(+2)")
}

func TestRowLineNoTargetIsInert(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()

	// Only a major release exists, so asking for patch leaves nothing to apply.
	r := targetRow()
	r.Update.PatchTag, r.Update.MinorTag = "", ""

	var m Model
	m.retarget(&r, TargetPatch)
	require.True(t, r.NoTarget)

	out := plain(th.RowLine(r, false, 120))
	assert.Contains(t, out, "[-]", "an unavailable row must not look tickable")
	assert.Contains(t, out, "no patch update")
	// The tag it used to point at must not be advertised any more.
	assert.NotContains(t, out, "3.7.8")
}

func TestRenderersSurviveDegenerateWidths(t *testing.T) {
	withColor(t)
	th := DefaultTheme()
	r := sampleRow()

	for _, w := range []int{-100, -1, 0, 1, 2, 7, 19, 20} {
		require.NotPanics(t, func() {
			th.Title(w)
			th.FileHeader("some/deeply/nested/path/docker-compose.yml", 3, 5, w)
			th.RowLine(r, true, w)
			th.Help(helpBindings, w)
			th.Empty("no updates found", w)
		}, "width %d", w)

		limit := clampWidth(w)
		for name, out := range map[string]string{
			"title":  th.Title(w),
			"header": th.FileHeader("some/deeply/nested/path/docker-compose.yml", 3, 5, w),
			"help":   th.Help(helpBindings, w),
			"empty":  th.Empty("no updates found", w),
		} {
			assert.LessOrEqual(t, lipgloss.Width(out), limit, "%s at width %d", name, w)
			assert.NotContains(t, out, "\n", "%s must stay on one line", name)
		}
	}
}

func TestStatusContent(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()

	assert.Equal(t, "✓ applied 3 updates", plain(th.Status(StatusSuccess, "applied 3 updates")))
	assert.Equal(t, "✗ nope", plain(th.Status(StatusError, "nope")))
	assert.Equal(t, "! careful", plain(th.Status(StatusWarn, "careful")))
	assert.Equal(t, "• scanning", plain(th.Status(StatusInfo, "scanning")))
}

func TestFileHeaderShowsCounts(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	assert.Equal(t,
		"tests/docker-compose.yml (3 of 5)",
		plain(th.FileHeader("tests/docker-compose.yml", 3, 5, 80)))
}

func TestNoTrailingWhitespaceOnSingleLineRenderers(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	r := sampleRow()

	for name, out := range map[string]string{
		"header": th.FileHeader("tests/docker-compose.yml", 1, 2, 80),
		"row":    th.RowLine(r, false, 80),
		"help":   th.Help(helpBindings, 80),
		"status": th.Status(StatusInfo, "hi"),
	} {
		assert.Equal(t, plain(out), strings.TrimRight(plain(out), " "), "%s has trailing whitespace", name)
	}
}

func TestRowLineMarksAPinnedImage(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()

	r := targetRow()
	assert.NotContains(t, plain(th.RowLine(r, false, 120)), "pin",
		"an image with no saved cap must carry no marker")

	r.Pin = policy.LevelMinor
	out := plain(th.RowLine(r, false, 120))
	assert.Contains(t, out, "[pin minor]")
	// The marker sits beside the existing hints rather than replacing them.
	assert.Contains(t, out, "v2.9.3 → 3.7.8")
	assert.Contains(t, out, "(+2)")

	// A row with nothing at its target still says what was pinned: the cap is
	// the likeliest reason it has nowhere to go.
	nt := targetRow()
	nt.Update.PatchTag, nt.Update.MinorTag = "", ""
	var m Model
	m.retarget(&nt, TargetPatch)
	nt.Pin = policy.LevelPatch
	assert.Contains(t, plain(th.RowLine(nt, false, 120)), "[pin patch]")
}

// A styled line is several runes wider than it is columns wide, so measuring it
// with len() under-pads it by however much colour it carries. Boxed, that shows
// up as a right-hand frame that does not close: every pane line has to render to
// one width, with the second box starting on one column.
func TestPaneColumnsLineUpRegardlessOfStyling(t *testing.T) {
	t.Parallel()

	for _, width := range []int{96, 118, 160, 240} {
		m := newTestModel()
		m = feed(t, m,
			levelEvent("alpine", "3.16", "3.16.9", "3.24.1", ""),
			levelEvent("library/traefik", "v2.9.3", "2.9.4", "2.11.5", "3.7.10"),
			levelEvent("redis", "7.0.0", "7.0.5", "7.2.0", "8.10"),
		)
		m.width, m.height = width, 20
		m = feed(t, m, keyMsg("j"), keyMsg("j"))

		require.NotZero(t, sidebarWidth(width), "width %d should draw two columns", width)

		lines := strings.Split(m.paneView(), "\n")
		require.Greater(t, len(lines), 3)

		// Where the second box has to start, computed rather than discovered: the
		// left box's content, its frame, and the gutter between them. Counted in
		// runes because a box-drawing glyph is three bytes and one column, so a
		// byte offset would report a frame that never moved as ragged.
		want := m.listWidth() + boxChrome + sidebarGutter

		for i, line := range lines {
			runes := []rune(plainText(line))
			require.Greater(t, len(runes), want, "width %d line %d: line too short to hold the second box", width, i)
			assert.Contains(t, "╭│╰┏┃┗", string(runes[want]),
				"width %d line %d: the right box moved — %q", width, i, string(runes))
			assert.Equal(t, lipgloss.Width(lines[0]), lipgloss.Width(line),
				"width %d line %d: pane lines must all render to one width", width, i)
		}
	}
}

// The cap and target values carry the same coloured chip the row carries in the
// list, so the level being set and the level being shown read as one thing.
func TestSidebarValuesUseLevelBadges(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"), keyMsg("j"), keyMsg("+")) // cap -> patch

	side := strings.Join(m.sidebarLines(sidebarWidth(m.width)-boxChrome, 20), "\n")

	assert.Contains(t, side, m.theme.BadgeTight("patch"), "the cap value should render as a level badge")
	assert.NotContains(t, plainText(side), "never above patch",
		"the level belongs in the badge, not repeated as plain text")

	// Tight rather than the list's fixed-width chip: nothing lines up after a
	// badge here, so the padding would only open a gap before the closing
	// chevron. Measured, because the padded chip is a prefix-plus-space away
	// from the tight one and a substring check cannot tell them apart.
	assert.Less(t, lipgloss.Width(m.theme.BadgeTight("patch")), badgeWidth)
}

// Colour alone is invisible on a terminal told not to use any, so the focused
// box changes weight too. Both borders are one cell wide, so nothing moves.
func TestFocusedBoxIsDrawnHeavierThanTheUnfocusedOne(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()

	focused := th.Box([]string{"x"}, 10, 1, true)
	unfocused := th.Box([]string{"x"}, 10, 1, false)

	assert.NotEqual(t, plainText(unfocused[0]), plainText(focused[0]),
		"the two states must be distinguishable without colour")
	assert.Contains(t, plainText(focused[0]), "┏")
	assert.Contains(t, plainText(unfocused[0]), "╭")

	require.Len(t, focused, len(unfocused))
	for i := range focused {
		assert.Equal(t, lipgloss.Width(unfocused[i]), lipgloss.Width(focused[i]),
			"line %d: focus must not change the box's geometry", i)
	}
}

// A terminal too short for everything keeps what can be changed and drops what
// is only context: hiding the cap field would put the setting out of reach.
func TestSidebarKeepsItsFieldsWhenSpaceRunsOut(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"), keyMsg("j"), keyMsg("+")) // a cap, so all four lines exist

	width := sidebarWidth(m.width) - boxChrome

	full := plainText(strings.Join(m.sidebarLines(width, 20), "\n"))
	require.Contains(t, full, "now ", "the path line is there when there is room")

	tight := plainText(strings.Join(m.sidebarLines(width, 4), "\n"))
	assert.Contains(t, tight, "library/traefik")
	assert.Contains(t, tight, "target")
	assert.Contains(t, tight, "cap")
	assert.Contains(t, tight, "save to")
	assert.NotContains(t, tight, "now ", "context is what gives way, not the fields")
}

// The chevrons say a value can be stepped, so the path is dropped rather than
// truncated: a missing closing chevron looks broken, not abbreviated.
func TestSidebarScopeDropsThePathRatherThanTheChevron(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"), keyMsg("j"), keyMsg("+"))

	// From the narrowest column the layout can actually produce upwards: the
	// sidebar is only drawn at all past sidebarMinTotal, and fit refuses to
	// clamp below minWidth, so anything narrower is a width no run reaches.
	for _, width := range []int{sidebarWidth(sidebarMinTotal) - boxChrome, 28, 31, 40} {
		line := plainText(m.theme.sideValue("save to", m.scopeValue(m.currentRow(), width), true, width))
		assert.Contains(t, line, "›", "width %d: the closing chevron must survive — %q", width, line)
		assert.LessOrEqual(t, lipgloss.Width(line), width, "width %d", width)
	}
}
