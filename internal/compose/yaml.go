package compose

import (
	"regexp"
	"strings"
)

var (
	// A mapping key with whatever it carries on the same line; only indentation
	// and the key itself matter.
	keyPattern  = regexp.MustCompile(`^(\s*)([^\s#][^:]*):(\s|$)`)
	servicesKey = regexp.MustCompile(`^(\s*)services:\s*$`)
)

// serviceTracker follows which service block the reader is inside, by
// indentation alone. Parsing the file as YAML would answer this properly, but
// the rest of ccu works line by line so it can rewrite the exact line it read;
// this keeps that property and is enough for the shapes compose files take.
type serviceTracker struct {
	servicesIndent int // indentation of `services:`, -1 when outside it
	serviceIndent  int // indentation of the service names below it, -1 until seen
	current        string
}

func newServiceTracker() *serviceTracker {
	return &serviceTracker{servicesIndent: -1, serviceIndent: -1}
}

// observe feeds one line to the tracker and returns the service it belongs to.
func (t *serviceTracker) observe(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return t.current
	}

	if m := servicesKey.FindStringSubmatch(line); m != nil {
		t.servicesIndent, t.serviceIndent, t.current = len(m[1]), -1, ""
		return ""
	}

	m := keyPattern.FindStringSubmatch(line)
	if m == nil {
		return t.current
	}
	indent := len(m[1])

	// Nothing below `services:` has been seen yet, or the block has ended.
	if t.servicesIndent < 0 {
		return ""
	}
	if indent <= t.servicesIndent {
		t.servicesIndent, t.serviceIndent, t.current = -1, -1, ""
		return ""
	}

	// The first key inside the block fixes the depth service names live at.
	if t.serviceIndent < 0 {
		t.serviceIndent = indent
	}
	if indent == t.serviceIndent {
		t.current = strings.TrimSpace(m[2])
	}

	return t.current
}

// scalarValue reads the value written after a mapping key on the same line,
// unquoted and without a trailing comment. An empty result means the key opened
// a block instead — including `build:  # built locally`, where reading the
// comment as a value would send path resolution looking for a directory.
func scalarValue(line string) string {
	_, rest, ok := strings.Cut(line, ":")
	if !ok {
		return ""
	}

	value := strings.TrimSpace(rest)
	if strings.HasPrefix(value, "#") {
		return ""
	}
	value, _, _ = strings.Cut(value, " #")
	value = strings.TrimSpace(value)
	return strings.Trim(value, `"'`)
}
