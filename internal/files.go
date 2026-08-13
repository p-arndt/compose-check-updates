package internal

import (
	"os"
	"path/filepath"
)

// GetComposeFilePaths walks root and returns every compose file below it that
// no exclude entry covers. See ExcludeMatcher for how an entry is read.
func GetComposeFilePaths(root string, exclude []string) ([]string, error) {
	var composeFilePaths []string

	matcher := NewExcludeMatcher(exclude)

	// Resolved once for the whole walk rather than per file: filepath.Abs on
	// every entry would be a syscall each, and it would not follow the symlink
	// that put the walk on a different spelling of the same path than the one an
	// absolute exclude entry names.
	rootAbs := ""
	if !matcher.Empty() {
		if abs, err := filepath.Abs(root); err == nil {
			rootAbs = resolve(abs)
		}
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

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// Absolute entries are the only ones that need the absolute path, and it
		// is built from the resolved root rather than looked up again per file.
		absPath := ""
		if rootAbs != "" {
			absPath = filepath.Join(rootAbs, relPath)
		}

		// Matching covers the parents too, so an excluded directory reached by a
		// symlink-free walk never has its children examined at all.
		if matcher.Match(relPath, absPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
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
