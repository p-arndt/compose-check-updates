package compose

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestImageMatcher covers the two ways a pattern can be written and the
// shorthand that makes `-image traefik` find the image ccu reports as
// "library/traefik".
func TestImageMatcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		patterns []string
		image    string
		want     bool
	}{
		{name: "no pattern selects everything", image: "library/traefik", want: true},

		// A bare name is the common case: nobody types the Docker Hub "library/".
		{name: "bare name matches the library image", patterns: []string{"traefik"}, image: "library/traefik", want: true},
		{name: "bare name matches the last element", patterns: []string{"immich-server"}, image: "ghcr.io/immich-app/immich-server", want: true},
		{name: "bare name is not a substring match", patterns: []string{"traefik"}, image: "library/traefik-forward-auth"},
		{name: "bare name does not match a middle element", patterns: []string{"immich-app"}, image: "ghcr.io/immich-app/immich-server"},

		{name: "full name", patterns: []string{"library/traefik"}, image: "library/traefik", want: true},
		{name: "full name elsewhere", patterns: []string{"library/traefik"}, image: "ghcr.io/library/traefik"},
		{name: "docker.io is stripped from the pattern", patterns: []string{"docker.io/library/traefik"}, image: "library/traefik", want: true},

		{name: "glob on the last element", patterns: []string{"immich-*"}, image: "ghcr.io/immich-app/immich-server", want: true},
		{name: "glob on the full name", patterns: []string{"ghcr.io/immich-app/*"}, image: "ghcr.io/immich-app/immich-server", want: true},
		{name: "glob spans separators", patterns: []string{"ghcr.io/*"}, image: "ghcr.io/immich-app/immich-server", want: true},
		{name: "glob in the middle", patterns: []string{"ghcr.io/*/immich-server"}, image: "ghcr.io/immich-app/immich-server", want: true},
		{name: "glob matching nothing", patterns: []string{"ghcr.io/*"}, image: "library/traefik"},

		{name: "no match", patterns: []string{"nginx"}, image: "library/traefik"},
		{name: "one of several patterns is enough", patterns: []string{"nginx", "traefik"}, image: "library/traefik", want: true},

		// Case-sensitive, like the repository names themselves.
		{name: "case matters", patterns: []string{"Traefik"}, image: "library/traefik"},

		{name: "blank patterns select everything", patterns: []string{"", "  "}, image: "library/traefik", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewImageMatcher(tt.patterns)
			assert.Equal(t, tt.want, m.Match(tt.image), "Match(%q) with patterns %q", tt.image, tt.patterns)
		})
	}
}

// A nil matcher is what every caller that was given no patterns holds, so it
// has to behave like one built from an empty list rather than panic.
func TestImageMatcherNil(t *testing.T) {
	t.Parallel()

	var m *ImageMatcher
	assert.True(t, m.Empty())
	assert.True(t, m.Match("library/traefik"))
	assert.Empty(t, m.Patterns())
}
