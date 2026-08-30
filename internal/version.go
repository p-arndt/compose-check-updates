package internal

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// versionTag pairs a parsed version with the tag it was parsed from, so the
// tag can be returned in its original form (e.g. keeping a "v" prefix), and
// with the number of numeric segments the tag actually named. The count is kept
// because the parsed version cannot tell "16" from "16.0.0" afterwards, and the
// two are not interchangeable — see sameTagFamily.
type versionTag struct {
	Version  *semver.Version
	Tag      string
	Segments int
}

// candidateVersions parses every tag that looks like a version and returns them
// sorted newest first. currentSegments is how many numeric segments the tag
// being upgraded named; tags of an incompatible shape are dropped here rather
// than later, so the callers below only ever see plausible targets.
func candidateVersions(tags []string, currentSegments int) []versionTag {
	var versionTags []versionTag

	for _, tag := range tags {
		vt, ok := parseVersionTag(tag)
		if !ok {
			continue
		}
		if !sameTagFamily(vt.Segments, currentSegments) {
			continue
		}
		versionTags = append(versionTags, vt)
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
func FindLatestPerLevel(currentTag string, tags []string) (patchTag, minorTag, majorTag string) {
	cur, ok := parseVersionTag(currentTag)
	if !ok {
		return "", "", ""
	}
	current := cur.Version

	// Sorted newest first, so the first match at a level is that level's best.
	for _, vt := range candidateVersions(tags, cur.Segments) {
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

func FindLatestVersion(currentTag string, tags []string, major, minor, patch bool) string {
	cur, ok := parseVersionTag(currentTag)
	if !ok {
		return ""
	}
	current := cur.Version

	if major {
		minor = true
		patch = true
	}
	if minor {
		patch = true
	}

	versionTags := candidateVersions(tags, cur.Segments)
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

// versionPattern captures the numeric segments a tag names, plus any
// prerelease or build metadata trailing them. The minor and patch segments are
// optional: Docker tags routinely name only part of a version ("16", "1.2"),
// and a tool that refuses to read them has nothing to say about some of the
// most commonly pinned images there are.
var versionPattern = regexp.MustCompile(`^(?P<major>0|[1-9]\d*)(?:\.(?P<minor>0|[1-9]\d*))?(?:\.(?P<patch>0|[1-9]\d*))?(?P<rest>(?:-[0-9A-Za-z-.]+)?(?:\+[0-9A-Za-z-.]+)?)$`)

// normalizeSemver accepts strict semantic versions as well as the "non-strict"
// forms Docker tags take: "1.1" and "16" stand for "1.1.0" and "16.0.0". It
// also reports how many segments were actually written, which the caller needs
// because the normalized string no longer says. If the tag is not a supported
// version format it returns ok=false.
func normalizeSemver(tag string) (normalized string, segments int, ok bool) {
	// Accept an optional leading "v" (e.g. "v1.2.3") as well as plain semver.
	tag = strings.TrimPrefix(tag, "v")

	matches := versionPattern.FindStringSubmatch(tag)
	if len(matches) == 0 {
		return "", 0, false
	}

	major, minor, patch, rest := matches[1], matches[2], matches[3], matches[4]

	segments = 1
	if minor != "" {
		segments++
	}
	if patch != "" {
		segments++
	}

	if minor == "" {
		minor = "0"
	}
	if patch == "" {
		patch = "0"
	}

	return major + "." + minor + "." + patch + rest, segments, true
}

// parseVersionTag turns a tag into a comparable version. It is the one place
// tags are parsed: the checker deciding whether an image has a version at all
// and the search for what to upgrade it to have to agree on the answer, or an
// image passes the first and then silently matches nothing in the second.
func parseVersionTag(tag string) (versionTag, bool) {
	normalized, segments, ok := normalizeSemver(tag)
	if !ok {
		return versionTag{}, false
	}

	v, err := semver.NewVersion(normalized)
	if err != nil {
		return versionTag{}, false
	}

	return versionTag{Version: v, Tag: tag, Segments: segments}, true
}

// sameTagFamily reports whether a candidate tag has a shape that may stand in
// for the current one. A tag naming only a major ("16") floats across its whole
// major line the way "latest" floats across the repository, so replacing a
// pinned "20.11.0" with "21" would quietly trade a fixed reference for a moving
// one — and replacing "16" with "17.2" would do the reverse. Everything else
// mixes freely: an image on "1.2" moving to "1.2.3" is the same release line
// spelled more precisely.
func sameTagFamily(candidate, current int) bool {
	return (candidate == 1) == (current == 1)
}

func isEqualMajor(current, tag *semver.Version) bool {
	return current.Major() == tag.Major()
}

func isEqualMinor(current, tag *semver.Version) bool {
	return current.Minor() == tag.Minor()
}
