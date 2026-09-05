package versioning

import (
	"sort"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// candidates parses every tag the scheme can read, drops the ones whose shape
// cannot stand in for the current tag, and returns the rest newest first.
func candidates(scheme Scheme, tags []string, current Version) []Version {
	var versions []Version

	for _, tag := range tags {
		v, ok := scheme.Parse(tag)
		if !ok || !SameFamily(v.Segments(), current.Segments()) {
			continue
		}
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool { return versions[i].GreaterThan(versions[j]) })

	return versions
}

// SameFamily reports whether a candidate with that many segments may stand in
// for a current one. A tag naming only a major ("16") floats across its whole
// major line, so replacing a pinned "20.11.0" with "21" would trade a fixed
// reference for a moving one — and "16" for "17.2" would do the reverse.
func SameFamily(candidate, current int) bool {
	return (candidate == 1) == (current == 1)
}

// isUpgrade keeps a stable release off prereleases and a prerelease within its
// own suffix, so an image on "3.19-alpine" is never handed a plain "3.20".
func isUpgrade(v, current Version) bool {
	return v.GreaterThan(current) && v.Suffix == current.Suffix
}

// Eligible reports whether a candidate tag may be offered at all. It is how a
// rule the version alone cannot express — a minimum age, which only a registry
// can answer — reaches the tag selection without versioning having to know
// where the answer comes from. A nil Eligible accepts every tag.
type Eligible func(tag string) bool

// allows applies an Eligible, treating a nil one as "no rule".
func (e Eligible) allows(tag string) bool { return e == nil || e(tag) }

// LatestPerLevel returns the newest tag available at each upgrade level relative
// to currentTag, or "" where none exists.
func LatestPerLevel(scheme Scheme, currentTag string, tags []string) (patchTag, minorTag, majorTag string) {
	return LatestPerLevelFunc(scheme, currentTag, tags, nil)
}

// LatestPerLevelFunc is LatestPerLevel with a rule the candidates have to pass.
// A rejected candidate is skipped rather than ending the walk, so a level falls
// back to the newest tag that does pass.
func LatestPerLevelFunc(scheme Scheme, currentTag string, tags []string, eligible Eligible) (patchTag, minorTag, majorTag string) {
	current, ok := scheme.Parse(currentTag)
	if !ok {
		return "", "", ""
	}

	best := map[policy.Level]string{}
	for _, v := range candidates(scheme, tags, current) {
		if !isUpgrade(v, current) {
			continue
		}
		if level := Diff(current, v); best[level] != "" || !eligible.allows(v.Tag) {
			// Asked only for a level still open, so a rule that costs something to
			// evaluate is never spent on a candidate that is already outranked.
			continue
		}
		best[Diff(current, v)] = v.Tag
		if len(best) == 3 {
			break
		}
	}

	return best[policy.LevelPatch], best[policy.LevelMinor], best[policy.LevelMajor]
}

// Latest returns the highest tag currentTag may move to under the requested
// levels, or "" when there is none.
func Latest(scheme Scheme, currentTag string, tags []string, major, minor, patch bool) string {
	return LatestFunc(scheme, currentTag, tags, major, minor, patch, nil)
}

// LatestFunc is Latest with a rule the candidate has to pass; a rejected one is
// skipped, so the result is the newest tag that does pass.
func LatestFunc(scheme Scheme, currentTag string, tags []string, major, minor, patch bool, eligible Eligible) string {
	current, ok := scheme.Parse(currentTag)
	if !ok {
		return ""
	}

	// Asking for a level implies accepting the smaller ones below it.
	if major {
		minor = true
	}
	if minor {
		patch = true
	}
	wanted := map[policy.Level]bool{
		policy.LevelMajor: major,
		policy.LevelMinor: minor,
		policy.LevelPatch: patch,
	}

	for _, v := range candidates(scheme, tags, current) {
		if isUpgrade(v, current) && wanted[Diff(current, v)] && eligible.allows(v.Tag) {
			return v.Tag
		}
	}

	return ""
}

// HasComparableTag reports whether the repository publishes any tag other than
// the current one that could ever stand in for it. It answers what Latest
// cannot: an empty result there means either "already newest" or "nothing here
// is comparable at all", and only the second is worth reporting.
func HasComparableTag(scheme Scheme, currentTag string, tags []string) bool {
	current, ok := scheme.Parse(currentTag)
	if !ok {
		return false
	}

	for _, tag := range tags {
		if tag == currentTag {
			continue
		}
		v, ok := scheme.Parse(tag)
		if !ok {
			continue
		}
		if SameFamily(v.Segments(), current.Segments()) && v.Suffix == current.Suffix {
			return true
		}
	}

	return false
}
