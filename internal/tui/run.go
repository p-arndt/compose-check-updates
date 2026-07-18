package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/padi2312/compose-check-updates/internal"
	"github.com/padi2312/compose-check-updates/internal/scanner"
)

// logCapture is a slog.Handler that keeps records in memory instead of writing
// them anywhere. The default handler main.go installs writes to os.Stdout, and
// internal logs a warning for every image whose tags cannot be fetched — those
// writes land on top of the rendered frame and scroll the UI away. While the
// TUI owns the screen nothing may reach the terminal except the frame itself.
//
// Every field is guarded by mu: the scan logs from one goroutine per image, and
// the UI drains from the Bubble Tea loop, all concurrently.
type logCapture struct {
	mu      sync.Mutex
	records []capturedLog
	drained int // how many records the UI has already seen
	level   slog.Level
}

// capturedLog is a flattened record: the message plus its attributes, which is
// everything the UI or the post-teardown dump needs.
type capturedLog struct {
	Level slog.Level
	Msg   string
	Attrs []string
}

// Error renders the record as a single line, so it can be appended to the same
// error surface the scanner's own failures use.
func (c capturedLog) Error() string {
	if len(c.Attrs) == 0 {
		return c.Msg
	}
	return c.Msg + " (" + strings.Join(c.Attrs, ", ") + ")"
}

// newLogCapture captures everything at or above min. Records below it are
// dropped rather than stored, so a debug-heavy scan cannot grow unbounded.
func newLogCapture(min slog.Level) *logCapture {
	return &logCapture{level: min}
}

func (c *logCapture) Enabled(_ context.Context, l slog.Level) bool { return l >= c.level }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	rec := capturedLog{Level: r.Level, Msg: r.Message}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs = append(rec.Attrs, a.Key+"="+a.Value.String())
		return true
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, rec)
	return nil
}

// WithAttrs and WithGroup return the receiver: the scan does not use grouped or
// pre-bound loggers, and inheriting the same store keeps every record in one
// ordered list.
func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *logCapture) WithGroup(string) slog.Handler      { return c }

// drain returns the records the UI has not shown yet.
func (c *logCapture) drain() []capturedLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]capturedLog(nil), c.records[c.drained:]...)
	c.drained = len(c.records)
	return out
}

// all returns every record captured so far, for the dump after teardown.
func (c *logCapture) all() []capturedLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedLog(nil), c.records...)
}

// captureSlog redirects the default logger and returns both the store and a
// restore function. The caller must defer the restore: leaving a torn-down UI's
// handler installed would swallow the log output of everything that follows.
func captureSlog(min slog.Level) (*logCapture, func()) {
	c := newLogCapture(min)
	prev := slog.Default()
	slog.SetDefault(slog.New(c))
	return c, func() { slog.SetDefault(prev) }
}

// Run starts the interactive UI and blocks until the user is done. Any restarts
// the user asked for happen after the program has returned, never during it.
func Run(opts scanner.Options) error {
	fd := os.Stdout.Fd()
	if !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd) {
		return errors.New("interactive mode needs a terminal, but stdout is not a TTY; run without -i to print the available updates instead")
	}

	// Only warnings and above: those are the ones that tell the user an image
	// was skipped. Debug and info are scan chatter with no place in the UI.
	logs, restore := captureSlog(slog.LevelWarn)
	defer restore()
	// Dump after the alt screen is gone, so nothing an image skipped is lost
	// even if the user never looked at the status line.
	defer dumpLogs(logs)

	p := tea.NewProgram(NewModel(opts).WithLogCapture(logs), tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		return err
	}

	m, ok := final.(Model)
	if !ok {
		return nil
	}
	if m.err != nil {
		return m.err
	}

	return runRestarts(m.restartTargets)
}

// dumpLogs writes the captured records to stderr. Stderr rather than stdout
// because the UI's own output is the thing a caller would pipe.
func dumpLogs(c *logCapture) {
	records := c.all()
	if len(records) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "\n%d message(s) logged during the scan:\n", len(records))
	for _, r := range records {
		fmt.Fprintf(os.Stderr, "  [%s] %s\n", r.Level, r.Error())
	}
}

// runRestarts shells out to docker once per affected compose file. It runs only
// after the alt screen is gone: Restart writes docker's output straight to
// os.Stdout, which would otherwise scribble over the rendered UI.
func runRestarts(targets []internal.UpdateInfo) error {
	var errs []error
	for _, t := range targets {
		fmt.Printf("Restarting %s\n", t.FilePath)
		if err := t.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "Error restarting %s: %v\n", t.FilePath, err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
