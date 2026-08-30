package registry

import (
	"net/url"
	"strings"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/registrytest"
	"github.com/stretchr/testify/assert"
)

func TestIsDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value    string
		expected bool
	}{
		{registrytest.DigestOld, true},
		{"sha512:" + strings.Repeat("a", 128), true},
		{"sha-e1c83ba", false},
		{"sha256-e1c83ba", false},
		{"1.2.3", false},
		{"latest", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, IsDigest(tt.value))
		})
	}
}

func TestDigest(t *testing.T) {
	t.Parallel()

	server := registrytest.Server(t, "library/myimage", []string{"latest"}, map[string]string{
		"latest": registrytest.DigestNew,
	})

	serverURL, err := url.Parse(server.URL)
	assert.NoError(t, err)

	client := New(serverURL.Host)
	got, err := client.Digest(serverURL.Host + "/library/myimage:latest")

	assert.NoError(t, err)
	assert.Equal(t, registrytest.DigestNew, got)
}

// TestCheckDigestPinned covers an image pinned by digest: the digest is refreshed
// in place and the tag, if any, is left alone.

func TestParseRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reference string
		name      string
		tag       string
		digest    string
	}{
		{"library/ubuntu:18.04", "library/ubuntu", "18.04", ""},
		{"library/ubuntu", "library/ubuntu", "", ""},
		{"library/ubuntu@" + registrytest.DigestOld, "library/ubuntu", "", registrytest.DigestOld},
		{"library/ubuntu:18.04@" + registrytest.DigestOld, "library/ubuntu", "18.04", registrytest.DigestOld},
		{"ghcr.io/vert-sh/vert:sha-e1c83ba", "ghcr.io/vert-sh/vert", "sha-e1c83ba", ""},
		{"ghcr.io/vert-sh/vert@" + registrytest.DigestOld, "ghcr.io/vert-sh/vert", "", registrytest.DigestOld},
	}

	for _, tt := range tests {
		t.Run(tt.reference, func(t *testing.T) {
			t.Parallel()

			name, tag, dgst := ParseRef(tt.reference)
			assert.Equal(t, tt.name, name)
			assert.Equal(t, tt.tag, tag)
			assert.Equal(t, tt.digest, dgst)
		})
	}
}
