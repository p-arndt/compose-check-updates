package tui

import (
	"errors"
	"log/slog"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

	m = feed(t, m, keyMsg("j")) // cursor on nginx
	require.Equal(t, "nginx", m.currentRow().Update.ImageName)

	// A row sorting above the cursor arrives mid-scan.
	m = feed(t, m, updateEvent("a/compose.yml", "postgres", "15", "16", "major"))

	assert.Equal(t, "nginx", m.currentRow().Update.ImageName)
	assert.Equal(t, 2, m.cursor)
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

func TestEnterWithNothingSelectedStaysBrowsing(t *testing.T) {
	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))

	m = feed(t, m, keyMsg("enter"))

	assert.Equal(t, phaseBrowsing, m.phase)
	assert.Equal(t, StatusWarn, m.statusKind)
	assert.NotEmpty(t, m.statusText)
}

func TestEnterWithSelectionEntersApplying(t *testing.T) {
	m := newTestModel()
	m = feed(t, m, updateEvent("a/compose.yml", "caddy", "2.7", "2.8", "minor"))
	m = feed(t, m, keyMsg(" "))

	next, cmd := m.Update(keyMsg("enter"))
	m = next.(Model)

	assert.Equal(t, phaseApplying, m.phase)
	assert.NotNil(t, cmd)
	assert.Equal(t, 1, m.applyActive)
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

	m = feed(t, m, keyMsg("j"), keyMsg("j"), keyMsg("j"))
	assert.Equal(t, len(m.visible)-1, m.cursor)

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, m.cursor)

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, len(m.visible)-1, m.cursor)
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
	assert.Contains(t, m.statusLine(), "Skipping (failed fetching tags)")
	assert.Contains(t, m.statusLine(), "1 issue(s)")

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

	m = feed(t, m, keyMsg(" "))
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

	m = feed(t, m, keyMsg("j")) // onto traefik
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
	m = feed(t, m, keyMsg("T"))
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
	m = feed(t, m, keyMsg("t"), keyMsg("T"))
	assert.False(t, m.rows[0].NoTarget)
	assert.Equal(t, "digest", m.rows[0].Level)
	assert.Equal(t, "latest", m.rows[0].Update.LatestTag)
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
