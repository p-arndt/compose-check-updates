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
)

func main() {
	// Set colorized logger
	logger := slog.New(logger.NewCustomHandler(slog.LevelInfo, os.Stdout))
	slog.SetDefault(logger)

	// Version metadata comes from internal/buildinfo, stamped at build time from
	// the repo-root VERSION file via -ldflags; unstamped dev builds report "dev".
	ccuFlags := internal.Parse(buildinfo.String())

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
