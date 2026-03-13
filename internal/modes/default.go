package modes

import (
	"log/slog"
	"sync"

	"github.com/padi2312/compose-check-updates/internal"
)

func Default(updateInfos []internal.UpdateInfo, ccuFlags internal.CCUFlags) {
	var wg sync.WaitGroup
	for _, i := range updateInfos {
		wg.Add(1)
		go func(i internal.UpdateInfo) {
			defer wg.Done()
			if i.HasNewVersion(ccuFlags.Major, ccuFlags.Minor, ccuFlags.Patch) {
				if !ccuFlags.Update && !ccuFlags.Restart {
					// If no flags are provided, just print the new version
					slog.Info("New version", "image", i.ImageName, "current", i.CurrentTag, "latest", i.LatestTag, "file", i.FilePath, "update_level", i.UpdateLevel())
				}

				if ccuFlags.Update {
					if err := i.Update(); err != nil {
						slog.Error("error updating file", "error", err)
						return
					}
					slog.Info("Updated file", "file", i.FilePath, "image", i.ImageName, "latest", i.LatestTag)
				}

				if ccuFlags.Restart {
					if err := i.Restart(); err != nil {
						slog.Error("error restarting service", "error", err)
						return
					}
					slog.Info("Compose file restarted", "file", i.FilePath)
				}
			}
		}(i)
	}
	wg.Wait()

}
