package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/forge"
)

const notesSource = "https://github.com/owner/app"

// fakeForge stands in for the forge client: it answers from a fixed list and
// counts the requests, per repository, so a test can tell a cached answer from
// a second fetch.
type fakeForge struct {
	mu       sync.Mutex
	calls    map[string]int
	releases []forge.Release
	err      error
}

func (f *fakeForge) fetch(_ context.Context, source string) ([]forge.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[source]++
	return f.releases, f.err
}

func (f *fakeForge) count(source string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[source]
}

func sampleReleases() []forge.Release {
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	return []forge.Release{
		{Tag: "v1.3.0", Name: "Too new", Body: "not in range", URL: notesSource + "/releases/tag/v1.3.0"},
		{Tag: "v1.2.0", Name: "Harbour", Body: "## Features\n\n- faster **startup**\n- quieter logs", URL: notesSource + "/releases/tag/v1.2.0", Published: day},
		{Tag: "v1.1.0", Body: "Fixes a crash on boot.", URL: notesSource + "/releases/tag/v1.1.0", Published: day.AddDate(0, -1, 0)},
		{Tag: "v1.0.0", Body: "already running this", URL: notesSource + "/releases/tag/v1.0.0"},
	}
}

// notesModel is a browsing model with the cursor on a row whose image records a
// source, and the forge replaced by f.
// Further rows can be passed as extra events. They go in, and the cursor is
// placed, before the sidebar has room to sit beside the list: feed drops the
// commands Update returns, and a prefetch marked but never run would leave the
// repository looking as if it were loading forever.
func notesModel(t *testing.T, f *fakeForge, extra ...tea.Msg) Model {
	t.Helper()
	m := newTestModel()
	m.fetchNotes = f.fetch
	ev := updateEvent("a/compose.yml", "owner/app", "1.0.0", "1.2.0", "minor")
	ev.ev.Update.SourceURL = notesSource
	m = feed(t, m, append([]tea.Msg{ev}, extra...)...)
	m = moveToRow(t, m)
	m.width, m.height = 120, 40
	return m
}

// step feeds one message and then runs whatever commands come back, feeding the
// notes answers among their results in turn — the part of Bubble Tea's loop the
// notes depend on, without the rest of it.
func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	m, ok := next.(Model)
	require.True(t, ok)
	for _, res := range runCmd(cmd) {
		if nm, ok := res.(notesMsg); ok {
			m = step(t, m, nm)
		}
	}
	return m
}

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestNotesKeyOpensAndClosesThePane(t *testing.T) {
	t.Parallel()

	for _, closer := range []string{"esc", "q", "r"} {
		t.Run(closer, func(t *testing.T) {
			t.Parallel()

			m := notesModel(t, &fakeForge{releases: sampleReleases()})
			m = step(t, m, keyMsg("r"))
			require.True(t, m.showNotes)
			assert.Equal(t, m.keys.NotesHints(), m.hintBindings())

			var msg tea.KeyMsg
			if closer == "esc" {
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			} else {
				msg = keyMsg(closer)
			}
			next, cmd := m.Update(msg)
			m = next.(Model)
			assert.False(t, m.showNotes, "%s closes the pane", closer)
			assert.NotEqual(t, phaseDone, m.phase, "%s must not quit from the pane", closer)
			assert.Nil(t, cmd)
		})
	}
}

// A header names no image, so there is nothing to read notes for.
func TestNotesKeyOnAHeaderDoesNotOpenThePane(t *testing.T) {
	t.Parallel()

	m := notesModel(t, &fakeForge{})
	m.cursor = 0
	require.Nil(t, m.currentRow())

	m = step(t, m, keyMsg("r"))
	assert.False(t, m.showNotes)
}

func TestNotesPaneShowsLoadingThenTheReleases(t *testing.T) {
	t.Parallel()

	m := notesModel(t, &fakeForge{releases: sampleReleases()})

	// The answer held back, to see the pane while it is on its way.
	next, cmd := m.Update(keyMsg("r"))
	m = next.(Model)
	require.NotNil(t, cmd)
	assert.Contains(t, plainText(m.View()), "loading release notes")

	for _, res := range runCmd(cmd) {
		m = feed(t, m, res)
	}
	view := plainText(m.View())

	assert.Contains(t, view, "owner/app")
	assert.Contains(t, view, "1.0.0 → 1.2.0")
	assert.Contains(t, view, "2 release(s)")
	assert.Contains(t, view, "v1.2.0 Harbour")
	assert.Contains(t, view, "2026-03-01")
	assert.Contains(t, view, "faster startup", "markdown is rendered, not shown raw")
	assert.NotContains(t, view, "**startup**")
	assert.Contains(t, view, "Fixes a crash on boot.")
	assert.NotContains(t, view, "Too new", "past the target is not part of the move")
	assert.NotContains(t, view, "already running this", "the current release is not news")

	// Newest first.
	assert.Less(t, strings.Index(view, "v1.2.0"), strings.Index(view, "v1.1.0"))

	// The newest release's own page, whole, so it can be copied.
	assert.Contains(t, view, notesSource+"/releases/tag/v1.2.0")
}

func TestNotesPaneErrorStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		err    error
		want   []string
	}{
		{
			name:   "rate limited",
			source: notesSource,
			err:    fmt.Errorf("github: %w", forge.ErrRateLimited),
			want:   []string{"GITHUB_TOKEN", "GH_TOKEN"},
		},
		{
			name:   "unsupported forge",
			source: "https://git.example.com/team/app",
			err:    forge.ErrUnsupported,
			want:   []string{"not readable from git.example.com", "https://git.example.com/team/app"},
		},
		{
			name:   "anything else",
			source: notesSource,
			err:    errors.New("connection reset"),
			want:   []string{"could not read the release notes", "connection reset"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := notesModel(t, &fakeForge{err: tt.err})
			m.currentRow().Update.SourceURL = tt.source
			m = step(t, m, keyMsg("r"))

			view := plainText(m.View())
			for _, w := range tt.want {
				assert.Contains(t, view, w)
			}
		})
	}
}

func TestNotesPaneWithoutASource(t *testing.T) {
	t.Parallel()

	f := &fakeForge{}
	m := notesModel(t, f)
	m.currentRow().Update.SourceURL = ""
	m = step(t, m, keyMsg("r"))

	assert.Contains(t, plainText(m.View()), "records no source repository")
	assert.Zero(t, f.count(""), "nothing to fetch without a source")
}

// A repository that released nothing in range still has a page listing what it
// did release, which is the one useful thing left to offer.
func TestNotesPaneWithNoMatchingReleases(t *testing.T) {
	t.Parallel()

	m := notesModel(t, &fakeForge{releases: []forge.Release{{Tag: "v0.1.0", Body: "ancient"}}})
	m = step(t, m, keyMsg("r"))

	view := plainText(m.View())
	assert.Contains(t, view, "no release notes between 1.0.0 and 1.2.0")
	assert.Contains(t, view, notesSource+"/releases")
	assert.NotContains(t, view, "/releases/tag/", "the constructed release link may not exist")
}

// A failed fetch is retried when the user opens the pane again on purpose, but
// the answer to one that worked is kept for the session.
func TestNotesAreFetchedOncePerRepository(t *testing.T) {
	t.Parallel()

	// A second row from the same repository: moving onto it costs no request.
	ev := updateEvent("b/compose.yml", "owner/app-worker", "1.0.0", "1.2.0", "minor")
	ev.ev.Update.SourceURL = notesSource
	f := &fakeForge{releases: sampleReleases()}
	m := notesModel(t, f, ev)

	m = step(t, m, keyMsg("r"))
	m = step(t, m, keyMsg("r"))
	m = step(t, m, keyMsg("r"))
	require.True(t, m.showNotes)
	m = step(t, m, keyMsg("r"))

	for range len(m.entries) {
		m = step(t, m, keyMsg("j"))
	}
	m = step(t, m, keyMsg("r"))
	assert.Equal(t, 1, f.count(notesSource))
}

func TestFailedNotesAreRetriedOnlyWhenAskedFor(t *testing.T) {
	t.Parallel()

	f := &fakeForge{err: forge.ErrRateLimited}
	m := notesModel(t, f)
	m = step(t, m, keyMsg("r"))
	m = step(t, m, keyMsg("r")) // closed
	require.Equal(t, 1, f.count(notesSource))

	// Wiggling the cursor is no request to try again.
	m = step(t, m, keyMsg("k"))
	m = step(t, m, keyMsg("j"))
	assert.Equal(t, 1, f.count(notesSource))

	f.err = nil
	f.releases = sampleReleases()
	m = step(t, m, keyMsg("r"))
	assert.Equal(t, 2, f.count(notesSource))
	assert.Contains(t, plainText(m.View()), "v1.2.0")
}

// An answer that arrives after the user moved on belongs to its repository, not
// to the row under the cursor.
func TestLateNotesLandOnTheirOwnRepository(t *testing.T) {
	t.Parallel()

	m := notesModel(t, &fakeForge{})
	m.fetchNotes = nil // no prefetch of the cursor's own repository
	m = feed(t, m, notesMsg{source: "https://github.com/other/thing", releases: sampleReleases()})

	assert.Contains(t, m.notes, "https://github.com/other/thing")
	assert.Empty(t, m.notesSummary(m.currentRow()))
}

// The sidebar learns about the notes before the pane is opened, so it can say
// there is something to read.
func TestSidebarPrefetchesAndSummarisesTheNotes(t *testing.T) {
	t.Parallel()

	f := &fakeForge{releases: sampleReleases()}
	m := notesModel(t, f)
	m.width = 200
	require.Equal(t, sidebarBeside, m.sidebarPlacement())

	// Any message will do: the prefetch follows the cursor, whatever moved it.
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m = next.(Model)
	require.NotNil(t, cmd, "landing on a row with a source prefetches its notes")
	assert.Contains(t, plainText(m.View()), "loading notes…")

	for _, res := range runCmd(cmd) {
		m = feed(t, m, res)
	}
	view := plainText(m.View())
	assert.Contains(t, view, "2 releases · r to read")
	assert.Equal(t, 1, f.count(notesSource))

	// The link now names the real release page, not one built from the image tag.
	assert.Equal(t, notesSource+"/releases/tag/v1.2.0", m.sidebarLink(m.currentRow()))
}

// A stacked or missing sidebar has no line for the summary, so it must not
// spend a request on one.
func TestNoPrefetchWithoutASidebarBeside(t *testing.T) {
	t.Parallel()

	for _, width := range []int{sidebarMinStacked - 1, sidebarMinTotal - 1} {
		f := &fakeForge{releases: sampleReleases()}
		step(t, notesModel(t, f), tea.WindowSizeMsg{Width: width, Height: 40})
		assert.Zero(t, f.count(notesSource), "width %d", width)
	}
}

func TestNotesPaneScrollsWithinItsBody(t *testing.T) {
	t.Parallel()

	var long strings.Builder
	for i := range 80 {
		fmt.Fprintf(&long, "- change number %d\n", i)
	}
	rels := sampleReleases()
	rels[1].Body = long.String()

	m := notesModel(t, &fakeForge{releases: rels})
	m.height = 20
	m = step(t, m, keyMsg("r"))

	body, h := len(m.notesBody()), m.notesBodyHeight()
	require.Greater(t, body, h, "the notes have to be longer than the pane")

	m = step(t, m, keyMsg("k"))
	assert.Zero(t, m.notesOffset, "no scrolling above the top")

	m = step(t, m, keyMsg("j"))
	assert.Equal(t, 1, m.notesOffset)

	m = step(t, m, keyMsg("G"))
	assert.Equal(t, body-h, m.notesOffset)
	m = step(t, m, keyMsg("j"))
	assert.Equal(t, body-h, m.notesOffset, "no scrolling past the end")
	assert.Contains(t, plainText(m.View()), "Fixes a crash on boot.", "the end of the notes is on screen")

	m = step(t, m, keyMsg("k"))
	assert.Equal(t, body-h-1, m.notesOffset, "one line up from the bottom, no overshoot to work off")

	m = step(t, m, keyMsg("g"))
	assert.Zero(t, m.notesOffset)
	m = step(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, h-1, m.notesOffset)

	// Every frame is exactly as tall as the terminal, whatever the offset.
	assert.Len(t, strings.Split(m.View(), "\n"), m.height)
}

func TestNotesPaneOnNarrowTerminals(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{20, minViewHeight}, {34, 10}, {60, 12}, {sidebarMinTotal, 30}} {
		m := notesModel(t, &fakeForge{releases: sampleReleases()})
		m = step(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = step(t, m, keyMsg("r"))
		m = step(t, m, keyMsg("G"))

		lines := strings.Split(m.View(), "\n")
		assert.Len(t, lines, size[1], "size %v", size)
		for _, l := range lines {
			assert.LessOrEqual(t, len([]rune(plainText(l))), clampWidth(size[0]), "size %v: %q", size, plainText(l))
		}
	}
}

// Notes are someone else's text; nothing in them may reach the terminal as an
// escape sequence, and GitHub's line endings must not leave stray \r behind.
func TestSanitizeMarkdown(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "line one\nline two[31m red", sanitizeMarkdown("line one\r\nline two\x1b[31m red"))
}

func TestPlainNotesKeepsLineBreaks(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"- one", "", "- two words"}, plainNotes("\n- one\n\n- two words\n", 20))
}
