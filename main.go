package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/padi2312/compose-check-updates/internal"
	"github.com/padi2312/compose-check-updates/internal/buildinfo"
	"github.com/padi2312/compose-check-updates/internal/logger"
	"github.com/padi2312/compose-check-updates/internal/modes"
	"github.com/padi2312/compose-check-updates/internal/scanner"
	"github.com/padi2312/compose-check-updates/internal/tui"
	"github.com/padi2312/compose-check-updates/internal/update"
)

func main() {
	// An update renames the running binary aside to "<exe>.old" — on Windows a
	// running executable cannot be replaced, only moved — so the leftover can
	// first be deleted by a later process, i.e. this one, before anything else.
	update.CleanupLeftovers()

	// Set colorized logger
	logger := slog.New(logger.NewCustomHandler(slog.LevelInfo, os.Stdout))
	slog.SetDefault(logger)

	// Version metadata comes from internal/buildinfo, stamped at build time from
	// the repo-root VERSION file via -ldflags; unstamped dev builds report "dev".
	ccuFlags := internal.Parse(buildinfo.String())

	// Both are terminal actions in their own right: the user asked about ccu
	// itself, not about their Compose files, so no scan may start behind them.
	if ccuFlags.SelfUpdate || ccuFlags.CheckUpdate {
		if err := update.Run(os.Stdout, buildinfo.Version, ccuFlags.CheckUpdate); err != nil {
			slog.Error("Error updating ccu", "error", err)
			os.Exit(1)
		}
		return
	}

	opts := scanner.Options{
		Root:    ccuFlags.Directory,
		Exclude: ccuFlags.Exclude,
		Major:   ccuFlags.Major,
		Minor:   ccuFlags.Minor,
		Patch:   ccuFlags.Patch,
	}

	if ccuFlags.Interactive {
		// The TUI narrows the list with its own in-UI level filter, so it needs every
		// level resolved up front — re-scanning whenever the filter changes would mean
		// hitting the registries again for versions we already looked up.
		opts.Major, opts.Minor, opts.Patch = true, true, true

		if err := tui.Run(opts); err != nil {
			if !isTerminal(os.Stdout) {
				slog.Error("Interactive mode needs a terminal; run without -i to check for updates non-interactively", "error", err)
			} else {
				slog.Error("Error running interactive mode", "error", err)
			}
			os.Exit(1)
		}
		return
	}

	// The TUI installs its own quit handling, so only the non-interactive path
	// has to translate a Ctrl-C into a cancelled scan.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := modes.Default(ctx, opts, ccuFlags); err != nil {
		slog.Error("Error checking for updates", "error", err)
		os.Exit(1)
	}

	// Only the non-interactive path gets the notice. The TUI swaps out the slog
	// handler and owns the alt screen, so a stray line written around its
	// teardown would land on top of the rendered frame; and -self-update /
	// -check-update returned long before here, where nagging about a version the
	// user just asked about would be pointless. Stderr rather than stdout, so
	// piping ccu's report somewhere keeps it machine-readable.
	update.NotifyIfAvailable(os.Stderr, buildinfo.Version)
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
