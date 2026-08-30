package modes

import (
	"context"

	"github.com/p-arndt/compose-check-updates/internal"
	"github.com/p-arndt/compose-check-updates/internal/report"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

// Outcome is what the run found, so the caller can turn it into an exit code
// without parsing its own output back.
type Outcome struct {
	Updates int // images with an update available
	Pending int // updates still to be made after this run: everything not applied
	// Unreadable counts the images ccu could resolve nothing for. Kept apart from
	// Updates on purpose: they are reported, but they are no reason for a check to
	// fail — nobody knows whether they have an update at all.
	Unreadable int
	Failed     bool // at least one non-fatal error was reported
}

// Default checks every compose file below opts.Root and reports — or applies —
// the updates the scanner finds. Events already arrive from concurrent workers,
// so handling them inline here keeps the output ordering the scanner produced;
// Update() serializes its own writes.
func Default(ctx context.Context, opts scanner.Options, ccuFlags internal.CCUFlags, out report.Writer) (Outcome, error) {
	var outcome Outcome

	events, err := scanner.Scan(ctx, opts)
	if err != nil {
		return outcome, err
	}

	for event := range events {
		switch event.Kind {
		case scanner.EventError:
			outcome.Failed = true
			out.Error(event.Path, event.Err)

		case scanner.EventUpdate:
			i := event.Update

			// An image ccu could not read is reported and nothing else: it names no
			// version to write, and counting it would have `ccu check` exit 1 over an
			// image that may well be perfectly up to date — the run simply cannot
			// tell. Unreadable is a fact about ccu's reading, not about the image.
			if i.IsUnreadable() {
				outcome.Unreadable++
				out.Update(i, i.UpdateLevel(), report.Result{})
				continue
			}

			outcome.Updates++

			res := report.Result{
				ApplyRequested:   ccuFlags.Update,
				RestartRequested: ccuFlags.Restart,
			}

			if ccuFlags.Update {
				if err := i.Update(); err != nil {
					outcome.Failed = true
					out.Error(i.FilePath, err)
				} else {
					res.Applied = true
				}
			}

			// A restart only follows a write that happened; restarting a service
			// onto the image it is already running would report progress that was
			// never made.
			if ccuFlags.Restart && (!ccuFlags.Update || res.Applied) {
				if err := i.Restart(); err != nil {
					outcome.Failed = true
					out.Error(i.FilePath, err)
				} else {
					res.Restarted = true
				}
			}

			// An update the run was never asked to apply is still pending, which
			// is exactly what a CI check wants to hear about.
			if !res.Applied {
				outcome.Pending++
			}

			out.Update(i, i.UpdateLevel(), res)
		}
	}

	return outcome, out.Close()
}
