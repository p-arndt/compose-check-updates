package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	digestOld = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	digestNew = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// newRegistryTestServer serves a minimal OCI registry: a tag list and a manifest
// endpoint resolving each tag to the digest given in tagDigests.
func newRegistryTestServer(t *testing.T, repo string, tags []string, tagDigests map[string]string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": tags})
			return
		}

		if i := strings.Index(r.URL.Path, "/manifests/"); i != -1 {
			reference := r.URL.Path[i+len("/manifests/"):]
			digest, ok := tagDigests[reference]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			body := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.Header().Set("Docker-Content-Digest", digest)
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				w.Write(body)
			}
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestIsDigest(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{digestOld, true},
		{"sha512:" + strings.Repeat("a", 128), true},
		{"sha-e1c83ba", false},
		{"sha256-e1c83ba", false},
		{"1.2.3", false},
		{"latest", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsDigest(tt.value))
		})
	}
}

func TestTagFamily(t *testing.T) {
	assert.Equal(t, "sha-", tagFamily("sha-e1c83ba"))
	assert.Equal(t, "sha256-", tagFamily("sha256-e1c83ba"))
	assert.Equal(t, "", tagFamily("latest"))
	assert.Equal(t, "1.2.", tagFamily("1.2.3"))
}

func TestDigestCandidates(t *testing.T) {
	t.Run("keeps only tags of the same family", func(t *testing.T) {
		tags := []string{"latest", "main", "sha-e1c83ba", "sha-49821e5", "sha-438f91a", "v2-beta"}

		candidates, dropped := digestCandidates(tags, "sha-e1c83ba")

		assert.Equal(t, []string{"sha-49821e5", "sha-438f91a"}, candidates)
		assert.Zero(t, dropped)
	})

	t.Run("reports how many tags were dropped by the cap", func(t *testing.T) {
		var tags []string
		for i := range maxDigestCandidates + 10 {
			tags = append(tags, fmt.Sprintf("sha-%04d", i))
		}

		candidates, dropped := digestCandidates(tags, "sha-9999")

		assert.Len(t, candidates, maxDigestCandidates)
		assert.Equal(t, 10, dropped)
	})
}

func TestFetchImageDigest(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage", []string{"latest"}, map[string]string{
		"latest": digestNew,
	})
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	assert.NoError(t, err)

	registry := NewRegistry(serverURL.Host)
	got, err := registry.FetchImageDigest(serverURL.Host + "/library/myimage:latest")

	assert.NoError(t, err)
	assert.Equal(t, digestNew, got)
}

// TestCheckDigestPinned covers an image pinned by digest: the digest is refreshed
// in place and the tag, if any, is left alone.
func TestCheckDigestPinned(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage", []string{"latest"}, map[string]string{
		"latest": digestNew,
	})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"

	tests := []struct {
		name          string
		reference     string
		expectedTag   string
		expectedLine  string
		expectedLevel string
	}{
		{
			name:          "digest only",
			reference:     image + "@" + digestOld,
			expectedTag:   "",
			expectedLine:  "image: " + image + "@" + digestNew,
			expectedLevel: "digest",
		},
		{
			name:          "tag and digest",
			reference:     image + ":latest@" + digestOld,
			expectedTag:   "latest",
			expectedLine:  "image: " + image + ":latest@" + digestNew,
			expectedLevel: "digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeComposeFile(t, "image: "+tt.reference)

			checker := NewUpdateChecker(file, NewRegistry(serverURL.Host))
			infos, err := checker.Check(true, true, true)
			assert.NoError(t, err)
			assert.Len(t, infos, 1)

			info := infos[0]
			assert.Equal(t, tt.expectedTag, info.CurrentTag)
			assert.Equal(t, digestOld, info.CurrentDigest)
			assert.Equal(t, digestNew, info.LatestDigest)
			assert.True(t, info.IsDigestUpdate())
			assert.True(t, info.HasNewVersion(false, false, false), "digest updates ignore the level filters")
			assert.Equal(t, tt.expectedLevel, info.UpdateLevel())

			assertUpdateWrites(t, info, file, tt.expectedLine)
		})
	}
}

// TestCheckShaTagMovesToNewestTag covers the case from issue #5: an image that
// publishes commit tags instead of semver, pinned to one of those tags.
func TestCheckShaTagMovesToNewestTag(t *testing.T) {
	server := newRegistryTestServer(t, "vert-sh/vert",
		[]string{"latest", "main", "sha-e1c83ba", "sha-49821e5"},
		map[string]string{
			"latest":      digestNew,
			"main":        digestNew,
			"sha-49821e5": digestNew,
			"sha-e1c83ba": digestOld,
		})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/vert-sh/vert"
	file := writeComposeFile(t, "image: "+image+":sha-e1c83ba")

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host))
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "sha-e1c83ba", info.CurrentTag)
	// "main" also resolves to the newest digest but is floating, so the commit
	// tag has to win.
	assert.Equal(t, "sha-49821e5", info.LatestTag)
	assert.Equal(t, digestOld, info.CurrentDigest)
	assert.Equal(t, digestNew, info.LatestDigest)
	assert.Equal(t, "digest", info.UpdateLevel())

	assertUpdateWrites(t, info, file, "image: "+image+":sha-49821e5")
}

func TestCheckDigestSkipsUpToDateAndFloatingTags(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"latest", "sha-49821e5"},
		map[string]string{"latest": digestNew, "sha-49821e5": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"

	tests := []struct {
		name      string
		reference string
	}{
		{"already newest digest", image + "@" + digestNew},
		{"tag already resolves to newest digest", image + ":sha-49821e5"},
		{"floating tag without a digest to compare", image + ":latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeComposeFile(t, "image: "+tt.reference)

			checker := NewUpdateChecker(file, NewRegistry(serverURL.Host))
			infos, err := checker.Check(true, true, true)
			assert.NoError(t, err)
			assert.Len(t, infos, 1)

			assert.False(t, infos[0].IsDigestUpdate())
			assert.False(t, infos[0].HasNewVersion(true, true, true))
			assert.Empty(t, infos[0].LatestDigest)
		})
	}
}

// TestCheckSemverWithDigestMovesBoth guards the trap of bumping a version tag
// while leaving the pinned digest of the previous release behind.
func TestCheckSemverWithDigestMovesBoth(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"1.19.0", "1.20.0"},
		map[string]string{"1.19.0": digestOld, "1.20.0": digestNew})
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":1.19.0@"+digestOld)

	checker := NewUpdateChecker(file, NewRegistry(serverURL.Host))
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "1.19.0", info.CurrentTag)
	assert.Equal(t, "1.20.0", info.LatestTag)
	assert.Equal(t, digestNew, info.LatestDigest)
	// A real version bump is more informative than "digest".
	assert.Equal(t, "minor", info.UpdateLevel())

	assertUpdateWrites(t, info, file, "image: "+image+":1.20.0@"+digestNew)
}

func TestGetNameTagAndDigest(t *testing.T) {
	u := &UpdateChecker{}

	tests := []struct {
		reference string
		name      string
		tag       string
		digest    string
	}{
		{"library/ubuntu:18.04", "library/ubuntu", "18.04", ""},
		{"library/ubuntu", "library/ubuntu", "", ""},
		{"library/ubuntu@" + digestOld, "library/ubuntu", "", digestOld},
		{"library/ubuntu:18.04@" + digestOld, "library/ubuntu", "18.04", digestOld},
		{"ghcr.io/vert-sh/vert:sha-e1c83ba", "ghcr.io/vert-sh/vert", "sha-e1c83ba", ""},
		{"ghcr.io/vert-sh/vert@" + digestOld, "ghcr.io/vert-sh/vert", "", digestOld},
	}

	for _, tt := range tests {
		t.Run(tt.reference, func(t *testing.T) {
			name, tag, dgst := u.getNameTagAndDigest(tt.reference)
			assert.Equal(t, tt.name, name)
			assert.Equal(t, tt.tag, tag)
			assert.Equal(t, tt.digest, dgst)
		})
	}
}

func writeComposeFile(t *testing.T, content string) string {
	t.Helper()

	file, err := os.CreateTemp("", "compose*.yaml")
	assert.NoError(t, err)
	defer file.Close()

	_, err = file.WriteString(content)
	assert.NoError(t, err)

	t.Cleanup(func() {
		os.Remove(file.Name())
		os.Remove(file.Name() + ".ccu")
	})

	return file.Name()
}

func assertUpdateWrites(t *testing.T, info UpdateInfo, path, expected string) {
	t.Helper()

	assert.NoError(t, info.Update())

	written, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.Equal(t, expected, string(written))
}
