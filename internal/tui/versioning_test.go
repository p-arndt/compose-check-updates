package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

// versioningWrite is one call the sidebar made to the config layer.
type versioningWrite struct {
	scope      pinScope
	image      string
	versioning policy.Versioning
}

// withVersioningWriter swaps the config writer for a recorder, so a test can see
// what would have been saved without a config file existing anywhere.
func (m Model) withVersioningWriter(writes *[]versioningWrite) Model {
	m.setVersioning = func(scope pinScope, image string, v policy.Versioning) error {
		*writes = append(*writes, versioningWrite{scope, image, v})
		return nil
	}
	return m
}

// onSidebar puts the cursor on the first row and the keyboard in the sidebar,
// with the given field under it.
func onSidebar(t *testing.T, m Model, field sideField) Model {
	t.Helper()
	m = feed(t, m, keyMsg("j"))
	require.NotNil(t, m.currentRow())
	m.focus = focusSide
	m.sideField = field
	return m
}

// The field exists where the reading failed and nowhere else: every other row is
// proof that the scheme it is read under works.
func TestVersioningFieldOnlyShowsOnUnreadableRows(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest),
		updateEvent("a/compose.yml", "traefik", "v2.9.3", "v2.9.4", "patch"),
	)

	m = feed(t, m, keyMsg("j"))
	require.Equal(t, "traefik", m.currentRow().Update.ImageName)
	assert.False(t, m.fieldVisible(fieldVersioning))
	assert.NotContains(t, sidebarText(m), "versioning")

	m = feed(t, m, keyMsg("j"))
	require.Equal(t, "vert", m.currentRow().Update.ImageName)
	assert.True(t, m.fieldVisible(fieldVersioning))

	panel := sidebarText(m)
	assert.Contains(t, panel, "versioning")
	assert.Contains(t, panel, "default")
	assert.Contains(t, panel, string(policy.VersioningSemver), "default alone says nothing about what it is")
}

// sidebarText is the panel for the row under the cursor as one string, which is
// all these tests want to look inside.
func sidebarText(m Model) string {
	return strings.Join(m.sidebarLines(40, 10), "\n")
}

// Stepping the field writes the scheme straight to the config, the way the cap
// field writes a cap, and asks for that one image to be checked again.
func TestCyclingVersioningWritesAndRechecks(t *testing.T) {
	t.Parallel()

	var writes []versioningWrite

	m := newTestModel().withVersioningWriter(&writes)
	m = feed(t, m, unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest))
	m = onSidebar(t, m, fieldVersioning)

	cmd := m.cycleSideValue(1)
	require.NotNil(t, cmd, "a changed scheme has to re-check the image it changed")

	// Cleared everywhere first, so the same image can never be recorded twice.
	require.Len(t, writes, 3)
	assert.Equal(t, versioningWrite{pinProject, "vert", ""}, writes[0])
	assert.Equal(t, versioningWrite{pinGlobal, "vert", ""}, writes[1])
	assert.Equal(t, versioningWrite{pinProject, "vert", policy.VersioningSemver}, writes[2])

	// What was written is what the field shows and what the next check reads.
	assert.Equal(t, policy.VersioningSemver, m.versioningFor("vert"))
	assert.Equal(t, policy.VersioningSemver, m.opts.Policies.For("vert").Versioning)

	writes = writes[:0]
	require.NotNil(t, m.cycleSideValue(1))
	assert.Equal(t, policy.VersioningLoose, m.versioningFor("vert"))
	assert.Equal(t, policy.VersioningLoose, m.opts.Policies.For("vert").Versioning)

	// And round to no preference at all, which is a removal, not a value.
	writes = writes[:0]
	require.NotNil(t, m.cycleSideValue(1))
	assert.Empty(t, m.versioningFor("vert"))
	assert.Empty(t, m.opts.Policies.For("vert").Versioning)
	for _, w := range writes {
		assert.Empty(t, w.versioning)
	}
}

// A failed write leaves the field alone: the sidebar may not show a scheme that
// is not on disk, and there is nothing to re-check either.
func TestFailedVersioningWriteChangesNothing(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.setVersioning = func(pinScope, string, policy.Versioning) error { return errors.New("read-only file system") }
	m = feed(t, m, unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest))
	m = onSidebar(t, m, fieldVersioning)

	assert.Nil(t, m.cycleSideValue(1))
	assert.Empty(t, m.versioningFor("vert"))
	assert.Equal(t, StatusError, m.statusKind)
}

// The re-check answers for one row, and the row it answers for is the one that
// changes — the rest of the list is untouched.
func TestRecheckReplacesOnlyItsOwnRow(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest),
		updateEvent("a/compose.yml", "traefik", "v2.9.3", "v2.9.4", "patch"),
	)

	key := rowKey(*rowFor(t, m, "vert"))
	resolved := check.Update{
		FilePath:      "a/compose.yml",
		ImageName:     "vert",
		FullImageName: "vert:sha-e1c83ba",
		RawLine:       "image: vert:sha-e1c83ba",
		CurrentTag:    "sha-e1c83ba",
		LatestTag:     "sha-49821e5",
		LatestDigest:  testDigest,
	}
	m = feed(t, m, recheckDoneMsg{key: key, ev: scanner.Event{
		Kind: scanner.EventUpdate, Path: "a/compose.yml", Update: resolved, Level: resolved.Level(),
	}})

	r := rowFor(t, m, "vert")
	assert.False(t, r.Update.IsUnreadable())
	assert.Equal(t, policy.Level("digest"), r.Level)
	assert.True(t, r.Actionable(), "it can be applied now, which is the point of having changed anything")

	assert.Equal(t, "v2.9.4", rowFor(t, m, "traefik").Update.LatestTag)
	assert.Equal(t, StatusSuccess, m.statusKind)
}

// An image that reads fine and turns out to be up to date has nothing left to
// say: it was only ever listed because it could not be read.
func TestRecheckDropsARowThatNeedsNothing(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		unreadableEvent("a/compose.yml", "vert", "1.2.3", check.ReasonNoComparableTag),
		updateEvent("a/compose.yml", "traefik", "v2.9.3", "v2.9.4", "patch"),
	)

	key := rowKey(*rowFor(t, m, "vert"))
	m = feed(t, m, recheckDoneMsg{key: key, ev: scanner.Event{
		Kind: scanner.EventUpdate,
		Path: "a/compose.yml",
		Update: check.Update{
			FilePath:      "a/compose.yml",
			ImageName:     "vert",
			FullImageName: "vert:1.2.3",
			RawLine:       "image: vert:1.2.3",
			CurrentTag:    "1.2.3",
		},
	}})

	assert.Equal(t, []string{"a/compose.yml/traefik"}, rowNames(m))
	assert.Equal(t, StatusSuccess, m.statusKind)
}

// A re-check that failed says so and leaves the row exactly as it was: the row
// still cannot be read, and pretending otherwise would hide that.
func TestFailedRecheckKeepsTheRow(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, unreadableEvent("a/compose.yml", "vert", "sha-e1c83ba", check.ReasonNoTagForDigest))

	key := rowKey(*rowFor(t, m, "vert"))
	m = feed(t, m, recheckDoneMsg{key: key, err: errors.New("fetching tags: 429")})

	assert.True(t, rowFor(t, m, "vert").Update.IsUnreadable())
	assert.Equal(t, StatusError, m.statusKind)
}

// A cap written next to a recorded scheme leaves the scheme alone: the file
// keeps both keys, so the sidebar has to keep showing both as well.
func TestCappingKeepsTheRecordedVersioning(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.recordVersioning(pinProject, "vert", policy.VersioningLoose)

	m.applyPin("vert", policy.LevelMinor, pinProject)
	assert.Equal(t, policy.LevelMinor, m.capFor("vert"))
	assert.Equal(t, policy.VersioningLoose, m.versioningFor("vert"))

	m.applyPin("vert", "", pinProject)
	assert.Empty(t, m.capFor("vert"))
	assert.Equal(t, policy.VersioningLoose, m.versioningFor("vert"))
}
