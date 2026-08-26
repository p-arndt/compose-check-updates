package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

// pinEvent is a floating tag the scan offered a digest for: same tag either
// side, the digest being the whole of the news.
func pinEvent(path, image, tag, digest string) scanEventMsg {
	ev := updateEvent(path, image, tag, tag, internal.LevelPin)
	u := &ev.ev.Update
	u.LatestDigest = digest
	u.PinsFloating = true
	ev.ev.Level = u.UpdateLevel()
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
	m := NewModel(scanner.Options{PinFloating: true})
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
	require.Equal(t, internal.LevelPin, r.Level)

	tail := rowTailPlain(r)
	assert.Equal(t, "latest → @"+shortDigest(testDigest), tail)
	assert.Contains(t, m.theme.rowTail(r, tail, 80), shortDigest(testDigest))
}

// The badge and the colour are the level's own, so a pin cannot be mistaken for
// a version bump at a glance.
func TestPinLevelHasItsOwnColour(t *testing.T) {
	theme := DefaultTheme()
	assert.Equal(t, theme.Pin, theme.LevelColor(internal.LevelPin))
	assert.NotEqual(t, theme.Digest, theme.LevelColor(internal.LevelPin))
	assert.Contains(t, theme.Badge(internal.LevelPin), "PIN")
}

// The bar's stop and the `p` key are the same setting, and the stop says which
// of the two states it is in.
func TestBarPinsStopTogglesTheSameSetting(t *testing.T) {
	m := newTestModel()
	m = feed(t, m, pinEvent("a/compose.yml", "nginx", "latest", testDigest))
	require.False(t, m.showFloating)

	m = focusStop(t, m, "pins")
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
	assert.Equal(t, internal.LevelPin, r.Level)
	assert.Equal(t, "latest", r.Update.LatestTag)
	assert.Equal(t, testDigest, r.Update.LatestDigest)
	assert.False(t, r.NoTarget, "a pin is applicable, whatever the target says")
}
