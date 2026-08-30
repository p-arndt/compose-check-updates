package versioning

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasComparableTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scheme   Scheme
		current  string
		tags     []string
		expected bool
	}{
		{
			name:     "another release of the same line",
			scheme:   semver,
			current:  "1.2.3",
			tags:     []string{"1.2.3", "1.2.4"},
			expected: true,
		},
		{
			name:     "an older release still counts: the image is readable either way",
			scheme:   semver,
			current:  "1.2.4",
			tags:     []string{"1.2.3", "1.2.4"},
			expected: true,
		},
		{
			name:     "the current tag alone cannot vouch for itself",
			scheme:   semver,
			current:  "1.2.3",
			tags:     []string{"1.2.3", "latest"},
			expected: false,
		},
		{
			name:     "a suffix nobody else shares",
			scheme:   loose,
			current:  "2024-01-01",
			tags:     []string{"2024-01-01", "2024-02-01"},
			expected: false,
		},
		{
			name:     "the same suffix on another release",
			scheme:   semver,
			current:  "3.19-alpine",
			tags:     []string{"3.19-alpine", "3.20-alpine"},
			expected: true,
		},
		{
			name:     "a tag the scheme cannot read at all",
			scheme:   semver,
			current:  "sha-e1c83ba",
			tags:     []string{"sha-e1c83ba", "1.2.3"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, HasComparableTag(tt.scheme, tt.current, tt.tags))
		})
	}
}

// An unreadable image has no tag to write, so a caller that hands one to
// Update() is told so rather than having its compose file rewritten to itself.
