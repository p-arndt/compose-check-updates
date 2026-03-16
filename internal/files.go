package internal

import (
	"os"
	"path/filepath"
)

func GetComposeFilePaths(root string, exclude []string) ([]string, error) {
	var composeFilePaths []string

	// Convert exclude slice to map for faster lookup
	excludeMap := make(map[string]bool)
	for _, path := range exclude {
		excludeMap[path] = true
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Ignore permission errors (e.g. when scanning system directories)
			if os.IsPermission(err) {
				if info != nil && info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return err
		}

		// Check if current path is in exclude list
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// Check if the path or any of its parent directories are excluded
		if info.IsDir() {
			if excludeMap[relPath] {
				return filepath.SkipDir
			}
			// Check parent directories
			for parent := filepath.Dir(relPath); parent != "." && parent != ".."; parent = filepath.Dir(parent) {
				if excludeMap[parent] {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip files in excluded directories
		if excludeMap[relPath] {
			return nil
		}
		// Check parent directories for files
		for parent := filepath.Dir(relPath); parent != "." && parent != ".."; parent = filepath.Dir(parent) {
			if excludeMap[parent] {
				return nil
			}
		}

		matched, err := filepath.Match("docker-compose.y*ml", filepath.Base(path))
		if err != nil {
			return err
		}
		if !matched {
			matched, err = filepath.Match("compose.y*ml", filepath.Base(path))
			if err != nil {
				return err
			}
		}
		if matched {
			composeFilePaths = append(composeFilePaths, path)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return composeFilePaths, nil
}
