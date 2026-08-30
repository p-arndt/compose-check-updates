package internal

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func loose(t *testing.T) Versioning {
	t.Helper()
	scheme, ok := VersioningByName(VersioningLoose)
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
		scheme, ok := VersioningByName(name)
		assert.True(t, ok, name)
		assert.Equal(t, VersioningSemver, scheme.Name())
	}

	scheme, ok := VersioningByName(VersioningLoose)
	assert.True(t, ok)
	assert.Equal(t, VersioningLoose, scheme.Name())

	_, ok = VersioningByName("lose")
	assert.False(t, ok)
}
