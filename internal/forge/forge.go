// Package forge reads release notes from the code forges an image's source
// label points at. It sits beside registry: registry answers what an image is,
// forge answers what changed in it.
package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// Release is one published release on a forge.
type Release struct {
	Tag        string
	Name       string
	Body       string // markdown, as the forge stores it
	URL        string // the release page
	Published  time.Time
	Prerelease bool
}

var (
	// ErrUnsupported: the source is not on a forge ccu can read releases from.
	ErrUnsupported = errors.New("forge not supported")
	// ErrRateLimited: the forge refused for rate limiting; a token lifts it.
	ErrRateLimited = errors.New("forge rate limit reached")
)

// errNotFound is a 404 from a release list. It is kept for the run like an
// answer, because it is one: a private or missing repository does not appear
// between two keypresses in the TUI.
var errNotFound = errors.New("repository not found")

// Options configures a Client. Zero values are usable.
type Options struct {
	HTTP        *http.Client
	GitHubToken string // "" → unauthenticated
	GitLabToken string
	// CacheDir is where answers are kept; "" disables the disk cache.
	CacheDir string
	TTL      time.Duration
	Refresh  bool
	// GitHubAPI and GitLabAPI override the API base URLs, for tests.
	GitHubAPI string
	GitLabAPI string
}

// Client fetches release lists. Safe for concurrent use.
type Client struct {
	opts Options

	// memo holds this run's answers per repository, errors that are answers
	// (a 404, a rate limit) included. The TUI asks again every time the cursor
	// comes back to an update, and a forge that allows 60 unauthenticated
	// requests an hour cannot be asked twice for the same list.
	memo sync.Map // repo key -> memoEntry

	// locks serializes the fetches of one repository. Several images in a stack
	// often share one source repository, and the pane may ask for all of them
	// at once; without this every one would miss the memo and hit the forge.
	locks sync.Map // repo key -> *sync.Mutex
}

type memoEntry struct {
	releases []Release
	err      error
}

// defaultTimeout bounds one release-list request when the caller brings no
// client of its own. A release list is a single JSON document; a forge that
// has not sent it in this long is not going to, and the pane waiting on it is
// better off saying so.
const defaultTimeout = 20 * time.Second

// New builds a Client.
func New(opts Options) *Client {
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: defaultTimeout}
	}
	if opts.GitHubAPI == "" {
		opts.GitHubAPI = defaultGitHubAPI
	}
	if opts.GitLabAPI == "" {
		opts.GitLabAPI = defaultGitLabAPI
	}
	return &Client{opts: opts}
}

// TokensFromEnv reads the forge tokens the usual tooling already exports.
// GITHUB_TOKEN wins over GH_TOKEN because it is what CI runners set, while
// GH_TOKEN is the gh CLI's own; either lifts GitHub's limit the same way.
func TokensFromEnv() (github, gitlab string) {
	github = os.Getenv("GITHUB_TOKEN")
	if github == "" {
		github = os.Getenv("GH_TOKEN")
	}
	return github, os.Getenv("GITLAB_TOKEN")
}

// Releases lists the releases of the repository sourceURL names, newest first.
// sourceURL is a normalized repository URL as check.Update.SourceURL holds it.
//
// Drafts are never returned. A repository that exists but has published
// nothing answers an empty list and no error.
func (c *Client) Releases(ctx context.Context, sourceURL string) ([]Release, error) {
	repo, err := parseRepo(sourceURL)
	if err != nil {
		return nil, err
	}

	if cached, ok := c.memo.Load(repo.key()); ok {
		entry := cached.(memoEntry)
		return entry.releases, entry.err
	}

	lock, _ := c.locks.LoadOrStore(repo.key(), &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	// Checked again under the lock: the caller that held it may have just
	// filled the memo this one was waiting on.
	if cached, ok := c.memo.Load(repo.key()); ok {
		entry := cached.(memoEntry)
		return entry.releases, entry.err
	}

	releases, err := c.cachedFetch(ctx, repo)
	if err == nil || errors.Is(err, errNotFound) || errors.Is(err, ErrRateLimited) {
		c.memo.Store(repo.key(), memoEntry{releases: releases, err: err})
	}
	return releases, err
}

// cachedFetch answers from the disk cache when it holds a fresh entry and asks
// the forge otherwise.
func (c *Client) cachedFetch(ctx context.Context, repo repository) ([]Release, error) {
	disk := c.diskCache()

	stored, found := disk.read(repo.key())
	// TTL zero reads nothing back, the same rule the registry cache follows for
	// `cache_ttl: 0`: the entry is still written, just never trusted.
	if found && !c.opts.Refresh && c.opts.TTL > 0 && time.Since(stored.Fetched) <= c.opts.TTL {
		return stored.Releases, nil
	}

	releases, err := c.fetch(ctx, repo)
	if err == nil {
		disk.write(repo.key(), releases)
		return releases, nil
	}

	// Stale-if-error: a rate limit or a forge that is down leaves the choice
	// between notes that are a little old and no notes at all. A 404 is the
	// forge telling the truth and a cancelled context is the caller giving up,
	// so neither is papered over.
	if found && !errors.Is(err, errNotFound) && ctx.Err() == nil {
		return stored.Releases, nil
	}
	return nil, err
}

func (c *Client) fetch(ctx context.Context, repo repository) ([]Release, error) {
	switch repo.forge {
	case forgeGitHub:
		return c.fetchGitHub(ctx, repo)
	case forgeGitLab:
		return c.fetchGitLab(ctx, repo)
	}
	return nil, fmt.Errorf("%s: %w", repo.display(), ErrUnsupported)
}

func (c *Client) diskCache() diskCache {
	return diskCache{dir: c.opts.CacheDir}
}
