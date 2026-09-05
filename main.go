package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/p-arndt/selfupdate"
	"github.com/p-arndt/selfupdate/layout"

	"github.com/p-arndt/compose-check-updates/internal/buildinfo"
	"github.com/p-arndt/compose-check-updates/internal/cli"
	"github.com/p-arndt/compose-check-updates/internal/config"
	"github.com/p-arndt/compose-check-updates/internal/logger"
	"github.com/p-arndt/compose-check-updates/internal/modes"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/report"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
	"github.com/p-arndt/compose-check-updates/internal/tui"
	"github.com/p-arndt/compose-check-updates/internal/versioning"
)

// Exit codes, so a CI step can gate on the result without reading the report
// back. A failure outranks a pending update: the run could not see everything,
// so "1" would understate it.
const (
	exitUpToDate = 0
	exitOutdated = 1
	exitError    = 2
)

func main() {
	// An update renames the running binary aside to "<exe>.old" — on Windows a
	// running executable cannot be replaced, only moved — so the leftover is
	// deleted by a later process, i.e. this one, before anything else.
	selfupdate.CleanupLeftovers()

	// Until the output format is settled, every line the logger carries is a
	// diagnostic about ccu itself, and `ccu check | jq` must not be fed one. Only
	// the pretty report, written through slog, moves it to stdout.
	setLogOutput(os.Stderr)

	flags := cli.Parse(buildinfo.String())

	updater, err := newUpdater()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(exitError)
	}

	code, err := run(flags, updater)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(exitError)
	}
	os.Exit(code)
}

func run(flags cli.Flags, updater *selfupdate.Updater) (int, error) {
	// A terminal action in its own right: the user asked about ccu itself, not
	// about their compose files, so no scan may start behind it.
	if flags.SelfUpdate || flags.CheckUpdate {
		if err := updater.Run(context.Background(), os.Stdout, buildinfo.Version, flags.CheckUpdate); err != nil {
			return 0, fmt.Errorf("updating ccu: %w", err)
		}
		return exitUpToDate, nil
	}

	// Read before anything is scanned, and before `ccu config` reports, so both
	// see the same resolution. A broken config file stops the run: scanning with
	// settings the user only thinks are in effect is worth failing over.
	cfg, err := config.Load(flags.Directory, flags.Config)
	if err != nil {
		return 0, fmt.Errorf("reading config: %w", err)
	}

	effective, err := effectiveConfig(cfg.Config, flags)
	if err != nil {
		return 0, fmt.Errorf("reading flags: %w", err)
	}

	// One cache for the whole run, handed to every registry client the scan
	// builds: the scanner makes one per compose file, and an image two stacks
	// share should cost one lookup, not two.
	cache := registry.NewCache(registry.CacheOptions{
		TTL:     effective.CacheTTLDuration(),
		Refresh: flags.Refresh,
	})

	// A report about ccu's own settings, like the commands above: it answers a
	// question about the tool, so no scan follows it.
	if flags.ShowConfig {
		// The flag's own scheme is handed over separately rather than read back
		// off effective, where a scheme named on the command line and one written
		// in a file are indistinguishable.
		if flags.Image != "" {
			config.Explain(os.Stdout, cfg, effective, flags.Image, flags.Versioning, flags.MinAge)
		} else {
			config.Show(os.Stdout, cfg, effective, cache.Dir())
		}
		return exitUpToDate, nil
	}

	// Housekeeping after the run, never before it: pruning walks the cache
	// directory, and no lookup should wait on that.
	defer cache.Prune()

	format, err := report.ParseFormat(flags.Format)
	if err != nil {
		return 0, fmt.Errorf("reading flags: %w", err)
	}

	// The TUI is what a bare `ccu` means; `ccu check` asks for the report
	// instead. A piped stdout has no frame to draw on, so the run falls back to
	// the report — what `ccu | tee`, CI or a cron entry wants anyway.
	onTerminal := isTerminal(os.Stdout)
	if !flags.Check && !onTerminal {
		slog.Warn("No terminal on stdout, running the non-interactive report instead of the TUI; use `ccu check` to select it explicitly")
		flags.Check = true
	}

	opts := scanOptions(flags, effective, cache)

	if !flags.Check {
		if err := tui.Run(opts, cfg.Project, cfg.Global); err != nil {
			return 0, fmt.Errorf("running interactive mode: %w", err)
		}
		return exitUpToDate, nil
	}

	return runReport(flags, updater, opts, format.Resolve(onTerminal), cache)
}

func runReport(flags cli.Flags, updater *selfupdate.Updater, opts scanner.Options, format report.Format, cache *registry.Cache) (int, error) {
	// Said once, before the report itself, so an existing script keeps working
	// and still gets told which spelling replaced it.
	if flags.LegacyPlain {
		slog.Warn("Report-only flags now belong to the `check` subcommand; use `ccu check ...` — the bare form still works for this release")
	}

	// The pretty report is written through slog, so for it — and only for it —
	// the logger owns stdout. A warning from deep inside a registry lookup stays
	// on stderr, where it cannot appear in the machine-readable stream.
	if format == report.FormatPretty {
		setLogOutput(os.Stdout)
	}

	// The TUI installs its own quit handling, so only this path has to translate
	// a Ctrl-C into a cancelled scan.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	outcome, err := modes.Default(ctx, opts, flags, report.New(format, os.Stdout))
	if err != nil {
		return 0, fmt.Errorf("checking for updates: %w", err)
	}

	// Always stderr, in both formats: a JSON stream stays a JSON stream, and the
	// line is about how the answers were obtained, not about the images.
	if summary := cache.Summary(); summary != "" {
		fmt.Fprintln(os.Stderr, summary)
	}

	// Only the non-interactive path gets the notice: the TUI owns the alt screen,
	// and -self-update returned long before here. Stderr rather than stdout, so
	// piping ccu's report somewhere keeps it machine-readable.
	updater.NotifyIfAvailable(os.Stderr, buildinfo.Version)

	// A selection that named nothing is not a failure — there was simply nothing
	// to check — but an empty report would otherwise read like a clean scan.
	// Stderr, so a JSON stream stays a JSON stream.
	if len(flags.Images) > 0 && outcome.Images == 0 {
		fmt.Fprintf(os.Stderr, "no image matches -image %s\n", strings.Join(flags.Images, ","))
		return exitUpToDate, nil
	}

	return exitCode(outcome), nil
}

// exitCode maps a finished run onto the codes above.
func exitCode(o modes.Outcome) int {
	switch {
	case o.Failed:
		return exitError
	case o.Pending > 0:
		return exitOutdated
	default:
		return exitUpToDate
	}
}

// effectiveConfig layers the command line over the config files. The command
// line adds to the config rather than replacing it: a directory written down
// once is meant to stay excluded, and -exclude adds one more on top.
func effectiveConfig(cfg config.Config, flags cli.Flags) (config.Config, error) {
	cfg.Exclude = config.Union(cfg.Exclude, flags.Exclude)

	// -versioning replaces the *default* scheme, not the per-image entries: a
	// config line naming an image is the more specific statement, and a flag
	// meant as a quick try should not silently undo it.
	if flags.Versioning != "" {
		// The stricter of the two checks: a default has no image to take a pattern
		// from, so `regex` is a scheme only an entry naming an image may ask for.
		if err := versioning.ValidateDefault(flags.Versioning); err != nil {
			return config.Config{}, err
		}
		cfg.Versioning = flags.Versioning
	}

	// -min-age replaces the run-wide settling time, and like -versioning leaves a
	// per-image entry alone: naming an image is the more specific statement.
	if flags.MinAge != "" {
		if err := config.ValidateMinAge(flags.MinAge); err != nil {
			return config.Config{}, err
		}
		cfg.MinAge = flags.MinAge
	}

	// A flag that was not spelled out says nothing about what the config decided,
	// so only one actually passed overrides it — in either direction:
	// -pin-floating=false is how a single run opts out of `pin_floating: true`.
	if flags.PinFloatingSet {
		cfg.PinFloating = &flags.PinFloating
	}
	if flags.DockerfilesSet {
		cfg.Dockerfiles = &flags.Dockerfiles
	}

	return cfg, nil
}

func scanOptions(flags cli.Flags, effective config.Config, cache *registry.Cache) scanner.Options {
	opts := scanner.Options{
		Cache:       cache,
		Root:        flags.Directory,
		Exclude:     effective.Exclude,
		Images:      flags.Images,
		Major:       flags.Major,
		Minor:       flags.Minor,
		Patch:       flags.Patch,
		Policies:    effective.Policies(),
		Dockerfiles: effective.DockerfilesEnabled(),
	}

	// The TUI narrows the list with its own level filter, so it needs every level
	// resolved up front, or every filter change would hit the registries again.
	// Floating tags are not forced on the same way: their digests cost a request
	// each, so the bar's "floating" stop fetches them if it is pressed.
	if !flags.Check {
		opts.Major, opts.Minor, opts.Patch = true, true, true
	}

	return opts
}

// newUpdater describes ccu's releases to the self-updater: raw binaries named
// after the tool rather than the repository, and the checksums file the release
// workflow writes beside them.
func newUpdater() (*selfupdate.Updater, error) {
	return selfupdate.New(selfupdate.Config{
		Owner:   "p-arndt",
		Repo:    "compose-check-updates",
		AppName: "ccu",
		Layout:  &layout.RawBinary{},
		// Spelled out because the default would name `ccu update`, which is not a
		// command ccu has.
		UpdateCmd: "ccu self-update",
		// The library's own client stops at 30s, which is the tighter of the two
		// limits and would abort a slow download the 60s deadline still allows.
		HTTP: &http.Client{Timeout: selfupdate.DefaultUpdateTimeout},
	})
}

// setLogOutput points the default logger at f, colourised.
func setLogOutput(f *os.File) {
	slog.SetDefault(slog.New(logger.NewCustomHandler(slog.LevelInfo, f)))
}

// isTerminal tells a piped stdout apart from any other reason the TUI refused to
// start.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
