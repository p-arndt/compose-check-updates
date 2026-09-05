package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// unreadableEvent is an image the scan could resolve nothing for: no target, no
// digest, only the reason it could not be read.
func unreadableEvent(path, image, tag, reason string) scanEventMsg {
	ev := updateEvent(path, image, tag, "", "")
	u := &ev.ev.Update
	u.MarkUnreadable(reason, "none of this image's tags matches its newest digest; if this image's tags are versions, try `versioning: loose` for it")
	ev.ev.Level = u.Level()
	return ev
}

// The row exists at all, which is the whole point: before this the image left
// the scan without a line anywhere.
func TestUnreadableRowIsListed(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest))

	require.Len(t, m.rows, 1)
	assert.Len(t, m.visible, 1)
	assert.Equal(t, policy.LevelUnreadable, m.rows[0].Level)
}

// The filter speaks about update levels and this row has none, so hiding it
// under anything but "all" would put it out of reach of the field that fixes it.
func TestUnreadableRowSurvivesEveryFilter(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest),
		updateEvent("a/compose.yml", "traefik", "v2.9.3", "v2.9.4", "patch"),
	)

	for _, f := range []Filter{FilterAll, FilterMajor, FilterMinor, FilterPatch, FilterDigest} {
		m.setFilter(f)
		assert.Contains(t, visibleNames(m), "vert", f.Label())
	}
}

// Nothing was resolved for it, so there is nothing to write: neither space nor
// the sweeping selects may tick it.
func TestUnreadableRowCannotBeSelected(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest))

	assert.False(t, rowFor(t, m, "vert").Actionable())

	// The cursor onto the row (the file header is the first entry), then space.
	m = feed(t, m, keyMsg("j"), keyMsg(" "))
	assert.False(t, rowFor(t, m, "vert").Selected)

	m = feed(t, m, keyMsg("a"))
	assert.False(t, rowFor(t, m, "vert").Selected)
	assert.Zero(t, m.selectedCount())
}

// `u` on the row says why rather than starting an apply that Update() would
// refuse: the reason is what the user is here for.
func TestApplyRowOnAnUnreadableRowExplainsItself(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest))
	m = feed(t, m, keyMsg("j"), keyMsg("u"))

	assert.Equal(t, phaseBrowsing, m.phase)
	assert.Equal(t, StatusWarn, m.statusKind)
	assert.Contains(t, m.statusText, "versioning: loose")
}

// The row has to read as a state of its own: not an update waiting to be ticked,
// and not a row that merely has nothing at the current target.
func TestUnreadableRowRendersAsItsOwnState(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest))

	r := *rowFor(t, m, "vert")
	assert.Equal(t, "unreadable · "+check.ReasonNoTagForDigest, rowTailPlain(r, r.otherTargets()))
	assert.Contains(t, m.theme.RowLine(r, false, 100), "[!]")

	// The badge has to name the state within the width a chip has.
	badge := m.theme.Badge(policy.LevelUnreadable)
	assert.Contains(t, badge, "UNREAD")
	assert.NotContains(t, badge, "…")

	// Its own colour, or the row reads as one of the levels it is not.
	theme := DefaultTheme()
	assert.Equal(t, theme.Unreadable, theme.LevelColor(policy.LevelUnreadable))
	assert.NotEqual(t, theme.Digest, theme.LevelColor(policy.LevelUnreadable))

	// The sidebar is where the way out lives: on a row ccu could not read, the
	// versioning field is the only setting that can change anything about it.
	m = feed(t, m, keyMsg("down")) // off the file header, onto the row itself
	assert.Contains(t, strings.Join(m.sidebarLines(38, 20), "\n"), "versioning")
}
