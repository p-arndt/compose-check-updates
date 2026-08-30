package check

import (
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A repository whose moving tag is "release": nothing in the built-in set names
// it, so without the setting there is nothing to pin.
func TestFloatingTagsPinsATagCcuDoesNotKnow(t *testing.T) {
	server := registrytest.Server(t, "internal/thing",
		[]string{"release", "latest"},
		map[string]string{"release": registrytest.DigestNew, "latest": registrytest.DigestOld})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/internal/thing"
	file := writeComposeFile(t, "image: "+image+":release")

	checker := New(file, registry.New(serverURL.Host), policy.Set{
		PinFloating: true,
		Images:      map[string]policy.Image{image: {FloatingTags: []string{"release", "canary"}}},
	})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.True(t, info.PinsFloating)
	assert.Equal(t, policy.LevelPin, info.Level())
	assert.Equal(t, "release", info.LatestTag)
	assert.Equal(t, registrytest.DigestNew, info.LatestDigest)

	assertUpdateWrites(t, info, file, "image: "+image+":release@"+registrytest.DigestNew)
}

// The same image without the setting: "release" is an ordinary tag, so it is
// compared against "latest" like any other unreadable tag rather than pinned.
func TestWithoutFloatingTagsAnUnknownMovingTagIsNotPinned(t *testing.T) {
	server := registrytest.Server(t, "internal/thing",
		[]string{"release", "latest"},
		map[string]string{"release": registrytest.DigestNew, "latest": registrytest.DigestOld})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/internal/thing"
	file := writeComposeFile(t, "image: "+image+":release")

	checker := New(file, registry.New(serverURL.Host), policy.Set{PinFloating: true})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)
	assert.False(t, infos[0].PinsFloating)
}

// CheckPins is the TUI's way in, and it decides what to pin by the same rule.
func TestCheckPinsHonoursTheExtraFloatingTags(t *testing.T) {
	server := registrytest.Server(t, "internal/thing",
		[]string{"release"},
		map[string]string{"release": registrytest.DigestNew})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/internal/thing"
	file := writeComposeFile(t, "image: "+image+":release")

	pins, err := New(file, registry.New(serverURL.Host), policy.Set{
		Images: map[string]policy.Image{image: {FloatingTags: []string{"release"}}},
	}).CheckPins()
	assert.NoError(t, err)
	assert.Len(t, pins, 1)
	assert.Equal(t, registrytest.DigestNew, pins[0].LatestDigest)

	// And nothing to pin for the same file without the setting.
	none, err := New(file, registry.New(serverURL.Host), policy.Set{}).CheckPins()
	assert.NoError(t, err)
	assert.Empty(t, none)
}

func TestDigestCandidatesDropsTheExtraFloatingTags(t *testing.T) {
	// Date tags, so the floating "release" shares their family (none) and is only
	// dropped because it was named floating.
	tags := []string{"latest", "release", "20260830", "20260101"}

	candidates, dropped := digestCandidates(tags, "20260101", policy.Image{ReferenceTag: policy.DefaultReferenceTag, FloatingTags: []string{"release"}})

	assert.Equal(t, []string{"20260830"}, candidates)
	assert.Zero(t, dropped)
}
