package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/padi2312/compose-check-updates/internal"
)

// applyResultMsg reports one finished Update() so the row can flip state while
// its siblings are still being written.
type applyResultMsg struct {
	key string
	err error
}

func applyCmd(r Row) tea.Cmd {
	key := rowKey(r)
	info := r.Update
	return func() tea.Msg {
		// Re-pointing a row at another target level clears its resolved digest,
		// and Update() refuses to write a tag/digest pair that no longer belongs
		// together. Resolving here — inside the command, off the UI thread — keeps
		// the network call out of the render loop. It is a no-op for images that
		// are not digest-pinned.
		if info.CurrentDigest != "" {
			if err := info.ResolveDigest(internal.NewRegistry("")); err != nil {
				return applyResultMsg{key: key, err: err}
			}
		}
		return applyResultMsg{key: key, err: info.Update()}
	}
}

// beginApply queues the given pending rows and starts up to applyConcurrency of
// them. Returns nil when there is nothing to do. Both apply keys funnel through
// here so the digest resolve, the concurrency budget and the restart prompt are
// written once rather than once per entry point.
func (m *Model) beginApply(rows []Row) tea.Cmd {
	if len(rows) == 0 {
		return nil
	}

	m.applyQueue = m.applyQueue[:0]
	for _, r := range rows {
		m.applyQueue = append(m.applyQueue, rowKey(r))
	}
	m.phase = phaseApplying
	m.setStatus(StatusInfo, "applying updates…")

	return m.pumpApply()
}

// pumpApply starts as many queued updates as the concurrency budget allows.
func (m *Model) pumpApply() tea.Cmd {
	var cmds []tea.Cmd
	for m.applyActive < applyConcurrency && len(m.applyQueue) > 0 {
		key := m.applyQueue[0]
		m.applyQueue = m.applyQueue[1:]
		row := m.rowByKey(key)
		if row == nil {
			continue
		}
		m.applyActive++
		cmds = append(cmds, applyCmd(*row))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleApplyResult(msg applyResultMsg) tea.Cmd {
	if row := m.rowByKey(msg.key); row != nil {
		if msg.err != nil {
			row.State = RowFailed
			row.Err = msg.err
		} else {
			row.State = RowApplied
		}
	}
	if m.applyActive > 0 {
		m.applyActive--
	}

	if cmd := m.pumpApply(); cmd != nil {
		return cmd
	}
	if m.applyActive > 0 {
		return nil
	}
	return m.finishApply()
}

// finishApply moves on to the restart question once every queued update has
// reported back.
func (m *Model) finishApply() tea.Cmd {
	applied, failed := 0, 0
	for _, r := range m.rows {
		switch r.State {
		case RowApplied:
			applied++
		case RowFailed:
			failed++
		}
	}

	if applied == 0 {
		m.phase = phaseDone
		m.setStatus(StatusError, "no updates were written")
		return tea.Quit
	}

	m.phase = phaseRestartPrompt
	if failed > 0 {
		m.setStatus(StatusWarn, "updated with failures")
	} else {
		m.setStatus(StatusSuccess, "updates written")
	}
	return nil
}

// affectedFiles is the deduplicated set of compose files that actually changed,
// in list order. The restart question is asked once for this set, not once per
// image, because `docker compose up -d` acts on the whole file anyway.
func (m Model) affectedFiles() []internal.UpdateInfo {
	seen := make(map[string]bool)
	var out []internal.UpdateInfo
	for _, r := range m.rows {
		if r.State != RowApplied || seen[r.Update.FilePath] {
			continue
		}
		seen[r.Update.FilePath] = true
		out = append(out, r.Update)
	}
	return out
}
