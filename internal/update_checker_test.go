package internal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateUpdateInfos(t *testing.T) {
	tests := []struct {
		name     string
		fileData string
		expected []UpdateInfo
	}{
		{
			name: "Single image",
			fileData: `
image: library/ubuntu:18.04.0
`,
			expected: []UpdateInfo{
				{
					RawLine:       "image: library/ubuntu:18.04.0",
					FullImageName: "library/ubuntu:18.04.0",
					ImageName:     "library/ubuntu",
					CurrentTag:    "18.04.0",
				},
			},
		},
		{
			name: "Multiple images",
			fileData: `
image: library/ubuntu:18.04.0
image: library/nginx:1.19.0
`,
			expected: []UpdateInfo{
				{
					RawLine:       "image: library/ubuntu:18.04.0",
					FullImageName: "library/ubuntu:18.04.0",
					ImageName:     "library/ubuntu",
					CurrentTag:    "18.04.0",
				},
				{
					RawLine:       "image: library/nginx:1.19.0",
					FullImageName: "library/nginx:1.19.0",
					ImageName:     "library/nginx",
					CurrentTag:    "1.19.0",
				},
			},
		},
		{
			name: "Duplicate images",
			fileData: `
image: library/ubuntu:18.04.0
image: library/ubuntu:18.04.0
`,
			expected: []UpdateInfo{
				{
					RawLine:       "image: library/ubuntu:18.04.0",
					FullImageName: "library/ubuntu:18.04.0",
					ImageName:     "library/ubuntu",
					CurrentTag:    "18.04.0",
				},
			},
		},
		{
			name: "No tag",
			fileData: `
image: library/ubuntu
`,
			expected: []UpdateInfo{
				{
					RawLine:       "image: library/ubuntu",
					FullImageName: "library/ubuntu",
					ImageName:     "library/ubuntu",
					CurrentTag:    "",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.CreateTemp("", "testfile.yaml")
			assert.NoError(t, err)
			defer os.Remove(file.Name())

			_, err = file.WriteString(tt.fileData)
			assert.NoError(t, err)
			file.Close()

			for i := range tt.expected {
				tt.expected[i].FilePath = file.Name()
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/v2/repositories/") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"count": 4, "results": [
						{"name": "1.18.0"},
						{"name": "1.18.1"},
						{"name": "1.19.0"},
						{"name": "1.20.0"}
					],"next": null}`))
					return
				}
				if strings.Contains(r.URL.Path, "/tags/list") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"name":"library/ubuntu","tags":["1.18.0","1.18.1","1.19.0","1.20.0"]}`))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			serverURL, _ := url.Parse(server.URL)
			registry := NewRegistry(serverURL.Host)
			updateChecker := NewUpdateChecker(file.Name(), registry)

			updateInfos, err := updateChecker.createUpdateInfos()
			assert.NoError(t, err)

			assert.Equal(t, tt.expected, updateInfos)
		})
	}
}

func TestUpdateCheckerCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tags/list") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"name":"library/myimage","tags":["1.18.0","1.18.1","1.19.0","1.20.0"]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)

	tests := []struct {
		name     string
		fileData string
		expected []UpdateInfo
	}{
		{
			name: "Single image",
			fileData: fmt.Sprintf(`
image: %s/library/myimage:1.19.0
`, serverURL.Host),

			expected: []UpdateInfo{
				{
					RawLine:       fmt.Sprintf("image: %s/library/myimage:1.19.0", serverURL.Host),
					FullImageName: fmt.Sprintf("%s/library/myimage:1.19.0", serverURL.Host),
					ImageName:     fmt.Sprintf("%s/library/myimage", serverURL.Host),
					CurrentTag:    "1.19.0",
					LatestTag:     "1.20.0",
					MinorTag:      "1.20.0",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.CreateTemp("", "testfile.yaml")
			assert.NoError(t, err)
			defer os.Remove(file.Name())

			_, err = file.WriteString(tt.fileData)
			assert.NoError(t, err)
			file.Close()

			for i := range tt.expected {
				tt.expected[i].FilePath = file.Name()
			}

			registry := NewRegistry(serverURL.Host)
			updateChecker := NewUpdateChecker(file.Name(), registry)

			result, err := updateChecker.Check(true, true, true)
			assert.NoError(t, err)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestVersioningFor covers the lookup the checker does per image: an entry
// naming the image wins, and everything else takes the run's default. The
// precedence between the flag and the config files is settled before this,
// in config.Config; here there is only one default left to fall back to.
func TestVersioningFor(t *testing.T) {
	checker := NewUpdateChecker("compose.yaml", nil).
		WithVersioning(map[string]string{
			"nousresearch/hermes-agent": VersioningLoose,
			"redis":                     "",
		}, VersioningSemver)

	tests := []struct {
		image string
		want  string
	}{
		{"nousresearch/hermes-agent", VersioningLoose},
		// An entry that records only a cap leaves the scheme to the default.
		{"redis", VersioningSemver},
		{"library/traefik", VersioningSemver},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			assert.Equal(t, tt.want, checker.versioningFor(tt.image))
		})
	}

	// A checker nobody configured reports the empty name, which VersioningByName
	// reads as the default rather than as an error.
	bare := NewUpdateChecker("compose.yaml", nil)
	assert.Equal(t, "", bare.versioningFor("redis"))
	scheme, ok := VersioningByName(bare.versioningFor("redis"))
	assert.True(t, ok)
	assert.Equal(t, VersioningSemver, scheme.Name())
}
