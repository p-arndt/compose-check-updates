package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/p-arndt/compose-check-updates/internal/buildinfo"
	"github.com/p-arndt/compose-check-updates/internal/cli"
	"github.com/p-arndt/compose-check-updates/internal/config"
	"github.com/p-arndt/compose-check-updates/internal/logger"
	"github.com/p-arndt/compose-check-updates/internal/modes"
	"github.com/p-arndt/compose-check-updates/internal/report"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
	"github.com/p-arndt/compose-check-updates/internal/tui"
	"github.com/p-arndt/compose-check-updates/internal/update"
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
	update.CleanupLeftovers()

	// Until the output format is settled, every line the logger carries is a
	// diagnostic about ccu itself, and `ccu check | jq` must not be fed one. Only
	// the pretty report, written through slog, moves it to stdout.
	setLogOutput(os.Stderr)

	flags := cli.Parse(buildinfo.String())

	code, err := run(flags)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(exitError)
	}
	os.Exit(code)
}

func run(flags cli.Flags) (int, error) {
	// A terminal action in its own right: the user asked about ccu itself, not
	// about their compose files, so no scan may start behind it.
	if flags.SelfUpdate || flags.CheckUpdate {
		if err := update.Run(os.Stdout, buildinfo.Version, flags.CheckUpdate); err != nil {
			return 0, fmt.Errorf("updating ccu: %w", err)
		}
		return exitUpToDate, nil
	}

	// Read before anything is scanned, and before `ccu config` reports, so both
	// see the same resolution. A broken config file stops the run rather than
	// being skipped: silently scanning with settings the user thinks are in
	// effect is the one outcome worth failing over.
	cfg, err := config.Load(flags.Directory, flags.Config)
	if err != nil {
		return 0, fmt.Errorf("reading config: %w", err)
	}

	effective, err := effectiveConfig(cfg.Config, flags)
	if err != nil {
		return 0, fmt.Errorf("reading flags: %w", err)
	}

	// A report about ccu's own settings, like the commands above: it answers a
	// question about the tool, so no scan follows it.
	if flags.ShowConfig {
		// The flag's own scheme is handed over separately rather than read back
		// off effective, where a scheme named on the command line and one written
		// in a file are indistinguishable.
		if flags.Image != "" {
			config.Explain(os.Stdout, cfg, effective, flags.Image, flags.Versioning)
		} else {
			config.Show(os.Stdout, cfg, effective)
		}
		return exitUpToDate, nil
	}

	format, err := report.ParseFormat(flags.Format)
	if err != nil {
		return 0, fmt.Errorf("reading flags: %w", err)
	}

	// The TUI is what a bare `ccu` means; `ccu check` asks for the report
	// instead. A piped or redirected stdout has no frame to draw on, so rather
	// than fail at something the user never spelled out, the run falls back to
	// the report — what `ccu | tee`, a CI job or a cron entry wants anyway.
	onTerminal := isTerminal(os.Stdout)
	if !flags.Check && !onTerminal {
		slog.Warn("No terminal on stdout, running the non-interactive report instead of the TUI; use `ccu check` to select it explicitly")
		flags.Check = true
	}

	opts := scanOptions(flags, effective)

	if !flags.Check {
		if err := tui.Run(opts, cfg.Project, cfg.Global); err != nil {
			return 0, fmt.Errorf("running interactive mode: %w", err)
		}
		return exitUpToDate, nil
	}

	return runReport(flags, opts, format.Resolve(onTerminal))
}

func runReport(flags cli.Flags, opts scanner.Options, format report.Format) (int, error) {
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

	// Only the non-interactive path gets the notice: the TUI owns the alt screen,
	// and -self-update returned long before here. Stderr rather than stdout, so
	// piping ccu's report somewhere keeps it machine-readable.
	update.NotifyIfAvailable(os.Stderr, buildinfo.Version)

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

func scanOptions(flags cli.Flags, effective config.Config) scanner.Options {
	opts := scanner.Options{
		Root:        flags.Directory,
		Exclude:     effective.Exclude,
		Major:       flags.Major,
		Minor:       flags.Minor,
		Patch:       flags.Patch,
		Policies:    effective.Policies(),
		Dockerfiles: effective.DockerfilesEnabled(),
	}

	// The TUI narrows the list with its own level filter, so it needs every level
	// resolved up front: re-scanning on every filter change would mean hitting
	// the registries again for versions already looked up.
	//
	// The floating tags are deliberately not forced on in the same way. Their
	// digests cost a request each and pin a reference the user left mutable on
	// purpose, so the bar's "floating" stop fetches them if and when it is
	// pressed.
	if !flags.Check {
		opts.Major, opts.Minor, opts.Patch = true, true, true
	}

	return opts
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
