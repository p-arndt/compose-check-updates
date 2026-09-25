package check

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/p-arndt/compose-check-updates/internal/forge"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

func releases(tags ...string) []forge.Release {
	out := make([]forge.Release, 0, len(tags))
	for _, tag := range tags {
		out = append(out, forge.Release{Tag: tag, Name: tag})
	}
	return out
}

func tagsOf(rs []forge.Release) []string {
	if rs == nil {
		return nil
	}
	tags := make([]string, 0, len(rs))
	for _, r := range rs {
		tags = append(tags, r.Tag)
	}
	return tags
}

func TestNotesFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		current    string
		latest     string
		versioning policy.Versioning
		pattern    string
		releases   []forge.Release
		want       []string
	}{
		{
			// The case that motivated the version match: a floating image tag
			// against a forge that tags every patch.
			name:     "floating target against full releases",
			current:  "0.27.1",
			latest:   "0.28",
			releases: releases("0.28.0", "0.27.1", "0.27.0", "0.26.0"),
			want:     []string{"0.28.0"},
		},
		{
			name:     "floating target covers its whole line but not the next",
			current:  "0.27.1",
			latest:   "0.28",
			releases: releases("0.29.0", "0.28.9", "0.28.1", "0.28.0", "0.27.2", "0.27.1"),
			want:     []string{"0.28.9", "0.28.1", "0.28.0", "0.27.2"},
		},
		{
			name:     "major-only floating target",
			current:  "1.4.0",
			latest:   "2",
			releases: releases("3.0.0", "2.1.0", "2.0.0", "1.5.0", "1.4.0"),
			want:     []string{"2.1.0", "2.0.0", "1.5.0"},
		},
		{
			name:     "floating current leaves its whole line out",
			current:  "0.27",
			latest:   "0.28.1",
			releases: releases("0.28.2", "0.28.1", "0.28.0", "0.27.3", "0.27.0"),
			want:     []string{"0.28.1", "0.28.0"},
		},
		{
			name:     "v prefix on releases",
			current:  "1.2.3",
			latest:   "1.3.0",
			releases: releases("v1.3.0", "v1.2.4", "v1.2.3"),
			want:     []string{"v1.3.0", "v1.2.4"},
		},
		{
			name:     "v prefix on image, capital V on releases",
			current:  "v1.2.3",
			latest:   "v1.2.4",
			releases: releases("V1.2.4", "V1.2.3"),
			want:     []string{"V1.2.4"},
		},
		{
			name:     "variant suffix on the image is ignored",
			current:  "1.2.3-alpine",
			latest:   "1.3.0-alpine",
			releases: releases("v1.3.0", "v1.2.3"),
			want:     []string{"v1.3.0"},
		},
		{
			name:       "regex scheme still matches plain release tags",
			current:    "1.2.3-bookworm",
			latest:     "1.3.0-bookworm",
			versioning: policy.VersioningRegex,
			pattern:    `^(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)-bookworm$`,
			releases:   releases("1.3.0", "1.2.3"),
			want:       []string{"1.3.0"},
		},
		{
			name:     "major jump spans several releases, newest first",
			current:  "1.9.2",
			latest:   "3.1.0",
			releases: releases("2.0.0", "3.1.0", "1.9.2", "3.0.0", "2.5.1", "1.10.0", "3.2.0"),
			want:     []string{"3.1.0", "3.0.0", "2.5.1", "2.0.0", "1.10.0"},
		},
		{
			name:    "prereleases left out for a final target",
			current: "1.2.3",
			latest:  "1.3.0",
			releases: append(releases("1.3.0", "1.3.0-rc1"),
				forge.Release{Tag: "1.2.9", Prerelease: true}),
			want: []string{"1.3.0"},
		},
		{
			name:     "prereleases kept for a prerelease target",
			current:  "1.2.3",
			latest:   "1.3.0-rc2",
			releases: releases("1.3.0", "1.3.0-rc3", "1.3.0-rc2", "1.3.0-rc1", "1.2.4"),
			want:     []string{"1.3.0-rc2", "1.3.0-rc1", "1.2.4"},
		},
		{
			name:     "prerelease current moving to its final release",
			current:  "1.3.0-rc1",
			latest:   "1.3.0",
			releases: releases("1.3.0", "1.3.0-rc2", "1.3.0-rc1"),
			want:     []string{"1.3.0"},
		},
		{
			name:     "build metadata is not a prerelease",
			current:  "1.0.0",
			latest:   "1.1.0",
			releases: releases("1.1.0+build.7"),
			want:     []string{"1.1.0+build.7"},
		},
		{
			name:     "unparseable and prefixed release tags dropped",
			current:  "1.0.0",
			latest:   "1.1.0",
			releases: releases("nightly", "app-v1.1.0", "1.1.0", "latest"),
			want:     []string{"1.1.0"},
		},
		{
			name:     "equal versions keep forge order",
			current:  "1.0.0",
			latest:   "1.1.0",
			releases: releases("1.1.0", "v1.1.0"),
			want:     []string{"1.1.0", "v1.1.0"},
		},
		{
			name:     "literal fallback for a target no version reads",
			current:  "1.0.0",
			latest:   "stable-2026",
			releases: releases("v2.0.0", "vstable-2026"),
			want:     []string{"vstable-2026"},
		},
		{
			name:     "nothing in range and no literal match",
			current:  "1.0.0",
			latest:   "1.1.0",
			releases: releases("0.9.0", "2.0.0"),
			want:     nil,
		},
		{
			name:     "current tag reads as no version",
			current:  "latest",
			latest:   "1.1.0",
			releases: releases("1.1.0"),
			want:     nil,
		},
		{
			name:     "no update",
			current:  "1.1.0",
			latest:   "1.1.0",
			releases: releases("1.1.0"),
			want:     nil,
		},
		{
			name:     "digest drift has no target of its own",
			current:  "1.1.0",
			releases: releases("1.1.0"),
			want:     nil,
		},
		{
			name:    "no releases",
			current: "1.0.0",
			latest:  "1.1.0",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u := &Update{
				CurrentTag:        tt.current,
				LatestTag:         tt.latest,
				Versioning:        tt.versioning,
				VersioningPattern: tt.pattern,
			}
			assert.Equal(t, tt.want, tagsOf(u.NotesFor(tt.releases)))
		})
	}
}

func TestIsPrerelease(t *testing.T) {
	t.Parallel()

	for suffix, want := range map[string]bool{
		"":             false,
		"-rc1":         true,
		"-rc.1":        true,
		"-beta":        true,
		"-RC2":         true,
		"-rc1-alpine":  true,
		"-alpine":      false,
		"-alpine3.20":  false,
		"-bookworm":    false,
		"+build.5":     false,
		"+rc1":         false,
		"_dev3":        true,
		"-development": false,
	} {
		assert.Equal(t, want, isPrerelease(suffix), suffix)
	}
}
