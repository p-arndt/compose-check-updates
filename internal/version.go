package internal

import (
	"sort"
)

// candidateVersions parses every tag the scheme can read and returns them sorted
// newest first. Tags whose shape cannot stand in for the current one are dropped
// here rather than later, so the callers below only ever see plausible targets.
func candidateVersions(scheme Versioning, tags []string, current Version) []Version {
	var versions []Version

	for _, tag := range tags {
		v, ok := scheme.Parse(tag)
		if !ok {
			continue
		}
		if !sameTagFamily(v.Segments(), current.Segments()) {
			continue
		}
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].GreaterThan(versions[j])
	})

	return versions
}

// sameTagFamily reports whether a candidate tag has a shape that may stand in
// for the current one. A tag naming only a major ("16") floats across its whole
// major line the way "latest" floats across the repository, so replacing a
// pinned "20.11.0" with "21" would quietly trade a fixed reference for a moving
// one — and replacing "16" with "17.2" would do the reverse. Everything else
// mixes freely: an image on "1.2" moving to "1.2.3" is the same release line
// spelled more precisely, and one on "2026.7.7" moving to "2026.7.7.2" is the
// whole point of the loose scheme.
func sameTagFamily(candidate, current int) bool {
	return (candidate == 1) == (current == 1)
}

// isUpgradeCandidate reports whether v is a newer release of the same release
// line as current: a stable current never moves onto a prerelease, and a
// prerelease current only moves within its own suffix, so an image on
// "3.19-alpine" is never handed a plain "3.20".
func isUpgradeCandidate(v, current Version) bool {
	if !v.GreaterThan(current) {
		return false
	}
	return v.Suffix == current.Suffix
}

// FindLatestPerLevel returns the newest tag available at each upgrade level
// relative to currentTag. Any return value is "" when no upgrade exists at that
// level. patchTag stays within the current major.minor; minorTag stays within
// the current major; majorTag crosses to a higher major.
func FindLatestPerLevel(scheme Versioning, currentTag string, tags []string) (patchTag, minorTag, majorTag string) {
	current, ok := scheme.Parse(currentTag)
	if !ok {
		return "", "", ""
	}

	// Sorted newest first, so the first match at a level is that level's best.
	for _, v := range candidateVersions(scheme, tags, current) {
		if !isUpgradeCandidate(v, current) {
			continue
		}

		switch {
		case v.Major() > current.Major():
			if majorTag == "" {
				majorTag = v.Tag
			}
		case v.Minor() > current.Minor():
			if minorTag == "" {
				minorTag = v.Tag
			}
		default:
			// Newer, but neither the major nor the minor moved. That is a patch,
			// including the case a fourth segment alone advanced: "2026.7.7.2" is
			// a rebuild of the release "2026.7.7" names, not a release of its own.
			if patchTag == "" {
				patchTag = v.Tag
			}
		}

		if patchTag != "" && minorTag != "" && majorTag != "" {
			break
		}
	}

	return patchTag, minorTag, majorTag
}

// FindLatestVersion returns the highest tag currentTag may move to under the
// requested levels, or "" when there is none.
func FindLatestVersion(scheme Versioning, currentTag string, tags []string, major, minor, patch bool) string {
	current, ok := scheme.Parse(currentTag)
	if !ok {
		return ""
	}

	if major {
		minor = true
		patch = true
	}
	if minor {
		patch = true
	}

	for _, v := range candidateVersions(scheme, tags, current) {
		// Skips versions not newer than current, and enforces the suffix rule.
		if !isUpgradeCandidate(v, current) {
			continue
		}

		accept := false
		switch {
		case major && v.Major() > current.Major():
			accept = true
		case minor && v.Major() == current.Major() && v.Minor() > current.Minor():
			accept = true
		case patch && v.Major() == current.Major() && v.Minor() == current.Minor():
			// Everything left within the same major.minor is a patch; see the
			// default branch of FindLatestPerLevel.
			accept = true
		}

		if accept {
			return v.Tag
		}
	}

	return ""
}
