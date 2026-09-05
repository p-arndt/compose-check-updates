// Package versioning reads Docker tags as versions. Tags are not required to be
// semantic versions, so the rule is a setting: an image whose tags ccu cannot
// read is given the scheme that can read them.
package versioning

import (
	"cmp"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// Version is a tag read as a version.
type Version struct {
	// Tag is the tag as the registry spells it, so it can be written back with
	// its "v" prefix intact.
	Tag string
	// Release is the numeric segments, most significant first. A slice rather
	// than a major/minor/patch trio because the count itself carries meaning:
	// "16" and "16.0.0" order the same but are not interchangeable, see
	// SameFamily.
	Release []int
	// Suffix is the prerelease or build metadata including its leading
	// separator: "-rc1", "-alpine", "+build.5".
	Suffix string
}

// Segment returns the i-th numeric segment, or 0 when the tag wrote none, which
// is what makes "2.1" and "2.1.0" compare equal.
func (v Version) Segment(i int) int {
	if i < 0 || i >= len(v.Release) {
		return 0
	}
	return v.Release[i]
}

// Major, Minor and Patch name the first three segments. A fourth segment orders
// the version but names no level of its own: it rebuilds the release the first
// three name, so it is reported as a patch.
func (v Version) Major() int { return v.Segment(0) }
func (v Version) Minor() int { return v.Segment(1) }
func (v Version) Patch() int { return v.Segment(2) }

func (v Version) Segments() int { return len(v.Release) }

// Compare orders two versions. A plain release outranks a suffixed one of the
// same number, so "1.0.0" is newer than "1.0.0-rc1".
func (v Version) Compare(o Version) int {
	for i := range max(len(v.Release), len(o.Release)) {
		if c := cmp.Compare(v.Segment(i), o.Segment(i)); c != 0 {
			return c
		}
	}

	switch {
	case v.Suffix == o.Suffix:
		return 0
	case v.Suffix == "":
		return 1
	case o.Suffix == "":
		return -1
	}
	return strings.Compare(v.Suffix, o.Suffix)
}

func (v Version) GreaterThan(o Version) bool { return v.Compare(o) > 0 }

// Diff names how far next moved beyond current, or "" when it did not.
func Diff(current, next Version) policy.Level {
	switch {
	case !next.GreaterThan(current):
		return ""
	case next.Major() > current.Major():
		return policy.LevelMajor
	case next.Minor() > current.Minor():
		return policy.LevelMinor
	default:
		return policy.LevelPatch
	}
}
