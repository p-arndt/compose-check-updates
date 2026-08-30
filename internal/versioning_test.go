package internal

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func loose(t *testing.T) Versioning {
	t.Helper()
	scheme, ok := VersioningByName(VersioningLoose, "")
	assert.True(t, ok)
	return scheme
}

func TestLooseVersioningParse(t *testing.T) {
	tests := []struct {
		tag         string
		wantOK      bool
		wantRelease []int
		wantSuffix  string
	}{
		{"2026.7.7.2", true, []int{2026, 7, 7, 2}, ""},
		{"v2026.7.7.2", true, []int{2026, 7, 7, 2}, ""},
		{"2026.07.07", true, []int{2026, 7, 7}, ""},
		{"1.2.3.4.5.6", true, []int{1, 2, 3, 4, 5, 6}, ""},
		{"16", true, []int{16}, ""},
		{"2026.7.7.2-cuda", true, []int{2026, 7, 7, 2}, "-cuda"},
		{"1.2.3_alpha", true, []int{1, 2, 3}, "_alpha"},
		// Past the six-segment cap, so not a version this scheme will order.
		{"1.2.3.4.5.6.7", false, nil, ""},
		{"latest", false, nil, ""},
		{"sha-abc123", false, nil, ""},
		{"", false, nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			v, ok := loose(t).Parse(tt.tag)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			assert.Equal(t, tt.wantRelease, v.Release)
			assert.Equal(t, tt.wantSuffix, v.Suffix)
			assert.Equal(t, tt.tag, v.Tag)
		})
	}
}

// TestLooseVersioningOrder is the heart of issue #13: the fourth segment has to
// order after the release it extends and before the next one. Treating it as
// semver build metadata would have made all three of these compare equal.
func TestLooseVersioningOrder(t *testing.T) {
	scheme := loose(t)

	ordered := []string{
		"v2026.7.7", "v2026.7.7.1", "v2026.7.7.2", "v2026.7.30",
		"v2026.8.16", "v2026.8.16.2", "v2026.8.27",
	}

	var versions []Version
	for _, tag := range ordered {
		v, ok := scheme.Parse(tag)
		assert.True(t, ok, tag)
		versions = append(versions, v)
	}

	for i := 1; i < len(versions); i++ {
		assert.True(t, versions[i].GreaterThan(versions[i-1]),
			"%s should order after %s", ordered[i], ordered[i-1])
	}

	// And sorting a shuffled list puts them back in exactly that order.
	shuffled := []Version{versions[3], versions[0], versions[6], versions[2], versions[5], versions[1], versions[4]}
	sort.Slice(shuffled, func(i, j int) bool { return shuffled[i].Compare(shuffled[j]) < 0 })
	for i, v := range shuffled {
		assert.Equal(t, ordered[i], v.Tag)
	}
}

func TestVersionCompare(t *testing.T) {
	scheme := loose(t)

	cmp := func(a, b string) int {
		va, ok := scheme.Parse(a)
		assert.True(t, ok, a)
		vb, ok := scheme.Parse(b)
		assert.True(t, ok, b)
		return va.Compare(vb)
	}

	// A tag naming fewer segments names the .0 release, so these are equal.
	assert.Equal(t, 0, cmp("2.1", "2.1.0"))
	assert.Equal(t, 0, cmp("1.2.3", "v1.2.3"))
	assert.Equal(t, 0, cmp("2026.7.7", "2026.07.07"))

	// A plain release outranks a suffixed one of the same number.
	assert.Equal(t, 1, cmp("1.0.0", "1.0.0-rc1"))
	assert.Equal(t, -1, cmp("1.0.0-rc1", "1.0.0"))

	assert.Equal(t, 1, cmp("1.0.1", "1.0.0"))
	assert.Equal(t, -1, cmp("2026.7.7", "2026.7.7.1"))
}

func TestVersioningByName(t *testing.T) {
	// Empty is the default rather than an error: it is what an image that was
	// never given a scheme has.
	for _, name := range []string{"", VersioningSemver} {
		scheme, ok := VersioningByName(name, "")
		assert.True(t, ok, name)
		assert.Equal(t, VersioningSemver, scheme.Name())
	}

	scheme, ok := VersioningByName(VersioningLoose, "")
	assert.True(t, ok)
	assert.Equal(t, VersioningLoose, scheme.Name())

	_, ok = VersioningByName("lose", "")
	assert.False(t, ok)

	scheme, ok = VersioningByName(VersioningRegex, calendarPattern)
	assert.True(t, ok)
	assert.Equal(t, VersioningRegex, scheme.Name())

	// The config layer rejects both of these on load, so reaching here with one
	// means the two sides have drifted; the checker warns and falls back.
	_, ok = VersioningByName(VersioningRegex, "")
	assert.False(t, ok, "regex without a pattern reads nothing")
	_, ok = VersioningByName(VersioningRegex, `^(?P<major>\d+$`)
	assert.False(t, ok, "a pattern that does not compile")
	_, ok = VersioningByName(VersioningRegex, `^\d+$`)
	assert.False(t, ok, "a pattern naming no group")
}

// calendarPattern is the case the regex scheme exists for: a dashed date, which
// loose reads as release 2024 with "-01-01" as a suffix and so orders by the
// alphabet rather than by the calendar.
const calendarPattern = `^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})(?P<suffix>-.+)?$`

func regexScheme(t *testing.T, pattern string) Versioning {
	t.Helper()
	scheme, ok := VersioningByName(VersioningRegex, pattern)
	assert.True(t, ok, pattern)
	return scheme
}

func TestRegexVersioningParse(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		tag         string
		wantOK      bool
		wantRelease []int
		wantSuffix  string
	}{
		{"dashed date", calendarPattern, "2024-01-01", true, []int{2024, 1, 1}, ""},
		{"dashed date with suffix", calendarPattern, "2024-01-01-alpine", true, []int{2024, 1, 1}, "-alpine"},
		// Not a tag this pattern describes, so not a version at all: Parse says so
		// and the checker falls back to comparing digests for it.
		{"dotted date", calendarPattern, "2024.01.01", false, nil, ""},
		{"commit tag", calendarPattern, "sha-e1c83ba", false, nil, ""},
		{"floating tag", calendarPattern, "latest", false, nil, ""},
		{"empty tag", calendarPattern, "", false, nil, ""},
		// The pattern is anchored, so a date buried in something else is not one.
		{"date inside a longer tag", calendarPattern, "build-2024-01-01-x", false, nil, ""},
		// An anchored pattern stays anchored rather than being anchored twice.
		{"pattern anchored by the user", `^v(?P<major>\d+)$`, "v7", true, []int{7}, ""},
		// A group the pattern does not name contributes 0.
		{"major only", `(?P<major>\d+)`, "16", true, []int{16}, ""},
		{"major and patch", `(?P<major>\d+)_(?P<patch>\d+)`, "16_4", true, []int{16, 0, 4}, ""},
		// The fourth segment orders the version but names no level of its own.
		{"build segment", `(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)\.(?P<build>\d+)`, "1.2.3.4", true, []int{1, 2, 3, 4}, ""},
		// An optional group that did not take part names the .0 release.
		{"optional patch left out", `(?P<major>\d+)\.(?P<minor>\d+)(?:\.(?P<patch>\d+))?`, "1.2", true, []int{1, 2, 0}, ""},
		// A group ccu has no use for is ignored rather than rejected: patterns are
		// easier to read with a name on every group.
		{"unknown group name", `(?P<year>\d{4})-(?P<major>\d+)`, "2024-7", true, []int{7}, ""},
		// The pattern let through something that is no number ccu can order.
		{"non-numeric segment", `(?P<major>\w+)`, "alpine", false, nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := regexScheme(t, tt.pattern).Parse(tt.tag)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			assert.Equal(t, tt.wantRelease, v.Release)
			assert.Equal(t, tt.wantSuffix, v.Suffix)
			assert.Equal(t, tt.tag, v.Tag)
		})
	}
}

// TestRegexVersioningOrder is what the scheme is for: dashed calendar tags
// ordering by date. Under loose these compare as release 2024 with the rest as a
// suffix, which puts "2024-12-31" before "2024-02-01" — the alphabet, not the
// calendar.
func TestRegexVersioningOrder(t *testing.T) {
	scheme := regexScheme(t, calendarPattern)

	ordered := []string{"2024-01-01", "2024-02-01", "2024-02-28", "2024-12-31", "2025-01-01"}

	var versions []Version
	for _, tag := range ordered {
		v, ok := scheme.Parse(tag)
		assert.True(t, ok, tag)
		versions = append(versions, v)
	}

	for i := 1; i < len(versions); i++ {
		assert.True(t, versions[i].GreaterThan(versions[i-1]),
			"%s should order after %s", ordered[i], ordered[i-1])
	}

	shuffled := []Version{versions[3], versions[0], versions[4], versions[2], versions[1]}
	sort.Slice(shuffled, func(i, j int) bool { return shuffled[i].Compare(shuffled[j]) < 0 })
	for i, v := range shuffled {
		assert.Equal(t, ordered[i], v.Tag)
	}

	// And the levels the ordering is reported at: a new year is a major, a new
	// month a minor, a new day a patch.
	patch, minor, major := FindLatestPerLevel(scheme, "2024-02-01", ordered)
	assert.Equal(t, "2024-02-28", patch, "patch")
	assert.Equal(t, "2024-12-31", minor, "minor")
	assert.Equal(t, "2025-01-01", major, "major")

	// A tag the pattern cannot read is no candidate, exactly as under loose.
	assert.Equal(t, "", FindLatestVersion(scheme, "sha-e1c83ba", ordered, true, true, true))
}
