package check

import (
	"fmt"
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/versioning"
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
		expected []Update
	}{
		{
			name: "Single image",
			fileData: `
image: library/ubuntu:18.04.0
`,
			expected: []Update{
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
			expected: []Update{
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
			expected: []Update{
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
			expected: []Update{
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
			registry := registry.New(serverURL.Host)
			updateChecker := New(file.Name(), registry, policy.Set{})

			updateInfos, err := updateChecker.updates()
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

	serverURL, _ := url.Parse(server.URL)

	tests := []struct {
		name     string
		fileData string
		expected []Update
	}{
		{
			name: "Single image",
			fileData: fmt.Sprintf(`
image: %s/library/myimage:1.19.0
`, serverURL.Host),

			expected: []Update{
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

			registry := registry.New(serverURL.Host)
			updateChecker := New(file.Name(), registry, policy.Set{})

			result, err := updateChecker.Check(true, true, true)
			assert.NoError(t, err)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestVersioningPatternReachesTheScheme is the seam the regex scheme lives or
// dies on: the pattern is recorded per image and has to arrive at the scheme
// that reads that one image's tags, and nowhere else.
func TestVersioningPatternReachesTheScheme(t *testing.T) {
	const calendar = `^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})$`

	policies := policy.Set{
		Versioning: policy.VersioningSemver,
		Images: map[string]policy.Image{
			"acme/dated": {Versioning: policy.VersioningRegex, VersioningPattern: calendar},
		},
	}

	dated := policies.For("acme/dated")
	scheme, ok := versioning.ByName(dated.Versioning, dated.VersioningPattern)
	assert.True(t, ok)
	assert.Equal(t, policy.VersioningRegex, scheme.Name())

	v, ok := scheme.Parse("2024-01-01")
	assert.True(t, ok)
	assert.Equal(t, []int{2024, 1, 1}, v.Release)

	// No other image is read with it.
	other := policies.For("redis")
	assert.Equal(t, policy.VersioningSemver, other.Versioning)
	assert.Empty(t, other.VersioningPattern)
}
