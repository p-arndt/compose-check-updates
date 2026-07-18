package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// chromeLines is what the list never gets: title, status, a blank line above
// and below the list, the legend, and the always-on key hint footer.
const chromeLines = 6

func (m Model) View() string {
	if m.phase == phaseDone && m.err != nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.theme.Title(m.width))
	b.WriteByte('\n')
	b.WriteString(m.statusLine())
	b.WriteString("\n\n")

	list := m.listView()
	b.WriteString(list)
	b.WriteString("\n\n")

	if m.showDetail {
		if r := m.currentRow(); r != nil {
			b.WriteString(m.theme.Detail(r.Update, r.Level, m.width))
			b.WriteByte('\n')
		}
	}

	b.WriteString(m.theme.Legend(m.filter, m.target, m.width))

	// The hint line is unconditional: keys nobody can see are keys nobody uses.
	// `?` then expands it into the grouped listing rather than revealing it.
	b.WriteByte('\n')
	if m.showHelp {
		b.WriteString(m.expandedHelp())
	} else {
		b.WriteString(m.theme.Help(m.hintBindings(), m.width))
	}

	return b.String()
}

// hintBindings is the footer's key set for the current phase. Showing the
// browsing keys during the restart question would advertise keys that phase
// throws away.
func (m Model) hintBindings() []key.Binding {
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

func (m Model) statusLine() string {
	switch m.phase {
	case phaseScanning:
		// Skipped images are logged while the scan is still running, so the
		// progress line has to carry them; otherwise they would only become
		// visible once the scan finished, long after the user could act on them.
		line := fmt.Sprintf("%s checked %d/%d files · %d update(s)",
			m.spinner.View(), m.checked, m.total, len(m.rows))
		if n := len(m.scanErrs); n > 0 {
			return m.theme.Status(StatusWarn, fmt.Sprintf("%s · %d issue(s), last: %v", line, n, m.scanErrs[n-1]))
		}
		return m.theme.Status(StatusInfo, line)
	case phaseApplying:
		return m.theme.Status(StatusInfo, fmt.Sprintf("applying… %d remaining", m.applyActive+len(m.applyQueue)))
	case phaseRestartPrompt:
		return m.theme.Status(StatusWarn, fmt.Sprintf("restart %d compose file(s) with docker compose up -d? (y/n)",
			len(m.affectedFiles())))
	}

	// Scan failures and captured log records share this line: both mean "an image
	// or a file was skipped". Only the newest is shown so the line stays exactly
	// one row tall and the list never shifts.
	if n := len(m.scanErrs); n > 0 && m.statusKind != StatusWarn {
		return m.theme.Status(StatusError, fmt.Sprintf("%d issue(s), last: %v", n, m.scanErrs[n-1]))
	}
	if m.statusText == "" {
		return m.theme.Status(StatusInfo, fmt.Sprintf("%d selected of %d", m.selectedCount(), len(m.rows)))
	}
	return m.theme.Status(m.statusKind, m.statusText)
}

// listHeight is how many terminal lines the row window may occupy.
func (m Model) listHeight() int {
	h := m.height - chromeLines
	if m.showDetail {
		h -= m.blockHeight(m.detailView())
	}
	if m.showHelp {
		// chromeLines already reserves the one hint line the footer always has;
		// the expanded listing only costs the extra rows on top of it.
		h -= m.blockHeight(m.expandedHelp()) - 1
	}
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
