package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/registry"
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
		// Re-pointing a row clears its resolved digest, and Update() refuses a
		// tag/digest pair that no longer belongs together. Resolving inside the
		// command keeps the network call off the UI thread; a no-op for images
		// that are not digest-pinned.
		if info.CurrentDigest != "" {
			if err := info.ResolveDigest(registry.New("")); err != nil {
				return applyResultMsg{key: key, err: err}
			}
		}
		return applyResultMsg{key: key, err: info.Apply()}
	}
}

// beginApply queues the given pending rows and starts up to applyConcurrency of
// them, returning nil when there is nothing to do. Both apply keys funnel through
// here, so the digest resolve, the budget and the restart prompt exist once.
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
// in list order. The restart question is asked once for the set, since
// `docker compose up -d` acts on the whole file anyway.
// Keyed by the compose file each update restarts through rather than by the file
// it was written to: a stack whose compose file *and* whose Dockerfile changed is
// still one `up`. Of those two the Dockerfile update wins, because only it asks
// for the rebuild that puts the new base image into the running container.
func (m Model) affectedFiles() []check.Update {
	at := make(map[string]int)
	var out []check.Update
	for _, r := range m.rows {
		if r.State != RowApplied {
			continue
		}
		path := r.Update.RestartPath()
		if i, ok := at[path]; ok {
			if r.Update.IsDockerfile() && !out[i].IsDockerfile() {
				out[i] = r.Update
			}
			continue
		}
		at[path] = len(out)
		out = append(out, r.Update)
	}
	return out
}
