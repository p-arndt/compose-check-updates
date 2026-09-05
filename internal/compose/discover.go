// Package compose reads the files a scan works on: the compose files below a
// directory, the images they declare, and the Dockerfiles their services build.
package compose

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	// WalkDir rather than Walk: only IsDir and the name are read here, and
	// WalkDir hands those over from the directory listing instead of paying an
	// lstat per entry.
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Scanning system directories is expected to hit these.
			if os.IsPermission(err) {
				if d != nil && d.IsDir() {
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
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
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
	return slices.ContainsFunc(fileNames, func(pattern string) bool {
		ok, err := filepath.Match(pattern, name)
		return err == nil && ok
	})
}
