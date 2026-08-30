package tui

import (
	"strings"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

// pinEvent is a floating tag the scan offered a digest for: same tag either
// side, the digest being the whole of the news.
func pinEvent(path, image, tag, digest string) scanEventMsg {
	ev := updateEvent(path, image, tag, tag, policy.LevelPin)
	u := &ev.ev.Update
	u.LatestDigest = digest
	u.PinsFloating = true
	ev.ev.Level = u.Level()
	return ev
}

const testDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

// The scan resolves the pins whichever way the setting is, so the list starts
// out following the setting alone — and hiding them is the default.
func TestPinRowsAreHiddenUntilAskedFor(t *testing.T) {
	m := newTestModel()
	m = feed(t, m,
		pinEvent("a/compose.yml", "nginx", "latest", testDigest),
		updateEvent("a/compose.yml", "traefik", "v2.9.3", "v2.9.4", "patch"),
	)

	require.Len(t, m.rows, 2, "the row exists, it is only not listed")
	assert.Len(t, m.visible, 1)
	assert.Equal(t, "traefik", m.rows[m.visible[0]].Update.ImageName)

	m = feed(t, m, keyMsg("p"))
	assert.True(t, m.showFloating)
	assert.Len(t, m.visible, 2)

	m = feed(t, m, keyMsg("p"))
	assert.False(t, m.showFloating)
	assert.Len(t, m.visible, 1)
}

// The setting the config and -pin-floating resolved to decides the first frame.
func TestPinDisplayStartsFromTheSetting(t *testing.T) {
	m := NewModel(scanner.Options{Policies: policy.Set{PinFloating: true}})
	assert.True(t, m.showFloating, "the scan was asked to pin, so the rows are listed")

	assert.False(t, NewModel(scanner.Options{}).showFloating)
	assert.True(t, newTestModel().withFloatingListed().showFloating)
}

// A pin keeps its tag, so the version column has to say what actually changes.
// "latest → latest" would be a row that reads as a no-op.
func TestPinRowShowsTheDigestItWouldWrite(t *testing.T) {
	m := newTestModel().withFloatingListed()
	m = feed(t, m, pinEvent("a/compose.yml", "nginx", "latest", testDigest))

	r := *rowFor(t, m, "nginx")
	require.Equal(t, policy.LevelPin, r.Level)

	tail := rowTailPlain(r)
	assert.Equal(t, "latest → @"+shortDigest(testDigest), tail)
	assert.Contains(t, m.theme.rowTail(r, tail, 80), shortDigest(testDigest))
}

// The badge and the colour are the level's own, so a pin cannot be mistaken for
// a version bump at a glance.
func TestPinLevelHasItsOwnColour(t *testing.T) {
	theme := DefaultTheme()
	assert.Equal(t, theme.Pin, theme.LevelColor(policy.LevelPin))
	assert.NotEqual(t, theme.Digest, theme.LevelColor(policy.LevelPin))
	assert.Contains(t, theme.Badge(policy.LevelPin), "PIN")
}

// The bar's stop and the `p` key are the same setting, and the stop says which
// of the two states it is in.
func TestBarPinsStopTogglesTheSameSetting(t *testing.T) {
	m := newTestModel()
	m = feed(t, m, pinEvent("a/compose.yml", "nginx", "latest", testDigest))
	require.False(t, m.showFloating)

	m = focusStop(t, m, "floating")
	stop := m.barStops()[m.barStop]
	assert.Equal(t, "hidden", stop.value)

	m = feed(t, m, keyMsg(" "))
	assert.True(t, m.showFloating)
	assert.Equal(t, "listed", m.barStops()[m.barStop].value)
	assert.Contains(t, strings.ToLower(m.statusText), "floating tag")
}

// Pinning offers no choice of level, so the target keys have to leave the row
// exactly as the scan resolved it rather than clearing the digest under it.
func TestTargetKeysLeaveAPinAlone(t *testing.T) {
	m := newTestModel().withFloatingListed()
	m = feed(t, m, pinEvent("a/compose.yml", "nginx", "latest", testDigest))

	m = feed(t, m, keyMsg("t"), keyMsg("t"))

	r := rowFor(t, m, "nginx")
	assert.Equal(t, policy.LevelPin, r.Level)
	assert.Equal(t, "latest", r.Update.LatestTag)
	assert.Equal(t, testDigest, r.Update.LatestDigest)
	assert.False(t, r.NoTarget, "a pin is applicable, whatever the target says")
}

// Hiding the rows has to disarm them too: `A` would otherwise write a digest
// into a line nobody can see, and the apply count would name rows no header
// reports. A version row's selection is none of the switch's business.
func TestHidingFloatingRowsClearsTheirSelection(t *testing.T) {
	m := newTestModel().withFloatingListed()
	m = feed(t, m,
		pinEvent("a/compose.yml", "nginx", "latest", testDigest),
		updateEvent("a/compose.yml", "traefik", "v2.9.3", "v2.9.4", "patch"),
	)

	m = feed(t, m, keyMsg("a")) // select everything
	require.Equal(t, 2, m.selectedCount())

	m = feed(t, m, keyMsg("p"))
	require.False(t, m.showFloating)
	assert.Equal(t, 1, m.selectedCount(), "the hidden pin may not stay armed")
	assert.False(t, rowFor(t, m, "nginx").Selected)
	assert.True(t, rowFor(t, m, "traefik").Selected, "the version row was not what `p` hid")

	// And listing them again does not silently re-arm what was cleared.
	m = feed(t, m, keyMsg("p"))
	assert.Equal(t, 1, m.selectedCount())
	assert.False(t, rowFor(t, m, "nginx").Selected)
}

// The header counts and the lines under them read the same predicates, so the
// switch has to move both: a group claiming "2 updates" over one line is the
// failure this guards.
func TestGroupCountersFollowTheFloatingSwitch(t *testing.T) {
	m := newTestModel().withFloatingListed()
	m = feed(t, m,
		pinEvent("a/compose.yml", "nginx", "latest", testDigest),
		updateEvent("a/compose.yml", "traefik", "v2.9.3", "v2.9.4", "patch"),
	)

	g := m.groupInfo(nodeIdx(t, m, "a/compose.yml"), false)
	assert.Equal(t, 2, g.Total)
	assert.Equal(t, 2, g.Shown)
	assert.Equal(t, 2, m.eligibleCount())

	m = feed(t, m, keyMsg("p"))
	g = m.groupInfo(nodeIdx(t, m, "a/compose.yml"), false)
	assert.Equal(t, 1, g.Total, "a hidden pin is not an update this group has")
	assert.Equal(t, 1, g.Shown)
	assert.Equal(t, 1, m.eligibleCount())
	assert.Len(t, m.visible, 1)
}

// The filter speaks about versions, and a pin moves none, so no setting of it
// may take the row away: `p` is the only switch that decides a pin's fate.
func TestPinRowsIgnoreTheLevelFilter(t *testing.T) {
	m := newTestModel().withFloatingListed()
	m = feed(t, m,
		pinEvent("a/compose.yml", "nginx", "latest", testDigest),
		updateEvent("a/compose.yml", "traefik", "v2.9.3", "v2.9.4", "patch"),
	)

	for _, f := range []Filter{FilterAll, FilterMajor, FilterMinor, FilterPatch, FilterDigest} {
		m.setFilter(f)
		m.rebuild(m.cursorKey())
		require.NotNil(t, rowFor(t, m, "nginx"))
		listed := false
		for _, i := range m.visible {
			if m.rows[i].Update.ImageName == "nginx" {
				listed = true
			}
		}
		assert.True(t, listed, "filter %s hid a pin", f.Label())
	}
}

// The detail column says what changes. For a pin that is the digest, so the tag
// is stated once rather than as a "latest → latest" delta that reads as a no-op.
func TestDetailPaneNamesThePinsTagInsteadOfADelta(t *testing.T) {
	u := check.Update{
		FullImageName: "nginx:latest",
		ImageName:     "library/nginx",
		CurrentTag:    "latest",
		LatestTag:     "latest",
		LatestDigest:  testDigest,
		RawLine:       "    image: nginx:latest",
		FilePath:      "a/compose.yml",
	}

	out := DefaultTheme().Detail(u, policy.LevelPin, 80)
	assert.Contains(t, out, "tag")
	assert.NotContains(t, out, "latest → latest")
	assert.Contains(t, out, shortDigest(testDigest))
	assert.NotContains(t, out, "version")

	// The same image without the pin still gets the ordinary version line.
	u.LatestTag = "1.29.4"
	assert.Contains(t, DefaultTheme().Detail(u, "minor", 80), "latest → 1.29.4")
}
