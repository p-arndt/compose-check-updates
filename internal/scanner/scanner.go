// Package scanner walks a directory for compose files and checks them for image
// updates, reporting progress as a stream of events so a caller can render
// results while the scan is still running.
package scanner

import (
	"context"
	"log/slog"
	"sync"

	"github.com/p-arndt/compose-check-updates/internal"
)

const (
	defaultConcurrency = 8
	eventBuffer        = 64
)

type Options struct {
	Root    string   // root directory to walk
	Exclude []string // directories to exclude
	Major   bool     // passed through to UpdateChecker.Check
	Minor   bool
	Patch   bool

	// Caps is the per-image cap the user recorded, keyed by image name without
	// tag or digest, valued "patch"/"minor"/"major". An image with no entry has
	// no cap.
	Caps        map[string]string
	Concurrency int // max compose files checked at once; <=0 means a sensible default (8)
}

type EventKind int

const (
	EventDiscovered EventKind = iota // emitted once, first, carrying Total
	EventFileStart                   // a compose file's check began
	EventUpdate                      // an image with an available update
	EventFileDone                    // a compose file's check finished
	EventError                       // a non-fatal error; scan continues
)

type Event struct {
	Kind   EventKind
	Path   string              // compose file involved (empty for EventDiscovered)
	Total  int                 // number of compose files found; only set on EventDiscovered
	Update internal.UpdateInfo // only set on EventUpdate
	Level  string              // update level of Update ("major"/"minor"/"patch"/"digest"); only on EventUpdate
	Err    error               // only set on EventError
}

// Scan walks opts.Root and checks every compose file it finds, emitting events
// on the returned channel as they resolve. The channel is closed when the scan
// finishes. Cancelling ctx stops the scan promptly and still closes the channel.
// The error return covers only the initial walk failing; per-file failures are
// delivered as EventError.
func Scan(ctx context.Context, opts Options) (<-chan Event, error) {
	paths, err := internal.GetComposeFilePaths(opts.Root, opts.Exclude)
	if err != nil {
		return nil, err
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}

	// Buffered so a consumer that renders between reads does not stall the
	// workers behind it.
	events := make(chan Event, eventBuffer)

	go func() {
		defer close(events)

		if !send(ctx, events, Event{Kind: EventDiscovered, Total: len(paths)}) {
			return
		}

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup

		for _, path := range paths {
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case sem <- struct{}{}:
			}

			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				defer func() { <-sem }()
				checkFile(ctx, events, opts, path)
			}(path)
		}

		wg.Wait()
	}()

	return events, nil
}

func checkFile(ctx context.Context, events chan<- Event, opts Options, path string) {
	if !send(ctx, events, Event{Kind: EventFileStart, Path: path}) {
		return
	}

	registry := internal.NewRegistry("")
	checker := internal.NewUpdateChecker(path, registry)
	infos, err := checker.Check(opts.Major, opts.Minor, opts.Patch)
	if err != nil {
		send(ctx, events, Event{Kind: EventError, Path: path, Err: err})
		return
	}

	for _, info := range infos {
		// Check resolved the highest tag the level flags allow, which for a
		// capped image may be a release it is not allowed to take. Re-pointing it
		// at the cap here — rather than letting HasNewVersion drop it — is what
		// makes a cap mean "no further than this" instead of "hide this image":
		// an image capped at minor still has its minor update offered.
		applyCap(&info, opts.Caps[info.ImageName], registry)

		if !info.HasNewVersion(opts.Major, opts.Minor, opts.Patch) {
			continue
		}
		if !send(ctx, events, Event{Kind: EventUpdate, Path: path, Update: info, Level: info.UpdateLevel()}) {
			return
		}
	}

	send(ctx, events, Event{Kind: EventFileDone, Path: path})
}

// applyCap records the cap on info and moves its selection down to it when the
// tag Check picked sits above it. A no-op for an uncapped image, which is every
// image until the user pins one.
func applyCap(info *internal.UpdateInfo, cap string, registry *internal.Registry) {
	if cap == "" {
		return
	}

	info.Cap = cap

	// Only a selection the cap actually forbids is moved; re-selecting an
	// already-permitted tag would throw away the digest Check resolved for it.
	if info.LatestTag == "" || info.AllowsLevel(info.UpdateLevel()) {
		return
	}

	info.SelectTarget(cap)

	// SelectTarget drops a digest resolved for the tag it replaced, and a
	// reference that pins one cannot be written without it.
	if info.CurrentDigest != "" && info.LatestTag != "" {
		if err := info.ResolveDigest(registry); err != nil {
			slog.Warn("Skipping (failed resolving digest for capped tag)", "image", info.ImageName, "tag", info.LatestTag, "path", info.FilePath)
			info.LatestTag = ""
		}
	}
}

// send reports whether the event was delivered; a false result means ctx was
// cancelled and the caller should stop rather than block on a consumer that has
// gone away.
func send(ctx context.Context, events chan<- Event, ev Event) bool {
	// Checked before the select below, which would otherwise pick a still-open
	// buffered channel over an already cancelled context at random.
	select {
	case <-ctx.Done():
		return false
	default:
	}

	select {
	case <-ctx.Done():
		return false
	case events <- ev:
		return true
	}
}
