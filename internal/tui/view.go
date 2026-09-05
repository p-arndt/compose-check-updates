package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// topChrome is the fixed block above the pane: title, status, the bar and a
// blank line. The bar lives here because it must never scroll or change height —
// listHeight is derived from this number.
const topChrome = 4

// minViewHeight is the shortest frame we will draw. Bubble Tea renders once
// before the first WindowSizeMsg, so height is legitimately 0 on that frame.
const minViewHeight = 8

// viewHeight is the number of terminal rows the frame occupies — always all of
// them, so the footer sits on the last row instead of floating in the middle of
// a tall terminal.
func (m Model) viewHeight() int { return max(m.height, minViewHeight) }

func (m Model) View() string {
	if m.phase == phaseDone && m.err != nil {
		return ""
	}

	top := make([]string, 0, topChrome+m.listHeight())
	top = append(top, m.theme.Title(m.width), m.statusLine(), m.barLine(m.width), "")
	top = append(top, strings.Split(m.paneView(), "\n")...)

	return strings.Join(m.frame(top, m.bottomBlock()), "\n")
}

// frame forces the rendered frame to exactly viewHeight lines, padding the gap
// above the bottom chrome so the footer lands on the final row. One line too
// many scrolls the alt screen and the UI shakes on every keypress.
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

	// One style for the whole frame: fit builds a fresh one per call, and this
	// runs for every line on every keypress.
	trim := lipgloss.NewStyle().MaxWidth(clampWidth(m.width))
	out := make([]string, 0, h)
	for _, l := range top {
		out = append(out, trim.Render(l))
	}
	// Padding, never negative: room is >= len(top) by the clamp above.
	for len(out) < room {
		out = append(out, "")
	}
	for _, l := range bottom {
		out = append(out, trim.Render(l))
	}
	return out
}

// bottomBlock is the chrome pinned to the last rows: a blank separator and the
// key hints. The filter and target are named by the bar at the top instead, so
// no row of list is spent saying it twice.
func (m Model) bottomBlock() []string {
	// The hint line is unconditional: keys nobody can see are keys nobody uses.
	// `?` opens the full listing as a dialog rather than growing the footer.
	return []string{"", m.theme.Help(m.hintBindings(), m.width)}
}

// bottomHeight is len(bottomBlock()) without rendering the hint line, which
// listHeight asks for several times per frame and on every scroll.
func (m Model) bottomHeight() int { return 2 } // the separator and the hint line

// paneView is the middle of the frame: the list, and beside it the sidebar for
// whatever the cursor is on. The issues pane takes the whole width, being a
// report about the scan rather than about one image.
func (m Model) paneView() string {
	// Drawn where the boxes are rather than over them: a terminal cannot float
	// anything, and a half-overwritten list reads worse than a replaced one.
	if m.showHelp {
		return m.helpDialog()
	}
	if m.showIssues {
		return m.issuesView()
	}

	// Below the two-column width the sidebar stacks under the list rather than
	// disappearing: it is the only way to reach a per-image target or a cap.
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

// listWidth is the width available inside the left box, once the sidebar, the gap
// and both of the box's border columns have taken their share.
func (m Model) listWidth() int {
	side := sidebarWidth(m.width)
	if side == 0 {
		return clampWidth(m.width)
	}
	return clampWidth(m.width) - side - sidebarGutter - boxChrome
}

// hintBindings is the footer's key set for the current phase, so it never
// advertises keys that phase throws away.
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

// helpDialog is the `?` view: every binding, grouped by what it acts on and laid
// out in columns, in a box centred over the pane. A dialog rather than a taller
// footer, which would shove the list off the top of the screen.
func (m Model) helpDialog() string {
	h := m.listHeight()
	if sidebarWidth(m.width) > 0 {
		h += 2 // the boxed pane's own frame rows, which the dialog occupies too
	}
	// The dialog replaces the whole pane, the stacked panel included, so those
	// rows are its to use.
	h += m.stackedSidebarHeight()

	// Sized to its contents: a fixed width silently truncates whichever group
	// grows past it.
	avail := clampWidth(m.width) - boxChrome

	title := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render("KEYS") +
		m.theme.dim().Render("  everything ccu binds")

	// Four lines belong to the title, the closing hint and the blank line before
	// each. The panel gets what is left and is what shrinks on a short terminal;
	// trimming the hint would leave a dialog that never says how to dismiss itself.
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

	// Centred and pushed down far enough to read as a dialog over the pane rather
	// than as the pane having changed shape.
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

// status renders one status line, truncated with an ellipsis before styling so a
// long error is visibly cut and can never wrap into a second row.
func (m Model) status(kind StatusKind, text string) string {
	return m.theme.Status(kind, truncatePlain(text, clampWidth(m.width)-2))
}

func (m Model) statusLine() string {
	switch m.phase {
	case phaseScanning:
		// Skipped images are logged while the scan runs, so the progress line has to
		// carry them or they surface only once the scan is over.
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

	// Scan failures and captured log records share this line, which stays exactly
	// one row tall so the list never shifts. Hence it names the key that shows
	// them all rather than trying to fit more than the newest.
	if n := len(m.scanErrs); n > 0 && m.statusKind != StatusWarn {
		return m.status(StatusError, fmt.Sprintf("%d issue(s) — press i · last: %v", n, m.scanErrs[n-1]))
	}
	if m.statusText == "" {
		return m.status(StatusInfo, fmt.Sprintf("%d selected of %d", m.selectedCount(), m.eligibleCount()))
	}
	return m.status(m.statusKind, m.statusText)
}

// listHeight is how many list lines fit: whatever the fixed chrome leaves over,
// which keeps the frame exactly as tall as the terminal. A boxed frame's two
// border rows come out here, so scrolling and drawing agree about them.
func (m Model) listHeight() int {
	h := m.viewHeight() - topChrome - m.bottomHeight()
	if sidebarWidth(m.width) > 0 {
		h -= 2
	}
	// The stacked panel is drawn inside the pane, so its rows come out of the same
	// budget.
	h -= m.stackedSidebarHeight()
	return max(h, 1)
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
	offset := max(min(m.offset, len(m.entries)-h), 0)
	end := min(offset+h, len(m.entries))
	w := m.listWidth()

	// Entries are one line each, so the window is a plain slice and a list far
	// longer than the terminal costs no more to draw than a short one.
	lines := make([]string, 0, end-offset)
	for i := offset; i < end; i++ {
		e := m.entries[i]
		if e.kind == entryHeader {
			lines = append(lines, m.theme.GroupHeader(m.groupInfo(e.node, i == m.cursor), w))
			continue
		}
		// A row is indented one level past the compose file that owns it. The indent
		// is unstyled and the line rendered into what is left, as the header does
		// it, so the cursor highlight starts at the text rather than the margin.
		indent := strings.Repeat("  ", m.rowDepth(e))
		lines = append(lines, indent+m.theme.RowLine(m.rows[e.row], i == m.cursor, w-len(indent)))
	}

	return strings.Join(lines, "\n")
}

// rowDepth is how far a row entry is indented: one level past its compose file's
// node. A row whose file is missing from the tree — possible only between a
// filter change and the rebuild that follows — falls back to a single level.
func (m Model) rowDepth(e entry) int {
	n := m.fileNode(e.path)
	if n < 0 {
		return 1
	}
	return m.nodes[n].depth + 1
}

// issueParts splits a collected issue into its message and its attributes. Only
// captured slog records carry attributes; the scanner's own failures are a bare
// message, which is why this asks the error what it is rather than reading a
// field off it.
func issueParts(err error) (msg string, attrs []string) {
	var c capturedLog
	if errors.As(err, &c) {
		return c.Msg, c.Attrs
	}
	return err.Error(), nil
}

// issueLines renders every captured issue and records the line each one starts
// on. Entries wrap rather than truncate — the status line is the one-line
// summary — so the cursor addresses issues while the window scrolls by lines.
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
	offset := max(min(m.issueOffset, len(lines)-h), 0)
	return strings.Join(lines[offset:min(offset+h, len(lines))], "\n")
}

func (m Model) emptyText() string {
	// Named before the filter is blamed: a file holding nothing but floating tags
	// looks empty, and `f` is not the key that changes that.
	if n := m.hiddenFloatingCount(); n > 0 && len(m.visible) == 0 {
		return fmt.Sprintf("%d floating tag(s) hidden, nothing else to report (p lists them)", n)
	}
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
		m.filter.Label(), targetLabel(m.target))
}
