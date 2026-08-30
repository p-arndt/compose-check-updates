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
	Caps map[string]string

	// Versionings is the versioning scheme the user recorded per image, keyed by
	// image name without tag or digest, valued "semver"/"loose"/"regex". An image
	// with no entry takes DefaultVersioning.
	Versionings map[string]string

	// VersioningPatterns is the regex the "regex" scheme reads an image's tags
	// with, keyed the same way. Only the images on that scheme have one, and the
	// config layer guarantees each of them does.
	VersioningPatterns map[string]string

	// DefaultVersioning is the scheme for images Versionings says nothing about.
	// Empty means "semver", which is what every image gets until a config file or
	// -versioning says otherwise. Never "regex": a pattern belongs to one image,
	// so there is nothing a run-wide default could read tags with.
	DefaultVersioning string

	// PinFloating turns on pinning bare floating tags ("latest", "main", …) to
	// the digest they currently resolve to. Off by default: it costs a request per
	// floating image and pins a reference the user left mutable on purpose.
	PinFloating bool

	// Dockerfiles turns on checking the base images of the Dockerfiles a compose
	// file's services build. On by default: a service with `build:` has no image
	// tag of its own, so without this the only images ccu can say anything about
	// are the ones nobody builds themselves.
	Dockerfiles bool

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
	Level  string              // update level of Update ("major"/"minor"/"patch"/"digest"/"pin"); only on EventUpdate
	Err    error               // only set on EventError
}

// Scan walks opts.Root and checks every compose file it finds, emitting events
// on the returned channel as they resolve. The channel is closed when the scan
// finishes. Cancelling ctx stops the scan promptly and still closes the channel.
// The error return covers only the initial walk failing; per-file failures are
// delivered as EventError.
func Scan(ctx context.Context, opts Options) (<-chan Event, error) {
	return walk(ctx, opts, true, checkFile)
}

// ScanPins walks opts.Root like Scan but resolves nothing except the digests
// bare floating tags point at, emitting one EventUpdate per image it can pin. It
// is the answer to a user asking for the pins mid-session: a full re-scan would
// re-fetch every tag list for versions already on screen, while this costs one
// manifest head per floating image.
//
// No progress events are emitted — the file counters belong to the scan that
// filled the list, and a second run bumping them would report files as checked
// twice.
func ScanPins(ctx context.Context, opts Options) (<-chan Event, error) {
	return walk(ctx, opts, false, checkFilePins)
}

// walk is the shared body of the two scans: discover the compose files, then run
// check over them with bounded concurrency. progress decides whether the
// file-level events are emitted at all.
func walk(ctx context.Context, opts Options, progress bool, check func(context.Context, chan<- Event, Options, string)) (<-chan Event, error) {
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
			go func(path string) {
				defer wg.Done()
				defer func() { <-sem }()
				check(ctx, events, opts, path)
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
	checker := internal.NewUpdateChecker(path, registry).
		WithPinFloating(opts.PinFloating).
		WithVersioning(opts.Versionings, opts.DefaultVersioning, opts.VersioningPatterns)
	infos, err := checker.Check(opts.Major, opts.Minor, opts.Patch)
	if err != nil {
		send(ctx, events, Event{Kind: EventError, Path: path, Err: err})
		return
	}

	// The Dockerfiles this compose file builds are checked as part of it: their
	// updates belong to the same stack, and the file counters count compose
	// files, which is what the user pointed ccu at.
	for _, d := range dockerfileCheckers(opts, registry, path) {
		more, err := d.checker.Check(opts.Major, opts.Minor, opts.Patch)
		if err != nil {
			// Not an EventError: a consumer counts those against the compose
			// files it is waiting for, and this failure is one file below. The
			// Dockerfile is named rather than the compose file, which read fine.
			slog.Warn("Skipping (failed reading Dockerfile)", "path", d.path, "error", err)
			continue
		}
		infos = append(infos, more...)
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

// dockerfileCheckers returns a checker for every Dockerfile the compose file at
// path builds, in declaration order. Nothing when the option is off.
func dockerfileCheckers(opts Options, registry *internal.Registry, path string) []dockerfileChecker {
	if !opts.Dockerfiles {
		return nil
	}

	var checkers []dockerfileChecker
	for _, target := range internal.GetBuildTargets(path) {
		checkers = append(checkers, dockerfileChecker{
			path: target.Dockerfile,
			checker: internal.NewDockerfileChecker(target.Dockerfile, path, target.Service, registry).
				WithPinFloating(opts.PinFloating).
				WithVersioning(opts.Versionings, opts.DefaultVersioning, opts.VersioningPatterns),
		})
	}
	return checkers
}

// dockerfileChecker pairs a checker with the file it reads, so a failure can
// name it. A slice rather than a map keyed by path: the order the Dockerfiles
// were declared in is the order their updates are reported in.
type dockerfileChecker struct {
	path    string
	checker *internal.UpdateChecker
}

// checkFilePins is ScanPins' per-file work: the floating tags of one compose
// file and nothing else. Caps are not consulted — a pin moves no version, so
// there is no level for a cap to clamp.
func checkFilePins(ctx context.Context, events chan<- Event, opts Options, path string) {
	registry := internal.NewRegistry("")
	pins, err := internal.NewUpdateChecker(path, registry).CheckPins()
	if err != nil {
		send(ctx, events, Event{Kind: EventError, Path: path, Err: err})
		return
	}

	for _, d := range dockerfileCheckers(opts, registry, path) {
		more, err := d.checker.CheckPins()
		if err != nil {
			slog.Warn("Skipping (failed reading Dockerfile)", "path", d.path, "error", err)
			continue
		}
		pins = append(pins, more...)
	}

	for _, info := range pins {
		if !send(ctx, events, Event{Kind: EventUpdate, Path: path, Update: info, Level: info.UpdateLevel()}) {
			return
		}
	}
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
