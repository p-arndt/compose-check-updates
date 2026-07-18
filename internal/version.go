package internal

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// versionTag pairs a parsed version with the tag it was parsed from, so the
// tag can be returned in its original form (e.g. keeping a "v" prefix).
type versionTag struct {
	Version *semver.Version
	Tag     string
}

// candidateVersions parses every tag that looks like a version and returns them
// sorted newest first.
func candidateVersions(tags []string) []versionTag {
	var versionTags []versionTag

	// Collect valid semantic versions (accept tags prefixed with 'v' and allow "non-strict" semver like "1.1")
	for _, tag := range tags {
		candidate := strings.TrimPrefix(tag, "v")
		normalized, ok := normalizeSemver(candidate)
		if !ok {
			continue
		}

		v, err := semver.NewVersion(normalized)
		if err != nil {
			continue
		}
		versionTags = append(versionTags, versionTag{Version: v, Tag: tag})
	}

	sort.Slice(versionTags, func(i, j int) bool {
		return versionTags[i].Version.GreaterThan(versionTags[j].Version)
	})

	return versionTags
}

// isUpgradeCandidate reports whether v is a newer release of the same release
// line as current: a stable current never moves onto a prerelease, and a
// prerelease current only moves within its own prerelease suffix.
func isUpgradeCandidate(v, current *semver.Version) bool {
	if !v.GreaterThan(current) {
		return false
	}
	if current.Prerelease() == "" {
		return v.Prerelease() == ""
	}
	return v.Prerelease() == current.Prerelease()
}

// FindLatestPerLevel returns the newest tag available at each upgrade level
// relative to current. Any return value is "" when no upgrade exists at that
// level. patchTag stays within the current major.minor; minorTag stays within
// the current major; majorTag crosses to a higher major.
func FindLatestPerLevel(current *semver.Version, tags []string) (patchTag, minorTag, majorTag string) {
	// Sorted newest first, so the first match at a level is that level's best.
	for _, vt := range candidateVersions(tags) {
		v := vt.Version
		if !isUpgradeCandidate(v, current) {
			continue
		}

		switch {
		case v.Major() > current.Major():
			if majorTag == "" {
				majorTag = vt.Tag
			}
		case v.Minor() > current.Minor():
			if minorTag == "" {
				minorTag = vt.Tag
			}
		case v.Patch() > current.Patch():
			if patchTag == "" {
				patchTag = vt.Tag
			}
		}

		if patchTag != "" && minorTag != "" && majorTag != "" {
			break
		}
	}

	return patchTag, minorTag, majorTag
}

func FindLatestVersion(current *semver.Version, tags []string, major, minor, patch bool) string {
	if major {
		minor = true
		patch = true
	}
	if minor {
		patch = true
	}

	versionTags := candidateVersions(tags)
	if len(versionTags) == 0 {
		return ""
	}

	for _, vt := range versionTags {
		v := vt.Version
		tag := vt.Tag

		// Skips versions not newer than current, and enforces the prerelease rules.
		if !isUpgradeCandidate(v, current) {
			continue
		}

		accept := false
		if major && v.Major() > current.Major() {
			accept = true
		} else if minor && isEqualMajor(v, current) && v.Minor() > current.Minor() {
			accept = true
		} else if patch && isEqualMajor(v, current) && isEqualMinor(v, current) && v.Patch() > current.Patch() {
			accept = true
		}

		if accept {
			return tag
		}
	}

	return ""
}

// normalizeSemver accepts strict semantic versions as well as "non-strict" forms
// like "1.1" (treated as "1.1.0"). If the tag is not a supported version
// format it returns ok=false.
func normalizeSemver(tag string) (normalized string, ok bool) {
	// Accept an optional leading "v" (e.g. "v1.2.3") as well as plain semver.
	tag = strings.TrimPrefix(tag, "v")

	// Capture major.minor[.patch] and optional prerelease/build metadata.
	regex := regexp.MustCompile(`^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)(?:\.(?P<patch>0|[1-9]\d*))?(?P<rest>(?:-[0-9A-Za-z-.]+)?(?:\+[0-9A-Za-z-.]+)?)$`)
	matches := regex.FindStringSubmatch(tag)
	if len(matches) == 0 {
		return "", false
	}

	major := matches[1]
	minor := matches[2]
	patch := matches[3]
	rest := matches[4]
	if patch == "" {
		patch = "0"
	}

	return major + "." + minor + "." + patch + rest, true
}

func isEqualMajor(current, tag *semver.Version) bool {
	return current.Major() == tag.Major()
}

func isEqualMinor(current, tag *semver.Version) bool {
	return current.Minor() == tag.Minor()
}
