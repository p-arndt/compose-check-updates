package internal

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A repository whose moving tag is "release": nothing in the built-in set names
// it, so without the setting there is nothing to pin.
func TestFloatingTagsPinsATagCcuDoesNotKnow(t *testing.T) {
	server := newRegistryTestServer(t, "internal/thing",
		[]string{"release", "latest"},
		map[string]string{"release": digestNew, "latest": digestOld})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/internal/thing"
	file := writeComposeFile(t, "image: "+image+":release")

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host)).
		WithPinFloating(true).
		WithFloatingTags(map[string][]string{image: {"release", "canary"}}, nil)
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.True(t, info.PinsFloating)
	assert.Equal(t, LevelPin, info.UpdateLevel())
	assert.Equal(t, "release", info.LatestTag)
	assert.Equal(t, digestNew, info.LatestDigest)

	assertUpdateWrites(t, info, file, "image: "+image+":release@"+digestNew)
}

// The same image without the setting: "release" is an ordinary tag, so it is
// compared against "latest" like any other unreadable tag rather than pinned.
func TestWithoutFloatingTagsAnUnknownMovingTagIsNotPinned(t *testing.T) {
	server := newRegistryTestServer(t, "internal/thing",
		[]string{"release", "latest"},
		map[string]string{"release": digestNew, "latest": digestOld})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/internal/thing"
	file := writeComposeFile(t, "image: "+image+":release")

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host)).WithPinFloating(true)
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)
	assert.False(t, infos[0].PinsFloating)
}

// CheckPins is the TUI's way in, and it decides what to pin by the same rule.
func TestCheckPinsHonoursTheExtraFloatingTags(t *testing.T) {
	server := newRegistryTestServer(t, "internal/thing",
		[]string{"release"},
		map[string]string{"release": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/internal/thing"
	file := writeComposeFile(t, "image: "+image+":release")

	pins, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).
		WithFloatingTags(map[string][]string{image: {"release"}}, nil).
		CheckPins()
	assert.NoError(t, err)
	assert.Len(t, pins, 1)
	assert.Equal(t, digestNew, pins[0].LatestDigest)

	// And nothing to pin for the same file without the setting.
	none, err := NewUpdateChecker(file, NewRegistry(serverURL.Host)).CheckPins()
	assert.NoError(t, err)
	assert.Empty(t, none)
}

// The built-in tags stay floating for an image that named its own: a repository
// publishing "release" almost certainly publishes "latest" beside it, and that
// one must not turn back into a pinnable version tag.
func TestFloatingTagsAddToTheBuiltInSet(t *testing.T) {
	assert.True(t, isFloatingTag("latest", []string{"release"}))
	assert.True(t, isFloatingTag("release", []string{"release"}))
	assert.True(t, isFloatingTag("nightly", nil))
	assert.False(t, isFloatingTag("release", nil))
	assert.False(t, isFloatingTag("1.2.3", []string{"release"}))
}

// One image's list does not reach another's.
func TestFloatingTagsLookupIsExact(t *testing.T) {
	checker := (&UpdateChecker{}).WithFloatingTags(map[string][]string{
		"internal/thing": {"release"},
	}, nil)

	assert.Equal(t, []string{"release"}, checker.floatingTagsFor("internal/thing"))
	assert.Empty(t, checker.floatingTagsFor("library/redis"))
	assert.Empty(t, (&UpdateChecker{}).floatingTagsFor("internal/thing"))
}

// An extra floating tag is no more an update target than a built-in one is:
// moving a commit tag onto the tag that floats would trade a fixed reference for
// a moving one.
func TestDigestCandidatesDropsTheExtraFloatingTags(t *testing.T) {
	// Date tags, so the floating "release" shares their family (none) and is only
	// dropped because it was named floating.
	tags := []string{"latest", "release", "20260830", "20260101"}

	candidates, dropped := digestCandidates(tags, "20260101", defaultReferenceTag, []string{"release"})

	assert.Equal(t, []string{"20260830"}, candidates)
	assert.Zero(t, dropped)
}

// TestFloatingTagsForCombines covers how the two sources add up. Neither
// replaces the other, and neither replaces the built-in set: a registry that
// spells its moving tag "release" across every repository is a global fact, and
// a single repository adding "canary" on top must not lose it again.
func TestFloatingTagsForCombines(t *testing.T) {
	tests := []struct {
		name     string
		perImage map[string][]string
		global   []string
		image    string
		want     []string
	}{
		{
			name:  "neither, so only the built-in set applies",
			image: "redis",
			want:  nil,
		},
		{
			name:   "global only, and it reaches every image",
			global: []string{"release"},
			image:  "redis",
			want:   []string{"release"},
		},
		{
			name:     "per-image only",
			perImage: map[string][]string{"internal/thing": {"canary"}},
			image:    "internal/thing",
			want:     []string{"canary"},
		},
		{
			name:     "both add up",
			perImage: map[string][]string{"internal/thing": {"canary"}},
			global:   []string{"release"},
			image:    "internal/thing",
			want:     []string{"release", "canary"},
		},
		{
			name:     "another image's entry does not leak, the global one still applies",
			perImage: map[string][]string{"internal/thing": {"canary"}},
			global:   []string{"release"},
			image:    "redis",
			want:     []string{"release"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewUpdateChecker("compose.yaml", nil).
				WithFloatingTags(tt.perImage, tt.global)

			assert.Equal(t, tt.want, checker.floatingTagsFor(tt.image))

			// The global slice is shared by every image, so combining must never
			// write an image's own tag into it.
			assert.Equal(t, tt.global, checker.globalFloatingTags)
		})
	}
}
