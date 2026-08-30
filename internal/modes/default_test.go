package modes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal"
	"github.com/p-arndt/compose-check-updates/internal/report"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

const (
	digestOld = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	digestNew = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// newRegistryTestServer serves the tag list and one manifest per tag, so a run
// resolves invented images instead of reaching for Docker Hub.
func newRegistryTestServer(t *testing.T, repo string, tags []string, tagDigests map[string]string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": tags})
			return
		}

		if i := strings.Index(r.URL.Path, "/manifests/"); i != -1 {
			digest, ok := tagDigests[r.URL.Path[i+len("/manifests/"):]]
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

// An image ccu cannot read is reported, and reported only: counting it as
// pending would have `ccu check` start exiting 1 over an image that may well be
// up to date — nobody, ccu included, can say.
func TestUnreadableImageIsReportedButNeverPending(t *testing.T) {
	server := newRegistryTestServer(t, "library/myimage",
		[]string{"latest", "sha-e1c83ba"},
		map[string]string{"latest": digestNew, "sha-e1c83ba": digestOld})
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv("CCU_REGISTRY_HOST", serverURL.Host)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "compose.yaml"),
		[]byte("services:\n  app:\n    image: library/myimage:sha-e1c83ba\n"), 0644))

	var buf bytes.Buffer
	outcome, err := Default(context.Background(),
		scanner.Options{Root: root, Major: true, Minor: true, Patch: true},
		internal.CCUFlags{},
		report.New(report.FormatJSONL, &buf))
	require.NoError(t, err)

	assert.Zero(t, outcome.Updates)
	assert.Zero(t, outcome.Pending)
	assert.False(t, outcome.Failed)
	assert.Equal(t, 1, outcome.Unreadable)

	// Reported all the same, under a kind a consumer can tell from an update.
	line := strings.TrimSpace(buf.String())
	assert.Contains(t, line, `"kind":"unreadable"`)
	assert.Contains(t, line, internal.ReasonNoTagForDigest)
}
