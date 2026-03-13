package main

import (
	"log/slog"
	"os"
	"sync"

	"github.com/padi2312/compose-check-updates/internal"
	"github.com/padi2312/compose-check-updates/internal/logger"
	"github.com/padi2312/compose-check-updates/internal/modes"
)

var version = "0.3.0"

func main() {
	// Set colorized logger
	logger := slog.New(logger.NewCustomHandler(slog.LevelInfo, os.Stdout))
	slog.SetDefault(logger)

	ccuFlags := internal.Parse(version)
	root := ccuFlags.Directory
	composeFilePaths, err := internal.GetComposeFilePaths(root)
	if err != nil {
		slog.Error("Error getting compose file paths", "error", err)
		os.Exit(1)
		return
	}

	var updateInfos []internal.UpdateInfo
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, path := range composeFilePaths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			updateChecker := internal.NewUpdateChecker(path, internal.NewRegistry(""))
			info, err := updateChecker.Check(ccuFlags.Major, ccuFlags.Minor, ccuFlags.Patch)
			if err != nil {
				slog.Error("Error checking for updates", "error", err)
				return
			}
			mu.Lock()
			updateInfos = append(updateInfos, info...)
			mu.Unlock()
		}(path)
	}

	wg.Wait()

	if ccuFlags.Interactive {
		modes.Interactive(updateInfos)
		return
	} else {
		modes.Default(updateInfos, ccuFlags)
	}

}
