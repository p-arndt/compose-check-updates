package versioning

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"sync"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// segmentGroups are the named groups a pattern may use for the numeric segments,
// in the order they order the version. A group the pattern does not name
// contributes 0. Four rather than three, the way loose counts: anything past the
// third orders the version but names no level of its own, and anything beyond
// that belongs in suffixGroup.
var segmentGroups = []string{"major", "minor", "patch", "build"}

// suffixGroup carries the prerelease or build metadata, separator included, and
// is matched as written — which is what keeps an image on "-alpine".
const suffixGroup = "suffix"

// regexScheme reads tags with a pattern the user wrote. Unlike the built-in
// schemes it is not a singleton: the pattern belongs to a single image.
type regexScheme struct {
	pattern *regexp.Regexp
	// segments is one past the highest group the pattern names. Derived from the
	// pattern rather than from each match, so every tag of a repository reports
	// the same count: SameFamily reads it, and a count that moved with the tag
	// would make a tag drop in and out of its own family.
	segments int
}

func (regexScheme) Name() policy.Versioning { return policy.VersioningRegex }

func (r regexScheme) Parse(tag string) (Version, bool) {
	// No "v" is trimmed here, unlike the numeric schemes: the pattern is the
	// user's own and says for itself what a tag may begin with.
	match := r.pattern.FindStringSubmatch(tag)
	if match == nil {
		return Version{}, false
	}

	release := make([]int, r.segments)
	suffix := ""

	for i, name := range r.pattern.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		value := match[i]

		if name == suffixGroup {
			suffix = value
			continue
		}

		// A group ccu has no use for is ignored rather than rejected, and one that
		// did not take part names the .0 release.
		index := segmentIndex(name)
		if index < 0 || value == "" {
			continue
		}

		n, err := strconv.Atoi(value)
		if err != nil {
			return Version{}, false
		}
		release[index] = n
	}

	return Version{Tag: tag, Release: release, Suffix: suffix}, true
}

func segmentIndex(name string) int { return slices.Index(segmentGroups, name) }

// namesAnyGroup reports whether a compiled pattern names at least one group.
// Without one there is nothing to read a version out of, which both the config
// check and the scheme itself have to refuse.
func namesAnyGroup(re *regexp.Regexp) bool {
	return slices.ContainsFunc(re.SubexpNames(), func(name string) bool { return name != "" })
}

// regexSchemes caches schemes by pattern: ByName runs once per image per check,
// but also every time an update is asked for its level, which the TUI does on
// every redraw.
var regexSchemes sync.Map

func regexSchemeFor(pattern string) (regexScheme, error) {
	if cached, ok := regexSchemes.Load(pattern); ok {
		return cached.(regexScheme), nil
	}
	scheme, err := newRegexScheme(pattern)
	if err != nil {
		return regexScheme{}, err
	}
	regexSchemes.Store(pattern, scheme)
	return scheme, nil
}

// newRegexScheme anchors the pattern so it matches the whole tag the way the
// numeric schemes do; unanchored, a pattern meant for dates would read the 2024
// out of "sha-2024ab12" and order a commit tag against real releases.
func newRegexScheme(pattern string) (regexScheme, error) {
	if pattern == "" {
		return regexScheme{}, fmt.Errorf("versioning %q needs a pattern", policy.VersioningRegex)
	}

	compiled, err := regexp.Compile(`^(?:` + pattern + `)$`)
	if err != nil {
		return regexScheme{}, err
	}

	if !namesAnyGroup(compiled) {
		return regexScheme{}, fmt.Errorf("pattern %q names no group, so there is nothing to read a version out of", pattern)
	}

	segments := 0
	for _, name := range compiled.SubexpNames() {
		if index := segmentIndex(name); index >= 0 {
			segments = max(segments, index+1)
		}
	}

	return regexScheme{pattern: compiled, segments: segments}, nil
}

// ValidatePattern rejects every way a scheme and a pattern can fail to agree. It
// runs at config load rather than at scan time: a pattern only rejected once the
// tags are in would leave the image quietly compared by digest, while the file
// to fix can be named right here.
func ValidatePattern(v policy.Versioning, pattern string) error {
	if v != policy.VersioningRegex {
		if pattern == "" {
			return nil
		}
		scheme := v.String()
		if scheme == "" {
			scheme = policy.VersioningSemver.String() + ", the default"
		}
		return fmt.Errorf("versioning_pattern: only %q reads a pattern, and this image is on %s", policy.VersioningRegex, scheme)
	}

	if pattern == "" {
		return fmt.Errorf(`versioning: %q needs a versioning_pattern, e.g. '^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})$'`, policy.VersioningRegex)
	}

	// Compiled rather than trusted, and never with MustCompile: a typo in a
	// config file must fail the load with a message, not take the process down.
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("versioning_pattern: %q is not a valid regular expression: %w", pattern, err)
	}
	if namesAnyGroup(compiled) {
		return nil
	}
	return fmt.Errorf("versioning_pattern: %q names no group, so there is nothing to read a version out of; write (?P<major>…) around the part that carries it", pattern)
}
