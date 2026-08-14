package internal

import (
	"path/filepath"
	"strings"
)

// ExcludeMatcher decides whether a walked path is excluded from the scan.
//
// An entry is read three ways, picked by how it is written, so one list can say
// both "not this directory" and "never this kind of directory":
//
//   - a bare name ("node_modules") matches a directory of that name at any
//     depth, which is what a config entry meant to hold across projects needs;
//   - anything containing a separator ("services/legacy") is a path relative to
//     the scan root, matched from the root down;
//   - an absolute path ("/mnt/backups") is matched as such, so a global config
//     can name a location that has nothing to do with the current root.
//
// All three accept the shell-style wildcards filepath.Match understands.
type ExcludeMatcher struct {
	names []string // match against a single path element, at any depth
	paths []string // match against the path relative to the scan root
	abs   []string // match against the absolute path
}

// NewExcludeMatcher builds a matcher from the raw entries as the user wrote
// them. Empty entries are dropped rather than turned into a pattern that would
// match the root itself.
func NewExcludeMatcher(exclude []string) *ExcludeMatcher {
	m := &ExcludeMatcher{}

	for _, raw := range exclude {
		e := strings.TrimSpace(raw)
		// Written the way a path is typed: "./cache/", "cache/" and "cache" all
		// name the same thing and must not be told apart by the trailing slash.
		e = filepath.ToSlash(e)
		e = strings.TrimSuffix(e, "/")
		e = strings.TrimPrefix(e, "./")
		if e == "" || e == "." {
			continue
		}

		switch {
		case isAbsEntry(e):
			// Resolved once, here, so an entry naming /tmp still matches a scan
			// that walked in through /private/tmp — the two are the same
			// directory, and only one of the spellings would otherwise match.
			m.abs = append(m.abs, filepath.ToSlash(resolve(filepath.FromSlash(e))))
		case strings.Contains(e, "/"):
			m.paths = append(m.paths, e)
		default:
			m.names = append(m.names, e)
		}
	}

	return m
}

// isAbsEntry reports whether an entry names an absolute location. A leading
// slash counts on every platform, not just where filepath.IsAbs says so: config
// files are written in slash form and get shared between machines, and on
// Windows "/mnt/backups" would otherwise be filed as a path relative to the
// scan root — which is not a thing the user could have meant, and which would
// silently exclude nothing at all.
func isAbsEntry(e string) bool {
	return strings.HasPrefix(e, "/") || filepath.IsAbs(filepath.FromSlash(e))
}

// Empty reports whether the matcher would exclude nothing, so a caller can skip
// the work of building the paths it compares against.
func (m *ExcludeMatcher) Empty() bool {
	return m == nil || len(m.names)+len(m.paths)+len(m.abs) == 0
}

// Match reports whether relPath — relative to the scan root, in the OS's own
// separator form — or any directory above it is excluded. absPath may be empty
// when the caller has no absolute path to offer; only the absolute entries need
// it.
func (m *ExcludeMatcher) Match(relPath, absPath string) bool {
	if m.Empty() {
		return false
	}

	rel := filepath.ToSlash(relPath)
	if rel == "." || rel == "" {
		return false
	}

	segments := strings.Split(rel, "/")

	// A name matches wherever it appears, which is what makes an entry like
	// "node_modules" worth writing down once.
	for _, seg := range segments {
		for _, pattern := range m.names {
			if matches(pattern, seg) {
				return true
			}
		}
	}

	// A rooted entry excludes the directory it names and everything below it, so
	// every prefix of the path is offered to it, not just the path itself.
	for i := range segments {
		prefix := strings.Join(segments[:i+1], "/")
		for _, pattern := range m.paths {
			if matches(pattern, prefix) {
				return true
			}
		}
	}

	if absPath != "" && len(m.abs) > 0 {
		abs := filepath.ToSlash(absPath)
		for _, pattern := range m.abs {
			if matches(pattern, abs) || strings.HasPrefix(abs, strings.TrimSuffix(pattern, "/")+"/") {
				return true
			}
		}
	}

	return false
}

// resolve follows symlinks in path, falling back to the path as given. A
// pattern containing wildcards has nothing to resolve — EvalSymlinks would fail
// on it — and an entry naming a directory that does not exist is not an error
// either: it simply matches nothing.
func resolve(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// matches applies one pattern. filepath.Match only reports an error for a
// malformed pattern, and a pattern nothing can match is the same as no match at
// all, so the error is deliberately not propagated: a stray bracket in a config
// file should not abort the scan.
func matches(pattern, name string) bool {
	if pattern == name {
		return true
	}
	ok, err := filepath.Match(pattern, name)
	return err == nil && ok
}
