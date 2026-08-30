package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestFindLatestVersionStruct struct {
	Current string
	Tags    []string
	Major   bool
	Minor   bool
	Patch   bool
	// Versioning names the scheme to read the tags under; empty means semver.
	Versioning string
	Expected   string
}

// hermesTags is the tag list nousresearch/hermes-agent actually publishes, the
// image from issue #13. It is the case worth pinning down: the repository mixes
// three- and four-segment calendar tags, so a scheme that reads only three of
// them cannot tell v2026.7.7 from v2026.7.7.2.
var hermesTags = []string{
	"latest", "main",
	"v2026.8.27", "v2026.8.19", "v2026.8.18", "v2026.8.16.2", "v2026.8.16",
	"v2026.8.13", "v2026.8.3", "v2026.7.30", "v2026.7.20", "v2026.7.7.2",
	"v2026.7.7", "v2026.7.1", "v2026.6.19", "v2026.6.5", "v2026.5.29.2",
	"v2026.5.29", "v2026.5.28", "v2026.5.16", "v2026.5.7", "v2026.4.30",
}

func TestFindLatestVersion(t *testing.T) {
	tests := []struct {
		name     string
		testData TestFindLatestVersionStruct
	}{
		{
			name: "patch update available",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.0.1", "1.0.2", "1.1.0"},
				Major:    false,
				Minor:    false,
				Patch:    true,
				Expected: "1.0.2",
			},
		},
		{
			name: "minor update available",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.0.1", "1.1.0", "1.2.0"},
				Major:    false,
				Minor:    true,
				Patch:    false,
				Expected: "1.2.0",
			},
		},
		{
			name: "minor update available with non-strict tags",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.1", "1.2"},
				Major:    false,
				Minor:    true,
				Patch:    false,
				Expected: "1.2",
			},
		},
		{
			name: "minor update with partial semver (minor only)",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0",
				Tags:     []string{"1.0", "1.1", "1.2", "2.0"},
				Major:    false,
				Minor:    true,
				Patch:    false,
				Expected: "1.2",
			},
		},
		{
			name: "major update with partial semver (minor only)",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0",
				Tags:     []string{"1.0", "1.1", "1.2", "2.0"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "2.0",
			},
		},
		{
			name: "mixed partial and valid semver",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.0", "1.0.0", "1.1", "1.1.0", "1.1.1"},
				Major:    false,
				Minor:    true,
				Patch:    false,
				Expected: "1.1.1",
			},
		},
		{
			name: "current semver with v prefix",
			testData: TestFindLatestVersionStruct{
				Current:  "v1.0.0",
				Tags:     []string{"1.0.1", "1.1.0"},
				Major:    false,
				Minor:    false,
				Patch:    true,
				Expected: "1.0.1",
			},
		},
		{
			name: "current with v prefix and v-prefixed tags",
			testData: TestFindLatestVersionStruct{
				Current:  "v1.0.0",
				Tags:     []string{"v1.0.1", "v1.1.0"},
				Major:    false,
				Minor:    false,
				Patch:    true,
				Expected: "v1.0.1",
			},
		},
		{
			name: "patch update with v-prefixed tags",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"v1.0.1", "v1.1.0"},
				Major:    false,
				Minor:    false,
				Patch:    true,
				Expected: "v1.0.1",
			},
		},
		{
			name: "major update available",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.0.1", "1.1.0", "2.0.0", "3.0.0"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "3.0.0",
			},
		},
		{
			name: "major update available with minor and patch",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.0.1", "1.1.0", "2.0.0", "3.0.0", "3.1.2"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "3.1.2",
			},
		},
		{
			name: "no update available",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"0.9.9", "1.0.0"},
				Major:    true,
				Minor:    true,
				Patch:    true,
				Expected: "",
			},
		},
		{
			name: "prerelease patch update available",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0-beta",
				Tags:     []string{"1.0.1-beta", "1.1.0-beta", "1.2.0"},
				Major:    false,
				Minor:    false,
				Patch:    true,
				Expected: "1.0.1-beta",
			},
		},
		{
			name: "prerelease patch update not available",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0-beta",
				Tags:     []string{"1.0.1-alpha", "1.1.0-beta", "1.1.0"},
				Major:    false,
				Minor:    false,
				Patch:    true,
				Expected: "",
			},
		},
		{
			name: "major update with prerelease",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"2.0.0-alpha", "2.0.0-beta", "2.0.0"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "2.0.0",
			},
		},
		{
			name: "stable current skips prerelease-only candidates",
			testData: TestFindLatestVersionStruct{
				Current:  "2.9.3",
				Tags:     []string{"v3.7.0-ea.1-windowsservercore-ltsc2022", "v2.9.3"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "",
			},
		},
		{
			name: "minor update with prerelease",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.1.0-alpha", "1.1.0-beta", "1.1.0"},
				Major:    false,
				Minor:    true,
				Patch:    false,
				Expected: "1.1.0",
			},
		},
		{
			name: "patch update with prerelease",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.0.1-alpha", "1.0.1-beta", "1.0.1"},
				Major:    false,
				Minor:    false,
				Patch:    true,
				Expected: "1.0.1",
			},
		},
		{
			name: "major-only tag finds the next major-only tag",
			testData: TestFindLatestVersionStruct{
				Current:  "16",
				Tags:     []string{"16", "17", "18"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "18",
			},
		},
		{
			name: "major-only tag with v prefix",
			testData: TestFindLatestVersionStruct{
				Current:  "v16",
				Tags:     []string{"v16", "v17"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "v17",
			},
		},
		{
			name: "major-only tag never moves to a more precise one",
			testData: TestFindLatestVersionStruct{
				Current:  "16",
				Tags:     []string{"16", "17", "17.2", "17.2.1"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "17",
			},
		},
		{
			name: "pinned tag never moves to a major-only one",
			testData: TestFindLatestVersionStruct{
				Current:  "20.11.0",
				Tags:     []string{"20.11.0", "20.11.1", "21"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "20.11.1",
			},
		},
		{
			name: "calendar tag with a fourth segment is unreadable as semver",
			testData: TestFindLatestVersionStruct{
				Current:  "v2026.7.7.2",
				Tags:     hermesTags,
				Major:    true,
				Minor:    true,
				Patch:    true,
				Expected: "",
			},
		},
		{
			name: "loose reads the fourth segment and stays within the month",
			testData: TestFindLatestVersionStruct{
				Current:    "v2026.7.7.2",
				Tags:       hermesTags,
				Patch:      true,
				Versioning: VersioningLoose,
				Expected:   "v2026.7.30",
			},
		},
		{
			name: "loose crosses months when asked for everything",
			testData: TestFindLatestVersionStruct{
				Current:    "v2026.7.7.2",
				Tags:       hermesTags,
				Major:      true,
				Versioning: VersioningLoose,
				Expected:   "v2026.8.27",
			},
		},
		{
			name: "loose moves a three-segment tag onto a four-segment rebuild",
			testData: TestFindLatestVersionStruct{
				Current:    "v2026.5.29",
				Tags:       []string{"v2026.5.28", "v2026.5.29", "v2026.5.29.2"},
				Patch:      true,
				Versioning: VersioningLoose,
				Expected:   "v2026.5.29.2",
			},
		},
		{
			name: "loose does not move backwards within the fourth segment",
			testData: TestFindLatestVersionStruct{
				Current:    "v2026.5.29.2",
				Tags:       []string{"v2026.5.28", "v2026.5.29", "v2026.5.29.2"},
				Major:      true,
				Versioning: VersioningLoose,
				Expected:   "",
			},
		},
		{
			name: "loose keeps the suffix rule",
			testData: TestFindLatestVersionStruct{
				Current:    "2026.7.7.2-cuda",
				Tags:       []string{"2026.7.7.2-cuda", "2026.7.30-cuda", "2026.8.27"},
				Major:      true,
				Versioning: VersioningLoose,
				Expected:   "2026.7.30-cuda",
			},
		},
		{
			name: "loose reads a calendar tag written with leading zeros",
			testData: TestFindLatestVersionStruct{
				Current:    "2026.07.07",
				Tags:       []string{"2026.07.07", "2026.07.30", "2026.08.03"},
				Patch:      true,
				Versioning: VersioningLoose,
				Expected:   "2026.07.30",
			},
		},
		{
			name: "huge version jump",
			testData: TestFindLatestVersionStruct{
				Current:  "1.0.0",
				Tags:     []string{"1.0.1", "1.1.0", "2.0.0", "3.0.0", "4.0.0", "5.0.0", "6.0.0", "7.0.0", "8.0.0", "9.0.0", "10.0.0"},
				Major:    true,
				Minor:    false,
				Patch:    false,
				Expected: "10.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, ok := VersioningByName(tt.testData.Versioning)
			assert.True(t, ok, "unknown versioning scheme")

			result := FindLatestVersion(scheme, tt.testData.Current, tt.testData.Tags, tt.testData.Major, tt.testData.Minor, tt.testData.Patch)
			assert.Equal(t, tt.testData.Expected, result)
		})
	}
}

func TestFindLatestPerLevel(t *testing.T) {
	tests := []struct {
		name                            string
		current                         string
		tags                            []string
		wantPatch, wantMinor, wantMajor string
	}{
		{
			name:      "one candidate per level",
			current:   "v2.9.3",
			tags:      []string{"2.9.4", "2.10.1", "2.11.3", "3.0.0", "3.7.8"},
			wantPatch: "2.9.4", wantMinor: "2.11.3", wantMajor: "3.7.8",
		},
		{
			name:    "no upgrade at all",
			current: "1.0.0",
			tags:    []string{"0.9.9", "1.0.0"},
		},
		{
			name:      "only a patch exists",
			current:   "1.2.3",
			tags:      []string{"1.2.4", "1.2.5"},
			wantPatch: "1.2.5",
		},
		{
			name:      "only a major exists",
			current:   "1.2.3",
			tags:      []string{"2.0.0"},
			wantMajor: "2.0.0",
		},
		{
			name:      "stable current skips prereleases",
			current:   "2.9.3",
			tags:      []string{"2.9.4-rc.1", "2.9.4", "3.0.0-beta", "v3.7.0-ea.1-windowsservercore-ltsc2022"},
			wantPatch: "2.9.4",
		},
		{
			name:      "prerelease current only matches the same suffix",
			current:   "1.0.0-beta",
			tags:      []string{"1.0.1-alpha", "1.0.1-beta", "1.1.0-beta", "2.0.0-alpha", "1.1.0"},
			wantPatch: "1.0.1-beta", wantMinor: "1.1.0-beta",
		},
		{
			name:      "non-strict tags keep their original form",
			current:   "1.0",
			tags:      []string{"1.0", "1.0.1", "1.1", "1.2", "2.0"},
			wantPatch: "1.0.1", wantMinor: "1.2", wantMajor: "2.0",
		},
		{
			name:      "v prefix is preserved",
			current:   "v1.0.0",
			tags:      []string{"v1.0.1", "v1.1.0", "v2.0.0"},
			wantPatch: "v1.0.1", wantMinor: "v1.1.0", wantMajor: "v2.0.0",
		},
		{
			name:      "unparsable tags are ignored",
			current:   "1.0.0",
			tags:      []string{"latest", "stable", "main", "1.0.1"},
			wantPatch: "1.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch, minor, major := FindLatestPerLevel(DefaultVersioning(), tt.current, tt.tags)
			assert.Equal(t, tt.wantPatch, patch, "patch")
			assert.Equal(t, tt.wantMinor, minor, "minor")
			assert.Equal(t, tt.wantMajor, major, "major")
		})
	}
}

func TestSuffixMismatch(t *testing.T) {
	test := TestFindLatestVersionStruct{
		Current:  "1.0.0-beta",
		Tags:     []string{"1.0.1-alpha", "1.1.0-beta", "1.1.0"},
		Major:    false,
		Minor:    false,
		Patch:    true,
		Expected: "",
	}
	result := FindLatestVersion(DefaultVersioning(), test.Current, test.Tags, test.Major, test.Minor, test.Patch)
	assert.Equal(t, test.Expected, result)
}

// TestParseVersionTag covers the shapes Docker tags actually take, and in
// particular that a tag naming only a major parses at all: it used to be
// accepted where the checker decides an image has a version, and rejected where
// the upgrade target is looked for, so an image on "16" reported nothing and
// fell through to no fallback either.
func TestSemverVersioningParse(t *testing.T) {
	tests := []struct {
		tag          string
		wantOK       bool
		wantRelease  []int
		wantSuffix   string
		wantSegments int
	}{
		{"1.2.3", true, []int{1, 2, 3}, "", 3},
		{"v1.2.3", true, []int{1, 2, 3}, "", 3},
		{"1.2", true, []int{1, 2}, "", 2},
		{"16", true, []int{16}, "", 1},
		{"v16", true, []int{16}, "", 1},
		{"0", true, []int{0}, "", 1},
		{"3.19-alpine", true, []int{3, 19}, "-alpine", 2},
		{"1.2.3-rc1", true, []int{1, 2, 3}, "-rc1", 3},
		{"1.2.3+build.5", true, []int{1, 2, 3}, "+build.5", 3},
		// Four segments, a leading zero and a plain word are all out of reach for
		// this scheme; the loose one below is what reads them.
		{"2026.7.7.2", false, nil, "", 0},
		{"2026.07.07", false, nil, "", 0},
		{"latest", false, nil, "", 0},
		{"", false, nil, "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			v, ok := DefaultVersioning().Parse(tt.tag)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			assert.Equal(t, tt.wantRelease, v.Release)
			assert.Equal(t, tt.wantSuffix, v.Suffix)
			assert.Equal(t, tt.wantSegments, v.Segments())
			assert.Equal(t, tt.tag, v.Tag)
		})
	}
}

// TestSameTagFamily states the one shape rule: a tag naming only a major floats
// across its major line, so it neither replaces nor is replaced by a tag that
// pins more than that.
func TestSameTagFamily(t *testing.T) {
	assert.True(t, sameTagFamily(1, 1))
	assert.True(t, sameTagFamily(2, 3))
	assert.True(t, sameTagFamily(3, 2))
	assert.False(t, sameTagFamily(1, 3))
	assert.False(t, sameTagFamily(3, 1))
}

func TestFindLatestPerLevelLoose(t *testing.T) {
	loose, ok := VersioningByName(VersioningLoose)
	assert.True(t, ok)

	patch, minor, major := FindLatestPerLevel(loose, "v2026.7.7.2", hermesTags)
	assert.Equal(t, "v2026.7.30", patch, "patch")
	assert.Equal(t, "v2026.8.27", minor, "minor")
	assert.Equal(t, "", major, "major")
}
