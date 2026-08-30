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
