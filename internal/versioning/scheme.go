package versioning

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// Scheme is one way of reading a repository's tags as versions.
type Scheme interface {
	Name() policy.Versioning
	// Parse reports false when this scheme cannot make sense of the tag; the
	// checker then falls back to comparing digests.
	Parse(tag string) (Version, bool)
}

// numeric reads a tag as dot-separated numbers plus an optional suffix. Both
// built-in schemes are this shape and differ only in how many segments they
// accept and how strict they are about each one.
type numeric struct {
	name    policy.Versioning
	pattern *regexp.Regexp
}

func (n numeric) Name() policy.Versioning { return n.name }

func (n numeric) Parse(tag string) (Version, bool) {
	matches := n.pattern.FindStringSubmatch(strings.TrimPrefix(tag, "v"))
	if matches == nil {
		return Version{}, false
	}

	parts := strings.Split(matches[1], ".")
	release := make([]int, 0, len(parts))
	for _, p := range parts {
		// The pattern guarantees digits, so the only failure is a segment too
		// large for an int — a tag nobody meant as a version.
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, false
		}
		release = append(release, n)
	}

	return Version{Tag: tag, Release: release, Suffix: matches[2]}, true
}

func newNumeric(name policy.Versioning, segment, suffix string, maxSegments int) numeric {
	return numeric{
		name:    name,
		pattern: regexp.MustCompile(fmt.Sprintf(`^(%s(?:\.%s){0,%d})(%s)$`, segment, segment, maxSegments-1, suffix)),
	}
}

var (
	// Deliberately strict: a tag semver cannot read is one to compare by digest
	// rather than guess about.
	semver = newNumeric(policy.VersioningSemver, `(?:0|[1-9]\d*)`, `(?:-[0-9A-Za-z-.]+)?(?:\+[0-9A-Za-z-.]+)?`, 3)

	// Six segments is where Renovate's loose versioning stops too. Without a cap,
	// a tag that is plainly not a version orders against real ones.
	loose = newNumeric(policy.VersioningLoose, `\d+`, `(?:[-+_].*)?`, 6)
)

// ByName returns the scheme configured under name; an empty name is the default.
// pattern belongs to a single image, so it is passed alongside the name rather
// than looked up here, and is ignored by every scheme but regex.
func ByName(name policy.Versioning, pattern string) (Scheme, bool) {
	switch name {
	case "", policy.VersioningSemver:
		return semver, true
	case policy.VersioningLoose:
		return loose, true
	case policy.VersioningRegex:
		scheme, err := regexSchemeFor(pattern)
		if err != nil {
			// ValidatePattern rejects these at load and names the file they came
			// from, so one arriving here never went through it.
			return nil, false
		}
		return scheme, true
	}
	return nil, false
}

// Default is the scheme an image with no preference recorded gets.
func Default() Scheme { return semver }

// Validate rejects a scheme name ccu does not know. Empty is fine: it means the
// image, or the run, said nothing and takes the default.
func Validate(v policy.Versioning) error {
	if v == "" || v.Valid() {
		return nil
	}

	// Once: every call clones the backing slice.
	all := policy.Versionings()
	known := make([]string, 0, len(all))
	for _, name := range all {
		known = append(known, name.String())
	}
	return fmt.Errorf("versioning: %q is not one of %s", v, strings.Join(known, ", "))
}

// ValidateDefault additionally rejects regex, which the two run-wide settings —
// the global `versioning:` key and -versioning — cannot use: there is no one
// image to take a pattern from, so every image would drop to comparing digests.
func ValidateDefault(v policy.Versioning) error {
	if err := Validate(v); err != nil {
		return err
	}
	if v == policy.VersioningRegex {
		return fmt.Errorf("versioning: %q is a per-image scheme, because the pattern it reads with belongs to one image: set it under images.<name>.versioning together with a versioning_pattern", v)
	}
	return nil
}
