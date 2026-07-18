package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// topChrome is the fixed block above the pane: title, status, and the blank
// line separating them from it.
const topChrome = 3

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
	top = append(top, m.theme.Title(m.width), m.statusLine(), "")
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
// detail pane, the legend, and the key hints.
func (m Model) bottomBlock() []string {
	lines := []string{""}
	if m.showDetail && !m.showIssues {
		if d := m.detailView(); d != "" {
			lines = append(lines, strings.Split(d, "\n")...)
		}
	}
	lines = append(lines, m.theme.Legend(m.filter, m.target, m.width))

	// The hint line is unconditional: keys nobody can see are keys nobody uses.
	// `?` then expands it into the grouped listing rather than revealing it.
	if m.showHelp {
		lines = append(lines, strings.Split(m.expandedHelp(), "\n")...)
	} else {
		lines = append(lines, m.theme.Help(m.hintBindings(), m.width))
	}
	return lines
}

// paneView is the scrollable middle of the screen: the update list, or the
// captured scan issues when the user has opened them.
func (m Model) paneView() string {
	if m.showIssues {
		return m.issuesView()
	}
	return m.listView()
}

// hintBindings is the footer's key set for the current phase. Showing the
// browsing keys during the restart question would advertise keys that phase
// throws away.
func (m Model) hintBindings() []key.Binding {
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

// expandedHelp is the `?` view: FullHelp's groups, one line each, so related
// keys stay together instead of wrapping into one undifferentiated run.
func (m Model) expandedHelp() string {
	groups := m.keys.FullHelp()
	lines := make([]string, 0, len(groups))
	for _, g := range groups {
		if line := m.theme.Help(g, m.width); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
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
func (m Model) listHeight() int {
	h := m.viewHeight() - topChrome - len(m.bottomBlock())
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
		return m.theme.Empty(m.emptyText(), m.width)
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
			lines = append(lines, m.theme.GroupHeader(m.groupInfo(e.path, i == m.cursor), m.width))
			continue
		}
		lines = append(lines, m.theme.RowLine(m.rows[e.row], i == m.cursor, m.width))
	}

	return strings.Join(lines, "\n")
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
