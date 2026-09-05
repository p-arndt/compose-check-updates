package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/regclient/regclient/types/errs"
)

// ImmutableTTL bounds how long a digest-addressed answer is kept. The answer
// itself cannot go stale — a digest names its content — so this is only a
// garbage collector: without an upper bound the cache directory would keep
// every tag any stack ever pointed at.
const ImmutableTTL = 30 * 24 * time.Hour

// Cache kinds, one subdirectory each, so pruning and inspecting the cache can
// tell the two policies apart by looking at the path.
const (
	kindTags   = "tags"   // repository -> tag list (mutable)
	kindDigest = "digest" // reference -> manifest digest (mutable)
	kindImage  = "image"  // repository@digest -> config blob answers (immutable)
)

// CacheOptions is what a run asks of the cache.
type CacheOptions struct {
	// TTL is how long a mutable answer — a tag list, a tag's digest — may be
	// reused. Zero means nothing is read back, which is how `cache_ttl: 0` turns
	// the cache into a write-only one.
	TTL time.Duration

	// Refresh ignores every entry on disk for this run while still writing the
	// answers it fetches. It is the escape hatch for "a release went out a
	// minute ago and I want it now".
	Refresh bool

	// Dir overrides where entries are kept. Empty means CacheDir().
	Dir string
}

// Cache is the on-disk store of registry answers, shared by every client of one
// run: the scanner builds a client per compose file, so an image in two stacks
// would otherwise be fetched twice. Entries are files, written whole and moved
// into place, so concurrent workers and even concurrent ccu processes only ever
// see a complete entry or none.
type Cache struct {
	dir      string
	ttl      time.Duration
	refresh  bool
	disabled bool

	// Counters only, never read back to decide anything, so they need no lock of
	// their own next to the workers that bump them.
	lookups atomic.Int64
	hits    atomic.Int64
	stale   atomic.Int64

	// locks serializes the lookups of one key. The scanner checks compose files
	// concurrently, so two workers reach the same image at the same moment and
	// would both miss an entry neither has written yet; the second one waits and
	// then finds what the first just wrote.
	locks sync.Map // entryKey -> *sync.Mutex
}

// entryKey is how a kind and a key make one identity. The NUL separator cannot
// occur in either half, so two different pairs can never collide.
func entryKey(kind, key string) string { return kind + "\x00" + key }

// lockFor returns the mutex guarding one key, creating it on first use. The
// locks are never reaped: a run asks about a bounded set of images, and a
// released mutex costs a pointer.
func (c *Cache) lockFor(kind, key string) *sync.Mutex {
	lock, _ := c.locks.LoadOrStore(entryKey(kind, key), &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// CacheStats is what a finished run can say about its cache use.
type CacheStats struct {
	Lookups int // answers that could have come from the cache
	Hits    int // answers that did, stale ones included
	Stale   int // answers served past their TTL because the registry failed
}

// NewCache returns the cache for a run. It never fails: a cache that cannot be
// written to is one ccu does without, not a reason to refuse to check images.
func NewCache(opts CacheOptions) *Cache {
	c := &Cache{ttl: opts.TTL, refresh: opts.Refresh, dir: opts.Dir}

	if c.dir == "" {
		c.dir = CacheDir()
	}
	// No home to write into, or a CI runner that said the cache is pointless.
	if c.dir == "" || cacheDisabledByEnv() {
		c.disabled = true
	}

	return c
}

// CacheDir is where entries are kept: the per-user cache directory, which is
// ~/.cache/ccu on Linux, ~/Library/Caches/ccu on macOS and %LocalAppData%\ccu
// on Windows. CCU_CACHE_DIR overrides it — the tests' way in, and an escape
// hatch for anyone who keeps their caches elsewhere.
func CacheDir() string {
	if dir := os.Getenv("CCU_CACHE_DIR"); dir != "" {
		return dir
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ccu")
}

// cacheDisabledByEnv reads CCU_NO_CACHE. Any value but the ones that plainly
// mean "off" counts: a runner that exports CCU_NO_CACHE=1 and one that exports
// CCU_NO_CACHE=true mean the same thing.
func cacheDisabledByEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CCU_NO_CACHE"))) {
	case "", "0", "false", "no":
		return false
	default:
		return true
	}
}

// Dir is where this cache writes, empty when it writes nowhere.
func (c *Cache) Dir() string {
	if c == nil || c.disabled {
		return ""
	}
	return c.dir
}

// TTL is the lifetime of a mutable entry for this run.
func (c *Cache) TTL() time.Duration {
	if c == nil {
		return 0
	}
	return c.ttl
}

// Stats reports what the run got out of the cache.
func (c *Cache) Stats() CacheStats {
	if c == nil {
		return CacheStats{}
	}
	return CacheStats{
		Lookups: int(c.lookups.Load()),
		Hits:    int(c.hits.Load()),
		Stale:   int(c.stale.Load()),
	}
}

// Summary is the one line a finished run prints when the cache spared it work,
// and "" when it did not — a run that fetched everything has nothing to report.
func (c *Cache) Summary() string {
	stats := c.Stats()
	if stats.Hits == 0 {
		return ""
	}
	return fmt.Sprintf("served %d of %d lookups from cache (%s, %s ttl; -refresh to bypass)",
		stats.Hits, stats.Lookups, shortenHome(c.dir), c.ttl)
}

// entry is one cached answer as it sits on disk. Key is stored beside the value
// because the file name is a hash: without it a corrupt or colliding entry
// could not be told apart from the answer that belongs there.
type entry struct {
	Key     string          `json:"key"`
	Fetched time.Time       `json:"fetched"`
	Value   json.RawMessage `json:"value"`
}

func (e entry) age() time.Duration { return time.Since(e.Fetched) }

// path is the file one key lives in. Hashed rather than escaped, because a
// reference carries slashes and colons that no filesystem takes as-is.
func (c *Cache) path(kind, key string) string {
	sum := sha256.Sum256([]byte(entryKey(kind, key)))
	return filepath.Join(c.dir, kind, hex.EncodeToString(sum[:])+".json")
}

// read returns the entry for a key. A file that is missing, unreadable, half
// written or holds another key at all is a miss: a cache is an optimisation,
// and there is nothing here worth failing a run over.
func (c *Cache) read(kind, key string) (entry, bool) {
	body, err := os.ReadFile(c.path(kind, key))
	if err != nil {
		return entry{}, false
	}

	var e entry
	if err := json.Unmarshal(body, &e); err != nil || e.Key != key || e.Fetched.IsZero() {
		return entry{}, false
	}
	return e, true
}

// write stores one answer. Written to a temporary file in the same directory
// and renamed over the target, so a reader — another worker, or another ccu
// running at the same time — never observes a partial entry.
func (c *Cache) write(kind, key string, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	body, err := json.Marshal(entry{Key: key, Fetched: time.Now(), Value: encoded})
	if err != nil {
		return
	}

	target := c.path(kind, key)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		slog.Debug("Failed creating the cache directory", "path", filepath.Dir(target), "error", err)
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		slog.Debug("Failed writing a cache entry", "path", target, "error", err)
		return
	}
	name := tmp.Name()

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return
	}
	if err := os.Rename(name, target); err != nil {
		os.Remove(name)
		slog.Debug("Failed replacing a cache entry", "path", target, "error", err)
	}
}

// Prune drops entries older than the immutable TTL, so the directory cannot
// grow forever as images come and go. Called at the end of a run and allowed to
// fail silently: it is housekeeping, not part of the answer.
func (c *Cache) Prune() {
	if c == nil || c.disabled {
		return
	}

	cutoff := time.Now().Add(-ImmutableTTL)
	_ = filepath.WalkDir(c.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry is one to skip, not to fail the run over
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(cutoff) {
			return nil //nolint:nilerr // same: only entries that are provably old are removed
		}
		_ = os.Remove(path)
		return nil
	})
}

// countHit records a served entry, stale ones separately: the summary reports
// both numbers.
func (c *Cache) countHit(stale bool) {
	c.hits.Add(1)
	if stale {
		c.stale.Add(1)
	}
}

// cached answers from the cache when it can, and from fetch when it cannot.
// label names the image in the warning a stale answer earns.
//
// The two TTLs the callers pass are the whole point of the cache: a tag list
// and a tag's digest are mutable, so they are kept for minutes — long enough
// that reopening the TUI is free, short enough that a release published a
// minute ago is still seen by the next run. What a *digest* was built from
// cannot change at all, so it is kept for a month.
func cached[T any](c *Cache, kind, key, label string, ttl time.Duration, fetch func() (T, error)) (T, error) {
	if c == nil || c.disabled {
		return fetch()
	}

	c.lookups.Add(1)

	lock := c.lockFor(kind, key)
	lock.Lock()
	defer lock.Unlock()

	stored, found := c.read(kind, key)
	// A cached entry is only trusted while it is fresh and the run did not ask
	// for -refresh. Serving expired data next to a registry that would have
	// answered is the one thing a cache must never do here.
	if found && !c.refresh && ttl > 0 && stored.age() <= ttl {
		if value, ok := decode[T](stored); ok {
			c.countHit(false)
			return value, nil
		}
	}

	value, err := fetch()
	if err == nil {
		c.write(kind, key, value)
		return value, nil
	}

	// Stale-if-error, and only on error: the registry did not answer, so the
	// choice is between month-old-at-worst data and no answer at all. A 404 is
	// an answer, though — the tag really is gone — so it is not covered.
	if found && ttl > 0 && servableStale(err) {
		if value, ok := decode[T](stored); ok {
			c.countHit(true)
			slog.Warn("Registry lookup failed, using cached data",
				"image", label, "age", stored.age().Round(time.Second), "error", err)
			return value, nil
		}
	}

	return value, err
}

func decode[T any](e entry) (T, bool) {
	var value T
	if err := json.Unmarshal(e.Value, &value); err != nil {
		return value, false
	}
	return value, true
}

// servableStale reports whether a failure is the kind an old answer is better
// than: a rate limit, a timeout, a registry that is down, a laptop with no
// network. A 404 is excluded because it is the registry telling the truth about
// a repository that no longer has this tag.
func servableStale(err error) bool {
	return !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrAPINotFound)
}

// shortenHome writes a path the way a user would: the home directory as "~".
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	rest, ok := strings.CutPrefix(path, home)
	if !ok {
		return path
	}
	return "~" + rest
}
