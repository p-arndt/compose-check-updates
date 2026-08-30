package check

import (
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A bare floating tag has nothing to compare against, so the only thing to do
// for it is to write down what it resolves to right now.
func TestPinFloatingTagWritesTheDigestItResolvesTo(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": registrytest.DigestNew})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":latest")

	checker := New(file, registry.New(serverURL.Host), policy.Set{PinFloating: true})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.True(t, info.PinsFloating)
	assert.Equal(t, policy.LevelPin, info.Level())
	assert.True(t, info.HasNewVersion())
	// The tag is what it was; only the digest is news.
	assert.Equal(t, "latest", info.CurrentTag)
	assert.Equal(t, "latest", info.LatestTag)
	assert.Empty(t, info.CurrentDigest)
	assert.Equal(t, registrytest.DigestNew, info.LatestDigest)
	// Pinning offers no choice of level, so nothing may claim there is one.
	assert.Empty(t, info.AvailableTargets())

	assertUpdateWrites(t, info, file, "image: "+image+":latest@"+registrytest.DigestNew)
}

// Every mutable tag, not only "latest": the point is that the tag floats.
func TestPinFloatingCoversEveryMutableTag(t *testing.T) {
	for _, tag := range policy.BuiltInFloatingTags() {
		t.Run(tag, func(t *testing.T) {
			server := registrytest.Server(t, "library/myimage",
				[]string{tag},
				map[string]string{tag: registrytest.DigestNew})
			defer server.Close()

			serverURL, _ := url.Parse(server.URL)
			image := serverURL.Host + "/library/myimage"
			file := writeComposeFile(t, "image: "+image+":"+tag)

			checker := New(file, registry.New(serverURL.Host), policy.Set{PinFloating: true})
			infos, err := checker.Check(true, true, true)
			assert.NoError(t, err)
			assert.Len(t, infos, 1)
			assert.Equal(t, policy.LevelPin, infos[0].Level())
			assert.Equal(t, registrytest.DigestNew, infos[0].LatestDigest)
		})
	}
}

// Off by default, and off means exactly what it meant before the feature
// existed: the floating tag is skipped without a single request spent on it.
func TestPinFloatingOffLeavesFloatingTagsAlone(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": registrytest.DigestNew})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":latest")

	checker := New(file, registry.New(serverURL.Host), policy.Set{})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.False(t, infos[0].PinsFloating)
	assert.Empty(t, infos[0].LatestDigest)
	assert.False(t, infos[0].HasNewVersion())
	assert.Empty(t, infos[0].Level())
}

// The whole reason for pinning: once the digest is in the file, the run after it
// can tell that the floating tag has moved. That path is the pre-existing
// digest-pinned one, so this is the seam between the two.
func TestPinnedFloatingTagThenDetectsDrift(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": registrytest.DigestNew})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":latest@"+registrytest.DigestOld)

	checker := New(file, registry.New(serverURL.Host), policy.Set{PinFloating: true})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	// A reference that already carries a digest is no longer being pinned, it is
	// being updated — hence "digest" rather than policy.LevelPin.
	assert.False(t, info.PinsFloating)
	assert.Equal(t, policy.Level("digest"), info.Level())
	assert.Equal(t, registrytest.DigestOld, info.CurrentDigest)
	assert.Equal(t, registrytest.DigestNew, info.LatestDigest)

	// The tag stays floating; only the digest under it moves.
	assertUpdateWrites(t, info, file, "image: "+image+":latest@"+registrytest.DigestNew)
}

// The digest is appended to the whole reference, not to the first thing that
// happens to read "latest" — a repository named after the tag would otherwise be
// rewritten into nonsense.
func TestPinFloatingAppendsToTheReferenceNotTheFirstMatch(t *testing.T) {
	server := registrytest.Server(t, "library/latest-app",
		[]string{"latest"},
		map[string]string{"latest": registrytest.DigestNew})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/latest-app"
	file := writeComposeFile(t, "image: "+image+":latest")

	checker := New(file, registry.New(serverURL.Host), policy.Set{PinFloating: true})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assertUpdateWrites(t, infos[0], file, "image: "+image+":latest@"+registrytest.DigestNew)
}

// A registry that cannot answer for the floating tag leaves the image alone
// rather than half-pinned.
func TestPinFloatingSkipsWhenTheTagCannotBeResolved(t *testing.T) {
	server := registrytest.Server(t, "library/myimage", []string{"latest"}, map[string]string{})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":latest")

	checker := New(file, registry.New(serverURL.Host), policy.Set{PinFloating: true})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.False(t, infos[0].PinsFloating)
	assert.Empty(t, infos[0].LatestDigest)
	assert.False(t, infos[0].HasNewVersion())
}

// A cap says how far a version may move. Pinning moves no version, so no cap has
// anything to say about it — otherwise an image capped at "patch" could never be
// pinned at all.
func TestPinFloatingIsNotBoundByACap(t *testing.T) {
	info := Update{
		FullImageName: "nginx:latest",
		ImageName:     "library/nginx",
		CurrentTag:    "latest",
		LatestTag:     "latest",
		LatestDigest:  registrytest.DigestNew,
		PinsFloating:  true,
		Cap:           "patch",
	}

	assert.Equal(t, policy.LevelPin, info.Level())
	assert.True(t, info.Cap.Allows(policy.LevelPin))
	assert.True(t, info.HasNewVersion())
}

// The bug this guards: a substring match on RawLine rewrote every line the
// reference was a prefix of, so `nginx:stable-alpine` next to `nginx:stable`
// became `nginx:stable@sha256:…-alpine` — a reference no registry can resolve.
// Only the line the reference was scanned from may be touched.
func TestPinFloatingLeavesALongerTagOnTheNextLineAlone(t *testing.T) {
	before := "services:\n" +
		"  a:\n" +
		"    image: nginx:stable\n" +
		"  b:\n" +
		"    image: nginx:stable-alpine\n"
	file := writeComposeFile(t, before)

	info := Update{
		FilePath:      file,
		FullImageName: "nginx:stable",
		ImageName:     "library/nginx",
		RawLine:       "    image: nginx:stable",
		CurrentTag:    "stable",
		LatestTag:     "stable",
		LatestDigest:  registrytest.DigestNew,
		digestFor:     "stable",
		PinsFloating:  true,
	}

	assertUpdateWrites(t, info, file, "services:\n"+
		"  a:\n"+
		"    image: nginx:stable@"+registrytest.DigestNew+"\n"+
		"  b:\n"+
		"    image: nginx:stable-alpine\n")
}

// The same for a version bump, which is the far more common shape: `myapp:1.0`
// must not drag `myapp:1.0.1` along with it.
func TestUpdateLeavesALongerTagOnTheNextLineAlone(t *testing.T) {
	file := writeComposeFile(t, "    image: myapp:1.0\n    image: myapp:1.0.1\n")

	info := Update{
		FilePath:      file,
		FullImageName: "myapp:1.0",
		RawLine:       "    image: myapp:1.0",
		CurrentTag:    "1.0",
		LatestTag:     "1.1",
	}

	assertUpdateWrites(t, info, file, "    image: myapp:1.1\n    image: myapp:1.0.1\n")
}

// A CRLF file, and a line the editor left a trailing space on: neither is
// visible in the reference, so neither may stop the line from being rewritten.
// The line endings themselves stay as they were found.
func TestUpdateMatchesThroughInvisibleTrailingCharacters(t *testing.T) {
	file := writeComposeFile(t, "    image: myapp:1.0  \r\n    image: other:2.0\r\n")

	info := Update{
		FilePath:      file,
		FullImageName: "myapp:1.0",
		RawLine:       "    image: myapp:1.0",
		CurrentTag:    "1.0",
		LatestTag:     "1.1",
	}

	assertUpdateWrites(t, info, file, "    image: myapp:1.1  \r\n    image: other:2.0\r\n")
}
