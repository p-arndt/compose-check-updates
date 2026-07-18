package tui

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/padi2312/compose-check-updates/internal"
	"github.com/padi2312/compose-check-updates/internal/scanner"
)

func newTestModel() Model {
	m := NewModel(scanner.Options{})
	m.phase = phaseBrowsing
	return m
}

func updateEvent(path, image, current, latest, level string) scanEventMsg {
	return scanEventMsg{ev: scanner.Event{
		Kind:  scanner.EventUpdate,
		Path:  path,
		Level: level,
		Update: internal.UpdateInfo{
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
	ev.ev.Level = u.UpdateLevel()
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
	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))

	m = feed(t, m, keyMsg("A"))

	assert.Equal(t, phaseBrowsing, m.phase)
	assert.Equal(t, StatusWarn, m.statusKind)
	assert.NotEmpty(t, m.statusText)
}

func TestApplyWithSelectionEntersApplying(t *testing.T) {
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
	for _, k := range []string{"A", "u"} {
		t.Run(k, func(t *testing.T) {
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
	prev := slog.Default()

	c, restore := captureSlog(slog.LevelWarn)
	require.NotSame(t, prev, slog.Default(), "the terminal handler must be displaced")

	slog.Warn("Skipping (failed fetching tags)", "Image", "myapp")
	require.Len(t, c.all(), 1, "the record must be captured, not written to the terminal")

	restore()
	assert.Same(t, prev, slog.Default())
}

func TestCapturedLogsSurfaceInTheStatusLine(t *testing.T) {
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
	m := newTestModel()
	m = feed(t, m, levelEvent("traefik", "v2.9.3", "2.9.4", "2.11.0", "3.7.8"))

	// The historical behaviour — offer the highest version — must survive.
	assert.Equal(t, TargetMajor, m.target)
	assert.Equal(t, "3.7.8", rowFor(t, m, "traefik").Update.LatestTag)
	assert.Equal(t, "major", rowFor(t, m, "traefik").Level)
}

func TestGlobalTargetCyclingRepointsRows(t *testing.T) {
	m := newTestModel()
	m = feed(t, m, levelEvent("traefik", "v2.9.3", "2.9.4", "2.11.0", "3.7.8"))
	require.Equal(t, "3.7.8", rowFor(t, m, "traefik").Update.LatestTag)

	m = feed(t, m, keyMsg("t")) // major → patch
	assert.Equal(t, TargetPatch, m.target)
	row := rowFor(t, m, "traefik")
	assert.Equal(t, "2.9.4", row.Update.LatestTag)
	assert.Equal(t, "patch", row.Level, "the badge must follow the selected tag")

	m = feed(t, m, keyMsg("t")) // patch → minor
	assert.Equal(t, "2.11.0", rowFor(t, m, "traefik").Update.LatestTag)
	assert.Equal(t, "minor", rowFor(t, m, "traefik").Level)

	m = feed(t, m, keyMsg("t")) // minor → major, back where we started
	assert.Equal(t, TargetMajor, m.target)
	assert.Equal(t, "3.7.8", rowFor(t, m, "traefik").Update.LatestTag)
}

func TestGlobalTargetDeselectsRowsWithNothingAtThatLevel(t *testing.T) {
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
	m := newTestModel()
	m = feed(t, m, levelEvent("postgres", "15", "", "", "16"))
	m = feed(t, m, keyMsg("t")) // → patch, nothing available

	m = feed(t, m, keyMsg("j"), keyMsg(" "))
	require.NotNil(t, m.currentRow())
	assert.Equal(t, 0, m.selectedCount())
}

func TestRowTargetCyclingStaysWithinAvailableTargets(t *testing.T) {
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
	require.Equal(t, []string{"patch", "major"}, avail)

	// Forward from major wraps to patch, skipping the minor level it has no
	// release for.
	m = feed(t, m, keyMsg("T"))
	assert.Equal(t, "2.9.4", m.currentRow().Update.LatestTag)
	assert.Equal(t, "patch", m.currentRow().Level)

	m = feed(t, m, keyMsg("T"))
	assert.Equal(t, "3.7.8", m.currentRow().Update.LatestTag)
	assert.Equal(t, "major", m.currentRow().Level)

	// Backwards, too — and never onto a level the image does not have.
	for i := 0; i < 5; i++ {
		m = feed(t, m, tea.KeyMsg{Type: tea.KeyLeft})
		require.Contains(t, avail, string(m.currentRow().Target))
		require.False(t, m.currentRow().NoTarget)
	}

	// The other row was not touched by a per-row change.
	assert.Equal(t, "8.0.0", rowFor(t, m, "redis").Update.LatestTag)
}

func TestRowTargetCyclingRecoversARowWithNoGlobalTarget(t *testing.T) {
	m := newTestModel()
	m = feed(t, m, levelEvent("postgres", "15", "", "", "16"))
	m = feed(t, m, keyMsg("t")) // → patch
	require.True(t, m.rows[0].NoTarget)

	// Per-image control is the point of the feature: the row can be pointed back
	// at the only level it actually has.
	m = feed(t, m, keyMsg("j"), keyMsg("T"))
	assert.False(t, m.rows[0].NoTarget)
	assert.Equal(t, TargetMajor, m.rows[0].Target)
	assert.Equal(t, "16", m.rows[0].Update.LatestTag)
	assert.True(t, m.rows[0].Actionable())
}

func TestRowTargetIsANoopForDigestOnlyRows(t *testing.T) {
	m := newTestModel()
	ev := updateEvent("a/compose.yml", "myapp", "latest", "latest", "digest")
	ev.ev.Update.CurrentDigest = "sha256:aaaa"
	ev.ev.Update.LatestDigest = "sha256:bbbb"
	m = feed(t, m, ev)

	// No levels to choose between: the row must survive both keys unchanged.
	m = feed(t, m, keyMsg("j"), keyMsg("t"), keyMsg("T"))
	assert.False(t, m.rows[0].NoTarget)
	assert.Equal(t, "digest", m.rows[0].Level)
	assert.Equal(t, "latest", m.rows[0].Update.LatestTag)
}

// frameHeight counts rendered rows. blockHeight trims trailing blanks, which is
// exactly what must NOT be ignored here: a frame one line off scrolls the alt
// screen and the UI shakes on every keypress.
func frameHeight(s string) int { return len(strings.Split(s, "\n")) }

func TestFrameIsExactlyTerminalHeight(t *testing.T) {
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
	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m.width, m.height = 200, 40 // far taller than the two-entry list needs

	lines := strings.Split(m.View(), "\n")
	require.Len(t, lines, 40)

	// The hints are the final row, the legend the one above it, and the space
	// between them and the list is padding rather than dead space at the bottom.
	assert.Contains(t, plainText(lines[39]), "q quit")
	assert.Contains(t, plainText(lines[38]), "target")
	assert.Equal(t, "", strings.TrimSpace(plainText(lines[30])))
}

// Two bindings answering the same key in the same phase means one of them
// silently never fires, which is exactly the bug adding fold keys could cause.
func TestNoKeyIsBoundTwiceInTheBrowsingPhase(t *testing.T) {
	k := DefaultKeyMap()
	named := map[string][]key.Binding{
		"up": {k.Up}, "down": {k.Down}, "pgup": {k.PageUp}, "pgdown": {k.PageDown},
		"home": {k.Home}, "end": {k.End}, "toggle": {k.Toggle},
		"selectAll": {k.SelectAll}, "selectNone": {k.SelectNone},
		"toggleGroup": {k.ToggleGroup}, "collapseAll": {k.CollapseAll}, "expandAll": {k.ExpandAll},
		"filter": {k.Filter}, "target": {k.Target}, "rowNext": {k.RowNext}, "rowPrev": {k.RowPrev},
		"detail": {k.Detail}, "issues": {k.Issues},
		"apply": {k.Apply}, "applyRow": {k.ApplyRow}, "help": {k.Help}, "quit": {k.Quit},
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

	// The two write keys are the whole point of the rebinding: enter toggles and
	// nothing else, and `a`/`A` are separate keys with separate meanings.
	assert.Equal(t, []string{" ", "enter"}, k.Toggle.Keys())
	assert.Equal(t, []string{"A"}, k.Apply.Keys())
	assert.Equal(t, []string{"u"}, k.ApplyRow.Keys())
	assert.Equal(t, []string{"a"}, k.SelectAll.Keys())
	assert.NotContains(t, k.Apply.Keys(), "enter", "enter must never write to disk")

	// IssuesClose is read only while the issues pane is open, where the browsing
	// keys above are not — the one reason it may share esc with Quit.
	assert.Equal(t, []string{"esc"}, k.IssuesClose.Keys())

	// Yes/No live in the restart phase only, where none of the above are read —
	// which is the one reason `n` may also mean SelectNone while browsing.
	assert.Equal(t, []string{"n"}, k.No.Keys())
	assert.Equal(t, []string{"y"}, k.Yes.Keys())
}

// The hint footer is the only thing telling a first-time user the keys exist,
// so it must be on screen without pressing anything.
func TestKeyHintFooterIsAlwaysVisible(t *testing.T) {
	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m.width, m.height = 200, 24
	require.False(t, m.showHelp, "the footer must not depend on ?")

	v := plainText(m.View())
	assert.Contains(t, v, "space/enter toggle")
	assert.Contains(t, v, "z fold group")
	assert.Contains(t, v, "A apply selected")
	assert.Contains(t, v, "u apply row")
	assert.Contains(t, v, "? help")

	// ? expands the one-liner into the grouped listing rather than revealing it.
	exp := feed(t, m, keyMsg("?"))
	require.True(t, exp.showHelp)
	ev := plainText(exp.View())
	assert.Contains(t, ev, "C collapse all")
	assert.Contains(t, ev, "E expand all")
	assert.Greater(t, exp.blockHeight(exp.expandedHelp()), 1, "the expanded help is multi-line")

	// Both forms are budgeted for: the list never spills past its window.
	for _, mm := range []Model{m, exp} {
		assert.LessOrEqual(t, len(strings.Split(mm.listView(), "\n")), mm.listHeight())
		assert.LessOrEqual(t, mm.blockHeight(mm.View()), mm.height)
	}
}

func TestHintFooterIsContextualPerPhase(t *testing.T) {
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
