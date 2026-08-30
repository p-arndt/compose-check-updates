package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

type scanStartedMsg struct{ events <-chan scanner.Event }

// The floating-tag scan is a second, much smaller run with its own channel: its
// events may not touch the file counters the first scan owns.
type floatingStartedMsg struct{ events <-chan scanner.Event }

type floatingEventMsg struct{ ev scanner.Event }

type floatingDoneMsg struct{}

type scanEventMsg struct{ ev scanner.Event }

// recheckDoneMsg carries the answer to a single image being looked at again
// after its settings changed. The row key travels with it because the list may
// have been re-sorted, filtered or folded in the meantime.
type recheckDoneMsg struct {
	key string
	ev  scanner.Event
	err error
}

type scanDoneMsg struct{}

type scanFailedMsg struct{ err error }

// logPollMsg drives the pull of captured slog records into the UI. The handler
// is written to from scan goroutines that know nothing about Bubble Tea, so the
// UI polls it rather than the handler pushing messages into the program.
type logPollMsg struct{}

const logPollInterval = 300 * time.Millisecond

func pollLogs() tea.Cmd {
	return tea.Tick(logPollInterval, func(time.Time) tea.Msg { return logPollMsg{} })
}

func (m Model) startScan() tea.Msg {
	events, err := scanner.Scan(m.ctx, m.opts)
	if err != nil {
		return scanFailedMsg{err: err}
	}
	return scanStartedMsg{events: events}
}

// startFloatingScan resolves the digests behind the bare floating tags, which the
// ordinary scan skipped because it was not asked for them.
func (m Model) startFloatingScan() tea.Msg {
	events, err := scanner.ScanPins(m.ctx, m.opts)
	if err != nil {
		// Not fatal: the list the user already has is unaffected, so this is one
		// more entry for the issues pane rather than a reason to quit.
		return floatingEventMsg{ev: scanner.Event{Kind: scanner.EventError, Err: err}}
	}
	return floatingStartedMsg{events: events}
}

// waitForFloatingEvent is waitForEvent for the second channel. Two channels
// rather than one, so a floating-tag event can never be mistaken for scan
// progress.
func waitForFloatingEvent(events <-chan scanner.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return floatingDoneMsg{}
		}
		return floatingEventMsg{ev: ev}
	}
}

// waitForEvent reads exactly one event and re-arms itself from Update. Draining
// the channel in a goroutine instead would only show rows once the scan is over.
func waitForEvent(events <-chan scanner.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return scanDoneMsg{}
		}
		return scanEventMsg{ev: ev}
	}
}

// recheckImage asks for one image to be resolved again, off the UI thread. It is
// what a changed versioning scheme costs: the requests of a single repository,
// rather than the whole scan the setting used to need.
func recheckImage(opts scanner.Options, r Row) tea.Cmd {
	key := rowKey(r)
	target := r.Update
	return func() tea.Msg {
		ev, err := scanner.CheckImage(opts, target)
		return recheckDoneMsg{key: key, ev: ev, err: err}
	}
}
