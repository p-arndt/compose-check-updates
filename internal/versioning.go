package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	VersioningRegex  = "regex"
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

// regexSegmentGroups are the named groups a regex pattern may use for the
// numeric segments, in the order they order the version. A group the pattern
// does not name contributes 0, so a pattern naming only "major" reads a
// one-segment version and one naming "major" and "patch" reads "x.0.y".
//
// Four rather than three, and named the way loose counts: anything past the
// third orders the version but names no level of its own, so a "build" moving on
// its own is reported as a patch — see Version.Major and sameTagFamily. Anything
// a repository writes past that belongs in the suffix, which is where the
// schemes ccu ships put it too.
var regexSegmentGroups = []string{"major", "minor", "patch", "build"}

// regexSuffixGroup is the named group carrying the prerelease or build metadata,
// separator included. It is matched against Version.Suffix as written, which is
// what makes an image on "-alpine" stay on "-alpine"; see isUpgradeCandidate.
const regexSuffixGroup = "suffix"

// regexVersioning reads tags with a pattern the user wrote. It is the way out
// for repositories whose tags are versions in a shape no fixed rule can read —
// dashed calendar tags like "2024-01-01" above all, which loose reads as release
// 2024 with the rest as a suffix, ordering "2024-12-31" before "2024-01-01".
//
// Unlike the two schemes above this one is not a package-level singleton: the
// pattern belongs to a single image, so a scheme is built per image from what
// the config recorded for it.
type regexVersioning struct {
	pattern *regexp.Regexp
	// segments is how many numeric segments a tag read by this pattern has: one
	// past the highest group the pattern names. Derived from the pattern rather
	// than from each match, so every tag of a repository reports the same count —
	// sameTagFamily reads it, and a count that moved with the tag would make a
	// tag drop in and out of its own family.
	segments int
}

func (regexVersioning) Name() string { return VersioningRegex }

func (r regexVersioning) Parse(tag string) (Version, bool) {
	// No "v" is trimmed here, unlike the numeric schemes: the pattern is the
	// user's own and says for itself what a tag may begin with.
	match := r.pattern.FindStringSubmatch(tag)
	if match == nil {
		return Version{}, false
	}

	release := make([]int, r.segments)
	suffix := ""

	for i, name := range r.pattern.SubexpNames() {
		// The whole match, and every group the user left unnamed.
		if i == 0 || name == "" {
			continue
		}
		value := match[i]

		if name == regexSuffixGroup {
			suffix = value
			continue
		}

		index := segmentIndex(name)
		if index < 0 {
			// A group named something else entirely. Patterns are easier to read
			// with a name on every group, so one ccu has no use for is ignored
			// rather than rejected.
			continue
		}
		// An optional group that did not take part names the .0 release, exactly
		// as a tag writing fewer segments does under the numeric schemes.
		if value == "" {
			continue
		}

		n, err := strconv.Atoi(value)
		if err != nil {
			// The pattern let something through that is not a number ccu can
			// order — letters, or a segment too large for an int. That is not a
			// version, so the checker falls back to comparing digests for it.
			return Version{}, false
		}
		release[index] = n
	}

	return Version{Tag: tag, Release: release, Suffix: suffix}, true
}

// segmentIndex returns the position name orders at, or -1 when it names no
// segment.
func segmentIndex(name string) int {
	for i, group := range regexSegmentGroups {
		if name == group {
			return i
		}
	}
	return -1
}

// newRegexVersioning builds a scheme from a pattern a user wrote. It returns an
// error rather than panicking, because this is user input: regexp.MustCompile
// here would turn a typo in a config file into a crash. The config layer rejects
// the same patterns on load, so a run only ever reaches this with one it has
// already accepted.
//
// The pattern is anchored, matching the whole tag the way semver and loose do.
// Go matches anywhere in the string otherwise, so a pattern meant for dates would
// read the 2024 out of "sha-2024ab12" as well and order a commit tag against real
// releases. A pattern the user already anchored is unharmed by being anchored
// again, which is why this needs no care about the difference.
func newRegexVersioning(pattern string) (regexVersioning, error) {
	if pattern == "" {
		return regexVersioning{}, fmt.Errorf("versioning %q needs a pattern", VersioningRegex)
	}

	compiled, err := regexp.Compile(`^(?:` + pattern + `)$`)
	if err != nil {
		return regexVersioning{}, err
	}

	segments := 0
	named := false
	for _, name := range compiled.SubexpNames() {
		if name == "" {
			continue
		}
		named = true
		if index := segmentIndex(name); index >= 0 {
			segments = max(segments, index+1)
		}
	}
	if !named {
		return regexVersioning{}, fmt.Errorf("pattern %q names no group, so there is nothing to read a version out of", pattern)
	}

	return regexVersioning{pattern: compiled, segments: segments}, nil
}

// regexSchemes caches the schemes built from user patterns, keyed by the pattern
// as written. VersioningByName is called once per image per check but also every
// time an update is asked for its level, which the TUI does on every redraw;
// compiling the same handful of patterns over and over for that is work nobody
// asked for. Patterns come from a config file, so the set is small and bounded.
var regexSchemes sync.Map

// VersioningByName returns the scheme configured under name. An empty name is
// the default rather than an error: it is what every image that was never given
// a scheme has.
//
// pattern is the regex an image on the `regex` scheme reads its tags with, and
// is ignored by every other scheme. It is passed alongside the name rather than
// looked up here because it belongs to one image, not to the scheme: the caller
// is the only one that knows which image is being asked about.
func VersioningByName(name, pattern string) (Versioning, bool) {
	switch name {
	case "", VersioningSemver:
		return semverVersioning, true
	case VersioningLoose:
		return looseVersioning, true
	case VersioningRegex:
		if cached, ok := regexSchemes.Load(pattern); ok {
			return cached.(regexVersioning), true
		}
		scheme, err := newRegexVersioning(pattern)
		if err != nil {
			// Not an error worth reporting from here: the config layer rejects
			// these on load and names the file they came from, so the only way
			// one arrives is a pattern that never went through it.
			return nil, false
		}
		regexSchemes.Store(pattern, scheme)
		return scheme, true
	}
	return nil, false
}

// DefaultVersioning is the scheme an image with no preference recorded gets.
func DefaultVersioning() Versioning { return semverVersioning }

// ResolveVersioning picks the scheme name for one image: an entry naming it
// wins, and everything else takes the run's default. This is the whole
// precedence rule, and the only copy of it — the ordering between the flag and
// the two config files is settled before this, leaving a single default here.
//
// Lookup is exact on the image name without tag or digest, the same key a cap is
// recorded under (e.g. "library/traefik").
func ResolveVersioning(perImage map[string]string, def, image string) string {
	if name, ok := perImage[image]; ok && name != "" {
		return name
	}
	return def
}
