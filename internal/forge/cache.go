package forge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// diskCache keeps one release list per repository across runs. It follows the
// registry cache's rules — hashed file names, whole entries moved into place,
// every failure a miss — but not its type: release notes are a different kind
// of answer with their own lifetime, and forge must not depend on registry.
type diskCache struct {
	dir string // "" disables it
}

// diskEntry is one repository's list as it sits on disk. Key is stored beside
// the releases because the file name is a hash: without it a corrupt or
// colliding entry could not be told apart from the one that belongs there.
type diskEntry struct {
	Key      string    `json:"key"`
	Fetched  time.Time `json:"fetched"`
	Releases []Release `json:"releases"`
}

// path is the file one repository lives in. Hashed rather than escaped because
// a GitLab path may nest arbitrarily deep, and a flat directory of fixed-length
// names is easier to prune than a tree mirroring every forge's namespaces.
func (d diskCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(d.dir, hex.EncodeToString(sum[:])+".json")
}

// read returns the entry for a key. Missing, unreadable, half-written or
// foreign files are all a miss: the cache only ever saves a request.
func (d diskCache) read(key string) (diskEntry, bool) {
	if d.dir == "" {
		return diskEntry{}, false
	}
	body, err := os.ReadFile(d.path(key))
	if err != nil {
		return diskEntry{}, false
	}

	var e diskEntry
	if err := json.Unmarshal(body, &e); err != nil || e.Key != key || e.Fetched.IsZero() {
		slog.Debug("Ignoring an unreadable release-notes cache entry", "path", d.path(key))
		return diskEntry{}, false
	}
	return e, true
}

// write stores one list. Written to a temporary file in the same directory and
// renamed over the target, so a reader — another goroutine, or another ccu
// running at the same time — never observes a partial entry.
func (d diskCache) write(key string, releases []Release) {
	if d.dir == "" {
		return
	}
	body, err := json.Marshal(diskEntry{Key: key, Fetched: time.Now(), Releases: releases})
	if err != nil {
		return
	}

	target := d.path(key)
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
		slog.Debug("Failed creating the release-notes cache directory", "path", d.dir, "error", err)
		return
	}

	tmp, err := os.CreateTemp(d.dir, ".tmp-*")
	if err != nil {
		slog.Debug("Failed writing a release-notes cache entry", "path", target, "error", err)
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
		slog.Debug("Failed replacing a release-notes cache entry", "path", target, "error", err)
	}
}
