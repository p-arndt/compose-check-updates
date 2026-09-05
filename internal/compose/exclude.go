package compose

import (
	"path/filepath"
	"strings"
)

// ExcludeMatcher decides whether a walked path is excluded from the scan.
// How an entry is written picks how it is read, all three accepting the
// wildcards filepath.Match understands:
//
//   - a bare name ("node_modules") matches at any depth;
//   - one with a separator ("services/legacy") is relative to the scan root;
//   - an absolute path ("/mnt/backups") is matched as such.
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

// isAbsEntry reports whether an entry names an absolute location. A leading slash
// counts on every platform, not just where filepath.IsAbs says so: config files
// are written in slash form and shared between machines.
func isAbsEntry(e string) bool {
	return strings.HasPrefix(e, "/") || filepath.IsAbs(filepath.FromSlash(e))
}

// Empty reports whether the matcher would exclude nothing, so a caller can skip
// the work of building the paths it compares against.
func (m *ExcludeMatcher) Empty() bool {
	return m == nil || len(m.names)+len(m.paths)+len(m.abs) == 0
}

// Match reports whether relPath — relative to the scan root, in the OS's own
// separator form — or any directory above it is excluded. Only the absolute
// entries need absPath, which may be empty.
func (m *ExcludeMatcher) Match(relPath, absPath string) bool {
	if m.Empty() {
		return false
	}

	rel := filepath.ToSlash(relPath)
	if rel == "." || rel == "" {
		return false
	}

	// A name matches wherever it appears, which is what makes an entry like
	// "node_modules" worth writing down once. Iterated rather than split: this
	// runs for every path the walk touches, and the segments outlive nothing.
	for seg := range strings.SplitSeq(rel, "/") {
		for _, pattern := range m.names {
			if matches(pattern, seg) {
				return true
			}
		}
	}

	// A rooted entry excludes the directory it names and everything below it, so
	// every prefix of the path is offered to it, not just the path itself. Walked
	// from the longest down, which is the same set without joining anything.
	for prefix := rel; prefix != ""; {
		for _, pattern := range m.paths {
			if matches(pattern, prefix) {
				return true
			}
		}
		slash := strings.LastIndex(prefix, "/")
		if slash < 0 {
			break
		}
		prefix = prefix[:slash]
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
// wildcard pattern, or a directory that does not exist, simply matches nothing.
func resolve(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// matches applies one pattern. filepath.Match only errors on a malformed
// pattern, which is the same as no match, so a stray bracket in a config file
// does not abort the scan.
func matches(pattern, name string) bool {
	if pattern == name {
		return true
	}
	ok, err := filepath.Match(pattern, name)
	return err == nil && ok
}
