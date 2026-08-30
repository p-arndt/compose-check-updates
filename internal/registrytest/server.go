// Package registrytest serves the slice of an OCI registry a check talks to, so
// tests resolve invented tags instead of reaching the real hub.
package registrytest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Digests are stable stand-ins for manifest digests, distinct enough that a
// mix-up shows up in a failure message.
const (
	DigestOld = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	DigestNew = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// Server serves a tag list for repo and a manifest per entry of tagDigests. It
// is closed when the test ends.
func Server(t *testing.T, repo string, tags []string, tagDigests map[string]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": tags})
			return
		}

		i := strings.Index(r.URL.Path, "/manifests/")
		if i < 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
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
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// Host is the server's address in the form CCU_REGISTRY_HOST and
// registry.New take.
func Host(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}
