package check

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

// sourceLabel is the label a registry serves for the tags below.
const sourceLabel = "org.opencontainers.image.source"

func TestResolveSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		current     string
		images      map[string]registrytest.Image
		wantSource  string
		wantRelease string
	}{
		{
			name:    "the label of the tag being moved to",
			current: "1.0.0",
			images: map[string]registrytest.Image{
				"1.1.0": {Labels: map[string]string{sourceLabel: "https://github.com/owner/repo"}},
			},
			wantSource:  "https://github.com/owner/repo",
			wantRelease: "https://github.com/owner/repo/releases/tag/1.1.0",
		},
		{
			name:    "an image recording no source",
			current: "1.0.0",
			images: map[string]registrytest.Image{
				"1.1.0": {},
			},
			wantSource:  "",
			wantRelease: "",
		},
		{
			name:    "a source no release page can be named for",
			current: "1.0.0",
			images: map[string]registrytest.Image{
				"1.1.0": {Labels: map[string]string{sourceLabel: "https://git.example.com/team/app"}},
			},
			wantSource:  "https://git.example.com/team/app",
			wantRelease: "",
		},
		{
			name:        "a registry that answers nothing about the tag leaves it empty",
			current:     "1.0.0",
			images:      nil,
			wantSource:  "",
			wantRelease: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := registrytest.ServerWith(t, registrytest.Options{
				Repo:   "library/myimage",
				Tags:   []string{"1.0.0", "1.1.0"},
				Images: tt.images,
			})
			host := registrytest.Host(server)
			file := writeComposeFile(t, "image: "+host+"/library/myimage:"+tt.current)

			updates, err := New(file, registry.New(host), policy.Set{}).Check(true, true, true)

			require.NoError(t, err)
			require.Len(t, updates, 1)
			assert.Equal(t, "1.1.0", updates[0].LatestTag)
			assert.Equal(t, tt.wantSource, updates[0].SourceURL)
			assert.Equal(t, tt.wantRelease, updates[0].ReleaseURL())
		})
	}
}

// The labels are metadata about a move, so an image that is not moving must not
// cost the requests reading them takes.
func TestResolveSourceSkipsImagesWithoutAnUpdate(t *testing.T) {
	t.Parallel()

	server := registrytest.ServerWith(t, registrytest.Options{
		Repo: "library/myimage",
		Tags: []string{"1.0.0"},
		Images: map[string]registrytest.Image{
			"1.0.0": {Labels: map[string]string{sourceLabel: "https://github.com/owner/repo"}},
		},
	})
	host := registrytest.Host(server)
	file := writeComposeFile(t, "image: "+host+"/library/myimage:1.0.0")

	updates, err := New(file, registry.New(host), policy.Set{}).Check(true, true, true)

	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.Empty(t, updates[0].LatestTag)
	assert.Empty(t, updates[0].SourceURL)
}

// Switching target after the scan must move the link with it: the notes wanted
// are the ones for the release actually being written.
func TestReleaseURLFollowsSelectedTarget(t *testing.T) {
	t.Parallel()

	u := Update{
		CurrentTag: "1.0.0",
		LatestTag:  "2.0.0",
		PatchTag:   "1.0.1",
		MinorTag:   "1.1.0",
		MajorTag:   "2.0.0",
		SourceURL:  "https://github.com/owner/repo",
	}

	assert.Equal(t, "https://github.com/owner/repo/releases/tag/2.0.0", u.ReleaseURL())

	u.SelectTarget(policy.LevelPatch)
	assert.Equal(t, "https://github.com/owner/repo/releases/tag/1.0.1", u.ReleaseURL())
}
