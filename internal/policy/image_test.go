package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// An image nobody named keeps comparing against "latest", so one entry cannot
// change what every other image is checked against.
func TestReferenceTagLookupIsExact(t *testing.T) {
	set := Set{Images: map[string]Image{
		"internal/thing": {ReferenceTag: "stable"},
		// A key written with nothing after it is not a tag; it takes the default
		// rather than asking the registry for the empty one.
		"internal/other": {ReferenceTag: ""},
	}}

	assert.Equal(t, "stable", set.For("internal/thing").ReferenceTag)
	for _, image := range []string{"internal/other", "library/redis"} {
		assert.Equal(t, DefaultReferenceTag, set.For(image).ReferenceTag, image)
	}
	assert.Equal(t, DefaultReferenceTag, Set{}.For("internal/thing").ReferenceTag)
}

// The reference is the tag whose digest is being chased, so it can never be
// offered as the tag now carrying it.

// IsZero decides whether an image's entry is worth writing back to the config at
// all, so every field has to count: dropping an entry that only names a floating
// tag would silently undo what the user recorded.
func TestImageIsZero(t *testing.T) {
	tests := []struct {
		name  string
		image Image
		want  bool
	}{
		{name: "nobody named this image", image: Image{}, want: true},
		{name: "a cap counts", image: Image{Max: LevelMinor}, want: false},
		{name: "a scheme counts", image: Image{Versioning: VersioningLoose}, want: false},
		{name: "a reference tag counts", image: Image{ReferenceTag: "stable"}, want: false},
		{name: "a pattern counts", image: Image{VersioningPattern: `^v(?P<major>\d+)$`}, want: false},
		{name: "a floating tag counts", image: Image{FloatingTags: []string{"release"}}, want: false},
		// An empty non-nil slice is what round-tripping an entry through YAML can
		// leave behind; it records nothing.
		{name: "an empty floating list records nothing", image: Image{FloatingTags: []string{}}, want: true},
		{name: "everything at once", image: Image{
			Max:               LevelMajor,
			Versioning:        VersioningRegex,
			ReferenceTag:      "edge",
			FloatingTags:      []string{"canary"},
			VersioningPattern: `^(?P<major>\d+)$`,
		}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.image.IsZero())
		})
	}
}

// An image resolved out of an empty Set gets a default reference tag, so it is
// no longer the zero policy — a caller must not use IsZero to decide the user
// recorded nothing after resolving.
func TestResolvedImageIsNotZero(t *testing.T) {
	assert.False(t, Set{}.For("library/redis").IsZero())
}
