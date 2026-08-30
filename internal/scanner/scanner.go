// Package scanner walks a directory for compose files and checks them for image
// updates, reporting progress as a stream of events so a caller can render
// results while the scan is still running.
package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/compose"
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
)

const (
	defaultConcurrency = 8
	eventBuffer        = 64
)

type Options struct {
	Root    string   // root directory to walk
	Exclude []string // directories to exclude

	// Major, Minor and Patch are the update levels the run asks for.
	Major bool
	Minor bool
	Patch bool

	// Policies is what the user recorded about the images being checked.
	Policies policy.Set

	// Dockerfiles turns on checking the base images of the Dockerfiles a compose
	// file's services build. On by default: a service with `build:` has no image
	// tag of its own.
	Dockerfiles bool

	Concurrency int // compose files checked at once; <=0 means defaultConcurrency
}

type EventKind int

const (
	EventDiscovered EventKind = iota // emitted once, first, carrying Total
	EventFileStart                   // a compose file's check began
	EventUpdate                      // an image with an update, or one ccu could not read
	EventFileDone                    // a compose file's check finished
	EventError                       // a non-fatal error; scan continues
)

type Event struct {
	Kind   EventKind
	Path   string       // compose file involved (empty for EventDiscovered)
	Total  int          // compose files found; only on EventDiscovered
	Update check.Update // only on EventUpdate
	Level  policy.Level // level of Update; only on EventUpdate
	Err    error        // only on EventError
}

// Scan walks opts.Root and checks every compose file it finds, emitting events
// as they resolve. The channel is closed when the scan finishes; cancelling ctx
// stops it promptly and still closes the channel. The error return covers only
// the initial walk failing — per-file failures arrive as EventError.
func Scan(ctx context.Context, opts Options) (<-chan Event, error) {
	return walk(ctx, opts, true, checkFile)
}

// ScanPins walks opts.Root like Scan but resolves nothing except the digests
// bare floating tags point at. A full re-scan would re-fetch every tag list for
// versions already on screen, while this costs one manifest head per floating
// image.
//
// No progress events are emitted: the file counters belong to the scan that
// filled the list, and a second run bumping them would report files twice.
func ScanPins(ctx context.Context, opts Options) (<-chan Event, error) {
	return walk(ctx, opts, false, checkFilePins)
}

// CheckImage re-checks a single image and returns the event a scan would have
// emitted for it, for the caller that changed one image's settings. The image is
// named by the update an earlier scan reported, because that is what identifies
// the line.
func CheckImage(opts Options, target check.Update) (Event, error) {
	update, found, err := checkerFor(opts, target).CheckImage(target.FullImageName, opts.Major, opts.Minor, opts.Patch)
	if err != nil {
		return Event{}, err
	}
	if !found {
		return Event{}, fmt.Errorf("%s no longer names %s", target.FilePath, target.FullImageName)
	}

	// Path is the compose file, as on every event a scan emits: a consumer groups
	// rows by it, and a Dockerfile's row belongs under the stack that builds it.
	return Event{Kind: EventUpdate, Path: update.RestartPath(), Update: update, Level: update.Level()}, nil
}

func checkerFor(opts Options, target check.Update) *check.Checker {
	reg := registry.New("")
	if target.ComposePath == "" {
		return check.New(target.FilePath, reg, opts.Policies)
	}

	// A Dockerfile is only ever reached through the service that builds it; a
	// checker that did not know about the two would leave a restart with no
	// compose file to act on. One service name, because that is all a checker
	// records.
	service := ""
	if len(target.Services) > 0 {
		service = target.Services[0]
	}
	return check.NewDockerfile(target.FilePath, target.ComposePath, service, reg, opts.Policies)
}

// walk discovers the compose files, then runs check over them with bounded
// concurrency. progress decides whether the file-level events are emitted.
func walk(ctx context.Context, opts Options, progress bool, check func(context.Context, chan<- Event, Options, string)) (<-chan Event, error) {
	paths, err := compose.Files(opts.Root, opts.Exclude)
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

		if progress && !send(ctx, events, Event{Kind: EventDiscovered, Total: len(paths)}) {
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
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				check(ctx, events, opts, path)
			}()
		}

		wg.Wait()
	}()

	return events, nil
}

func checkFile(ctx context.Context, events chan<- Event, opts Options, path string) {
	if !send(ctx, events, Event{Kind: EventFileStart, Path: path}) {
		return
	}

	updates, err := checkAll(opts, path, func(c *check.Checker) ([]check.Update, error) {
		return c.Check(opts.Major, opts.Minor, opts.Patch)
	})
	if err != nil {
		send(ctx, events, Event{Kind: EventError, Path: path, Err: err})
		return
	}

	for _, u := range updates {
		// An image ccu could not read is reported rather than dropped: dropping it
		// left a warning on stderr as its only trace, which no row and no report
		// line could be hung off.
		if !u.HasNewVersion() && !u.IsUnreadable() {
			continue
		}
		if !send(ctx, events, Event{Kind: EventUpdate, Path: path, Update: u, Level: u.Level()}) {
			return
		}
	}

	send(ctx, events, Event{Kind: EventFileDone, Path: path})
}

// checkFilePins is ScanPins' per-file work: the floating tags of one compose
// file and nothing else.
func checkFilePins(ctx context.Context, events chan<- Event, opts Options, path string) {
	pins, err := checkAll(opts, path, (*check.Checker).CheckPins)
	if err != nil {
		send(ctx, events, Event{Kind: EventError, Path: path, Err: err})
		return
	}

	for _, u := range pins {
		if !send(ctx, events, Event{Kind: EventUpdate, Path: path, Update: u, Level: u.Level()}) {
			return
		}
	}
}

// checkAll runs one kind of check over a compose file and the Dockerfiles its
// services build. Their updates belong to the same stack, and the file counters
// count compose files, which is what the user pointed ccu at.
func checkAll(opts Options, path string, run func(*check.Checker) ([]check.Update, error)) ([]check.Update, error) {
	reg := registry.New("")

	updates, err := run(check.New(path, reg, opts.Policies))
	if err != nil {
		return nil, err
	}

	if !opts.Dockerfiles {
		return updates, nil
	}

	for _, target := range compose.BuildTargets(path) {
		more, err := run(check.NewDockerfile(target.Dockerfile, path, target.Service, reg, opts.Policies))
		if err != nil {
			// Not an EventError: a consumer counts those against the compose files
			// it is waiting for, and this failure is one file below.
			slog.Warn("Skipping (failed reading Dockerfile)", "path", target.Dockerfile, "error", err)
			continue
		}
		updates = append(updates, more...)
	}

	return updates, nil
}

// send reports whether the event was delivered; false means ctx was cancelled
// and the caller should stop rather than block on a consumer that has gone away.
func send(ctx context.Context, events chan<- Event, ev Event) bool {
	// Checked first, because the select below would otherwise pick a still-open
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
