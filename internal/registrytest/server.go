// Package registrytest serves the slice of an OCI registry a check talks to, so
// tests resolve invented tags instead of reaching the real hub.
package registrytest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// Media types the manifests below are served under, spelled out rather than
// imported so this package stays a plain HTTP server.
const (
	mediaTypeManifest = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeIndex    = "application/vnd.oci.image.index.v1+json"
	mediaTypeConfig   = "application/vnd.oci.image.config.v1+json"
)

// Image is what a tag serves beyond its digest: the labels and build date of
// its config blob, annotations on its manifest, and whether it is published as
// a multi-platform index rather than a single manifest.
type Image struct {
	Labels      map[string]string
	Annotations map[string]string
	Index       bool
	// Created is written into the config blob's `created` field; zero leaves the
	// field out, which is how an image built without a date looks.
	Created time.Time
}

// Options is the registry a test wants served.
type Options struct {
	Repo       string
	Tags       []string
	TagDigests map[string]string
	// Images are the tags served with a real manifest and config blob. A tag
	// missing here is served the placeholder manifest, which is all a digest
	// lookup ever reads.
	Images map[string]Image

	// Intercept, when set, sees every request before it is served. Returning true
	// means it answered the request itself — how a test counts what a cache saved
	// it, or serves the 429 a rate-limited registry would.
	Intercept func(w http.ResponseWriter, r *http.Request) bool
}

// Server serves a tag list for repo and a manifest per entry of tagDigests. It
// is closed when the test ends.
func Server(t *testing.T, repo string, tags []string, tagDigests map[string]string) *httptest.Server {
	t.Helper()
	return ServerWith(t, Options{Repo: repo, Tags: tags, TagDigests: tagDigests})
}

// ServerWithCreated is Server plus a build date per tag, served the way a real
// registry does it: the manifest points at a config blob, and only that blob
// says when the image was built. Tags left out of created keep the bare
// placeholder manifest, which is how a repository with no config blob behaves.
func ServerWithCreated(t *testing.T, repo string, tags []string, tagDigests map[string]string, created map[string]time.Time) *httptest.Server {
	t.Helper()
	images := make(map[string]Image, len(created))
	for tag, at := range created {
		images[tag] = Image{Created: at}
	}
	return ServerWith(t, Options{Repo: repo, Tags: tags, TagDigests: tagDigests, Images: images})
}

// ServerWith serves a registry that also answers for manifest bodies and config
// blobs, for a test about what an image says about itself rather than about
// which digest a tag has.
func ServerWith(t *testing.T, opts Options) *httptest.Server {
	t.Helper()

	blobs, manifests, tagManifests := buildImages(opts.Images)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.Intercept != nil && opts.Intercept(w, r) {
			return
		}

		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": opts.Repo, "tags": opts.Tags})
			return
		}

		if _, digest, ok := strings.Cut(r.URL.Path, "/blobs/"); ok {
			blob, ok := blobs[digest]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			serveBody(w, r, blob, mediaTypeConfig, "")
			return
		}

		_, reference, ok := strings.Cut(r.URL.Path, "/manifests/")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// A digest reference only ever names one of the manifests built above:
		// that is how an index is followed down to a platform.
		if m, ok := manifests[reference]; ok {
			serveBody(w, r, m.body, m.mediaType, m.digest)
			return
		}

		digest, hasDigest := opts.TagDigests[reference]
		m, hasImage := tagManifests[reference]

		// The placeholder body carries the invented digest of the tag, which a HEAD
		// reports and nothing verifies. A GET has to hand back a body that really
		// hashes to the digest in its header, so a tag with an image of its own is
		// served that — its GET digest is then not its HEAD digest, which no caller
		// of this server compares.
		switch {
		case hasImage && (r.Method != http.MethodHead || !hasDigest):
			serveBody(w, r, m.body, m.mediaType, m.digest)
		case hasDigest:
			serveBody(w, r, []byte(`{"schemaVersion":2,"mediaType":"`+mediaTypeManifest+`"}`), mediaTypeManifest, digest)
		default:
			w.WriteHeader(http.StatusNotFound)
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

// blob is one served body, addressed by the digest of its content.
type blob struct {
	body      []byte
	digest    string
	mediaType string
}

// buildImages turns the wanted images into the bodies the handler serves: the
// config blobs by digest, every manifest by digest, and the top-level manifest
// of each tag.
func buildImages(images map[string]Image) (blobs map[string][]byte, byDigest map[string]blob, byTag map[string]blob) {
	blobs = map[string][]byte{}
	byDigest = map[string]blob{}
	byTag = map[string]blob{}

	for tag, image := range images {
		configBody := map[string]any{
			"architecture": "amd64",
			"os":           "linux",
			"config":       map[string]any{"Labels": image.Labels},
			"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{}},
		}
		if !image.Created.IsZero() {
			configBody["created"] = image.Created.UTC().Format(time.RFC3339)
		}
		config := mustJSON(configBody)
		configDigest := digestOf(config)
		blobs[configDigest] = config

		manifestBody := map[string]any{
			"schemaVersion": 2,
			"mediaType":     mediaTypeManifest,
			"config": map[string]any{
				"mediaType": mediaTypeConfig,
				"digest":    configDigest,
				"size":      len(config),
			},
			"layers": []any{},
		}
		if !image.Index && len(image.Annotations) > 0 {
			manifestBody["annotations"] = image.Annotations
		}

		manifest := newBlob(mustJSON(manifestBody), mediaTypeManifest)
		byDigest[manifest.digest] = manifest

		top := manifest
		if image.Index {
			index := map[string]any{
				"schemaVersion": 2,
				"mediaType":     mediaTypeIndex,
				"manifests": []any{
					// An attestation entry, which buildx lists first and which has no
					// image config behind it: a client following the index must skip it.
					map[string]any{
						"mediaType": mediaTypeManifest,
						"digest":    DigestOld,
						"size":      1,
						"platform":  map[string]any{"architecture": "unknown", "os": "unknown"},
					},
					map[string]any{
						"mediaType": mediaTypeManifest,
						"digest":    manifest.digest,
						"size":      len(manifest.body),
						"platform":  map[string]any{"architecture": "amd64", "os": "linux"},
					},
				},
			}
			if len(image.Annotations) > 0 {
				index["annotations"] = image.Annotations
			}

			top = newBlob(mustJSON(index), mediaTypeIndex)
			byDigest[top.digest] = top
		}

		byTag[tag] = top
	}

	return blobs, byDigest, byTag
}

func newBlob(body []byte, mediaType string) blob {
	return blob{body: body, digest: digestOf(body), mediaType: mediaType}
}

func serveBody(w http.ResponseWriter, r *http.Request, body []byte, mediaType, digest string) {
	if digest == "" {
		digest = digestOf(body)
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mustJSON(v any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return body
}
