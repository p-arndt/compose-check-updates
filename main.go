package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/p-arndt/compose-check-updates/internal"
	"github.com/p-arndt/compose-check-updates/internal/buildinfo"
	"github.com/p-arndt/compose-check-updates/internal/config"
	"github.com/p-arndt/compose-check-updates/internal/logger"
	"github.com/p-arndt/compose-check-updates/internal/modes"
	"github.com/p-arndt/compose-check-updates/internal/report"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
	"github.com/p-arndt/compose-check-updates/internal/tui"
	"github.com/p-arndt/compose-check-updates/internal/update"
)

func main() {
	// An update renames the running binary aside to "<exe>.old" — on Windows a
	// running executable cannot be replaced, only moved — so the leftover can
	// first be deleted by a later process, i.e. this one, before anything else.
	update.CleanupLeftovers()

	// Set colorized logger. It starts on stderr, because until the output format
	// is settled every line it carries is a diagnostic about ccu itself — a
	// broken config, an unreadable flag — and a `ccu check | jq` must not be fed
	// one of those. Only the pretty report, which is written through slog, moves
	// it to stdout below.
	setLogOutput(os.Stderr)

	// Version metadata comes from internal/buildinfo, stamped at build time from
	// the repo-root VERSION file via -ldflags; unstamped dev builds report "dev".
	ccuFlags := internal.Parse(buildinfo.String())

	// Both are terminal actions in their own right: the user asked about ccu
	// itself, not about their Compose files, so no scan may start behind them.
	if ccuFlags.SelfUpdate || ccuFlags.CheckUpdate {
		if err := update.Run(os.Stdout, buildinfo.Version, ccuFlags.CheckUpdate); err != nil {
			slog.Error("Error updating ccu", "error", err)
			os.Exit(exitError)
		}
		return
	}

	// Read before anything is scanned, and before the `config` command reports,
	// so both see exactly the same resolution. A broken config file stops the run
	// rather than being skipped: silently scanning with settings the user thinks
	// are in effect is the one outcome worth failing over.
	cfg, err := config.Load(ccuFlags.Directory, ccuFlags.Config)
	if err != nil {
		slog.Error("Error reading config", "error", err)
		os.Exit(exitError)
	}

	// The command line adds to the config rather than replacing it: a directory
	// written down once is meant to stay excluded, and -exclude is how a run adds
	// one more on top.
	effective := config.Config{
		Exclude: config.Union(cfg.Exclude, ccuFlags.Exclude),
		Images:  cfg.Images,
	}

	// A report about ccu's own settings, like the version and update commands
	// above: it answers a question about the tool, so no scan follows it.
	if ccuFlags.ShowConfig {
		config.Show(os.Stdout, cfg, effective)
		return
	}

	opts := scanner.Options{
		Root:    ccuFlags.Directory,
		Exclude: effective.Exclude,
		Caps:    effective.Caps(),
		Major:   ccuFlags.Major,
		Minor:   ccuFlags.Minor,
		Patch:   ccuFlags.Patch,
	}

	format, err := report.ParseFormat(ccuFlags.Format)
	if err != nil {
		slog.Error("Error reading flags", "error", err)
		os.Exit(exitError)
	}

	// The TUI is what a bare `ccu` means; `ccu check` is the way to ask for the
	// report instead. A piped or redirected stdout has no frame to draw on, so
	// rather than fail at something the user never spelled out, the run falls
	// back to the report — that is what `ccu | tee`, a CI job or a cron entry
	// wants anyway.
	stdoutIsTerminal := isTerminal(os.Stdout)
	if !ccuFlags.Check && !stdoutIsTerminal {
		slog.Warn("No terminal on stdout, running the non-interactive report instead of the TUI; use `ccu check` to select it explicitly")
		ccuFlags.Check = true
	}

	if !ccuFlags.Check {
		// The TUI narrows the list with its own in-UI level filter, so it needs every
		// level resolved up front — re-scanning whenever the filter changes would mean
		// hitting the registries again for versions we already looked up.
		opts.Major, opts.Minor, opts.Patch = true, true, true

		if err := tui.Run(opts, cfg.Project, cfg.Global); err != nil {
			slog.Error("Error running interactive mode", "error", err)
			os.Exit(exitError)
		}
		return
	}

	// Said once, before the report itself, so an existing script keeps working
	// and still gets told which spelling replaced it.
	if ccuFlags.LegacyPlain {
		slog.Warn("Report-only flags now belong to the `check` subcommand; use `ccu check ...` — the bare form still works for this release")
	}

	// Resolved only now: -format speaks about the report, and until the fallback
	// above has had its say it is not settled that there is one.
	format = format.Resolve(stdoutIsTerminal)

	// A warning from deep inside a registry lookup stays on stderr, where it
	// cannot appear in the machine-readable stream as a line no JSON parser can
	// read.
	// The pretty report is written through slog, so for it — and only for it —
	// the logger owns stdout.
	if format == report.FormatPretty {
		setLogOutput(os.Stdout)
	}

	// The TUI installs its own quit handling, so only the non-interactive path
	// has to translate a Ctrl-C into a cancelled scan.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	out := report.New(format, os.Stdout)

	outcome, err := modes.Default(ctx, opts, ccuFlags, out)
	if err != nil {
		slog.Error("Error checking for updates", "error", err)
		os.Exit(exitError)
	}

	// Only the non-interactive path gets the notice. The TUI swaps out the slog
	// handler and owns the alt screen, so a stray line written around its
	// teardown would land on top of the rendered frame; and -self-update /
	// -check-update returned long before here, where nagging about a version the
	// user just asked about would be pointless. Stderr rather than stdout, so
	// piping ccu's report somewhere keeps it machine-readable.
	update.NotifyIfAvailable(os.Stderr, buildinfo.Version)

	os.Exit(exitCode(outcome))
}

// Exit codes, so a CI step can gate on the result without reading the report
// back. A run that only reports is "successful" when there was nothing to
// report: anything else is what the caller wanted to be told about.
const (
	exitUpToDate = 0
	exitOutdated = 1
	exitError    = 2
)

// exitCode maps a finished run onto those codes. A failure outranks a pending
// update: the run could not see everything, so "1" would understate it.
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

// setLogOutput points the default logger at f, colourised as before.
func setLogOutput(f *os.File) {
	slog.SetDefault(slog.New(logger.NewCustomHandler(slog.LevelInfo, f)))
}

// isTerminal reports whether f is attached to a terminal, used to tell a piped
// stdout apart from any other reason the TUI refused to start.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
