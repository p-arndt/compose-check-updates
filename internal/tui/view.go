package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// topChrome is the fixed block above the pane: title, status, the bar, and the
// blank line separating them from it. The bar is in here rather than in the
// pane because it must never scroll and never change height — listHeight is
// derived from this number, so a block that grew would shrink the list.
const topChrome = 4

// minViewHeight is the shortest frame we will draw. Bubble Tea renders once
// before it delivers the first WindowSizeMsg, so height is legitimately 0 on
// the first frame; drawing a degenerate frame then is worse than briefly
// drawing a short one.
const minViewHeight = 8

// viewHeight is the number of terminal rows the frame occupies — always all of
// them, so the footer sits on the last row instead of floating in the middle of
// a tall terminal.
func (m Model) viewHeight() int {
	if m.height < minViewHeight {
		return minViewHeight
	}
	return m.height
}

func (m Model) View() string {
	if m.phase == phaseDone && m.err != nil {
		return ""
	}

	top := make([]string, 0, topChrome+m.listHeight())
	top = append(top, m.theme.Title(m.width), m.statusLine(), m.barLine(m.width), "")
	top = append(top, strings.Split(m.paneView(), "\n")...)

	return strings.Join(m.frame(top, m.bottomBlock()), "\n")
}

// frame forces the rendered frame to exactly viewHeight lines: the gap between
// the pane and the bottom chrome is padded so the footer lands on the final
// row, and nothing is ever emitted past it. One line too many scrolls the alt
// screen and the whole UI visibly shakes on every keypress, so this is the one
// place allowed to decide the line count.
func (m Model) frame(top, bottom []string) []string {
	h := m.viewHeight()
	// The bottom chrome is the way out of every state, so it is what survives a
	// terminal too short to hold everything.
	if len(bottom) > h {
		bottom = bottom[len(bottom)-h:]
	}
	room := h - len(bottom)
	if len(top) > room {
		top = top[:room]
	}

	out := make([]string, 0, h)
	for _, l := range top {
		out = append(out, fit(l, m.width))
	}
	// Padding, never negative: room is >= len(top) by the clamp above.
	for len(out) < room {
		out = append(out, "")
	}
	for _, l := range bottom {
		out = append(out, fit(l, m.width))
	}
	return out
}

// bottomBlock is the chrome pinned to the last rows: a blank separator, the
// detail pane, and the key hints. The legend used to sit here too, naming the
// filter and the target; the bar says both at the top now, and saying it twice
// on one frame cost a row of list to no purpose.
func (m Model) bottomBlock() []string {
	lines := []string{""}
	if m.showDetail && !m.showIssues {
		if d := m.detailView(); d != "" {
			lines = append(lines, strings.Split(d, "\n")...)
		}
	}
	// The hint line is unconditional: keys nobody can see are keys nobody uses.
	// `?` opens the full listing as a dialog over the pane rather than growing
	// the footer, which used to push the list up by however many groups there
	// happened to be.
	lines = append(lines, m.theme.Help(m.hintBindings(), m.width))
	return lines
}

// paneView is the scrollable middle of the screen: the update list, or the
// captured scan issues when the user has opened them.
// paneView is the middle of the frame: the list, and beside it the sidebar for
// whatever the cursor is on. The issues pane takes the whole width — it is a
// report about the scan rather than about one image, so there is nothing for a
// sidebar to describe next to it.
func (m Model) paneView() string {
	// The dialog is drawn where the boxes are rather than over them: a terminal
	// gives no way to float anything, and a listing that overwrote half the list
	// would be harder to read than one that replaces it outright.
	if m.showHelp {
		return m.helpDialog()
	}
	if m.showIssues {
		return m.issuesView()
	}

	// Below the two-column width the sidebar goes under the list rather than
	// away: it is the only way to reach a per-image target or a cap, so losing
	// it would cost the feature, not just the layout.
	if m.sidebarPlacement() == sidebarStacked {
		lines := strings.Split(m.listView(), "\n")
		// Padded to the height the list was given, so the panel sits on the same
		// row whatever the list happens to hold.
		for len(lines) < m.listHeight() {
			lines = append(lines, "")
		}
		return strings.Join(append(lines, m.stackedSidebar()...), "\n")
	}

	side := sidebarWidth(m.width)
	if side == 0 {
		return m.listView()
	}

	// listHeight already discounts the border rows, so the boxes get them back
	// here: what the list can show plus the two lines its frame occupies.
	h := m.listHeight() + 2
	left := strings.Split(m.listView(), "\n")

	return m.joinColumns(left, m.sidebarLines(side-boxChrome, h-2), m.listWidth(), h)
}

// listWidth is what the left column has to itself once the sidebar and the rule
// between them have taken their share.
// listWidth is the width available *inside* the left box, once the sidebar, the
// gap and both of the left box's border columns have taken their share.
func (m Model) listWidth() int {
	side := sidebarWidth(m.width)
	if side == 0 {
		return clampWidth(m.width)
	}
	return clampWidth(m.width) - side - sidebarGutter - boxChrome
}

// hintBindings is the footer's key set for the current phase. Showing the
// browsing keys during the restart question would advertise keys that phase
// throws away.
func (m Model) hintBindings() []key.Binding {
	if m.showHelp {
		return m.keys.HelpHints()
	}
	if m.focus == focusBar {
		return m.keys.BarHints()
	}
	if m.focus == focusSide {
		return m.keys.SideHints()
	}
	if m.showIssues {
		return m.keys.IssueHints()
	}
	switch m.phase {
	case phaseScanning:
		return m.keys.ScanHints()
	case phaseApplying:
		return m.keys.ApplyHints()
	case phaseRestartPrompt, phaseRestarting:
		return m.keys.RestartHints()
	default:
		return m.keys.BrowseHints()
	}
}

// helpDialog is the `?` view: every binding, grouped by what it acts on and
// laid out in columns under a heading, in a box centred over the pane. It is a
// dialog rather than a taller footer because the listing is many lines and
// growing the footer by that much shoved the list off the top of the screen.
func (m Model) helpDialog() string {
	h := m.listHeight()
	if sidebarWidth(m.width) > 0 {
		h += 2 // the boxed pane's own frame rows, which the dialog occupies too
	}
	// The dialog replaces the whole pane, the stacked panel included, so those
	// rows are its to use.
	h += m.stackedSidebarHeight()

	// Sized to its contents rather than to a number picked in advance: the
	// groups are as wide as their keys happen to be, and a fixed width silently
	// truncated whichever group grew past it.
	avail := clampWidth(m.width) - boxChrome

	title := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render("KEYS") +
		m.theme.dim().Render("  everything ccu binds")

	// Four lines of the box belong to the title, the closing hint and the blank
	// line before each: the panel gets what is left, and is what shrinks when
	// the terminal is short. Trimming the hint instead would leave a dialog on
	// screen that never says how to dismiss itself.
	room := max(h-2-4, 1)
	panel := m.theme.HelpPanel(m.keys.HelpSections(), avail, room)
	if len(panel) > room {
		panel = panel[:room]
	}

	body := append([]string{title, ""}, panel...)
	body = append(body, "", m.theme.dim().Render("? or esc closes this"))

	inner := 0
	for _, l := range body {
		inner = max(inner, lipgloss.Width(l))
	}
	inner = min(max(inner, 40), avail)

	box := m.theme.Box(body, inner, min(len(body), max(h-2, 1)), true)

	// Centred horizontally, and pushed down far enough to read as a dialog over
	// the pane rather than as the pane itself having changed shape.
	indent := strings.Repeat(" ", max((clampWidth(m.width)-inner-boxChrome)/2, 0))
	out := make([]string, 0, h)
	for i := 0; i < max((h-len(box))/2, 0); i++ {
		out = append(out, "")
	}
	for _, l := range box {
		out = append(out, indent+l)
	}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}

// status renders one status line, truncated with an ellipsis before styling so
// a long error is visibly cut rather than hard-clipped by the frame — and so it
// can never wrap into a second row and cost the list a line.
func (m Model) status(kind StatusKind, text string) string {
	return m.theme.Status(kind, truncatePlain(text, clampWidth(m.width)-2))
}

func (m Model) statusLine() string {
	switch m.phase {
	case phaseScanning:
		// Skipped images are logged while the scan is still running, so the
		// progress line has to carry them; otherwise they would only become
		// visible once the scan finished, long after the user could act on them.
		line := fmt.Sprintf("%s checked %d/%d files · %d update(s)",
			m.spinner.View(), m.checked, m.total, len(m.rows))
		if n := len(m.scanErrs); n > 0 {
			return m.status(StatusWarn, fmt.Sprintf("%s · %d issue(s) — press i", line, n))
		}
		return m.status(StatusInfo, line)
	case phaseApplying:
		return m.status(StatusInfo, fmt.Sprintf("applying… %d remaining", m.applyActive+len(m.applyQueue)))
	case phaseRestartPrompt:
		return m.status(StatusWarn, fmt.Sprintf("restart %d compose file(s) with docker compose up -d? (y/n)",
			len(m.affectedFiles())))
	}

	// Scan failures and captured log records share this line: both mean "an image
	// or a file was skipped". The line stays exactly one row tall so the list
	// never shifts, which is why it names the key that shows all of them rather
	// than trying to fit more than the newest.
	if n := len(m.scanErrs); n > 0 && m.statusKind != StatusWarn {
		return m.status(StatusError, fmt.Sprintf("%d issue(s) — press i · last: %v", n, m.scanErrs[n-1]))
	}
	if m.statusText == "" {
		return m.status(StatusInfo, fmt.Sprintf("%d selected of %d", m.selectedCount(), len(m.rows)))
	}
	return m.status(m.statusKind, m.statusText)
}

// listHeight is how many terminal lines the pane may occupy: whatever the fixed
// chrome leaves over. Deriving it this way is what keeps the frame exactly as
// tall as the terminal however many lines the detail pane or expanded help take.
// listHeight is how many list lines fit. When the frame is boxed, the two
// border rows come out of it here rather than at the renderer, so scrolling and
// drawing agree on the same number without either having to know about borders.
func (m Model) listHeight() int {
	h := m.viewHeight() - topChrome - len(m.bottomBlock())
	if sidebarWidth(m.width) > 0 {
		h -= 2
	}
	// The stacked panel comes out of the same budget: it is drawn below the list
	// inside the pane, so the rows it takes are rows the list cannot have.
	h -= m.stackedSidebarHeight()
	if h < 1 {
		return 1
	}
	return h
}

func (m Model) detailView() string {
	r := m.currentRow()
	if r == nil {
		return ""
	}
	return m.theme.Detail(r.Update, r.Level, m.width)
}

func (m Model) blockHeight(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}

func (m Model) listView() string {
	if len(m.entries) == 0 {
		return m.theme.Empty(m.emptyText(), m.listWidth())
	}

	h := m.listHeight()
	offset := m.offset
	if total := len(m.entries); offset > total-h {
		offset = total - h
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + h
	if end > len(m.entries) {
		end = len(m.entries)
	}

	// Entries are already one line each, so the window is a plain slice: only
	// what is on screen is rendered, and a list far longer than the terminal
	// costs no more to draw than a short one.
	lines := make([]string, 0, end-offset)
	for i := offset; i < end; i++ {
		e := m.entries[i]
		if e.kind == entryHeader {
			lines = append(lines, m.theme.GroupHeader(m.groupInfo(e.node, i == m.cursor), m.listWidth()))
			continue
		}
		// A row is indented one level past the compose file that owns it, so an
		// update sitting three directories deep does not line up with one at the
		// root. The indent is unstyled and the line is rendered into the width
		// that is left, which is how the header does it too — the cursor
		// highlight therefore starts at the text rather than at the margin.
		indent := strings.Repeat("  ", m.rowDepth(e))
		lines = append(lines, indent+m.theme.RowLine(m.rows[e.row], i == m.cursor, m.listWidth()-len(indent)))
	}

	return strings.Join(lines, "\n")
}

// rowDepth is how far a row entry is indented: one level past its compose
// file's node. A row whose file is missing from the tree — only possible between
// a filter change and the rebuild that follows — falls back to the one level of
// indent every row had before the tree existed.
func (m Model) rowDepth(e entry) int {
	n := m.fileNode(e.path)
	if n < 0 {
		return 1
	}
	return m.nodes[n].depth + 1
}

// issueParts splits a collected issue into its message and its attributes. Only
// captured slog records carry attributes; the scanner's own failures are a bare
// message, which is why this is a type switch and not a field access.
func issueParts(err error) (msg string, attrs []string) {
	if c, ok := err.(capturedLog); ok {
		return c.Msg, c.Attrs
	}
	return err.Error(), nil
}

// issueLines renders every captured issue and records the line each one starts
// on. Entries wrap rather than truncate — a one-line summary is exactly what
// the status line already gives, so a pane that also elided them would be no
// improvement — which means the cursor addresses issues while the window
// scrolls by lines.
func (m Model) issueLines() (lines []string, starts []int) {
	for i, e := range m.scanErrs {
		starts = append(starts, len(lines))
		msg, attrs := issueParts(e)
		lines = append(lines, m.theme.IssueEntry(i+1, msg, attrs, i == m.issueCursor, m.width)...)
	}
	return lines, starts
}

func (m Model) issuesView() string {
	if len(m.scanErrs) == 0 {
		return m.theme.Empty("No issues were logged during the scan", m.width)
	}

	lines, _ := m.issueLines()
	h := m.listHeight()
	offset := m.issueOffset
	if offset > len(lines)-h {
		offset = len(lines) - h
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + h
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[offset:end], "\n")
}

func (m Model) emptyText() string {
	if len(m.rows) == 0 {
		if m.phase == phaseScanning {
			return "scanning…"
		}
		if len(m.scanErrs) > 0 && m.checked > 0 && len(m.scanErrs) >= m.checked {
			return fmt.Sprintf("No file could be checked — %d error(s)", len(m.scanErrs))
		}
		return "Everything is up to date"
	}
	return fmt.Sprintf("No %s updates at target %s (f changes the filter, t the target)",
		m.filter.Label(), m.target.Label())
}
