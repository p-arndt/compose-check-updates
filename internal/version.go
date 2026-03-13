package internal

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

func FindLatestVersion(current *semver.Version, tags []string, major, minor, patch bool) string {
	if major {
		minor = true
		patch = true
	}
	if minor {
		patch = true
	}

	type VersionTag struct {
		Version *semver.Version
		Tag     string
	}
	var versionTags []VersionTag

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
		versionTags = append(versionTags, VersionTag{Version: v, Tag: tag})
	}

	if len(versionTags) == 0 {
		return ""
	}

	// Sort versions in descending order
	// This is necessary to find the latest version
	sort.Slice(versionTags, func(i, j int) bool {
		return versionTags[i].Version.GreaterThan(versionTags[j].Version)
	})

	for _, vt := range versionTags {
		v := vt.Version
		tag := vt.Tag

		// Skip versions not newer than current
		if !v.GreaterThan(current) {
			continue
		}

		// If current is a stable release, skip prerelease candidates.
		// If current is a prerelease, only accept candidates with the same prerelease suffix.
		if current.Prerelease() == "" {
			if v.Prerelease() != "" {
				continue
			}
		} else {
			if v.Prerelease() != current.Prerelease() {
				continue
			}
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
