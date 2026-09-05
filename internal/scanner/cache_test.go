package scanner

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

// A scan builds one registry client per compose file, so before the shared
// cache an image used by two stacks was listed twice in one run. The workers
// race for it, which is what the -race build is watching here.
func TestSharedCacheListsOneImageOnce(t *testing.T) {
	t.Setenv("CCU_CACHE_DIR", t.TempDir())

	var (
		mu       sync.Mutex
		tagLists int
	)
	server := registrytest.ServerWith(t, registrytest.Options{
		Repo:       "library/myimage",
		Tags:       []string{"1.2.3", "1.2.4"},
		TagDigests: map[string]string{"1.2.3": registrytest.DigestOld, "1.2.4": registrytest.DigestNew},
		Intercept: func(_ http.ResponseWriter, r *http.Request) bool {
			if strings.HasSuffix(r.URL.Path, "/tags/list") {
				mu.Lock()
				tagLists++
				mu.Unlock()
			}
			return false
		},
	})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	root := t.TempDir()
	stack := "services:\n  app:\n    image: library/myimage:1.2.3\n"
	writeFile(t, root, "compose.yaml", stack)
	writeFile(t, root, "docker-compose.yaml", stack)

	cache := registry.NewCache(registry.CacheOptions{TTL: time.Hour})
	ch, err := Scan(context.Background(), Options{Root: root, Patch: true, Cache: cache})
	require.NoError(t, err)

	updates := updateEvents(collect(t, ch))
	require.Len(t, updates, 2, "both stacks report the same image")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, tagLists)
}
