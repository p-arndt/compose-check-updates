package tui

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/config"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

func newTestModel() Model {
	m := NewModel(scanner.Options{})
	m.phase = phaseBrowsing
	return m
}

// withFloatingListed is the model as a run that was asked to pin leaves it: the
// floating rows listed and their digests already resolved, so a test can look at
// them without driving the second scan.
func (m Model) withFloatingListed() Model {
	m.showFloating = true
	m.floatingResolved = true
	return m
}

func updateEvent(path, image, current, latest string, level policy.Level) scanEventMsg {
	return scanEventMsg{ev: scanner.Event{
		Kind:  scanner.EventUpdate,
		Path:  path,
		Level: level,
		Update: check.Update{
			FilePath:      path,
			ImageName:     image,
			FullImageName: image + ":" + current,
			RawLine:       "image: " + image + ":" + current,
			CurrentTag:    current,
			LatestTag:     latest,
		},
	}}
}

// levelEvent is an update whose per-level candidates are populated, i.e. one
// the target keys can actually move around.
func levelEvent(image, current, patch, minor, major string) scanEventMsg {
	ev := updateEvent("a/compose.yml", image, current, "", "")
	u := &ev.ev.Update
	u.PatchTag, u.MinorTag, u.MajorTag = patch, minor, major
	// The scanner offers the highest available tag, which is what the model then
	// re-points as the target changes.
	u.LatestTag = u.TagForTarget("major")
	ev.ev.Level = u.Level()
	return ev
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		var ok bool
		m, ok = next.(Model)
		require.True(t, ok)
	}
	return m
}

func keyMsg(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func rowNames(m Model) []string {
	var out []string
	for _, r := range m.rows {
		out = append(out, r.Update.FilePath+"/"+r.Update.ImageName)
	}
	return out
}

func visibleNames(m Model) []string {
	var out []string
	for _, i := range m.visible {
		out = append(out, m.rows[i].Update.ImageName)
	}
	return out
}

func TestUpdateEventsAppendSortedRows(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("b/compose.yml", "redis", "7.0", "7.2", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
	)

	assert.Equal(t, []string{
		"a/compose.yml/caddy",
		"a/compose.yml/postgres",
		"b/compose.yml/redis",
	}, rowNames(m))
	assert.Len(t, m.visible, 3)
}

func TestCursorStaysOnSameRowWhenRowInsertedAbove(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("b/compose.yml", "redis", "7.0", "7.2", "minor"),
		updateEvent("c/compose.yml", "nginx", "1.24", "1.25", "minor"),
	)

	// Entries are [hdr b, redis, hdr c, nginx] — headers are navigable lines too.
	m = feed(t, m, keyMsg("j"), keyMsg("j"), keyMsg("j")) // cursor on nginx
	require.Equal(t, "nginx", m.currentRow().Update.ImageName)

	// A row sorting above the cursor arrives mid-scan.
	m = feed(t, m, updateEvent("a/compose.yml", "postgres", "15", "16", "major"))

	assert.Equal(t, "nginx", m.currentRow().Update.ImageName)
	assert.Equal(t, 5, m.cursor)
}

func TestFilterCyclingChangesVisibleRows(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
		updateEvent("a/compose.yml", "redis", "7.0.1", "7.0.2", "patch"),
	)
	require.Equal(t, FilterAll, m.filter)
	require.Len(t, m.visible, 3)

	m = feed(t, m, keyMsg("f")) // major
	assert.Equal(t, FilterMajor, m.filter)
	assert.Equal(t, []string{"postgres"}, visibleNames(m))

	m = feed(t, m, keyMsg("f")) // minor
	assert.Equal(t, []string{"caddy"}, visibleNames(m))

	m = feed(t, m, keyMsg("f")) // patch
	assert.Equal(t, []string{"redis"}, visibleNames(m))

	m = feed(t, m, keyMsg("f")) // digest — matches nothing here
	assert.Empty(t, m.visible)
	assert.Equal(t, 0, m.cursor)

	m = feed(t, m, keyMsg("f")) // back to all
	assert.Equal(t, FilterAll, m.filter)
	assert.Len(t, m.visible, 3)
}

func TestSelectAllOnlySelectsVisibleRows(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
	)

	m = feed(t, m, keyMsg("f"), keyMsg("a")) // filter=major, select all visible
	require.Equal(t, FilterMajor, m.filter)

	assert.Equal(t, 1, m.selectedCount())
	assert.Len(t, m.selectedRows(), 1)
	assert.Equal(t, "postgres", m.selectedRows()[0].Update.ImageName)
}

func TestToggleAndSelectNone(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
	)

	m = feed(t, m, keyMsg("j")) // off the file header, onto the first row
	m = feed(t, m, keyMsg(" "))
	assert.Equal(t, 1, m.selectedCount())
	m = feed(t, m, keyMsg(" "))
	assert.Equal(t, 0, m.selectedCount())

	m = feed(t, m, keyMsg("a"))
	assert.Equal(t, 2, m.selectedCount())
	m = feed(t, m, keyMsg("n"))
	assert.Equal(t, 0, m.selectedCount())
}

func TestApplyResultsSetRowState(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
	)
	m = feed(t, m, keyMsg("a"))

	m.phase = phaseApplying
	m.applyActive = 2

	boom := errors.New("permission denied")
	m = feed(t, m,
		applyResultMsg{key: rowKey(m.rows[0])},
		applyResultMsg{key: rowKey(m.rows[1]), err: boom},
	)

	assert.Equal(t, RowApplied, m.rows[0].State)
	assert.NoError(t, m.rows[0].Err)
	assert.Equal(t, RowFailed, m.rows[1].State)
	assert.Equal(t, boom, m.rows[1].Err)

	// One row was written, so the restart question is asked, once, for the one
	// affected file.
	assert.Equal(t, phaseRestartPrompt, m.phase)
	assert.Len(t, m.affectedFiles(), 1)
}

func TestApplyAllFailedSkipsRestartPrompt(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m = feed(t, m, keyMsg("a"))

	m.phase = phaseApplying
	m.applyActive = 1
	m = feed(t, m, applyResultMsg{key: rowKey(m.rows[0]), err: errors.New("nope")})

	assert.Equal(t, phaseDone, m.phase)
	assert.Empty(t, m.affectedFiles())
}

func TestAffectedFilesAreDeduplicated(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
		updateEvent("b/compose.yml", "redis", "7.0", "7.2", "minor"),
	)
	for i := range m.rows {
		m.rows[i].State = RowApplied
	}

	files := m.affectedFiles()
	require.Len(t, files, 2)
	assert.Equal(t, "a/compose.yml", files[0].FilePath)
	assert.Equal(t, "b/compose.yml", files[1].FilePath)
}

func TestApplyWithNothingSelectedStaysBrowsing(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))

	m = feed(t, m, keyMsg("A"))

	assert.Equal(t, phaseBrowsing, m.phase)
	assert.Equal(t, StatusWarn, m.statusKind)
	assert.NotEmpty(t, m.statusText)
}

func TestApplyWithSelectionEntersApplying(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m = feed(t, m, keyMsg("j"), keyMsg(" "))

	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)

	assert.Equal(t, phaseApplying, m.phase)
	assert.NotNil(t, cmd)
	assert.Equal(t, 1, m.applyActive)
}

// The safety property the whole rebinding exists for: enter is the key a user
// hits by reflex, so it must never reach the disk.
func TestEnterOnlyTogglesAndNeverApplies(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))

	next, cmd := m.Update(keyMsg("j"))
	m = next.(Model)
	next, cmd = m.Update(keyMsg("enter"))
	m = next.(Model)

	assert.Equal(t, 1, m.selectedCount(), "enter still toggles")
	assert.Equal(t, phaseBrowsing, m.phase, "enter must not start an apply")
	assert.Nil(t, cmd)
	assert.Equal(t, 0, m.applyActive)
	assert.Empty(t, m.applyQueue)

	// Even with a selection already staged, enter only un-toggles it.
	next, cmd = m.Update(keyMsg("enter"))
	m = next.(Model)
	assert.Equal(t, 0, m.selectedCount())
	assert.Equal(t, phaseBrowsing, m.phase)
	assert.Nil(t, cmd)
}

// `a` and `A` are distinct keys; a select-all that also wrote files would be the
// worst possible confusion of the two.
func TestLowercaseAOnlySelectsAndNeverApplies(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
	)

	next, cmd := m.Update(keyMsg("a"))
	m = next.(Model)

	assert.Equal(t, 2, m.selectedCount())
	assert.Equal(t, phaseBrowsing, m.phase)
	assert.Nil(t, cmd)
	assert.Equal(t, 0, m.applyActive)

	// And `A` in turn selects nothing new — it only commits what `a` staged.
	next, cmd = m.Update(keyMsg("A"))
	m = next.(Model)
	assert.Equal(t, phaseApplying, m.phase)
	assert.NotNil(t, cmd)
	assert.Equal(t, 2, m.selectedCount(), "A must not touch the selection")
}

func TestApplyRowAppliesOnlyTheCursorRowAndLeavesTheSelection(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
	)

	// Stage a selection on caddy, then apply postgres with the cursor on it.
	m = feed(t, m, keyMsg("j"), keyMsg(" "))
	require.Equal(t, 1, m.selectedCount())

	m = feed(t, m, keyMsg("j"))
	require.Equal(t, "postgres", m.currentRow().Update.ImageName)

	next, cmd := m.Update(keyMsg("u"))
	m = next.(Model)
	require.NotNil(t, cmd)
	assert.Equal(t, phaseApplying, m.phase)
	assert.Equal(t, 1, m.applyActive, "exactly one row is queued")

	// The staged selection is untouched: u reads the cursor, not the selection.
	assert.True(t, rowFor(t, m, "caddy").Selected)
	assert.False(t, rowFor(t, m, "postgres").Selected)

	m = feed(t, m, applyResultMsg{key: rowKey(*rowFor(t, m, "postgres"))})
	assert.Equal(t, RowApplied, rowFor(t, m, "postgres").State)
	assert.Equal(t, RowPending, rowFor(t, m, "caddy").State, "the selected row was not written")
}

func TestApplyRowIsANoopOnHeaderNoTargetAndAppliedRows(t *testing.T) {
	t.Parallel()

	// On a header: the cursor starts there.
	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	require.Nil(t, m.currentRow())

	next, cmd := m.Update(keyMsg("u"))
	m = next.(Model)
	assert.Nil(t, cmd)
	assert.Equal(t, phaseBrowsing, m.phase)
	assert.NotEmpty(t, m.statusText)

	// On a NoTarget row: nothing to write at the current target.
	nt := newTestModel()
	nt = feed(t, nt, levelEvent("postgres", "15", "", "", "16"))
	nt = feed(t, nt, keyMsg("t"), keyMsg("j")) // → patch, onto the row
	require.True(t, nt.currentRow().NoTarget)

	next, cmd = nt.Update(keyMsg("u"))
	nt = next.(Model)
	assert.Nil(t, cmd)
	assert.Equal(t, phaseBrowsing, nt.phase)
	assert.Equal(t, StatusWarn, nt.statusKind)

	// On an already-applied row: re-writing it is a no-op, not a second write.
	ap := newTestModel()
	ap = feed(t, ap, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	ap = feed(t, ap, keyMsg("j"))
	ap.rows[0].State = RowApplied

	next, cmd = ap.Update(keyMsg("u"))
	ap = next.(Model)
	assert.Nil(t, cmd)
	assert.Equal(t, phaseBrowsing, ap.phase)
	assert.Contains(t, ap.statusText, "already")
}

// Re-pointing a row clears its resolved digest, and Update() then refuses the
// stale tag/digest pair outright. Both apply keys must therefore resolve first,
// or digest-pinned rows fail visibly on either path.
//
// The image points at a dead local port so the resolve fails offline and fast:
// what matters is that the row reports a *fetch* failure, which only happens if
// ResolveDigest ran, and never Update()'s "refusing to update" refusal.
func TestBothApplyPathsResolveDigestsBeforeWriting(t *testing.T) {
	t.Parallel()

	for _, k := range []string{"A", "u"} {
		t.Run(k, func(t *testing.T) {
			t.Parallel()

			m := newTestModel()
			ev := updateEvent("a/compose.yml", "127.0.0.1:1/myrepo/myapp", "1.0", "2.0", "major")
			// A digest resolved for the OLD tag: exactly the state re-pointing leaves.
			ev.ev.Update.CurrentDigest = "sha256:aaaa"
			ev.ev.Update.LatestDigest = "sha256:bbbb"
			m = feed(t, m, ev)
			m = feed(t, m, keyMsg("j"))
			if k == "A" {
				m = feed(t, m, keyMsg(" "))
			}

			next, cmd := m.Update(keyMsg(k))
			m = next.(Model)
			require.NotNil(t, cmd, "the row must actually be queued")
			assert.Equal(t, phaseApplying, m.phase, "the write runs off the UI thread")

			// The work is deferred into the command rather than done inside Update.
			// tea.Batch collapses a single command, so unwrap only if it batched.
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				require.Len(t, batch, 1)
				msg = batch[0]()
			}

			res, ok := msg.(applyResultMsg)
			require.True(t, ok)
			require.Error(t, res.err)
			assert.NotContains(t, res.err.Error(), "refusing to update",
				"a skipped ResolveDigest is what produces this refusal")
		})
	}
}

func TestCursorClampsAndEmptyListIsSafe(t *testing.T) {
	t.Parallel()

	m := newTestModel()

	// Nothing to move over: must not panic or index out of range.
	m = feed(t, m, keyMsg("k"), keyMsg("j"), keyMsg(" "))
	assert.Equal(t, 0, m.cursor)
	assert.Nil(t, m.currentRow())

	m = feed(t, m,
		updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("a/compose.yml", "postgres", "15", "16", "major"),
	)

	m = feed(t, m, keyMsg("k"), keyMsg("k"))
	assert.Equal(t, 0, m.cursor)

	m = feed(t, m, keyMsg("j"), keyMsg("j"), keyMsg("j"), keyMsg("j"))
	assert.Equal(t, len(m.entries)-1, m.cursor)

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, m.cursor)

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, len(m.entries)-1, m.cursor)
}

func TestScanErrorsAreCollectedAndCounted(t *testing.T) {
	t.Parallel()

	m := NewModel(scanner.Options{})
	m = feed(t, m,
		scanEventMsg{ev: scanner.Event{Kind: scanner.EventDiscovered, Total: 2}},
		scanEventMsg{ev: scanner.Event{Kind: scanner.EventError, Path: "a", Err: errors.New("broken yaml")}},
		scanEventMsg{ev: scanner.Event{Kind: scanner.EventFileDone, Path: "b"}},
	)

	assert.Equal(t, 2, m.total)
	assert.Equal(t, 2, m.checked)
	require.Len(t, m.scanErrs, 1)
	assert.EqualError(t, m.scanErrs[0], "broken yaml")
}

func TestLogCaptureIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	c := newLogCapture(slog.LevelWarn)
	log := slog.New(c)

	const goroutines, each = 8, 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				log.Warn("Skipping (failed fetching tags)", "image", "img", "g", g)
			}
		}(g)
	}
	// Drain concurrently with the writers — this is what the UI's poll does.
	var wgd sync.WaitGroup
	wgd.Add(1)
	drained := 0
	go func() {
		defer wgd.Done()
		for i := 0; i < 20; i++ {
			drained += len(c.drain())
		}
	}()

	wg.Wait()
	wgd.Wait()
	drained += len(c.drain())

	assert.Equal(t, goroutines*each, drained, "no record may be lost or seen twice")
	assert.Len(t, c.all(), goroutines*each)
}

func TestLogCaptureFiltersBelowLevelAndFlattensAttrs(t *testing.T) {
	t.Parallel()

	c := newLogCapture(slog.LevelWarn)
	log := slog.New(c)

	log.Debug("noise")
	log.Info("also noise")
	log.Warn("Skipping (failed fetching tags)", "Image", "127.0.0.1:5000/myrepo/myapp")

	records := c.all()
	require.Len(t, records, 1, "debug and info must not be stored")
	assert.Contains(t, records[0].Error(), "Skipping (failed fetching tags)")
	assert.Contains(t, records[0].Error(), "Image=127.0.0.1:5000/myrepo/myapp")
}

func TestCaptureSlogInstallsAndRestoresDefault(t *testing.T) {
	t.Parallel()

	prev := slog.Default()

	c, restore := captureSlog(slog.LevelWarn)
	require.NotSame(t, prev, slog.Default(), "the terminal handler must be displaced")

	slog.Warn("Skipping (failed fetching tags)", "Image", "myapp")
	require.Len(t, c.all(), 1, "the record must be captured, not written to the terminal")

	restore()
	assert.Same(t, prev, slog.Default())
}

func TestCapturedLogsSurfaceInTheStatusLine(t *testing.T) {
	t.Parallel()

	c := newLogCapture(slog.LevelWarn)
	m := NewModel(scanner.Options{}).WithLogCapture(c)

	slog.New(c).Warn("Skipping (failed fetching tags)", "Image", "myapp")

	// The poll is what carries a record from the scan goroutines into the UI.
	m = feed(t, m, logPollMsg{})

	require.Len(t, m.scanErrs, 1)
	// The scanning line has no room for the message itself, so it points at the
	// key that shows every one of them instead.
	assert.Contains(t, m.statusLine(), "1 issue(s)")
	assert.Contains(t, m.statusLine(), "press i")
	assert.Contains(t, plainText(m.issuesView()), "Skipping (failed fetching tags)")

	// Draining twice must not duplicate it.
	m = feed(t, m, logPollMsg{})
	assert.Len(t, m.scanErrs, 1)
}

func TestScanDoneMovesToBrowsing(t *testing.T) {
	t.Parallel()

	m := NewModel(scanner.Options{})
	m = feed(t, m, scanDoneMsg{})
	assert.Equal(t, phaseBrowsing, m.phase)
}

// rowFor finds a row by image name; the list is sorted, so index arithmetic in
// the target tests would be fragile.
func rowFor(t *testing.T, m Model, image string) *Row {
	t.Helper()
	for i := range m.rows {
		if m.rows[i].Update.ImageName == image {
			return &m.rows[i]
		}
	}
	t.Fatalf("no row for %q", image)
	return nil
}

func TestDefaultTargetIsMajor(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, levelEvent("traefik", "v2.9.3", "2.9.4", "2.11.0", "3.7.8"))

	// The historical behaviour — offer the highest version — must survive.
	assert.Equal(t, TargetMajor, m.target)
	assert.Equal(t, "3.7.8", rowFor(t, m, "traefik").Update.LatestTag)
	assert.Equal(t, policy.Level("major"), rowFor(t, m, "traefik").Level)
}

func TestGlobalTargetCyclingRepointsRows(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, levelEvent("traefik", "v2.9.3", "2.9.4", "2.11.0", "3.7.8"))
	require.Equal(t, "3.7.8", rowFor(t, m, "traefik").Update.LatestTag)

	m = feed(t, m, keyMsg("t")) // major → patch
	assert.Equal(t, TargetPatch, m.target)
	row := rowFor(t, m, "traefik")
	assert.Equal(t, "2.9.4", row.Update.LatestTag)
	assert.Equal(t, policy.Level("patch"), row.Level, "the badge must follow the selected tag")

	m = feed(t, m, keyMsg("t")) // patch → minor
	assert.Equal(t, "2.11.0", rowFor(t, m, "traefik").Update.LatestTag)
	assert.Equal(t, policy.Level("minor"), rowFor(t, m, "traefik").Level)

	m = feed(t, m, keyMsg("t")) // minor → major, back where we started
	assert.Equal(t, TargetMajor, m.target)
	assert.Equal(t, "3.7.8", rowFor(t, m, "traefik").Update.LatestTag)
}

func TestGlobalTargetDeselectsRowsWithNothingAtThatLevel(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		levelEvent("traefik", "v2.9.3", "2.9.4", "2.11.0", "3.7.8"),
		levelEvent("postgres", "15", "", "", "16"), // major only
	)
	m = feed(t, m, keyMsg("a"))
	require.Equal(t, 2, m.selectedCount())

	m = feed(t, m, keyMsg("t")) // → patch

	pg := rowFor(t, m, "postgres")
	assert.True(t, pg.NoTarget)
	assert.False(t, pg.Selected, "a row with no patch release must not stay selected")
	assert.False(t, pg.Actionable())
	assert.Empty(t, pg.Level)
	// Crucially it must not be applied with the major tag the user just moved off.
	for _, r := range m.selectedRows() {
		assert.NotEqual(t, "postgres", r.Update.ImageName)
	}
	require.Len(t, m.selectedRows(), 1)
	assert.Equal(t, "2.9.4", m.selectedRows()[0].Update.LatestTag)

	// Selecting everything again must still skip it.
	m = feed(t, m, keyMsg("a"))
	assert.False(t, rowFor(t, m, "postgres").Selected)

	// Widening the target brings it back.
	m = feed(t, m, keyMsg("t"), keyMsg("t")) // → major
	assert.False(t, rowFor(t, m, "postgres").NoTarget)
	assert.Equal(t, "16", rowFor(t, m, "postgres").Update.LatestTag)
}

func TestToggleCannotSelectARowWithNoTarget(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, levelEvent("postgres", "15", "", "", "16"))
	m = feed(t, m, keyMsg("t")) // → patch, nothing available

	m = feed(t, m, keyMsg("j"), keyMsg(" "))
	require.NotNil(t, m.currentRow())
	assert.Equal(t, 0, m.selectedCount())
}

func TestRowTargetCyclingStaysWithinAvailableTargets(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		levelEvent("traefik", "v2.9.3", "2.9.4", "", "3.7.8"), // no minor release
		levelEvent("redis", "7.0.1", "7.0.2", "7.2.0", "8.0.0"),
	)

	// Cursor is on redis? Rows sort by image name: redis after traefik.
	require.Equal(t, "redis", m.rows[0].Update.ImageName)

	m = feed(t, m, keyMsg("j"), keyMsg("j")) // past the file header, onto traefik
	require.Equal(t, "traefik", m.currentRow().Update.ImageName)
	avail := m.currentRow().Update.AvailableTargets()
	require.Equal(t, []policy.Level{"patch", "major"}, avail)

	// The target is changed in the sidebar now, so the focus goes there first.
	// Forward from major wraps to patch, skipping the minor level it has no
	// release for.
	m.width = 200
	m = feed(t, m, keyMsg("tab"), keyMsg("+"))
	assert.Equal(t, "2.9.4", m.currentRow().Update.LatestTag)
	assert.Equal(t, policy.Level("patch"), m.currentRow().Level)

	m = feed(t, m, keyMsg("+"))
	assert.Equal(t, "3.7.8", m.currentRow().Update.LatestTag)
	assert.Equal(t, policy.Level("major"), m.currentRow().Level)

	// Backwards, too — and never onto a level the image does not have.
	for i := 0; i < 5; i++ {
		m = feed(t, m, keyMsg("-"))
		require.Contains(t, avail, m.currentRow().Target)
		require.False(t, m.currentRow().NoTarget)
	}

	// The other row was not touched by a per-row change.
	assert.Equal(t, "8.0.0", rowFor(t, m, "redis").Update.LatestTag)
}

func TestRowTargetCyclingRecoversARowWithNoGlobalTarget(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, levelEvent("postgres", "15", "", "", "16"))
	m = feed(t, m, keyMsg("t")) // → patch
	require.True(t, m.rows[0].NoTarget)

	// Per-image control is the point of the feature: the row can be pointed back
	// at the only level it actually has.
	m.width = 200
	m = feed(t, m, keyMsg("j"), keyMsg("tab"), keyMsg("+"))
	assert.False(t, m.rows[0].NoTarget)
	assert.Equal(t, TargetMajor, m.rows[0].Target)
	assert.Equal(t, "16", m.rows[0].Update.LatestTag)
	assert.True(t, m.rows[0].Actionable())
}

func TestRowTargetIsANoopForDigestOnlyRows(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	ev := updateEvent("a/compose.yml", "myapp", "latest", "latest", "digest")
	ev.ev.Update.CurrentDigest = "sha256:aaaa"
	ev.ev.Update.LatestDigest = "sha256:bbbb"
	m = feed(t, m, ev)

	// No levels to choose between: the row must survive both keys unchanged.
	m.width = 200
	m = feed(t, m, keyMsg("j"), keyMsg("t"), keyMsg("tab"), keyMsg("+"))
	assert.False(t, m.rows[0].NoTarget)
	assert.Equal(t, policy.Level("digest"), m.rows[0].Level)
	assert.Equal(t, "latest", m.rows[0].Update.LatestTag)
}

// frameHeight counts rendered rows. blockHeight trims trailing blanks, which is
// exactly what must NOT be ignored here: a frame one line off scrolls the alt
// screen and the UI shakes on every keypress.
func frameHeight(s string) int { return len(strings.Split(s, "\n")) }

func TestFrameIsExactlyTerminalHeight(t *testing.T) {
	t.Parallel()

	issue := scanEventMsg{ev: scanner.Event{
		Kind: scanner.EventError, Path: "x",
		Err: errors.New("a deliberately long failure message that has to wrap somewhere on a narrow terminal"),
	}}

	for _, rows := range []int{0, 1, 3, 50} {
		base := newTestModel()
		for i := 0; i < rows; i++ {
			base = feed(t, base, updateEvent(
				string(rune('a'+i%5))+"/compose.yml",
				"img"+string(rune('a'+i%26)), "1.0", "2.0", "major"))
		}
		base = feed(t, base, issue, issue, issue)

		for _, h := range []int{0, 1, 5, 8, 9, 12, 24, 40, 100} {
			for _, w := range []int{20, 40, 80, 200} {
				for _, mode := range []string{"plain", "detail", "help", "detail+help", "issues", "issues+help"} {
					m := base
					m.width, m.height = w, h
					m.showDetail = strings.Contains(mode, "detail")
					m.showHelp = strings.Contains(mode, "help")
					m.showIssues = strings.Contains(mode, "issues")
					m.syncScroll()
					m.syncIssueScroll()

					v := m.View()
					assert.Equal(t, m.viewHeight(), frameHeight(v),
						"rows=%d h=%d w=%d mode=%s", rows, h, w, mode)
					if h >= minViewHeight {
						assert.Equal(t, h, frameHeight(v), "rows=%d h=%d w=%d mode=%s", rows, h, w, mode)
					}
					for _, l := range strings.Split(v, "\n") {
						require.LessOrEqual(t, lipgloss.Width(l), clampWidth(w),
							"a line wider than the terminal wraps and breaks the height")
					}
				}
			}
		}
	}
}

func TestFooterIsPinnedToTheLastRow(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m.width, m.height = 200, 40 // far taller than the two-entry list needs

	lines := strings.Split(m.View(), "\n")
	require.Len(t, lines, 40)

	// The hints are the final row however tall the terminal is: the frame pads
	// between the boxes and the bottom chrome rather than letting the chrome
	// float in the middle of the screen.
	assert.Contains(t, plainText(lines[39]), "q quit")

	// And the bar is the third row, pinned under the title and the status line.
	// It is fixed chrome, not part of the pane: a bar that scrolled with the
	// list would be a bar you could lose.
	assert.Contains(t, plainText(lines[2]), "target")
	assert.Contains(t, plainText(lines[2]), "show")
}

// Two bindings answering the same key in the same phase means one of them
// silently never fires, which is exactly the bug adding fold keys could cause.
func TestNoKeyIsBoundTwiceInTheBrowsingPhase(t *testing.T) {
	t.Parallel()

	k := DefaultKeyMap()
	named := map[string][]key.Binding{
		"up": {k.Up}, "down": {k.Down}, "pgup": {k.PageUp}, "pgdown": {k.PageDown},
		"home": {k.Home}, "end": {k.End}, "toggle": {k.Toggle},
		"selectAll": {k.SelectAll}, "selectNone": {k.SelectNone},
		"toggleGroup": {k.ToggleGroup}, "collapseAll": {k.CollapseAll}, "expandAll": {k.ExpandAll},
		"collapse": {k.Collapse}, "expand": {k.Expand},
		"filter": {k.Filter}, "target": {k.Target}, "focus": {k.Focus}, "issues": {k.Issues},
		"apply": {k.Apply}, "applyRow": {k.ApplyRow}, "help": {k.Help}, "quit": {k.Quit},
		"bar": {k.Bar}, "focusPrev": {k.FocusPrev},
	}

	owner := map[string]string{}
	for name, bs := range named {
		for _, b := range bs {
			for _, s := range b.Keys() {
				prev, taken := owner[s]
				assert.False(t, taken, "key %q is bound to both %s and %s", s, prev, name)
				owner[s] = name
			}
		}
	}

	// The fold and issues keys in particular must have landed on free keys.
	assert.Equal(t, []string{"z"}, k.ToggleGroup.Keys())
	assert.Equal(t, []string{"C"}, k.CollapseAll.Keys())
	assert.Equal(t, []string{"E"}, k.ExpandAll.Keys())
	assert.Equal(t, []string{"i"}, k.Issues.Keys())

	// `m` reaches the bar from anywhere, so it has to be free everywhere the
	// browsing keys are read — the map above is what proves it still is.
	assert.Equal(t, []string{"m"}, k.Bar.Keys())

	// ←/h and →/l walk the tree while the list has the focus, which is the only
	// place this test's uniqueness rule applies: ValueNext/ValuePrev share those
	// keys deliberately, and are read only once the sidebar has taken over.
	assert.Equal(t, []string{"left", "h"}, k.Collapse.Keys())
	assert.Equal(t, []string{"right", "l"}, k.Expand.Keys())
	assert.Contains(t, k.Focus.Keys(), "tab")

	// The two write keys are the whole point of the rebinding: enter toggles and
	// nothing else, and `a`/`A` are separate keys with separate meanings.
	assert.Equal(t, []string{" ", "enter"}, k.Toggle.Keys())
	assert.Equal(t, []string{"A"}, k.Apply.Keys())
	assert.Equal(t, []string{"u"}, k.ApplyRow.Keys())
	assert.Equal(t, []string{"a"}, k.SelectAll.Keys())
	assert.NotContains(t, k.Apply.Keys(), "enter", "enter must never write to disk")

	// esc means "back" wherever it is read, and nothing else: it must never be
	// one of the keys that ends the program.
	assert.Equal(t, []string{"esc"}, k.IssuesClose.Keys())
	assert.NotContains(t, k.Quit.Keys(), "esc", "esc goes back, it does not quit")
	assert.Equal(t, []string{"q", "ctrl+c"}, k.Quit.Keys())

	// Yes/No live in the restart phase only, where none of the above are read —
	// which is the one reason `n` may also mean SelectNone while browsing.
	assert.Equal(t, []string{"n"}, k.No.Keys())
	assert.Equal(t, []string{"y"}, k.Yes.Keys())
}

// The hint footer is the only thing telling a first-time user the keys exist,
// so it must be on screen without pressing anything.
func TestKeyHintFooterIsAlwaysVisible(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m.width, m.height = 200, 24
	require.False(t, m.showHelp, "the footer must not depend on ?")

	v := plainText(m.View())
	assert.Contains(t, v, "space/enter toggle")
	// The tree is only usable if the keys that walk it are on screen; ←/h and →/l
	// are what the footer leads with now that the list has depth. They say what
	// they do on a row as well as on a header, since that is where they differ.
	assert.Contains(t, v, "collapse / back")
	assert.Contains(t, v, "expand / details")
	assert.Contains(t, v, "A apply selected")
	assert.Contains(t, v, "u apply row")
	assert.Contains(t, v, "? help")

	// ? opens the grouped listing as a dialog over the pane.
	exp := feed(t, m, keyMsg("?"))
	require.True(t, exp.showHelp)
	// The dialog aligns its keys into a column, so the key and its description
	// are separated by however much padding that alignment costs.
	ev := plainText(exp.View())
	assert.Regexp(t, `C\s+collapse all`, ev)
	assert.Regexp(t, `E\s+expand all`, ev)
	assert.Regexp(t, `z\s+fold node`, ev, "z still folds; only the footer dropped it")

	// Grouped under headings rather than run together as one list.
	for _, s := range exp.keys.HelpSections() {
		assert.Contains(t, ev, s.Title, "every group is headed")
	}
	assert.Greater(t, exp.blockHeight(exp.helpDialog()), 1, "the help dialog is multi-line")
	assert.Contains(t, ev, "closes this", "a dialog has to say how to leave it")

	// Both forms are budgeted for: the list never spills past its window.
	for _, mm := range []Model{m, exp} {
		assert.LessOrEqual(t, len(strings.Split(mm.listView(), "\n")), mm.listHeight())
		assert.LessOrEqual(t, mm.blockHeight(mm.View()), mm.height)
	}
}

// The dialog covers the pane, so a key that would act on a row it is hiding has
// to be inert until it is closed — and esc has to close it rather than quit the
// program behind it.
func TestHelpDialogOwnsTheKeyboardUntilItIsClosed(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m.width, m.height = 200, 30
	m = moveToRow(t, m)

	open := feed(t, m, keyMsg("?"))
	require.True(t, open.showHelp)

	blocked := feed(t, open, keyMsg(" "))
	assert.False(t, blocked.currentRow().Selected, "space must not reach a row the dialog is covering")
	assert.True(t, blocked.showHelp)

	assert.False(t, feed(t, open, keyMsg("esc")).showHelp, "esc closes the dialog")
	assert.False(t, feed(t, open, keyMsg("?")).showHelp, "? toggles it shut again")
}

func TestHintFooterIsContextualPerPhase(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m.width = 200

	assert.Equal(t, m.keys.BrowseHints(), m.hintBindings())

	m.phase = phaseScanning
	assert.Equal(t, m.keys.ScanHints(), m.hintBindings())

	m.phase = phaseApplying
	assert.Equal(t, m.keys.ApplyHints(), m.hintBindings())

	// The restart question accepts y/n only; advertising the list keys there
	// would name keys the phase throws away.
	m.phase = phaseRestartPrompt
	assert.Equal(t, m.keys.RestartHints(), m.hintBindings())
	v := plainText(m.View())
	assert.Contains(t, v, "y yes")
	assert.Contains(t, v, "n no")
	assert.NotContains(t, v, "space/enter toggle")
	assert.NotContains(t, v, "A apply selected")
}

// issueEvent is a scanner failure for one file.
func issueEvent(path, msg string) scanEventMsg {
	return scanEventMsg{ev: scanner.Event{Kind: scanner.EventError, Path: path, Err: errors.New(msg)}}
}

// The whole point: the status line shows one issue, the pane shows all of them.
func TestIssuesViewListsEveryIssue(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m,
		issueEvent("a", "broken yaml in a"),
		issueEvent("b", "broken yaml in b"),
		issueEvent("c", "broken yaml in c"),
	)
	m.width, m.height = 100, 30
	require.Len(t, m.scanErrs, 3)

	assert.Contains(t, plainText(m.statusLine()), "3 issue(s)")

	m = feed(t, m, keyMsg("i"))
	require.True(t, m.showIssues)

	pane := plainText(m.issuesView())
	for i, want := range []string{"broken yaml in a", "broken yaml in b", "broken yaml in c"} {
		assert.Contains(t, pane, want)
		assert.Contains(t, pane, fmt.Sprintf("%d. ", i+1), "entries are numbered so 3 of 3 is visible")
	}
	assert.Contains(t, plainText(m.View()), "broken yaml in c")
}

// Captured slog records carry the image and path as attributes; a pane that
// only showed the message would be no better than the truncated status line.
func TestIssuesViewShowsRecordAttributes(t *testing.T) {
	t.Parallel()

	c := newLogCapture(slog.LevelWarn)
	m := NewModel(scanner.Options{}).WithLogCapture(c)
	m.width, m.height = 100, 30

	slog.New(c).Warn("Skipping (failed fetching tags)",
		"Image", "127.0.0.1:5000/myrepo/myapp", "Path", "tests/docker-compose.yml")
	m = feed(t, m, logPollMsg{}, keyMsg("i"))
	require.True(t, m.showIssues)

	pane := plainText(m.issuesView())
	assert.Contains(t, pane, "Skipping (failed fetching tags)")
	assert.Contains(t, pane, "Image=127.0.0.1:5000/myrepo/myapp")
	assert.Contains(t, pane, "Path=tests/docker-compose.yml")
}

func TestIssuesViewTogglesAndSwapsTheFooter(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"), issueEvent("a", "broken yaml"))
	m.width, m.height = 200, 24

	assert.Equal(t, m.keys.BrowseHints(), m.hintBindings())

	m = feed(t, m, keyMsg("i"))
	require.True(t, m.showIssues)
	assert.Equal(t, m.keys.IssueHints(), m.hintBindings())
	v := plainText(m.View())
	assert.Contains(t, v, "esc back to list", "the way out must be on screen")
	assert.NotContains(t, v, "A apply selected", "list keys the pane ignores must not be advertised")

	// esc goes back rather than quitting, and the list is where it was.
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m.showIssues)
	assert.NotEqual(t, phaseDone, m.phase)
	assert.Equal(t, m.keys.BrowseHints(), m.hintBindings())
	assert.Contains(t, plainText(m.View()), "caddy")

	// `i` toggles it closed too.
	m = feed(t, m, keyMsg("i"))
	require.True(t, m.showIssues)
	m = feed(t, m, keyMsg("i"))
	assert.False(t, m.showIssues)

	// q still quits from inside the pane.
	q := feed(t, m, keyMsg("i"), keyMsg("q"))
	assert.Equal(t, phaseDone, q.phase)
}

func TestIssuesKeyIsANoopWithoutIssues(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m.width, m.height = 100, 24

	m = feed(t, m, keyMsg("i"))
	assert.False(t, m.showIssues, "an empty pane the user has to escape is worse than nothing")
	assert.Contains(t, m.statusText, "no issues")
	assert.Contains(t, plainText(m.View()), "caddy")

	// And the pane itself still renders an empty state if it is ever forced open.
	m.showIssues = true
	assert.NotEmpty(t, m.issuesView())
	assert.Equal(t, m.viewHeight(), frameHeight(m.View()))
}

func TestIssuesPaneScrollsAndClampsTheCursor(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	for i := 0; i < 40; i++ {
		m = feed(t, m, issueEvent("f", fmt.Sprintf("issue number %d with a fairly long message attached to it", i)))
	}
	m.width, m.height = 60, 16
	m = feed(t, m, keyMsg("i"))
	require.True(t, m.showIssues)

	lines, starts := m.issueLines()
	require.Greater(t, len(lines), m.listHeight(), "the pane must actually need scrolling")

	for i := 0; i < 60; i++ {
		m = feed(t, m, keyMsg("j"))
		require.GreaterOrEqual(t, m.issueCursor, 0)
		require.Less(t, m.issueCursor, len(m.scanErrs))
		require.GreaterOrEqual(t, m.issueOffset, 0)
		require.LessOrEqual(t, len(strings.Split(m.issuesView(), "\n")), m.listHeight())
		require.Equal(t, m.viewHeight(), frameHeight(m.View()))
	}
	assert.Equal(t, len(m.scanErrs)-1, m.issueCursor)

	for i := 0; i < 60; i++ {
		m = feed(t, m, keyMsg("k"))
		require.GreaterOrEqual(t, m.issueCursor, 0)
		require.Equal(t, m.viewHeight(), frameHeight(m.View()))
	}
	assert.Equal(t, 0, m.issueCursor)
	assert.Equal(t, 0, m.issueOffset)

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, len(m.scanErrs)-1, m.issueCursor)
	// The last entry must actually be on screen, not scrolled past.
	last := starts[len(starts)-1]
	assert.LessOrEqual(t, m.issueOffset, last)
	assert.Less(t, last, m.issueOffset+m.listHeight())

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, m.issueCursor)
	assert.Equal(t, 0, m.issueOffset)
}

func TestWrapPlainNeverExceedsWidth(t *testing.T) {
	t.Parallel()

	long := "Skipping (failed fetching tags) (Image=127.0.0.1:5000/a/very/long/repository/name/that/never/ends, Path=x)"
	for _, w := range []int{-1, 0, 1, 3, 10, 40} {
		for _, l := range wrapPlain(long, w) {
			require.LessOrEqual(t, len([]rune(l)), max(w, 1), "width %d", w)
		}
	}
	assert.Equal(t, []string{""}, wrapPlain("   ", 10), "blank input still yields one line")
	assert.Equal(t, []string{"a b", "c"}, wrapPlain("a b c", 3))
}

func TestRestartPromptAnswers(t *testing.T) {
	t.Parallel()

	base := newTestModel()
	base = feed(t, base, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	base.rows[0].State = RowApplied
	base.phase = phaseRestartPrompt

	yes := feed(t, base, keyMsg("y"))
	assert.Equal(t, phaseRestarting, yes.phase)
	require.Len(t, yes.restartTargets, 1)
	assert.Equal(t, "a/compose.yml", yes.restartTargets[0].FilePath)

	no := feed(t, base, keyMsg("n"))
	assert.Equal(t, phaseDone, no.phase)
	assert.Empty(t, no.restartTargets)
}

// capWrite is one call the pin keys would have made. Tests record these rather
// than letting the model near the user's config file — which is the reason the
// writer is a field on the Model at all.
type capWrite struct {
	scope pinScope
	image string
	level policy.Level
}

type capRecorder struct {
	writes []capWrite
	err    error
}

func (c *capRecorder) set(scope pinScope, image string, max policy.Level) error {
	c.writes = append(c.writes, capWrite{scope: scope, image: image, level: max})
	return c.err
}

// pinModel is a browsing model with one row that has a release at every level,
// the cursor parked on it, and a recorder standing in for the config writer.
func pinModel(t *testing.T) (Model, *capRecorder) {
	t.Helper()

	rec := &capRecorder{}
	m := newTestModel()
	m.setCap = rec.set
	m = feed(t, m, levelEvent("library/traefik", "v2.9.3", "2.9.4", "2.11.0", "3.7.8"))
	return moveToRow(t, m), rec
}

// moveToRow walks the cursor down until it leaves the tree headers and lands on
// the first row, so a test does not have to know how deep the path nests.
func moveToRow(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < len(m.entries); i++ {
		if m.currentRow() != nil {
			return m
		}
		m = feed(t, m, keyMsg("j"))
	}
	require.NotNil(t, m.currentRow(), "no row to put the cursor on")
	return m
}

// pinModel parks the cursor on a row and gives the sidebar room to render, so a
// test can tab across to it the way a user would.
func sidebarModel(t *testing.T) (Model, *capRecorder) {
	t.Helper()
	m, rec := pinModel(t)
	m.width = 200
	return m, rec
}

func TestTabMovesFocusToTheSidebarAndBack(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	require.Equal(t, focusList, m.focus)

	m = feed(t, m, keyMsg("tab"))
	assert.Equal(t, focusSide, m.focus, "tab on a row hands the keyboard to the sidebar")

	m = feed(t, m, keyMsg("tab"))
	assert.Equal(t, focusList, m.focus, "tab again gives it back")
}

// A header describes no image, so there is nothing beside it to focus. tab goes
// up to the bar instead, which acts on the whole list and is just as meaningful
// standing on a directory as on a row.
func TestTabOnAHeaderGoesToTheBar(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m.cursor = 0
	require.Nil(t, m.currentRow(), "this test needs the cursor on a header")

	m = feed(t, m, keyMsg("tab"))
	assert.Equal(t, focusBar, m.focus)
	assert.Equal(t, 0, m.barStop)
}

// A terminal with no room for the sidebar in any layout has none to focus, and
// the keyboard must not land somewhere the user cannot see. The bar is drawn at
// any width, so tab still has somewhere to go — which is the point of putting
// the list-wide controls there rather than in a column that can vanish.
func TestTabGoesToTheBarWhenTheSidebarIsTooNarrowToDraw(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m.width = sidebarMinStacked - 1
	require.Equal(t, sidebarNowhere, m.sidebarPlacement())

	m = feed(t, m, keyMsg("tab"))
	assert.Equal(t, focusBar, m.focus)
	assert.NotEqual(t, focusSide, m.focus)
}

// Between the two widths the sidebar moves under the list rather than away. The
// cap and the per-image target have no keys of their own, so a layout that
// dropped it would take the settings with it.
func TestTabReachesTheStackedSidebar(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m.width = sidebarMinTotal - 1
	require.Equal(t, sidebarStacked, m.sidebarPlacement())
	require.Zero(t, sidebarWidth(m.width), "no room for a second column at this width")

	m = feed(t, m, keyMsg("tab"))
	assert.Equal(t, focusSide, m.focus)
}

// With no cap set there is no scope to choose, so the field is not drawn and
// must not be stopped on: a cursor on a line the user cannot see reads as a
// dead keypress.
func TestSidebarSkipsTheScopeFieldUntilACapExists(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"))
	require.Equal(t, fieldTarget, m.sideField)

	m = feed(t, m, keyMsg("j"))
	assert.Equal(t, fieldCap, m.sideField)

	m = feed(t, m, keyMsg("j"))
	assert.Equal(t, fieldTarget, m.sideField, "the scope field is skipped while there is no cap")

	m = feed(t, m, keyMsg("k"))
	assert.Equal(t, fieldCap, m.sideField)
}

func TestSidebarReachesTheScopeFieldOnceACapIsSet(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"), keyMsg("j"), keyMsg("+"))
	require.NotEmpty(t, m.currentRow().Pin, "the cap should be set now")

	m = feed(t, m, keyMsg("j"))
	assert.Equal(t, fieldScope, m.sideField)
}

func TestSidebarTargetFieldRetargetsTheRow(t *testing.T) {
	t.Parallel()

	m, rec := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"))
	before := m.currentRow().Update.LatestTag

	m = feed(t, m, keyMsg("+"))
	assert.NotEqual(t, before, m.currentRow().Update.LatestTag, "→ on the target field picks another release")
	assert.Empty(t, rec.writes, "changing the target alone writes nothing to disk")
}

// The cap names its own level. Reading it off the target field is what made the
// two fields impossible to tell apart, so the first step from "off" is the
// lowest level and never whatever the target happens to show.
func TestSidebarCapFieldStepsThroughLevelsIndependentOfTheTarget(t *testing.T) {
	t.Parallel()

	m, rec := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"))
	require.Equal(t, TargetMajor, m.currentRow().Target, "this test needs the target on major")

	m = feed(t, m, keyMsg("j"), keyMsg("+"))
	require.Len(t, rec.writes, 1)
	assert.Equal(t, policy.LevelPatch, rec.writes[0].level, "first step off is patch, not the target's major")
	assert.Equal(t, pinProject, rec.writes[0].scope, "a new cap goes to the narrower file")
	assert.Equal(t, "library/traefik", rec.writes[0].image)
	assert.Equal(t, StatusSuccess, m.statusKind)

	m = feed(t, m, keyMsg("+"))
	assert.Equal(t, policy.LevelMinor, rec.writes[len(rec.writes)-1].level)

	// Nothing in semver sits above major, so capping at it would say exactly what
	// no cap says. The cycle wraps back to off instead of offering a no-op.
	m = feed(t, m, keyMsg("+"))
	assert.Equal(t, policy.Level(""), rec.writes[len(rec.writes)-1].level)
}

// Major is not a no-op in one case: the project file overrides the global one,
// so a project cap of major is how a project lifts a global ceiling. It is
// offered exactly when it would mean that, and not otherwise.
func TestCapOffersMajorOnlyToLiftAGlobalCap(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	assert.NotContains(t, m.capChoicesFor("library/traefik"), policy.LevelMajor,
		"with no global cap, major would say the same as off")

	m = m.WithPins(
		config.Config{},
		config.Config{Images: map[string]policy.Image{"library/traefik": {Max: policy.LevelMinor}}},
	)
	assert.Contains(t, m.capChoicesFor("library/traefik"), policy.LevelMajor,
		"with a global cap in force, major is how the project lifts it")
}

// A cap is a ceiling on the image, so it has to bind the selection too —
// otherwise `A` would write exactly the release the user just forbade.
func TestSettingACapPullsTheTargetDownToIt(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"))
	require.Equal(t, TargetMajor, m.currentRow().Target)

	m = feed(t, m, keyMsg("j"), keyMsg("+")) // cap -> patch
	assert.Equal(t, TargetPatch, m.currentRow().Target)
	assert.Equal(t, "2.9.4", m.currentRow().Update.LatestTag)
}

func TestSidebarScopeFieldMovesAnExistingCap(t *testing.T) {
	t.Parallel()

	m, rec := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"), keyMsg("j"), keyMsg("+")) // cap on, project
	m = feed(t, m, keyMsg("j"))                             // onto the scope field
	require.Equal(t, fieldScope, m.sideField)

	rec.writes = nil
	m = feed(t, m, keyMsg("+"))

	// Cleared where it was before being written where it is going, so the image
	// is never capped in two files at once.
	require.GreaterOrEqual(t, len(rec.writes), 2)
	assert.Equal(t, pinProject, rec.writes[0].scope)
	assert.Equal(t, policy.Level(""), rec.writes[0].level)
	last := rec.writes[len(rec.writes)-1]
	assert.Equal(t, pinGlobal, last.scope)
	assert.Equal(t, policy.LevelPatch, last.level)
}

func TestSidebarCapCyclesBackToOff(t *testing.T) {
	t.Parallel()

	m, rec := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"), keyMsg("j"))

	m = feed(t, m, keyMsg("+"))
	require.NotEmpty(t, m.currentRow().Pin, "the row should now be capped")

	m = feed(t, m, keyMsg("-"))
	assert.Empty(t, m.currentRow().Pin, "stepping back off the levels stops capping")
	last := rec.writes[len(rec.writes)-1]
	assert.Equal(t, policy.Level(""), last.level)
}

func TestSidebarCapWriteFailureIsAStatusError(t *testing.T) {
	t.Parallel()

	m, rec := sidebarModel(t)
	rec.err = errors.New("permission denied writing .ccu.yaml")

	m = feed(t, m, keyMsg("tab"), keyMsg("j"), keyMsg("+"))

	assert.Equal(t, StatusError, m.statusKind)
	assert.Contains(t, plainText(m.statusLine()), "permission denied writing .ccu.yaml")
	assert.Empty(t, m.currentRow().Pin, "a failed write must not leave the row looking capped")
}

// The detail column claims only the keys it needs. A key it does not claim has
// to reach the list, or tabbing across would trap the user in a mode.
func TestSidebarPassesUnclaimedKeysToTheList(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"))
	require.Equal(t, focusSide, m.focus)
	require.Empty(t, m.collapsed)

	m = feed(t, m, keyMsg("C"))
	assert.NotEmpty(t, m.collapsed, "collapse-all still reaches the list")
	assert.Equal(t, focusSide, m.focus, "and the keyboard stays in the column")
}

// space acts on whatever has the focus. In the column that is the field under
// the cursor, so it steps that setting rather than selecting the row.
func TestSpaceInTheColumnStepsTheFieldNotTheRow(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"))
	selected := m.currentRow().Selected
	before := m.currentRow().Update.LatestTag

	m = feed(t, m, keyMsg(" "))
	assert.Equal(t, selected, m.currentRow().Selected, "the row is not what has the focus")
	assert.NotEqual(t, before, m.currentRow().Update.LatestTag, "the field is")
}

// The footer has to advertise the keys that actually work right now. While the
// sidebar holds the keyboard, that is its own set and not the browsing one.
func TestSidebarFocusSwapsTheFooterHints(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	assert.Equal(t, m.keys.BrowseHints(), m.hintBindings())

	m = feed(t, m, keyMsg("tab"))
	assert.Equal(t, m.keys.SideHints(), m.hintBindings())
}

func TestSidebarRendersTheCurrentImage(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	out := plainText(strings.Join(m.sidebarLines(sidebarWidth(m.width)-boxChrome, 20), "\n"))

	assert.Contains(t, out, "library/traefik")
	assert.Contains(t, out, "target")
	assert.Contains(t, out, "cap")
	assert.Contains(t, out, "off", "an uncapped image reads as cap off")
	assert.NotContains(t, out, "save to", "the scope has nothing to answer until a cap exists")

	// The value the arrow keys would change is between chevrons, which is the
	// only thing on the frame saying the field is editable at all.
	assert.Contains(t, out, "‹")
	assert.Contains(t, out, "›")
}

// The panel says how to reach it only while the list still has the keyboard;
// once the sidebar holds it, the footer names the same keys.
func TestSidebarHintOnlyShowsWhileTheListHasFocus(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	assert.Contains(t, plainText(strings.Join(m.sidebarLines(40, 20), "\n")), "→ to change")

	m = feed(t, m, keyMsg("tab"))
	assert.NotContains(t, plainText(strings.Join(m.sidebarLines(40, 20), "\n")), "→ to change")
}

func TestHelpDialogSwapsTheFooterHints(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m.width, m.height = 200, 30

	assert.Equal(t, m.keys.BrowseHints(), m.hintBindings())
	assert.Equal(t, m.keys.HelpHints(), feed(t, m, keyMsg("?")).hintBindings())
}

// →/l on a row is the way into the detail column. It used to step to the next
// row of the same file, which is what `j` already does — a key spent on nothing.
func TestRightOpensTheDetailColumnOnARow(t *testing.T) {
	t.Parallel()

	for _, k := range []string{"right", "l"} {
		t.Run(k, func(t *testing.T) {
			t.Parallel()

			m, _ := sidebarModel(t)
			require.NotNil(t, m.currentRow())

			m = feed(t, m, keyMsg(k))
			assert.Equal(t, focusSide, m.focus)
		})
	}
}

// ←/→ in the column step the options of the field under the cursor. A column of
// settings is walked along, not folded, so the arrows are worth more here than a
// second way out — which tab and esc already are.
func TestArrowsStepTheColumnsOptions(t *testing.T) {
	t.Parallel()

	for _, keys := range [][2]string{{"right", "left"}, {"l", "h"}} {
		t.Run(keys[0], func(t *testing.T) {
			t.Parallel()

			m, _ := sidebarModel(t)
			m = feed(t, m, keyMsg("tab"))
			require.Equal(t, focusSide, m.focus)
			before := m.currentRow().Update.LatestTag

			m = feed(t, m, keyMsg(keys[0]))
			assert.Equal(t, focusSide, m.focus, "the arrow must not leave the column")
			assert.NotEqual(t, before, m.currentRow().Update.LatestTag, "→ steps to the next option")

			m = feed(t, m, keyMsg(keys[1]))
			assert.Equal(t, focusSide, m.focus)
			assert.Equal(t, before, m.currentRow().Update.LatestTag, "← steps back")
		})
	}
}

// With the arrows spent on the options, tab and esc are the whole way out — and
// both have to work, or a user who reaches for either is stuck in the column.
func TestTabAndEscLeaveTheDetailColumn(t *testing.T) {
	t.Parallel()

	for _, k := range []string{"tab", "esc"} {
		t.Run(k, func(t *testing.T) {
			t.Parallel()

			m, _ := sidebarModel(t)
			m = feed(t, m, keyMsg("tab"))
			require.Equal(t, focusSide, m.focus)

			m = feed(t, m, keyMsg(k))
			assert.Equal(t, focusList, m.focus)
		})
	}
}

// On a header the tree keys still walk the tree: there is no column to open
// beside a directory.
func TestRightStillExpandsOnAHeader(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m.cursor = 0
	require.Nil(t, m.currentRow())
	m = feed(t, m, keyMsg("z")) // fold it first, so there is something to expand
	require.NotEmpty(t, m.collapsed)

	m = feed(t, m, keyMsg("right"))
	assert.NotEqual(t, focusSide, m.focus)
	assert.False(t, m.collapsed[m.cursorGroup()], "→ on a header expands it")
}

// With no column to open, →/l falls back to walking the tree rather than
// silently doing nothing.
func TestRightWalksTheTreeWhenTheColumnIsTooNarrow(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m.width = sidebarMinStacked - 1
	require.Equal(t, sidebarNowhere, m.sidebarPlacement())
	require.NotNil(t, m.currentRow())

	m = feed(t, m, keyMsg("right"))
	assert.Equal(t, focusList, m.focus)
}

// The values in the column moved off the arrows and onto +/-, which is what
// freed ←/h and →/l to mean the same thing in both panes.
func TestColumnValuesStepWithPlusAndMinus(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m = feed(t, m, keyMsg("tab"))
	require.Equal(t, focusSide, m.focus)
	before := m.currentRow().Update.LatestTag

	m = feed(t, m, keyMsg("+"))
	assert.NotEqual(t, before, m.currentRow().Update.LatestTag, "+ steps the value")

	m = feed(t, m, keyMsg("-"))
	assert.Equal(t, before, m.currentRow().Update.LatestTag, "- steps it back")
}
