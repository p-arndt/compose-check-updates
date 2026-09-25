package check

import (
	"slices"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/forge"
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/versioning"
)

// prereleaseMarkers are the words a suffix opens with when it names a
// prerelease rather than a variant. An image suffix is ambiguous in a way a
// forge tag is not: "-rc1" and "-alpine" are both just a suffix to the scheme,
// but only one of them says the release is not final yet.
var prereleaseMarkers = []string{
	"alpha", "a", "beta", "b", "rc", "pre", "preview", "dev",
	"next", "canary", "nightly", "snapshot", "m", "milestone",
}

// NotesFor picks, out of a repository's releases, the ones this update moves
// across: newer than CurrentTag, up to and including the target tag. Newest
// first.
//
// Image tags and release tags rarely spell a version the same way, so the two
// are matched by version, not by name:
//
//   - A "v" or "V" prefix is ignored on either side.
//   - A variant suffix on the image tag ("-alpine", "-bookworm") is dropped:
//     forges release the source, not the flavours an image is built in.
//   - A target shorter than the releases ("0.28", "2") is a floating tag and
//     covers every release in its line, up to but not past the next one.
//   - A current tag shorter than the releases ("0.27") covers its whole line
//     too, and all of it is left out: which patch of it the stack runs is not
//     knowable from the tag, and showing notes for changes it may already have
//     is worse than missing a few.
//   - Prefixed release tags ("app-v1.2.3", "pkg@1.2.3") are dropped rather than
//     read. They mostly come from monorepos releasing several components, and
//     reading them would attribute another component's notes to this image.
//
// Nil when the update moves nowhere or the current tag reads as no version:
// with no lower bound, every release would look new.
func (u *Update) NotesFor(releases []forge.Release) []forge.Release {
	target := u.targetTag()
	if target == u.CurrentTag {
		return nil
	}

	scheme := u.scheme()
	current, ok := imageVersion(scheme, u.CurrentTag)
	if !ok {
		return nil
	}

	var picked []forge.Release
	var versions []versioning.Version
	if to, ok := imageVersion(scheme, target); ok {
		allowPrerelease := to.Suffix != ""
		for _, r := range releases {
			v, ok := releaseVersion(r.Tag)
			if !ok {
				continue
			}
			if (r.Prerelease || v.Suffix != "") && !allowPrerelease {
				continue
			}
			if compareWithin(v, current) <= 0 || compareWithin(v, to) > 0 {
				continue
			}
			picked = append(picked, r)
			versions = append(versions, v)
		}
	}

	if len(picked) == 0 {
		// A target the version match cannot place, but a forge that tags its
		// releases exactly like the image, is still worth one set of notes.
		for _, r := range releases {
			if trimV(r.Tag) == trimV(target) {
				return []forge.Release{r}
			}
		}
		return nil
	}

	// Sorted through indices because the versions were parsed once, alongside
	// the releases they describe; stable so equal versions keep forge order.
	order := make([]int, len(picked))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return versions[b].Compare(versions[a])
	})

	sorted := make([]forge.Release, len(order))
	for i, idx := range order {
		sorted[i] = picked[idx]
	}
	return sorted
}

// imageVersion reads an image tag under the image's own scheme and reduces it
// to what a forge would tag: the numbers, plus the suffix only when that suffix
// is a prerelease. The raw tag is tried first because a regex scheme may expect
// the "v" it was written for.
func imageVersion(scheme versioning.Scheme, tag string) (versioning.Version, bool) {
	v, ok := scheme.Parse(tag)
	if !ok {
		if v, ok = scheme.Parse(trimV(tag)); !ok {
			return versioning.Version{}, false
		}
	}
	if !isPrerelease(v.Suffix) {
		v.Suffix = ""
	}
	return v, true
}

// releaseVersion reads a forge tag. Loose rather than the image's scheme: the
// scheme was chosen to read image tags, and a regex written for
// "1.2.3-alpine" rejects the plain "1.2.3" the forge tags. Build metadata is
// dropped because it does not order a release, and any other suffix is taken
// as a prerelease, as semver has it.
func releaseVersion(tag string) (versioning.Version, bool) {
	loose, _ := versioning.ByName(policy.VersioningLoose, "")
	v, ok := loose.Parse(trimV(tag))
	if !ok {
		return versioning.Version{}, false
	}
	if strings.HasPrefix(v.Suffix, "+") {
		v.Suffix = ""
	}
	return v, true
}

// compareWithin orders a release against a bound read at the bound's own
// precision. A release with more segments than the bound, and equal to it on
// the ones the bound has, falls inside it: "0.28.3" is part of what "0.28"
// names, as a floating target and as a current tag alike.
func compareWithin(release, bound versioning.Version) int {
	n := bound.Segments()
	for i := range n {
		if c := release.Segment(i) - bound.Segment(i); c != 0 {
			return c
		}
	}
	if release.Segments() > n {
		return 0
	}
	// Equal numbers from here on, so only the suffixes are left to order.
	return versioning.Version{Suffix: release.Suffix}.Compare(versioning.Version{Suffix: bound.Suffix})
}

// isPrerelease reports whether a suffix names a prerelease: its first word,
// stripped of a trailing counter, is one of prereleaseMarkers. Only the first
// word counts, so "-alpine3.20" stays a variant and "-rc1-alpine" a prerelease.
func isPrerelease(suffix string) bool {
	// Build metadata, not a prerelease, whatever words follow.
	if strings.HasPrefix(suffix, "+") {
		return false
	}
	word := strings.TrimLeft(suffix, "-._")
	if i := strings.IndexAny(word, "-._+"); i >= 0 {
		word = word[:i]
	}
	word = strings.TrimRight(strings.ToLower(word), "0123456789")
	return word != "" && slices.Contains(prereleaseMarkers, word)
}

func trimV(tag string) string {
	if strings.HasPrefix(tag, "v") || strings.HasPrefix(tag, "V") {
		return tag[1:]
	}
	return tag
}
