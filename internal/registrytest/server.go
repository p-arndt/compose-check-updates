// Package registrytest serves the slice of an OCI registry a check talks to, so
// tests resolve invented tags instead of reaching the real hub.
package registrytest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	return ServerWithCreated(t, repo, tags, tagDigests, nil)
}

// ServerWithCreated is Server plus a build date per tag, served the way a real
// registry does it: the manifest points at a config blob, and only that blob
// says when the image was built. Tags left out of created keep the bare
// manifest Server serves, which is how a repository with no config blob at all
// behaves.
func ServerWithCreated(t *testing.T, repo string, tags []string, tagDigests map[string]string, created map[string]time.Time) *httptest.Server {
	t.Helper()

	// The blobs are built once, so a handler can answer both the manifest that
	// names a digest and the request that then fetches it.
	blobs := map[string][]byte{}
	configs := map[string]string{}
	for tag, at := range created {
		body := configBlob(at)
		digest := sha256Digest(body)
		blobs[digest] = body
		configs[tag] = digest
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": tags})
			return
		}

		if i := strings.Index(r.URL.Path, "/blobs/"); i >= 0 {
			body, ok := blobs[r.URL.Path[i+len("/blobs/"):]]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.oci.image.config.v1+json")
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(body)
			}
			return
		}

		i := strings.Index(r.URL.Path, "/manifests/")
		if i < 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tag := r.URL.Path[i+len("/manifests/"):]
		digest, ok := tagDigests[tag]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		body := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
		if config, ok := configs[tag]; ok {
			body = imageManifest(config, len(blobs[config]))
		}

		// A GET is answered with the body's own digest, a HEAD with the invented
		// one the test asked for: regclient verifies what it downloads, so a
		// stand-in digest can only survive where there is no body to check it
		// against. Which is where the tests use it — comparing manifest digests
		// never pulls one.
		if r.Method != http.MethodHead {
			digest = sha256Digest(body)
		}

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

// configBlob is the smallest OCI image config that carries a build date.
func configBlob(at time.Time) []byte {
	body, _ := json.Marshal(map[string]any{
		"created":      at.UTC().Format(time.RFC3339),
		"architecture": "amd64",
		"os":           "linux",
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{}},
	})
	return body
}

// imageManifest points at a config blob by digest, which is the only way a
// client can find it — and the digest has to be the real one, since regclient
// verifies what it downloads against it.
func imageManifest(configDigest string, size int) []byte {
	body, _ := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    configDigest,
			"size":      size,
		},
		"layers": []any{},
	})
	return body
}

func sha256Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Host is the server's address in the form CCU_REGISTRY_HOST and
// registry.New take.
func Host(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}
