package internal

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The image whose tags no scheme can read used to leave the run entirely: a
// warning on stderr and nothing else. It is reported now, with the reason it
// could not be resolved attached to it.
func TestUnreadableImageIsReportedWhenNoTagMatchesTheNewestDigest(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"latest", "sha-e1c83ba"},
		map[string]string{"latest": digestNew, "sha-e1c83ba": digestOld})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":sha-e1c83ba")

	infos, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.True(t, info.IsUnreadable())
	assert.Equal(t, ReasonNoTagForDigest, info.UnreadableReason)
	assert.Equal(t, LevelUnreadable, info.UpdateLevel())
	// The way out is part of the message, not only of the log line.
	assert.Contains(t, info.UnreadableMessage, "versioning: loose")

	// Nothing about it may look like an update to apply.
	assert.False(t, info.HasNewVersion(true, true, true))
	assert.Empty(t, info.LatestTag)
	assert.Empty(t, info.LatestDigest)
	assert.Empty(t, info.AvailableTargets())
}

// A repository with no floating reference tag leaves an unreadable tag with
// nothing at all to compare against.
func TestUnreadableImageIsReportedWhenThereIsNoReferenceTag(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"sha-e1c83ba"},
		map[string]string{"sha-e1c83ba": digestOld})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":sha-e1c83ba")

	infos, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.Equal(t, ReasonNoReferenceTag, infos[0].UnreadableReason)
	assert.Equal(t, LevelUnreadable, infos[0].UpdateLevel())
}

// A tag that parses but shares its suffix with nothing else — every date under
// the loose scheme, "2024-01-01" being release [2024] plus the suffix "-01-01" —
// never reaches the digest fallback, so this is the only place it can be caught.
func TestUnreadableImageIsReportedWhenNoTagIsComparable(t *testing.T) {
	tags := []string{"2024-01-01", "2024-02-01", "2024-03-01"}
	digests := map[string]string{"2024-01-01": digestOld, "2024-02-01": digestNew, "2024-03-01": digestNew}

	server := newRegistryTestServer(t, "library/myimage", tags, digests)
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":2024-01-01")

	infos, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).
		WithVersioning(nil, VersioningLoose, nil).
		Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.Equal(t, ReasonNoComparableTag, infos[0].UnreadableReason)
	assert.Equal(t, LevelUnreadable, infos[0].UpdateLevel())
	// Already on loose, so there is no other scheme worth suggesting.
	assert.NotContains(t, infos[0].UnreadableMessage, "versioning: loose")
}

// The ordinary image must be left exactly as it was: an image sitting on the
// newest release of a readable repository is up to date, not unreadable.
func TestImageOnTheNewestVersionIsNotUnreadable(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"1.0.0", "1.1.0"},
		map[string]string{"1.0.0": digestOld, "1.1.0": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":1.1.0")

	infos, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.False(t, infos[0].IsUnreadable())
	assert.False(t, infos[0].HasNewVersion(true, true, true))
	assert.Empty(t, infos[0].UpdateLevel())
}

// The only tag of a repository is comparable with nothing else, but the image is
// not: the level flags simply left nothing to move to. Guarding the difference
// here, because getting it wrong would report every up-to-date image.
func TestOnlyLevelFlagsNarrowingTheChoiceIsNotUnreadable(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"1.0.0", "2.0.0"},
		map[string]string{"1.0.0": digestOld, "2.0.0": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":1.0.0")

	// Patches only, and the only other release is a major one.
	infos, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).Check(false, false, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.False(t, infos[0].IsUnreadable())
	assert.Empty(t, infos[0].LatestTag)
}

func TestHasComparableTag(t *testing.T) {
	tests := []struct {
		name     string
		scheme   Versioning
		current  string
		tags     []string
		expected bool
	}{
		{
			name:     "another release of the same line",
			scheme:   semverVersioning,
			current:  "1.2.3",
			tags:     []string{"1.2.3", "1.2.4"},
			expected: true,
		},
		{
			name:     "an older release still counts: the image is readable either way",
			scheme:   semverVersioning,
			current:  "1.2.4",
			tags:     []string{"1.2.3", "1.2.4"},
			expected: true,
		},
		{
			name:     "the current tag alone cannot vouch for itself",
			scheme:   semverVersioning,
			current:  "1.2.3",
			tags:     []string{"1.2.3", "latest"},
			expected: false,
		},
		{
			name:     "a suffix nobody else shares",
			scheme:   looseVersioning,
			current:  "2024-01-01",
			tags:     []string{"2024-01-01", "2024-02-01"},
			expected: false,
		},
		{
			name:     "the same suffix on another release",
			scheme:   semverVersioning,
			current:  "3.19-alpine",
			tags:     []string{"3.19-alpine", "3.20-alpine"},
			expected: true,
		},
		{
			name:     "a tag the scheme cannot read at all",
			scheme:   semverVersioning,
			current:  "sha-e1c83ba",
			tags:     []string{"sha-e1c83ba", "1.2.3"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasComparableTag(tt.scheme, tt.current, tt.tags))
		})
	}
}

// An unreadable image has no tag to write, so a caller that hands one to
// Update() is told so rather than having its compose file rewritten to itself.
func TestUpdateRefusesAnUnreadableImage(t *testing.T) {
	file := writeComposeFile(t, "image: library/myimage:sha-e1c83ba")

	info := UpdateInfo{
		FilePath:      file,
		RawLine:       "image: library/myimage:sha-e1c83ba",
		FullImageName: "library/myimage:sha-e1c83ba",
		ImageName:     "library/myimage",
		CurrentTag:    "sha-e1c83ba",
	}
	info.MarkUnreadable(ReasonNoTagForDigest, "nothing to move to")

	assert.Error(t, info.Update())
}

// MarkUnreadable clears whatever was resolved before it: a digest fetched for a
// tag that was then never found is not an update, it is a leftover.
func TestMarkUnreadableClearsTheHalfResolvedTarget(t *testing.T) {
	info := UpdateInfo{
		CurrentTag:   "sha-e1c83ba",
		LatestTag:    "sha-49821e5",
		LatestDigest: digestNew,
		PatchTag:     "1.2.4",
	}
	info.MarkUnreadable(ReasonNoTagForDigest, "nothing to move to")

	assert.Empty(t, info.LatestTag)
	assert.Empty(t, info.LatestDigest)
	assert.Empty(t, info.PatchTag)
	assert.False(t, info.IsDigestUpdate())
	assert.False(t, info.HasNewVersion(true, true, true))
}

// Changing one image's scheme has to be answerable without re-scanning the tree,
// and the answer has to be the one a full scan would have given.
func TestCheckImageResolvesOneImageUnderANewScheme(t *testing.T) {
	tags := []string{"2026.7.7", "2026.7.7.2"}
	server := newRegistryTestServer(t, "library/myimage", tags,
		map[string]string{"2026.7.7": digestOld, "2026.7.7.2": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	reference := image + ":2026.7.7"
	file := writeComposeFile(t, "image: "+reference+"\nimage: "+image+":other\n")

	// Under semver the fourth segment is unreadable, so nothing can be compared
	// with the tag in the file.
	infos, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).Check(true, true, true)
	assert.NoError(t, err)
	assert.Equal(t, ReasonNoComparableTag, infos[0].UnreadableReason)

	// The same image alone, read as the user has just asked for it to be read.
	info, found, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).
		WithVersioning(map[string]string{image: VersioningLoose}, "", nil).
		CheckImage(reference, true, true, true)
	assert.NoError(t, err)
	assert.True(t, found)

	assert.False(t, info.IsUnreadable())
	assert.Equal(t, "2026.7.7.2", info.LatestTag)
	assert.Equal(t, "patch", info.UpdateLevel())
}

// A reference the file no longer names is not an error: the user may have edited
// the file while the session was open.
func TestCheckImageReportsAnImageThatIsGone(t *testing.T) {
	file := writeComposeFile(t, "image: library/myimage:1.0.0")

	_, found, err := NewUpdateChecker(file, nil).CheckImage("library/other:1.0.0", true, true, true)
	assert.NoError(t, err)
	assert.False(t, found)
}
