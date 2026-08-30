package check

import (
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

func TestTagFamily(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "sha-", tagFamily("sha-e1c83ba"))
	assert.Equal(t, "sha256-", tagFamily("sha256-e1c83ba"))
	assert.Equal(t, "", tagFamily("latest"))
	assert.Equal(t, "1.2.", tagFamily("1.2.3"))
}

func TestDigestCandidates(t *testing.T) {
	t.Parallel()

	t.Run("keeps only tags of the same family", func(t *testing.T) {
		t.Parallel()

		tags := []string{"latest", "main", "sha-e1c83ba", "sha-49821e5", "sha-438f91a", "v2-beta"}

		candidates, dropped := digestCandidates(tags, "sha-e1c83ba", policy.Image{ReferenceTag: policy.DefaultReferenceTag})

		assert.Equal(t, []string{"sha-49821e5", "sha-438f91a"}, candidates)
		assert.Zero(t, dropped)
	})

	t.Run("reports how many tags were dropped by the cap", func(t *testing.T) {
		t.Parallel()

		var tags []string
		for i := range maxDigestCandidates + 10 {
			tags = append(tags, fmt.Sprintf("sha-%04d", i))
		}

		candidates, dropped := digestCandidates(tags, "sha-9999", policy.Image{ReferenceTag: policy.DefaultReferenceTag})

		assert.Len(t, candidates, maxDigestCandidates)
		assert.Equal(t, 10, dropped)
	})
}

func TestCheckDigestPinned(t *testing.T) {
	t.Parallel()

	server := registrytest.Server(t, "library/myimage", []string{"latest"}, map[string]string{
		"latest": registrytest.DigestNew,
	})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"

	tests := []struct {
		name          string
		reference     string
		expectedTag   string
		expectedLine  string
		expectedLevel policy.Level
	}{
		{
			name:          "digest only",
			reference:     image + "@" + registrytest.DigestOld,
			expectedTag:   "",
			expectedLine:  "image: " + image + "@" + registrytest.DigestNew,
			expectedLevel: "digest",
		},
		{
			name:          "tag and digest",
			reference:     image + ":latest@" + registrytest.DigestOld,
			expectedTag:   "latest",
			expectedLine:  "image: " + image + ":latest@" + registrytest.DigestNew,
			expectedLevel: "digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := writeComposeFile(t, "image: "+tt.reference)

			checker := New(file, registry.New(serverURL.Host), policy.Set{})
			infos, err := checker.Check(true, true, true)
			assert.NoError(t, err)
			assert.Len(t, infos, 1)

			info := infos[0]
			assert.Equal(t, tt.expectedTag, info.CurrentTag)
			assert.Equal(t, registrytest.DigestOld, info.CurrentDigest)
			assert.Equal(t, registrytest.DigestNew, info.LatestDigest)
			assert.True(t, info.IsDigestUpdate())
			assert.True(t, info.HasNewVersion(), "digest updates ignore the level filters")
			assert.Equal(t, tt.expectedLevel, info.Level())

			assertUpdateWrites(t, info, file, tt.expectedLine)
		})
	}
}

// TestCheckShaTagMovesToNewestTag covers the case from issue #5: an image that
// publishes commit tags instead of semver, pinned to one of those tags.
func TestCheckShaTagMovesToNewestTag(t *testing.T) {
	t.Parallel()

	server := registrytest.Server(t, "vert-sh/vert",
		[]string{"latest", "main", "sha-e1c83ba", "sha-49821e5"},
		map[string]string{
			"latest":      registrytest.DigestNew,
			"main":        registrytest.DigestNew,
			"sha-49821e5": registrytest.DigestNew,
			"sha-e1c83ba": registrytest.DigestOld,
		})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/vert-sh/vert"
	file := writeComposeFile(t, "image: "+image+":sha-e1c83ba")

	checker := New(file, registry.New(serverURL.Host), policy.Set{})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "sha-e1c83ba", info.CurrentTag)
	// "main" also resolves to the newest digest but is floating, so the commit
	// tag has to win.
	assert.Equal(t, "sha-49821e5", info.LatestTag)
	assert.Equal(t, registrytest.DigestOld, info.CurrentDigest)
	assert.Equal(t, registrytest.DigestNew, info.LatestDigest)
	assert.Equal(t, policy.Level("digest"), info.Level())

	assertUpdateWrites(t, info, file, "image: "+image+":sha-49821e5")
}

func TestCheckDigestSkipsUpToDateAndFloatingTags(t *testing.T) {
	t.Parallel()

	server := registrytest.Server(t, "library/myimage",
		[]string{"latest", "sha-49821e5"},
		map[string]string{"latest": registrytest.DigestNew, "sha-49821e5": registrytest.DigestNew})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"

	tests := []struct {
		name      string
		reference string
	}{
		{"already newest digest", image + "@" + registrytest.DigestNew},
		{"tag already resolves to newest digest", image + ":sha-49821e5"},
		{"floating tag without a digest to compare", image + ":latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := writeComposeFile(t, "image: "+tt.reference)

			checker := New(file, registry.New(serverURL.Host), policy.Set{})
			infos, err := checker.Check(true, true, true)
			assert.NoError(t, err)
			assert.Len(t, infos, 1)

			assert.False(t, infos[0].IsDigestUpdate())
			assert.False(t, infos[0].HasNewVersion())
			assert.Empty(t, infos[0].LatestDigest)
		})
	}
}

// TestCheckSemverWithDigestMovesBoth guards the trap of bumping a version tag
// while leaving the pinned digest of the previous release behind.
func TestCheckSemverWithDigestMovesBoth(t *testing.T) {
	t.Parallel()

	server := registrytest.Server(t, "library/myimage",
		[]string{"1.19.0", "1.20.0"},
		map[string]string{"1.19.0": registrytest.DigestOld, "1.20.0": registrytest.DigestNew})

	serverURL, _ := url.Parse(server.URL)
	image := serverURL.Host + "/library/myimage"
	file := writeComposeFile(t, "image: "+image+":1.19.0@"+registrytest.DigestOld)

	checker := New(file, registry.New(serverURL.Host), policy.Set{})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "1.19.0", info.CurrentTag)
	assert.Equal(t, "1.20.0", info.LatestTag)
	assert.Equal(t, registrytest.DigestNew, info.LatestDigest)
	// A real version bump is more informative than "digest".
	assert.Equal(t, policy.Level("minor"), info.Level())

	assertUpdateWrites(t, info, file, "image: "+image+":1.20.0@"+registrytest.DigestNew)
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

func assertUpdateWrites(t *testing.T, info Update, path, expected string) {
	t.Helper()

	assert.NoError(t, info.Apply())

	written, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.Equal(t, expected, string(written))
}
