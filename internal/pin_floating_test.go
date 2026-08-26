package internal

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A bare floating tag has nothing to compare against, so the only thing to do
// for it is to write down what it resolves to right now.
func TestPinFloatingTagWritesTheDigestItResolvesTo(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":latest")

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host)).WithPinFloating(true)
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.True(t, info.PinsFloating)
	assert.Equal(t, LevelPin, info.UpdateLevel())
	assert.True(t, info.HasNewVersion(true, true, true))
	// The tag is what it was; only the digest is news.
	assert.Equal(t, "latest", info.CurrentTag)
	assert.Equal(t, "latest", info.LatestTag)
	assert.Empty(t, info.CurrentDigest)
	assert.Equal(t, digestNew, info.LatestDigest)
	// Pinning offers no choice of level, so nothing may claim there is one.
	assert.Empty(t, info.AvailableTargets())

	assertUpdateWrites(t, info, file, "image: "+image+":latest@"+digestNew)
}

// Every mutable tag, not only "latest": the point is that the tag floats.
func TestPinFloatingCoversEveryMutableTag(t *testing.T) {
	for tag := range mutableTags {
		t.Run(tag, func(t *testing.T) {
			server := newRegistryTestServer(t, "library/myimage",
				[]string{tag},
				map[string]string{tag: digestNew})
			defer server.Close()

			serverURL, _ := url.Parse(server.URL)
			image := serverURL.Host + "/library/myimage"
			file := writeComposeFile(t, "image: "+image+":"+tag)

			checker := NewUpdateChecker(file, NewRegistry(serverURL.Host)).WithPinFloating(true)
			infos, err := checker.Check(true, true, true)
			assert.NoError(t, err)
			assert.Len(t, infos, 1)
			assert.Equal(t, LevelPin, infos[0].UpdateLevel())
			assert.Equal(t, digestNew, infos[0].LatestDigest)
		})
	}
}

// Off by default, and off means exactly what it meant before the feature
// existed: the floating tag is skipped without a single request spent on it.
func TestPinFloatingOffLeavesFloatingTagsAlone(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":latest")

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host))
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.False(t, infos[0].PinsFloating)
	assert.Empty(t, infos[0].LatestDigest)
	assert.False(t, infos[0].HasNewVersion(true, true, true))
	assert.Empty(t, infos[0].UpdateLevel())
}

// The whole reason for pinning: once the digest is in the file, the run after it
// can tell that the floating tag has moved. That path is the pre-existing
// digest-pinned one, so this is the seam between the two.
func TestPinnedFloatingTagThenDetectsDrift(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":latest@"+digestOld)

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host)).WithPinFloating(true)
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	// A reference that already carries a digest is no longer being pinned, it is
	// being updated — hence "digest" rather than LevelPin.
	assert.False(t, info.PinsFloating)
	assert.Equal(t, "digest", info.UpdateLevel())
	assert.Equal(t, digestOld, info.CurrentDigest)
	assert.Equal(t, digestNew, info.LatestDigest)

	// The tag stays floating; only the digest under it moves.
	assertUpdateWrites(t, info, file, "image: "+image+":latest@"+digestNew)
}

// The digest is appended to the whole reference, not to the first thing that
// happens to read "latest" — a repository named after the tag would otherwise be
// rewritten into nonsense.
func TestPinFloatingAppendsToTheReferenceNotTheFirstMatch(t *testing.T) {
	server := newRegistryTestServer(t, "library/latest-app",
		[]string{"latest"},
		map[string]string{"latest": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/latest-app"
	file := writeComposeFile(t, "image: "+image+":latest")

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host)).WithPinFloating(true)
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assertUpdateWrites(t, infos[0], file, "image: "+image+":latest@"+digestNew)
}

// A registry that cannot answer for the floating tag leaves the image alone
// rather than half-pinned.
func TestPinFloatingSkipsWhenTheTagCannotBeResolved(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage", []string{"latest"}, map[string]string{})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":latest")

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host)).WithPinFloating(true)
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.False(t, infos[0].PinsFloating)
	assert.Empty(t, infos[0].LatestDigest)
	assert.False(t, infos[0].HasNewVersion(true, true, true))
}

// A cap says how far a version may move. Pinning moves no version, so no cap has
// anything to say about it — otherwise an image capped at "patch" could never be
// pinned at all.
func TestPinFloatingIsNotBoundByACap(t *testing.T) {
	info := UpdateInfo{
		FullImageName: "nginx:latest",
		ImageName:     "library/nginx",
		CurrentTag:    "latest",
		LatestTag:     "latest",
		LatestDigest:  digestNew,
		PinsFloating:  true,
		Cap:           "patch",
	}

	assert.Equal(t, LevelPin, info.UpdateLevel())
	assert.True(t, info.AllowsLevel(LevelPin))
	assert.True(t, info.HasNewVersion(false, false, true))
}
