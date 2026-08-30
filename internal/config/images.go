package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Level names an update level a cap can be set to. It is the same vocabulary
// internal.UpdateInfo speaks, so a cap read from a config file can be compared
// against an UpdateLevel without translation.
type Level string

const (
	LevelPatch Level = "patch"
	LevelMinor Level = "minor"
	LevelMajor Level = "major"
)

// rank orders the levels so a cap can be compared against an update. A level
// missing from this map is one no cap can express — "digest", for one, which
// carries no version to be higher or lower than anything.
var rank = map[Level]int{LevelPatch: 1, LevelMinor: 2, LevelMajor: 3}

// Valid reports whether l is a level a cap may be set to.
func (l Level) Valid() bool {
	_, ok := rank[l]
	return ok
}

// Versioning names the scheme an image's tags are read as versions under. Docker
// tags are not required to be semantic versions, so an image whose tags ccu
// cannot read is given the scheme that can read them rather than every image
// being taught a looser rule it never needed.
type Versioning string

const (
	// VersioningSemver is the default: at most three segments, no leading zeros.
	VersioningSemver Versioning = "semver"
	// VersioningLoose reads up to six numeric segments and tolerates the leading
	// zeros a calendar tag brings with it, so "2026.7.7.2" orders after
	// "2026.7.7" and before "2026.7.30".
	VersioningLoose Versioning = "loose"
	// VersioningRegex reads tags with a pattern written beside it, for the
	// repositories neither of the above can read at all — dashed calendar tags
	// ("2024-01-01") above all, which loose reads as release 2024 with the date
	// as a suffix and so orders by month name rather than by date.
	VersioningRegex Versioning = "regex"
)

// versionings is every scheme a config may name, for validation and for the
// error message that lists them.
var versionings = []Versioning{VersioningSemver, VersioningLoose, VersioningRegex}

// Valid reports whether v names a scheme ccu knows.
func (v Versioning) Valid() bool {
	for _, known := range versionings {
		if v == known {
			return true
		}
	}
	return false
}

// ImagePolicy is what the user recorded about one image: how far it may move,
// and how its tags are to be read.
type ImagePolicy struct {
	// Max is the highest level this image may be updated to. Empty means no cap,
	// which is also what an image with no entry at all gets.
	Max Level `yaml:"max"`

	// Versioning is the scheme this image's tags are read under. Empty means the
	// scheme the run resolved as its default, which is `semver` unless the config
	// or -versioning said otherwise.
	Versioning Versioning `yaml:"versioning"`

	// VersioningPattern is the regex `versioning: regex` reads this image's tags
	// with, using Go's named groups: (?P<major>…), (?P<minor>…), (?P<patch>…),
	// (?P<build>…) and (?P<suffix>…). It belongs to the image rather than to the
	// run, which is why there is no global key for it — a pattern that fits one
	// repository's tags is meaningless for the next one's.
	VersioningPattern string `yaml:"versioning_pattern"`
}

// MaxLevel returns the cap recorded for an image, or "" when there is none.
// Lookup is exact: the key is the image name without tag or digest, as
// internal.UpdateInfo.ImageName reports it (e.g. "library/traefik").
func (c Config) MaxLevel(image string) Level {
	if c.Images == nil {
		return ""
	}
	return c.Images[image].Max
}

// Caps flattens the per-image policies into the map the scanner reads: image
// name to cap level, with uncapped images left out entirely so a lookup miss and
// "no cap" are the same thing.
func (c Config) Caps() map[string]string {
	if len(c.Images) == 0 {
		return nil
	}

	caps := make(map[string]string, len(c.Images))
	for image, policy := range c.Images {
		if policy.Max == "" {
			continue
		}
		caps[image] = string(policy.Max)
	}
	if len(caps) == 0 {
		return nil
	}
	return caps
}

// Versionings flattens the per-image schemes into the map the scanner reads,
// leaving out images that named none so a lookup miss and "use the default" are
// the same thing.
func (c Config) Versionings() map[string]string {
	if len(c.Images) == 0 {
		return nil
	}

	schemes := make(map[string]string, len(c.Images))
	for image, policy := range c.Images {
		if policy.Versioning == "" {
			continue
		}
		schemes[image] = string(policy.Versioning)
	}
	if len(schemes) == 0 {
		return nil
	}
	return schemes
}

// VersioningPatterns flattens the per-image patterns into the map the scanner
// reads. Only images on the `regex` scheme have one — validateImages rejects a
// pattern anywhere else — so a lookup miss and "this image needs no pattern" are
// the same thing.
func (c Config) VersioningPatterns() map[string]string {
	if len(c.Images) == 0 {
		return nil
	}

	patterns := make(map[string]string, len(c.Images))
	for image, policy := range c.Images {
		if policy.VersioningPattern == "" {
			continue
		}
		patterns[image] = policy.VersioningPattern
	}
	if len(patterns) == 0 {
		return nil
	}
	return patterns
}

// DefaultVersioning is the scheme for images that named none of their own.
func (c Config) DefaultVersioning() string {
	if c.Versioning == "" {
		return string(VersioningSemver)
	}
	return string(c.Versioning)
}

// Allows reports whether an update of the given level is permitted under cap.
// An empty cap permits everything, and so does a level that carries no version
// to compare — a digest update is either a different image or it is not, and a
// cap has nothing to say about that.
func Allows(cap Level, level string) bool {
	if cap == "" {
		return true
	}
	want, ok := rank[Level(level)]
	if !ok {
		return true
	}
	return want <= rank[cap]
}

// mergeImages layers over onto base. Unlike Exclude, which unions, a per-image
// policy *replaces*: the project file has to be able to raise a cap the global
// file set, not only tighten it. An image only the base names keeps its policy.
func mergeImages(base, over map[string]ImagePolicy) map[string]ImagePolicy {
	if len(base) == 0 {
		return over
	}
	if len(over) == 0 {
		return base
	}

	merged := make(map[string]ImagePolicy, len(base)+len(over))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range over {
		merged[k] = v
	}
	return merged
}

// validateImages rejects a value that names no level or no scheme, for the same
// reason an unknown key is rejected: a silently ignored `max: mayor` looks
// exactly like a feature that does not work.
func validateImages(images map[string]ImagePolicy) error {
	for image, policy := range images {
		if policy.Max != "" && !policy.Max.Valid() {
			return fmt.Errorf("image %q: max: %q is not one of %s", image, policy.Max, strings.Join([]string{
				string(LevelPatch), string(LevelMinor), string(LevelMajor),
			}, ", "))
		}
		if err := ValidateVersioning(policy.Versioning); err != nil {
			return fmt.Errorf("image %q: %w", image, err)
		}
		if err := validateVersioningPattern(policy.Versioning, policy.VersioningPattern); err != nil {
			return fmt.Errorf("image %q: %w", image, err)
		}
	}
	return nil
}

// validateVersioningPattern rejects every way a scheme and a pattern can fail to
// agree: a pattern under a scheme that never reads one, `regex` without a
// pattern to read with, a pattern that does not compile, and one that names no
// group and so has nothing to read a version out of.
//
// It runs at load rather than at scan time on purpose. A pattern only rejected
// once the tags are in would leave the image quietly compared by digest, in the
// middle of a report about something else entirely — while the file that has to
// be fixed is right here, and can be named.
func validateVersioningPattern(v Versioning, pattern string) error {
	if v != VersioningRegex {
		if pattern == "" {
			return nil
		}
		scheme := string(v)
		if scheme == "" {
			scheme = string(VersioningSemver) + ", the default"
		}
		return fmt.Errorf("versioning_pattern: only %q reads a pattern, and this image is on %s", VersioningRegex, scheme)
	}

	if pattern == "" {
		return fmt.Errorf(`versioning: %q needs a versioning_pattern, e.g. '^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})$'`, VersioningRegex)
	}

	// Compiled rather than trusted, and never with MustCompile: this is a line
	// out of a config file, and a typo in one must fail the load with a message,
	// not take the process down with it.
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("versioning_pattern: %q is not a valid regular expression: %w", pattern, err)
	}
	for _, name := range compiled.SubexpNames() {
		if name != "" {
			return nil
		}
	}
	return fmt.Errorf("versioning_pattern: %q names no group, so there is nothing to read a version out of; write (?P<major>…) around the part that carries it", pattern)
}

// ValidateVersioning rejects a scheme name ccu does not know. An empty name is
// fine: it means the image, or the run, said nothing and takes the default.
func ValidateVersioning(v Versioning) error {
	if v == "" || v.Valid() {
		return nil
	}

	names := make([]string, 0, len(versionings))
	for _, known := range versionings {
		names = append(names, string(known))
	}
	return fmt.Errorf("versioning: %q is not one of %s", v, strings.Join(names, ", "))
}

// ValidateDefaultVersioning is ValidateVersioning for the two places that set
// the scheme for the whole run — the global `versioning:` key and -versioning —
// where `regex` is additionally rejected. A default reaches every image at once
// and there is no one image to take a versioning_pattern from, so accepting it
// there would mean a scheme that reads no tag at all: every image silently
// dropped to comparing digests.
func ValidateDefaultVersioning(v Versioning) error {
	if err := ValidateVersioning(v); err != nil {
		return err
	}
	if v == VersioningRegex {
		return fmt.Errorf("versioning: %q is a per-image scheme, because the pattern it reads with belongs to one image: set it under images.<name>.versioning together with a versioning_pattern", v)
	}
	return nil
}
