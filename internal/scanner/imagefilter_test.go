package scanner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The whole point of -image is that the images it leaves out cost nothing, so
// the test is about what the registry was asked, not only about which rows came
// back: a filter applied after the lookups would pass a row count assertion and
// still fetch every tag list.
func TestScanImageFilterAsksTheRegistryAboutNothingElse(t *testing.T) {
	var (
		mu        sync.Mutex
		requested []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()

		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"1.0.0", "1.1.0"}})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	t.Setenv("CCU_REGISTRY_HOST", strings.TrimPrefix(server.URL, "http://"))

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "compose.yaml"), []byte(
		"services:\n"+
			"  proxy:\n    image: library/traefik:1.0.0\n"+
			"  web:\n    image: library/nginx:1.0.0\n"), 0644))

	events, err := Scan(context.Background(), Options{
		Root: root, Images: []string{"traefik"},
		Major: true, Minor: true, Patch: true,
	})
	require.NoError(t, err)

	var (
		updates []imageRow
		images  int
	)
	for _, ev := range collect(t, events) {
		switch ev.Kind {
		case EventUpdate:
			updates = append(updates, imageRow{ev.Update.ImageName, ev.Update.LatestTag})
		case EventFileDone:
			images += ev.Images
		}
	}

	require.Len(t, updates, 1)
	assert.Equal(t, "library/traefik", updates[0].name)
	assert.Equal(t, "1.1.0", updates[0].latest)

	// The excluded image is still declared in the file; it must simply never
	// reach a registry.
	assert.Equal(t, 1, images, "the file contributed one image after the filter")

	mu.Lock()
	defer mu.Unlock()
	for _, path := range requested {
		assert.NotContains(t, path, "nginx", "the filtered-out image was looked up anyway")
	}
	assert.NotEmpty(t, requested, "the selected image was never looked up")
}

// imageRow is the pair the assertions above care about, kept out of the
// event so a failure names the image rather than printing a whole Update.
type imageRow struct {
	name   string
	latest string
}
