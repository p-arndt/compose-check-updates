package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

func TestSourceURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		image    registrytest.Image
		expected string
	}{
		{
			name:     "source label",
			image:    registrytest.Image{Labels: map[string]string{"org.opencontainers.image.source": "https://github.com/owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name: "url label when there is no source",
			image: registrytest.Image{Labels: map[string]string{
				"org.opencontainers.image.url": "https://github.com/owner/other",
			}},
			expected: "https://github.com/owner/other",
		},
		{
			name: "source wins over the older labels",
			image: registrytest.Image{Labels: map[string]string{
				"org.opencontainers.image.source": "https://github.com/owner/repo",
				"org.label-schema.vcs-url":        "https://github.com/owner/legacy",
			}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "vcs-url of an image predating the OCI labels",
			image:    registrytest.Image{Labels: map[string]string{"org.label-schema.vcs-url": "https://github.com/owner/legacy"}},
			expected: "https://github.com/owner/legacy",
		},
		{
			name:     "index followed to its platform manifest",
			image:    registrytest.Image{Index: true, Labels: map[string]string{"org.opencontainers.image.source": "https://github.com/owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "annotation on the index, without reading a config blob",
			image:    registrytest.Image{Index: true, Annotations: map[string]string{"org.opencontainers.image.source": "https://github.com/owner/annotated"}},
			expected: "https://github.com/owner/annotated",
		},
		{
			name:     "no labels at all",
			image:    registrytest.Image{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := registrytest.ServerWith(t, registrytest.Options{
				Repo:   "library/myimage",
				Tags:   []string{"1.2.3"},
				Images: map[string]registrytest.Image{"1.2.3": tt.image},
			})
			host := registrytest.Host(server)

			got, err := New(host).SourceURL(host + "/library/myimage:1.2.3")

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// A repository that answers nothing must not be an error a caller has to handle
// beyond leaving the fields empty, and asking twice must not cost two requests.
func TestSourceURLCachesPerTag(t *testing.T) {
	t.Parallel()

	server := registrytest.ServerWith(t, registrytest.Options{
		Repo: "library/myimage",
		Tags: []string{"1.2.3"},
		Images: map[string]registrytest.Image{
			"1.2.3": {Labels: map[string]string{"org.opencontainers.image.source": "https://github.com/owner/repo"}},
		},
	})
	host := registrytest.Host(server)
	client := New(host)
	image := host + "/library/myimage:1.2.3"

	first, err := client.SourceURL(image)
	require.NoError(t, err)

	server.Close()

	second, err := client.SourceURL(image)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestSourceURLUnknownTag(t *testing.T) {
	t.Parallel()

	server := registrytest.Server(t, "library/myimage", []string{"1.2.3"}, nil)
	host := registrytest.Host(server)

	_, err := New(host).SourceURL(host + "/library/myimage:9.9.9")

	assert.Error(t, err)
}

func TestSourceLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		tag     string
		wantURL string
		wantRel string
	}{
		{
			name:    "official image with commit fragment",
			source:  "https://github.com/traefik/traefik-library-image.git#06814bb:v3.7/alpine",
			tag:     "3.7.13",
			wantURL: "https://github.com/traefik/traefik-library-image",
			wantRel: "https://github.com/traefik/traefik-library-image/releases/tag/3.7.13",
		},
		{
			name:    "github",
			source:  "https://github.com/owner/repo",
			tag:     "v1.2.3",
			wantURL: "https://github.com/owner/repo",
			wantRel: "https://github.com/owner/repo/releases/tag/v1.2.3",
		},
		{
			name:    "github with a .git suffix",
			source:  "https://github.com/owner/repo.git",
			tag:     "1.2.3",
			wantURL: "https://github.com/owner/repo",
			wantRel: "https://github.com/owner/repo/releases/tag/1.2.3",
		},
		{
			name:    "github ssh remote",
			source:  "git@github.com:owner/repo.git",
			tag:     "v1",
			wantURL: "https://github.com/owner/repo",
			wantRel: "https://github.com/owner/repo/releases/tag/v1",
		},
		{
			name:    "github subdirectory link",
			source:  "https://github.com/owner/repo/tree/main/docker",
			tag:     "v2",
			wantURL: "https://github.com/owner/repo/tree/main/docker",
			wantRel: "https://github.com/owner/repo/releases/tag/v2",
		},
		{
			name:    "gitlab, subgroups included",
			source:  "https://gitlab.com/group/subgroup/project",
			tag:     "v4.0.0",
			wantURL: "https://gitlab.com/group/subgroup/project",
			wantRel: "https://gitlab.com/group/subgroup/project/-/releases/v4.0.0",
		},
		{
			name:    "another forge keeps the source link only",
			source:  "https://git.example.com/owner/repo",
			tag:     "v1.0.0",
			wantURL: "https://git.example.com/owner/repo",
			wantRel: "",
		},
		{
			name:    "host without a scheme",
			source:  "github.com/owner/repo",
			tag:     "v1.0.0",
			wantURL: "https://github.com/owner/repo",
			wantRel: "https://github.com/owner/repo/releases/tag/v1.0.0",
		},
		{
			name:    "a tag with a slash is escaped into the path",
			source:  "https://github.com/owner/repo",
			tag:     "release/1.0",
			wantURL: "https://github.com/owner/repo",
			wantRel: "https://github.com/owner/repo/releases/tag/release%2F1.0",
		},
		{
			name:    "no tag to link a release for",
			source:  "https://github.com/owner/repo",
			tag:     "",
			wantURL: "https://github.com/owner/repo",
			wantRel: "",
		},
		{
			name:    "a label that is not a link at all",
			source:  "mailto:maintainer@example.com",
			tag:     "v1",
			wantURL: "",
			wantRel: "",
		},
		{
			name:    "empty label",
			source:  "",
			tag:     "v1",
			wantURL: "",
			wantRel: "",
		},
		{
			name:    "the forge itself is no repository",
			source:  "https://github.com/owner",
			tag:     "v1",
			wantURL: "https://github.com/owner",
			wantRel: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotURL, gotRel := SourceLinks(tt.source, tt.tag)

			assert.Equal(t, tt.wantURL, gotURL)
			assert.Equal(t, tt.wantRel, gotRel)
		})
	}
}
