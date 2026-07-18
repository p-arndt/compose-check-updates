package modes

import (
	"context"
	"log/slog"

	"github.com/p-arndt/compose-check-updates/internal"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

// Default checks every compose file below opts.Root and reports — or applies —
// the updates the scanner finds. Events already arrive from concurrent workers,
// so handling them inline here keeps the output ordering the scanner produced;
// Update() serializes its own writes.
func Default(ctx context.Context, opts scanner.Options, ccuFlags internal.CCUFlags) error {
	events, err := scanner.Scan(ctx, opts)
	if err != nil {
		return err
	}

	for event := range events {
		switch event.Kind {
		case scanner.EventError:
			slog.Error("Error checking for updates", "error", event.Err)

		case scanner.EventUpdate:
			i := event.Update

			if !ccuFlags.Update && !ccuFlags.Restart {
				// If no flags are provided, just print the new version
				attrs := []any{"image", i.ImageName, "current", i.CurrentTag, "latest", i.LatestTag, "file", i.FilePath, "update_level", i.UpdateLevel()}
				if i.IsDigestUpdate() {
					attrs = append(attrs, "current_digest", i.CurrentDigest, "latest_digest", i.LatestDigest)
				}
				slog.Info("New version", attrs...)
			}

			if ccuFlags.Update {
				if err := i.Update(); err != nil {
					slog.Error("error updating file", "error", err)
					continue
				}
				slog.Info("Updated file", "file", i.FilePath, "image", i.ImageName, "latest", i.LatestTag)
			}

			if ccuFlags.Restart {
				if err := i.Restart(); err != nil {
					slog.Error("error restarting service", "error", err)
					continue
				}
				slog.Info("Compose file restarted", "file", i.FilePath)
			}
		}
	}

	return nil
}
