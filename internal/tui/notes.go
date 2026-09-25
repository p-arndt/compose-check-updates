package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/forge"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

// The release notes pane answers the question the list cannot: what the move
// from the current tag to the target actually changes. The notes come from the
// forge the image's source label names, fetched once per repository and kept for
// the session, since two rows of the same image — or two images built from one
// repository — read the very same release list.

// notesFetcher lists a repository's releases, newest first. A field on the Model
// rather than a direct forge call, in the same spirit as setCap: a test hands in
// a stub and never reaches the network.
type notesFetcher func(ctx context.Context, sourceURL string) ([]forge.Release, error)

// notesMinTTL is the floor under the notes cache's lifetime. A published
// release's notes hardly ever change, and an unauthenticated GitHub client gets
// sixty requests an hour — a run TTL of minutes would spend that quota on
// answers that were already right.
const notesMinTTL = time.Hour

// newNotesFetcher builds the real client. It keeps its answers under the run's
// registry cache, so CCU_NO_CACHE and CCU_CACHE_DIR govern both: a cache the
// user switched off is off for notes too.
func newNotesFetcher(opts scanner.Options) notesFetcher {
	gh, gl := forge.TokensFromEnv()
	dir := ""
	if d := opts.Cache.Dir(); d != "" {
		dir = filepath.Join(d, "notes")
	}
	client := forge.New(forge.Options{
		GitHubToken: gh,
		GitLabToken: gl,
		CacheDir:    dir,
		TTL:         max(opts.Cache.TTL(), notesMinTTL),
		Refresh:     opts.Cache.Refresh(),
	})
	return client.Releases
}

type notesState int

const (
	notesLoading notesState = iota + 1
	notesLoaded
	notesFailed
)

// notesEntry is what is known about one repository's releases. It is keyed by
// source URL, never by row: rows are re-sorted and dropped under it, a
// repository is not.
type notesEntry struct {
	state    notesState
	releases []forge.Release
	err      error
}

// notesMsg carries a fetch's answer. The source travels with it so an answer
// arriving after the cursor has moved on lands in the cache for the repository
// it belongs to, which is where it would have gone anyway.
type notesMsg struct {
	source   string
	releases []forge.Release
	err      error
}

// requestNotes starts a fetch for source unless one has already been made. A
// failed fetch is only retried when retry is set, which is the user opening the
// pane on purpose; the prefetch never retries, or a rate-limited forge would be
// asked again on every cursor move.
func (m *Model) requestNotes(source string, retry bool) tea.Cmd {
	if source == "" || m.fetchNotes == nil {
		return nil
	}
	if m.notes == nil {
		m.notes = make(map[string]notesEntry)
	}
	if e, ok := m.notes[source]; ok && (e.state != notesFailed || !retry) {
		return nil
	}
	m.notes[source] = notesEntry{state: notesLoading}

	ctx, fetch := m.ctx, m.fetchNotes
	return func() tea.Msg {
		releases, err := fetch(ctx, source)
		return notesMsg{source: source, releases: releases, err: err}
	}
}

// handleNotes stores an answer. Nothing else about the model depends on it, so
// an answer for a repository the cursor has left is harmless.
func (m *Model) handleNotes(msg notesMsg) {
	if m.notes == nil {
		m.notes = make(map[string]notesEntry)
	}
	if msg.err != nil {
		m.notes[msg.source] = notesEntry{state: notesFailed, err: msg.err}
	} else {
		m.notes[msg.source] = notesEntry{state: notesLoaded, releases: msg.releases}
	}
	m.clampNotesOffset()
}

// prefetchNotes asks for the notes of the row under the cursor while the
// sidebar is beside the list, so the sidebar can say how many releases there
// are to read before the user opens the pane. Only beside: the stacked panel has
// no line to spare for it, and a fetch nobody sees is quota spent for nothing.
func (m *Model) prefetchNotes() tea.Cmd {
	if m.showNotes || (m.phase != phaseScanning && m.phase != phaseBrowsing) {
		return nil
	}
	if m.sidebarPlacement() != sidebarBeside {
		return nil
	}
	r := m.currentRow()
	if r == nil {
		return nil
	}
	return m.requestNotes(r.Update.SourceURL, false)
}

// openNotes opens the pane for the row under the cursor. A header describes no
// image, so there is nothing to open there.
func (m *Model) openNotes() tea.Cmd {
	r := m.currentRow()
	if r == nil {
		m.setStatus(StatusInfo, "move onto an image to read its release notes")
		return nil
	}
	m.showNotes = true
	m.notesOffset = 0
	return m.requestNotes(r.Update.SourceURL, true)
}

// handleNotesKey drives the pane. esc, q and the key that opened it all close
// it — q reads as "done reading" in every pager — and ctrl+c still quits.
func (m Model) handleNotesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := max(m.notesBodyHeight()-1, 1)
	switch {
	case key.Matches(msg, m.keys.NotesClose):
		m.showNotes = false
		m.syncScroll()
		return m, nil
	case key.Matches(msg, m.keys.Quit):
		return m, m.quit()
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.notesOffset--
	case key.Matches(msg, m.keys.Down):
		m.notesOffset++
	case key.Matches(msg, m.keys.PageUp):
		m.notesOffset -= page
	case key.Matches(msg, m.keys.PageDown):
		m.notesOffset += page
	case key.Matches(msg, m.keys.NotesTop):
		m.notesOffset = 0
	case key.Matches(msg, m.keys.NotesBottom):
		m.notesOffset = len(m.notesBody())
	}
	m.clampNotesOffset()
	return m, nil
}

// clampNotesOffset keeps the window inside the body, so a G followed by k moves
// one line up rather than first working off the overshoot.
func (m *Model) clampNotesOffset() {
	if !m.showNotes {
		return
	}
	m.notesOffset = max(min(m.notesOffset, len(m.notesBody())-m.notesBodyHeight()), 0)
}

// notesPaneHeight is every row of the pane: the full-width views take the rows
// the boxes' borders and the stacked panel would otherwise have used.
func (m Model) notesPaneHeight() int { return m.fullPaneHeight() }

// notesBodyHeight is what is left for the scrolling part once the fixed header
// and the link at the bottom are drawn.
func (m Model) notesBodyHeight() int {
	return max(m.notesPaneHeight()-len(m.notesHeader())-len(m.notesFooter()), 1)
}

// notesWidth is the width the notes are wrapped to.
func (m Model) notesWidth() int { return clampWidth(m.width) }

// notesView renders the pane: a fixed header naming the image and the move, the
// scrolling body, and the link pinned to the bottom so it can always be copied.
func (m Model) notesView() string {
	header, footer := m.notesHeader(), m.notesFooter()
	body := m.notesBody()

	h := m.notesBodyHeight()
	offset := max(min(m.notesOffset, len(body)-h), 0)
	window := body[offset:min(offset+h, len(body))]

	out := make([]string, 0, m.notesPaneHeight())
	out = append(out, header...)
	out = append(out, window...)
	// The link sits on the last rows whatever the body's length, so the eye
	// finds it in the same place on every image.
	for len(out)+len(footer) < m.notesPaneHeight() {
		out = append(out, "")
	}
	out = append(out, footer...)
	return strings.Join(out, "\n")
}

// notesHeader is the part of the pane that does not scroll.
func (m Model) notesHeader() []string {
	w := m.notesWidth()
	r := m.currentRow()
	if r == nil {
		return []string{m.theme.dim().Render(fit("no image under the cursor", w))}
	}
	u := r.Update
	title := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render("RELEASE NOTES") +
		m.theme.dim().Render("  ") + lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render(u.ImageName)
	move := m.theme.VersionDelta(u.CurrentTag, u.LatestTag, r.Level)
	if notes, ok := m.rowNotes(r); ok {
		move += m.theme.dim().Render(fmt.Sprintf("  · %d release(s)", len(notes)))
	}
	return []string{fit(title, w), fit(move, w), ""}
}

// notesFooter is the link at the bottom: the newest release in range when there
// is one, otherwise the best page ccu can name for reading the rest by hand.
func (m Model) notesFooter() []string {
	r := m.currentRow()
	if r == nil {
		return nil
	}
	link := m.sidebarLink(r)
	if link == "" {
		return nil
	}
	w := m.notesWidth()
	// The whole URL, scheme included, while it fits: this is the line a user
	// copies into a browser.
	if lipgloss.Width(link) > w {
		link = fitLink(link, w)
	}
	return []string{"", m.theme.dim().Render(link)}
}

// rowNotes is the releases a row moves across, once its repository's list is in.
func (m Model) rowNotes(r *Row) ([]forge.Release, bool) {
	e, ok := m.notes[r.Update.SourceURL]
	if !ok || e.state != notesLoaded {
		return nil, false
	}
	return r.Update.NotesFor(e.releases), true
}

// notesBody is the scrolling part of the pane for the row under the cursor.
// Markdown is rendered once per repository, move and width and kept: View runs
// on every keypress, and glamour is far too slow to run that often.
func (m Model) notesBody() []string {
	r := m.currentRow()
	if r == nil {
		return nil
	}
	w := m.notesWidth()
	source := r.Update.SourceURL
	e := m.notes[source]

	switch {
	case source == "":
		return m.notesMessage("this image records no source repository, so there are no release notes to read", w)
	case m.fetchNotes == nil:
		return m.notesMessage("release notes are not available in this session", w)
	case e.state == notesLoading || e.state == 0:
		return m.notesMessage(m.spinner.View()+" loading release notes…", w)
	case e.state == notesFailed:
		return m.notesMessage(notesErrorText(source, e.err), w)
	}

	notes := r.Update.NotesFor(e.releases)
	if len(notes) == 0 {
		return m.notesMessage(fmt.Sprintf("%s publishes no release notes between %s and %s — the link below lists every release",
			repoOnly(stripScheme(source)), r.Update.CurrentTag, r.Update.LatestTag), w)
	}

	cacheKey := strings.Join([]string{source, r.Update.CurrentTag, r.Update.LatestTag, fmt.Sprint(w), fmt.Sprint(m.notesDark)}, "\x00")
	if m.notesRendered != nil && m.notesRendered.key == cacheKey {
		return m.notesRendered.lines
	}
	lines := m.renderReleases(notes, w)
	if m.notesRendered != nil {
		m.notesRendered.key, m.notesRendered.lines = cacheKey, lines
	}
	return lines
}

// notesRenderCache holds the last rendered body. A pointer on the Model, so the
// copies Bubble Tea makes share it and a render done in View is not lost.
type notesRenderCache struct {
	key   string
	lines []string
}

// notesMessage wraps a one-paragraph explanation to the pane.
func (m Model) notesMessage(text string, w int) []string {
	lines := wrapPlain(text, w)
	for i, l := range lines {
		lines[i] = m.theme.dim().Render(l)
	}
	return lines
}

// notesErrorText says why there are no notes, and what would change that.
func notesErrorText(source string, err error) string {
	switch {
	case errors.Is(err, forge.ErrRateLimited):
		return "the forge's rate limit is used up — set GITHUB_TOKEN or GH_TOKEN (GITLAB_TOKEN for GitLab) to lift it"
	case errors.Is(err, forge.ErrUnsupported):
		host := source
		if u, perr := url.Parse(source); perr == nil && u.Host != "" {
			host = u.Host
		}
		return "releases are not readable from " + host + " — the link below is the source repository"
	default:
		return "could not read the release notes: " + err.Error()
	}
}

// renderReleases lays out every release, newest first: a heading with the tag,
// its name where it says more than the tag, and the date, then the notes.
func (m Model) renderReleases(notes []forge.Release, w int) []string {
	tagStyle := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true)

	var out []string
	for i, rel := range notes {
		if i > 0 {
			out = append(out, "", m.theme.dim().Render(strings.Repeat("─", w)), "")
		}
		head := tagStyle.Render(rel.Tag)
		if name := strings.TrimSpace(rel.Name); name != "" && name != rel.Tag {
			head += " " + nameStyle.Render(name)
		}
		var meta []string
		if !rel.Published.IsZero() {
			meta = append(meta, rel.Published.Format("2006-01-02"))
		}
		if rel.Prerelease {
			meta = append(meta, "prerelease")
		}
		if len(meta) > 0 {
			head += m.theme.dim().Render("  " + strings.Join(meta, " · "))
		}
		out = append(out, fit(head, w))

		body := sanitizeMarkdown(rel.Body)
		if strings.TrimSpace(body) == "" {
			out = append(out, m.theme.dim().Render("no notes were written for this release"))
			continue
		}
		out = append(out, "")
		out = append(out, renderMarkdown(body, w, m.notesDark)...)
	}
	return out
}

// sanitizeMarkdown drops the carriage returns GitHub stores bodies with and any
// control character that could reach the terminal as an escape sequence: the
// notes are text someone else wrote, and the screen belongs to ccu.
func sanitizeMarkdown(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// renderMarkdown renders one release's notes to the pane width. Anything glamour
// cannot handle is shown as wrapped plain text: unformatted notes beat none.
func renderMarkdown(body string, w int, dark bool) []string {
	style := styles.LightStyleConfig
	if dark {
		style = styles.DarkStyleConfig
	}
	// The pane already has its own edges; the stock two-column document margin
	// would cost a sidebar-sized terminal a noticeable share of every line.
	zero := uint(0)
	style.Document.Margin = &zero
	style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""

	r, err := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(w))
	if err != nil {
		return plainNotes(body, w)
	}
	out, err := r.Render(body)
	if err != nil {
		return plainNotes(body, w)
	}
	return trimBlankLines(strings.Split(out, "\n"))
}

// plainNotes is the fallback: each line of the source wrapped on its own, so
// lists and paragraphs keep their breaks.
func plainNotes(body string, w int) []string {
	var out []string
	for l := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(l) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapPlain(l, w)...)
	}
	return trimBlankLines(out)
}

// trimBlankLines drops the empty lines around a rendered block. glamour pads
// with lines that are blank only once their styling is stripped.
func trimBlankLines(lines []string) []string {
	blank := func(s string) bool { return strings.TrimSpace(ansi.Strip(s)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// notesSummary is the sidebar's line about the notes: how many releases there
// are to read once the list is in, a note while it is on its way, and nothing
// when there is nothing to offer — the pane explains failures, the sidebar has
// no room to.
func (m Model) notesSummary(r *Row) string {
	e, ok := m.notes[r.Update.SourceURL]
	if !ok || r.Update.SourceURL == "" {
		return ""
	}
	switch e.state {
	case notesLoading:
		return "loading notes…"
	case notesLoaded:
		n := len(r.Update.NotesFor(e.releases))
		if n == 0 {
			return ""
		}
		noun := "releases"
		if n == 1 {
			noun = "release"
		}
		return fmt.Sprintf("%d %s · %s to read", n, noun, m.keys.Notes.Help().Key)
	}
	return ""
}

// sidebarLink is the link the sidebar and the pane name for a row. Once the
// forge has answered it is the real page of the target release — the tag an
// image carries is often not the tag the release was published under (0.28
// against 0.28.0), so the constructed link can 404. With the list in and no
// release in range, the releases list is the honest answer.
func (m Model) sidebarLink(r *Row) string {
	if notes, ok := m.rowNotes(r); ok {
		if len(notes) > 0 && notes[0].URL != "" {
			return notes[0].URL
		}
		if list := releasesListURL(r.Update); list != "" {
			return list
		}
	}
	return notesLink(r.Update)
}

// releasesListURL is the page listing every release of the image's repository,
// derived from the release link so each forge keeps its own path shape
// (GitLab's /-/releases). Empty when the forge has no release page ccu knows.
func releasesListURL(u check.Update) string {
	release := u.ReleaseURL()
	if i := strings.Index(release, "/releases/"); i > 0 {
		return release[:i+len("/releases")]
	}
	return ""
}

func stripScheme(link string) string {
	return strings.TrimPrefix(strings.TrimPrefix(link, "https://"), "http://")
}
