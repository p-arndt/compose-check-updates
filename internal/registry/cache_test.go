package registry

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

// counter records what a test's registry was actually asked for, which is the
// only thing that proves a cache saved a request rather than merely agreeing
// with one.
type counter struct {
	mu     sync.Mutex
	byKind map[string]int
	// fail, when set, answers every request with it instead of serving it.
	fail int
}

func newCounter() *counter { return &counter{byKind: map[string]int{}} }

func (c *counter) intercept(w http.ResponseWriter, r *http.Request) bool {
	c.mu.Lock()
	kind := "manifest"
	switch {
	case strings.HasSuffix(r.URL.Path, "/tags/list"):
		kind = "tags"
	case strings.Contains(r.URL.Path, "/blobs/"):
		kind = "blob"
	}
	c.byKind[kind]++
	fail := c.fail
	c.mu.Unlock()

	if fail != 0 {
		w.WriteHeader(fail)
		return true
	}
	return false
}

func (c *counter) count(kind string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byKind[kind]
}

func (c *counter) failWith(status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail = status
}

// tagServer is a repository with one tagged image that counts what it is asked.
func tagServer(t *testing.T, requests *counter) (host string) {
	t.Helper()

	built := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	server := registrytest.ServerWith(t, registrytest.Options{
		Repo:       "library/myimage",
		Tags:       []string{"1.2.3", "1.2.4"},
		TagDigests: map[string]string{"1.2.4": registrytest.DigestNew},
		Images: map[string]registrytest.Image{
			"1.2.4": {
				Created: built,
				Labels:  map[string]string{"org.opencontainers.image.source": "https://github.com/owner/repo"},
			},
		},
		Intercept: requests.intercept,
	})
	return registrytest.Host(server)
}

// A second run inside the TTL is the case the cache exists for: reopening the
// TUI, or a `check` straight after one, must not ask the registries again.
func TestTagListCacheHonoursTheTTL(t *testing.T) {
	tests := []struct {
		name         string
		opts         CacheOptions
		wantRequests int
	}{
		{name: "inside the ttl the list is reused", opts: CacheOptions{TTL: time.Hour}, wantRequests: 1},
		// Anything older is refetched, because a tag list is the answer that must
		// never be stale: a release published a minute ago is what ccu is for.
		{name: "past the ttl the list is fetched again", opts: CacheOptions{TTL: time.Nanosecond}, wantRequests: 2},
		{name: "-refresh ignores a fresh entry", opts: CacheOptions{TTL: time.Hour, Refresh: true}, wantRequests: 2},
		{name: "a zero ttl reads nothing back", opts: CacheOptions{TTL: 0}, wantRequests: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CCU_CACHE_DIR", t.TempDir())

			requests := newCounter()
			host := tagServer(t, requests)
			image := host + "/library/myimage"

			first, err := NewWithCache(host, NewCache(tt.opts)).Tags(image)
			require.NoError(t, err)
			assert.Equal(t, []string{"1.2.3", "1.2.4"}, first)

			// A second client, as a second run — or a second compose file naming the
			// same image — would build it.
			second, err := NewWithCache(host, NewCache(tt.opts)).Tags(image)
			require.NoError(t, err)

			assert.Equal(t, first, second)
			assert.Equal(t, tt.wantRequests, requests.count("tags"))
		})
	}
}

// The whole point of the disk cache being shared: the scanner builds a client
// per compose file, so an image in two stacks must not be looked up twice.
func TestCacheIsSharedBetweenClientsOfOneRun(t *testing.T) {
	t.Setenv("CCU_CACHE_DIR", t.TempDir())

	requests := newCounter()
	host := tagServer(t, requests)
	cache := NewCache(CacheOptions{TTL: time.Hour})

	for range 3 {
		_, err := NewWithCache(host, cache).Tags(host + "/library/myimage")
		require.NoError(t, err)
	}

	assert.Equal(t, 1, requests.count("tags"))
	assert.Equal(t, CacheStats{Lookups: 3, Hits: 2}, cache.Stats())
}

// Stale-if-error: a rate-limited registry gives no answer at all, so month-old
// data beats failing the run — and the run is told which image it happened to.
func TestRateLimitFallsBackToAnExpiredEntry(t *testing.T) {
	t.Setenv("CCU_CACHE_DIR", t.TempDir())

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	requests := newCounter()
	host := tagServer(t, requests)
	image := host + "/library/myimage"

	// A TTL that has expired by the time the second run reads it.
	cache := NewCache(CacheOptions{TTL: time.Nanosecond})
	first, err := NewWithCache(host, cache).Tags(image)
	require.NoError(t, err)

	requests.failWith(http.StatusTooManyRequests)

	second, err := NewWithCache(host, cache).Tags(image)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, cache.Stats().Stale)
	assert.Contains(t, logs.String(), "using cached data")
}

// Without an entry there is nothing to fall back on, and the failure has to
// reach the caller exactly as it did before the cache existed.
func TestRateLimitWithoutAnEntryStaysAnError(t *testing.T) {
	t.Setenv("CCU_CACHE_DIR", t.TempDir())

	requests := newCounter()
	host := tagServer(t, requests)
	requests.failWith(http.StatusTooManyRequests)

	_, err := NewWithCache(host, NewCache(CacheOptions{TTL: time.Hour})).Tags(host + "/library/myimage")

	assert.Error(t, err)
}

// A half-written or corrupted file is a miss. Anything else would turn a cache
// — an optimisation — into a way for a run to fail.
func TestCorruptEntryIsAMissNotACrash(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CCU_CACHE_DIR", dir)

	requests := newCounter()
	host := tagServer(t, requests)
	image := host + "/library/myimage"
	cache := NewCache(CacheOptions{TTL: time.Hour})

	want, err := NewWithCache(host, cache).Tags(image)
	require.NoError(t, err)

	corruptEntries(t, dir)

	got, err := NewWithCache(host, NewCache(CacheOptions{TTL: time.Hour})).Tags(image)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, 2, requests.count("tags"))
}

// Created and SourceURL are two questions about one config blob, and the blob is
// addressed by digest, so neither a second question nor a second run refetches
// it. Only the tag's digest is resolved again, which is the cheap part.
func TestConfigBlobIsFetchedOncePerDigest(t *testing.T) {
	t.Setenv("CCU_CACHE_DIR", t.TempDir())

	requests := newCounter()
	host := tagServer(t, requests)
	image := host + "/library/myimage:1.2.4"
	cache := NewCache(CacheOptions{TTL: time.Hour})

	client := NewWithCache(host, cache)
	created, err := client.Created(image)
	require.NoError(t, err)
	assert.Equal(t, 2026, created.Year())

	source, err := client.SourceURL(image)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/owner/repo", source)

	// A second run: its own memory, the same directory.
	source, err = NewWithCache(host, NewCache(CacheOptions{TTL: time.Hour})).SourceURL(image)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/owner/repo", source)

	assert.Equal(t, 1, requests.count("blob"))
}

// A digest-addressed answer survives an expired mutable TTL: what an image was
// built from cannot change, only which digest the tag points at can.
func TestExpiredTTLStillReusesTheConfigBlob(t *testing.T) {
	t.Setenv("CCU_CACHE_DIR", t.TempDir())

	requests := newCounter()
	host := tagServer(t, requests)
	image := host + "/library/myimage:1.2.4"

	for range 2 {
		_, err := NewWithCache(host, NewCache(CacheOptions{TTL: time.Nanosecond})).SourceURL(image)
		require.NoError(t, err)
	}

	assert.Equal(t, 1, requests.count("blob"))
}

// CCU_NO_CACHE is for a runner where a cache directory is thrown away anyway:
// nothing is read, nothing is written, and there is no directory to report.
func TestNoCacheEnvReadsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CCU_CACHE_DIR", dir)
	t.Setenv("CCU_NO_CACHE", "1")

	requests := newCounter()
	host := tagServer(t, requests)
	image := host + "/library/myimage"
	cache := NewCache(CacheOptions{TTL: time.Hour})

	for range 2 {
		_, err := NewWithCache(host, cache).Tags(image)
		require.NoError(t, err)
	}

	assert.Equal(t, 2, requests.count("tags"))
	assert.Empty(t, cache.Dir())
	assert.Equal(t, CacheStats{}, cache.Stats())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCacheDirRespectsTheOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CCU_CACHE_DIR", dir)
	assert.Equal(t, dir, CacheDir())

	t.Setenv("CCU_CACHE_DIR", "")
	assert.True(t, strings.HasSuffix(CacheDir(), string(os.PathSeparator)+"ccu"), "want a ccu directory, got %q", CacheDir())
}

// Pruning is what keeps the directory from growing with every tag any stack
// ever pointed at.
func TestPruneDropsEntriesPastTheImmutableTTL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CCU_CACHE_DIR", dir)

	cache := NewCache(CacheOptions{TTL: time.Hour})
	cache.write(kindTags, "old/entry", []string{"1.0.0"})
	cache.write(kindTags, "fresh/entry", []string{"1.0.0"})

	old := cache.path(kindTags, "old/entry")
	require.NoError(t, os.Chtimes(old, time.Now(), time.Now().Add(-2*ImmutableTTL)))

	cache.Prune()

	assert.NoFileExists(t, old)
	assert.FileExists(t, cache.path(kindTags, "fresh/entry"))
}

func TestSummary(t *testing.T) {
	t.Setenv("CCU_CACHE_DIR", t.TempDir())

	cache := NewCache(CacheOptions{TTL: 10 * time.Minute})
	assert.Empty(t, cache.Summary(), "a run that fetched everything has nothing to report")

	cache.countLookup()
	cache.countHit(false)
	assert.Contains(t, cache.Summary(), "served 1 of 1 lookups from cache")
	assert.Contains(t, cache.Summary(), "10m0s ttl; -refresh to bypass")
}

// corruptEntries overwrites every entry with something that is not an entry,
// which is what a run interrupted mid-write used to be able to leave behind.
func corruptEntries(t *testing.T, dir string) {
	t.Helper()

	found := 0
	require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		found++
		return os.WriteFile(path, []byte("{not json"), 0o644)
	}))
	require.NotZero(t, found, "no cache entry was written at all")
}
