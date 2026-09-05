package policy

import (
	"maps"
	"slices"
)

// floating are the tags ccu knows move to whatever is newest. They are never
// proposed as an update target: moving from one floating tag to another says
// nothing about the image having changed.
var floating = map[string]struct{}{
	"latest":  {},
	"main":    {},
	"master":  {},
	"edge":    {},
	"stable":  {},
	"nightly": {},
	"dev":     {},
	"develop": {},
}

// Floats reports whether tag moves. Image.FloatingTags adds to the built-in set
// rather than replacing it: a repository tagging its releases "release" almost
// certainly still publishes a "latest" beside them, and treating that as an
// ordinary version tag is the one thing the built-in set exists to prevent.
func (i Image) Floats(tag string) bool {
	if _, ok := floating[tag]; ok {
		return true
	}
	return slices.Contains(i.FloatingTags, tag)
}

// BuiltInFloatingTags lists the tags ccu knows float, in no particular order.
func BuiltInFloatingTags() []string {
	// A fresh slice every call: the built-in set is the one thing a caller must
	// not be able to write into.
	return slices.Collect(maps.Keys(floating))
}
