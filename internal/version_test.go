package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestFindLatestVersionStruct struct {
	Current  string
	Tags     []string
	Major    bool
	Minor    bool
	Patch    bool
	Expected string
}

func TestFindLatestVersion(t *testing.T) {
	tests := []struct {
		name     string
		testData struct {
			Current  string
			Tags     []string
			Major    bool
			Minor    bool
			Patch    bool
			Expected string
		}
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
			result := FindLatestVersion(tt.testData.Current, tt.testData.Tags, tt.testData.Major, tt.testData.Minor, tt.testData.Patch)
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
			patch, minor, major := FindLatestPerLevel(tt.current, tt.tags)
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
	result := FindLatestVersion(test.Current, test.Tags, test.Major, test.Minor, test.Patch)
	assert.Equal(t, test.Expected, result)
}

// TestParseVersionTag covers the shapes Docker tags actually take, and in
// particular that a tag naming only a major parses at all: it used to be
// accepted where the checker decides an image has a version, and rejected where
// the upgrade target is looked for, so an image on "16" reported nothing and
// fell through to no fallback either.
func TestParseVersionTag(t *testing.T) {
	tests := []struct {
		tag          string
		wantOK       bool
		wantVersion  string
		wantSegments int
	}{
		{"1.2.3", true, "1.2.3", 3},
		{"v1.2.3", true, "1.2.3", 3},
		{"1.2", true, "1.2.0", 2},
		{"16", true, "16.0.0", 1},
		{"v16", true, "16.0.0", 1},
		{"0", true, "0.0.0", 1},
		{"3.19-alpine", true, "3.19.0-alpine", 2},
		{"1.2.3-rc1", true, "1.2.3-rc1", 3},
		{"2026.7.7.2", false, "", 0},
		{"latest", false, "", 0},
		{"", false, "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			vt, ok := parseVersionTag(tt.tag)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			assert.Equal(t, tt.wantVersion, vt.Version.String())
			assert.Equal(t, tt.wantSegments, vt.Segments)
			assert.Equal(t, tt.tag, vt.Tag)
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
