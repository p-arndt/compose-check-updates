// Package compose reads the files a scan works on: the compose files below a
// directory, the images they declare, and the Dockerfiles their services build.
package compose

import (
	"os"
	"path/filepath"
)

var fileNames = []string{"docker-compose.y*ml", "compose.y*ml"}

// Files walks root and returns every compose file below it that no exclude entry
// covers. See ExcludeMatcher for how an entry is read.
func Files(root string, exclude []string) ([]string, error) {
	matcher := NewExcludeMatcher(exclude)

	// Resolved once for the whole walk: filepath.Abs per entry would be a syscall
	// each, and it would not follow the symlink that put the walk on a different
	// spelling of the same path than an absolute exclude entry names.
	rootAbs := ""
	if !matcher.Empty() {
		if abs, err := filepath.Abs(root); err == nil {
			rootAbs = resolve(abs)
		}
	}

	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Scanning system directories is expected to hit these.
			if os.IsPermission(err) {
				if info != nil && info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return err
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		absPath := ""
		if rootAbs != "" {
			absPath = filepath.Join(rootAbs, relPath)
		}

		if matcher.Match(relPath, absPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		if isComposeFile(filepath.Base(path)) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func isComposeFile(name string) bool {
	for _, pattern := range fileNames {
		if ok, err := filepath.Match(pattern, name); err == nil && ok {
			return true
		}
	}
	return false
}
