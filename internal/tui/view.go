package tui

import (
	"fmt"
	"strings"
)

// chromeLines is what the list never gets: title, status, a blank line above
// and below the list, and the legend.
const chromeLines = 5

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
	if m.showHelp {
		b.WriteByte('\n')
		b.WriteString(m.theme.Help(m.keys.Bindings(), m.width))
	}

	return b.String()
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
		h -= m.blockHeight(m.theme.Help(m.keys.Bindings(), m.width))
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
	if len(m.visible) == 0 {
		return m.theme.Empty(m.emptyText(), m.width)
	}

	h := m.listHeight()
	offset := m.offset
	if total := m.displayCount(); offset > total-h {
		offset = total - h
	}
	if offset < 0 {
		offset = 0
	}

	// Only the lines inside the window are rendered, so a list far longer than
	// the terminal costs no more to draw than a short one.
	var lines []string
	di := 0
	lastPath := ""
	for vi, ri := range m.visible {
		row := m.rows[ri]
		if row.FilePath() != lastPath {
			lastPath = row.FilePath()
			if di >= offset && di < offset+h {
				shown, total := m.fileCounts(lastPath)
				lines = append(lines, m.theme.FileHeader(lastPath, shown, total, m.width))
			}
			di++
		}
		if di >= offset && di < offset+h {
			lines = append(lines, m.theme.RowLine(row, vi == m.cursor, m.width))
		}
		di++
		if di >= offset+h {
			break
		}
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

// fileCounts reports how many of a file's rows pass the filter and how many it
// has in total, for the "shown/total" hint in the header.
func (m Model) fileCounts(path string) (shown, total int) {
	for _, r := range m.rows {
		if r.FilePath() != path {
			continue
		}
		total++
		if m.filter.Matches(r.Level) {
			shown++
		}
	}
	return shown, total
}
