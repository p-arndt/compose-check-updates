package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The built-in tags stay floating for an image that named its own: a repository
// publishing "release" almost certainly publishes "latest" beside it, and that
// one must not turn back into a pinnable version tag.
func TestFloatingTagsAddToTheBuiltInSet(t *testing.T) {
	assert.True(t, Image{FloatingTags: []string{"release"}}.Floats("latest"))
	assert.True(t, Image{FloatingTags: []string{"release"}}.Floats("release"))
	assert.True(t, Image{}.Floats("nightly"))
	assert.False(t, Image{}.Floats("release"))
	assert.False(t, Image{FloatingTags: []string{"release"}}.Floats("1.2.3"))
}

// One image's list does not reach another's.
func TestFloatingTagsLookupIsExact(t *testing.T) {
	set := Set{Images: map[string]Image{"internal/thing": {FloatingTags: []string{"release"}}}}

	assert.Equal(t, []string{"release"}, set.For("internal/thing").FloatingTags)
	assert.Empty(t, set.For("library/redis").FloatingTags)
	assert.Empty(t, Set{}.For("internal/thing").FloatingTags)
}

// An extra floating tag is no more an update target than a built-in one is:
// moving a commit tag onto the tag that floats would trade a fixed reference for
// a moving one.

// TestFloatingTagsForCombines covers how the two sources add up. Neither
// replaces the other, and neither replaces the built-in set: a registry that
// spells its moving tag "release" across every repository is a global fact, and
// a single repository adding "canary" on top must not lose it again.
func TestFloatingTagsCombine(t *testing.T) {
	tests := []struct {
		name     string
		perImage map[string]Image
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
			perImage: map[string]Image{"internal/thing": {FloatingTags: []string{"canary"}}},
			image:    "internal/thing",
			want:     []string{"canary"},
		},
		{
			name:     "both add up",
			perImage: map[string]Image{"internal/thing": {FloatingTags: []string{"canary"}}},
			global:   []string{"release"},
			image:    "internal/thing",
			want:     []string{"release", "canary"},
		},
		{
			name:     "another image's entry does not leak, the global one still applies",
			perImage: map[string]Image{"internal/thing": {FloatingTags: []string{"canary"}}},
			global:   []string{"release"},
			image:    "redis",
			want:     []string{"release"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := Set{Images: tt.perImage, FloatingTags: tt.global}

			assert.Equal(t, tt.want, set.For(tt.image).FloatingTags)

			// The global slice is shared by every image, so combining must never
			// write an image's own tag into it.
			assert.Equal(t, tt.global, set.FloatingTags)
		})
	}
}
