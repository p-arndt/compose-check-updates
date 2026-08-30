package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Versioning is how a repository's tags are read as versions. Docker tags are
// not required to be semantic versions and plenty of images publish something
// else entirely, so rather than guess a single rule that fits all of them, the
// rule is a setting: an image whose tags ccu cannot read is given the scheme
// that can read them, and every other image is left alone.
type Versioning interface {
	// Name is the word this scheme is configured under.
	Name() string
	// Parse reads a tag as a version, reporting false when this scheme has no
	// way to make sense of it. A tag no scheme can read is not a version, and the
	// checker falls back to comparing digests for it.
	Parse(tag string) (Version, bool)
}

// Version is a tag read as a version: the numeric segments it named, in the
// order they were written, plus whatever suffix trailed them.
//
// The segments are kept as a slice rather than the fixed major/minor/patch trio
// because the schemes disagree about how many there may be, and because the
// count itself carries meaning — "16" and "16.0.0" order the same but are not
// interchangeable, see sameTagFamily.
type Version struct {
	// Tag is the tag exactly as the registry spells it, so it can be written
	// back into the compose file with its "v" prefix intact.
	Tag string
	// Release is the numeric segments, most significant first.
	Release []int
	// Suffix is the prerelease or build metadata, including its leading
	// separator: "-rc1", "-alpine", "+build.5". Empty for a plain release.
	Suffix string
}

// Segment returns the i-th numeric segment, or 0 when the tag did not write one.
// Padding with zero is what makes "2.1" and "2.1.0" compare equal, which is the
// behaviour both schemes want: a tag naming fewer segments names the .0 release.
func (v Version) Segment(i int) int {
	if i < 0 || i >= len(v.Release) {
		return 0
	}
	return v.Release[i]
}

// Major, Minor and Patch name the first three segments. Every segment past the
// third contributes to the ordering below but to no level of its own: a fourth
// segment is a rebuild of the release the first three name, so it is reported as
// a patch. This is the same choice Renovate's generic versioning makes, and the
// same one NuGet makes for its Major.Minor.Patch.Revision.
func (v Version) Major() int { return v.Segment(0) }
func (v Version) Minor() int { return v.Segment(1) }
func (v Version) Patch() int { return v.Segment(2) }

// Segments is how many numeric segments the tag actually wrote.
func (v Version) Segments() int { return len(v.Release) }

// Compare orders two versions, returning -1, 0 or 1. Segments are compared
// left to right with the shorter side padded with zeros, and a plain release
// outranks a suffixed one of the same number, so "1.0.0" is newer than
// "1.0.0-rc1".
func (v Version) Compare(o Version) int {
	n := max(len(v.Release), len(o.Release))
	for i := range n {
		a, b := v.Segment(i), o.Segment(i)
		if a != b {
			if a < b {
				return -1
			}
			return 1
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

// GreaterThan reports whether v orders after o.
func (v Version) GreaterThan(o Version) bool { return v.Compare(o) > 0 }

// numericVersioning is a scheme that reads a tag as dot-separated numbers
// followed by an optional suffix. Both schemes ccu ships are this shape; they
// differ only in how many segments they accept and how strict they are about
// what a segment may look like.
type numericVersioning struct {
	name    string
	pattern *regexp.Regexp
}

func (n numericVersioning) Name() string { return n.name }

func (n numericVersioning) Parse(tag string) (Version, bool) {
	// An optional leading "v" is how a great many repositories spell a version
	// tag, and it says nothing about the version itself.
	trimmed := strings.TrimPrefix(tag, "v")

	matches := n.pattern.FindStringSubmatch(trimmed)
	if matches == nil {
		return Version{}, false
	}

	parts := strings.Split(matches[1], ".")
	release := make([]int, 0, len(parts))
	for _, p := range parts {
		// The pattern already guarantees digits, so the only way this fails is a
		// segment too large for an int — a tag nobody meant as a version.
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, false
		}
		release = append(release, n)
	}

	return Version{Tag: tag, Release: release, Suffix: matches[2]}, true
}

// newNumericVersioning builds a scheme from what a single segment may look like
// and how many of them are allowed.
func newNumericVersioning(name, segment, suffix string, maxSegments int) numericVersioning {
	pattern := fmt.Sprintf(`^(%s(?:\.%s){0,%d})(%s)$`, segment, segment, maxSegments-1, suffix)
	return numericVersioning{name: name, pattern: regexp.MustCompile(pattern)}
}

// Scheme names, as they are written in a config file or passed to -versioning.
const (
	VersioningSemver = "semver"
	VersioningLoose  = "loose"
)

var (
	// semverVersioning is what every image gets unless it was given something
	// else: at most three segments, no leading zeros, and a suffix that is a
	// semver prerelease or build metadata. It is deliberately strict — a tag it
	// cannot read is one ccu should compare by digest rather than guess about.
	semverVersioning = newNumericVersioning(
		VersioningSemver,
		`(?:0|[1-9]\d*)`,
		`(?:-[0-9A-Za-z-.]+)?(?:\+[0-9A-Za-z-.]+)?`,
		3,
	)

	// looseVersioning is the opt-in for repositories whose tags are versions but
	// not semantic ones — calendar tags above all, where "2026.7.7.2" is the
	// second build of a release the first three segments already name. It reads
	// up to six segments, tolerates the leading zeros a date brings with it
	// ("2026.07.07"), and accepts any suffix, since an image publishing this kind
	// of tag is unlikely to be careful about the rest either.
	//
	// Six is where Renovate's loose versioning stops as well. The cap matters:
	// without one, a tag that is plainly not a version orders against real ones
	// on rules nobody can follow.
	looseVersioning = newNumericVersioning(
		VersioningLoose,
		`\d+`,
		`(?:[-+_].*)?`,
		6,
	)
)

// VersioningByName returns the scheme configured under name. An empty name is
// the default rather than an error: it is what every image that was never given
// a scheme has.
func VersioningByName(name string) (Versioning, bool) {
	switch name {
	case "", VersioningSemver:
		return semverVersioning, true
	case VersioningLoose:
		return looseVersioning, true
	}
	return nil, false
}

// DefaultVersioning is the scheme an image with no preference recorded gets.
func DefaultVersioning() Versioning { return semverVersioning }
