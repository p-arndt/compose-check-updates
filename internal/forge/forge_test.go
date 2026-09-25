package forge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeForge is one test's forge API. It counts requests, because a cache is
// only proven by the requests it saved, and records what the last one carried.
type fakeForge struct {
	server *httptest.Server
	hits   atomic.Int64

	mu       sync.Mutex
	lastReq  *http.Request
	status   int
	headers  map[string]string
	body     string
	released chan struct{} // when set, every answer waits on it
}

func newFakeForge(t *testing.T, body string) *fakeForge {
	t.Helper()
	f := &fakeForge{status: http.StatusOK, body: body}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.mu.Lock()
		f.lastReq = r.Clone(context.Background())
		status, headers, body, gate := f.status, f.headers, f.body, f.released
		f.mu.Unlock()

		if gate != nil {
			<-gate
		}
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeForge) answer(status int, headers map[string]string, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.headers, f.body = status, headers, body
}

func (f *fakeForge) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

// githubBody lists releases in creation order, which is not publication order:
// v1.1.0 was drafted before v1.2.0 but published after it.
const githubBody = `[
  {"tag_name":"v2.0.0-rc.1","name":"RC","body":"rc notes","html_url":"https://github.com/o/r/releases/tag/v2.0.0-rc.1","published_at":"2026-03-01T00:00:00Z","prerelease":true,"draft":false},
  {"tag_name":"v3.0.0","name":"Draft","body":"secret","html_url":"https://github.com/o/r/releases/tag/untagged","published_at":null,"prerelease":false,"draft":true},
  {"tag_name":"v1.2.0","name":"One two","body":"## Fixed\n- a bug","html_url":"https://github.com/o/r/releases/tag/v1.2.0","published_at":"2026-01-01T00:00:00Z","prerelease":false,"draft":false},
  {"tag_name":"v1.1.0","name":"","body":"late","html_url":"https://github.com/o/r/releases/tag/v1.1.0","published_at":"2026-02-01T00:00:00Z","prerelease":false,"draft":false}
]`

const gitlabBody = `[
  {"tag_name":"v1.3.0","name":"Soon","description":"upcoming","released_at":"2027-01-01T00:00:00Z","upcoming_release":true,"_links":{"self":"https://gitlab.com/g/sub/p/-/releases/v1.3.0"}},
  {"tag_name":"v1.2.0","name":"One two","description":"notes","released_at":"2026-01-01T00:00:00Z","upcoming_release":false,"_links":{"self":"https://gitlab.com/g/sub/p/-/releases/v1.2.0"}}
]`

func githubClient(f *fakeForge, opts Options) *Client {
	opts.GitHubAPI = f.server.URL
	return New(opts)
}

func gitlabClient(f *fakeForge, opts Options) *Client {
	opts.GitLabAPI = f.server.URL + "/api/v4"
	return New(opts)
}

func TestGitHubReleases(t *testing.T) {
	f := newFakeForge(t, githubBody)
	c := githubClient(f, Options{GitHubToken: "secret-token"})

	releases, err := c.Releases(context.Background(), "https://github.com/o/r")
	require.NoError(t, err)

	req := f.last()
	assert.Equal(t, "/repos/o/r/releases", req.URL.Path)
	assert.Equal(t, "100", req.URL.Query().Get("per_page"))
	assert.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
	assert.Equal(t, "2022-11-28", req.Header.Get("X-GitHub-Api-Version"))
	assert.Equal(t, "Bearer secret-token", req.Header.Get("Authorization"))

	tags := make([]string, 0, len(releases))
	for _, r := range releases {
		tags = append(tags, r.Tag)
	}
	assert.Equal(t, []string{"v2.0.0-rc.1", "v1.1.0", "v1.2.0"}, tags, "drafts dropped, newest published first")

	rc := releases[0]
	assert.True(t, rc.Prerelease)
	assert.Equal(t, "RC", rc.Name)
	assert.Equal(t, "rc notes", rc.Body)
	assert.Equal(t, "https://github.com/o/r/releases/tag/v2.0.0-rc.1", rc.URL)
	assert.Equal(t, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), rc.Published.UTC())
}

func TestGitHubUnauthenticatedSendsNoAuthorization(t *testing.T) {
	f := newFakeForge(t, `[]`)
	releases, err := githubClient(f, Options{}).Releases(context.Background(), "https://github.com/o/r")
	require.NoError(t, err)
	assert.Empty(t, releases)
	assert.Empty(t, f.last().Header.Get("Authorization"))
}

func TestGitHubSourcePointingIntoSubdirectory(t *testing.T) {
	f := newFakeForge(t, `[]`)
	_, err := githubClient(f, Options{}).Releases(context.Background(), "https://www.github.com/o/r.git/tree/main/docker")
	require.NoError(t, err)
	assert.Equal(t, "/repos/o/r/releases", f.last().URL.Path)
}

func TestGitLabReleasesEscapeSubgroups(t *testing.T) {
	f := newFakeForge(t, gitlabBody)
	c := gitlabClient(f, Options{GitLabToken: "gl-token"})

	releases, err := c.Releases(context.Background(), "https://gitlab.com/g/sub/p/-/tree/main/docker")
	require.NoError(t, err)

	req := f.last()
	assert.Equal(t, "/api/v4/projects/g%2Fsub%2Fp/releases", req.URL.EscapedPath())
	assert.Equal(t, "100", req.URL.Query().Get("per_page"))
	assert.Equal(t, "gl-token", req.Header.Get("PRIVATE-TOKEN"))

	require.Len(t, releases, 2)
	assert.Equal(t, Release{
		Tag:        "v1.3.0",
		Name:       "Soon",
		Body:       "upcoming",
		URL:        "https://gitlab.com/g/sub/p/-/releases/v1.3.0",
		Published:  time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Prerelease: true,
	}, normalizeTimes(releases[0]))
	assert.False(t, releases[1].Prerelease)
}

func normalizeTimes(r Release) Release {
	r.Published = r.Published.UTC()
	return r
}

func TestParseRepo(t *testing.T) {
	cases := map[string]repository{
		"https://github.com/o/r":                     {forgeGitHub, "o/r"},
		"https://github.com/o/r/tree/main/sub":       {forgeGitHub, "o/r"},
		"https://GitHub.com/o/r.git":                 {forgeGitHub, "o/r"},
		"https://gitlab.com/g/p":                     {forgeGitLab, "g/p"},
		"https://www.gitlab.com/g/a/b/p/-/blob/x.go": {forgeGitLab, "g/a/b/p"},
		"https://gitlab.com/g/p/tree/master":         {forgeGitLab, "g/p"},
		"https://gitlab.com/tree/p":                  {forgeGitLab, "tree/p"},
	}
	for source, want := range cases {
		got, err := parseRepo(source)
		require.NoError(t, err, source)
		assert.Equal(t, want, got, source)
	}

	for _, source := range []string{
		"https://codeberg.org/o/r",
		"https://gitlab.example.com/g/p",
		"https://github.com/o",
		"https://github.com",
		"not a url",
		"",
	} {
		_, err := parseRepo(source)
		assert.ErrorIs(t, err, ErrUnsupported, source)
	}
}

func TestUnsupportedHostMakesNoRequest(t *testing.T) {
	f := newFakeForge(t, `[]`)
	_, err := githubClient(f, Options{}).Releases(context.Background(), "https://codeberg.org/o/r")
	require.ErrorIs(t, err, ErrUnsupported)
	assert.Zero(t, f.hits.Load())
}

func TestRateLimit(t *testing.T) {
	t.Run("github primary limit", func(t *testing.T) {
		f := newFakeForge(t, "")
		f.answer(http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1790000000"}, `{"message":"API rate limit exceeded"}`)
		_, err := githubClient(f, Options{}).Releases(context.Background(), "https://github.com/o/r")
		require.ErrorIs(t, err, ErrRateLimited)
		assert.Contains(t, err.Error(), "GITHUB_TOKEN")
		assert.Contains(t, err.Error(), "GH_TOKEN")
	})
	t.Run("github with a token says so", func(t *testing.T) {
		f := newFakeForge(t, "")
		f.answer(http.StatusTooManyRequests, nil, `{}`)
		_, err := githubClient(f, Options{GitHubToken: "secret-token"}).Releases(context.Background(), "https://github.com/o/r")
		require.ErrorIs(t, err, ErrRateLimited)
		assert.NotContains(t, err.Error(), "secret-token")
		assert.NotContains(t, err.Error(), "GITHUB_TOKEN")
	})
	t.Run("gitlab 429", func(t *testing.T) {
		f := newFakeForge(t, "")
		f.answer(http.StatusTooManyRequests, nil, `{}`)
		_, err := gitlabClient(f, Options{}).Releases(context.Background(), "https://gitlab.com/g/p")
		require.ErrorIs(t, err, ErrRateLimited)
		assert.Contains(t, err.Error(), "GITLAB_TOKEN")
	})
	t.Run("plain 403 is not a rate limit", func(t *testing.T) {
		f := newFakeForge(t, "")
		f.answer(http.StatusForbidden, nil, `{}`)
		_, err := githubClient(f, Options{}).Releases(context.Background(), "https://github.com/o/r")
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrRateLimited)
	})
}

func TestNotFound(t *testing.T) {
	f := newFakeForge(t, "")
	f.answer(http.StatusNotFound, nil, `{"message":"Not Found"}`)
	c := githubClient(f, Options{})

	_, err := c.Releases(context.Background(), "https://github.com/o/private")
	require.ErrorIs(t, err, errNotFound)
	assert.Contains(t, err.Error(), "github.com/o/private")
	assert.Contains(t, err.Error(), "private")

	// A 404 is an answer; asking again in the same run costs nothing.
	_, err = c.Releases(context.Background(), "https://github.com/o/private")
	require.ErrorIs(t, err, errNotFound)
	assert.Equal(t, int64(1), f.hits.Load())
}

func TestTransientErrorIsRetried(t *testing.T) {
	f := newFakeForge(t, "")
	f.answer(http.StatusBadGateway, nil, ``)
	c := githubClient(f, Options{})

	_, err := c.Releases(context.Background(), "https://github.com/o/r")
	require.Error(t, err)

	f.answer(http.StatusOK, nil, githubBody)
	releases, err := c.Releases(context.Background(), "https://github.com/o/r")
	require.NoError(t, err)
	assert.Len(t, releases, 3)
	assert.Equal(t, int64(2), f.hits.Load())
}

func TestContextIsRespected(t *testing.T) {
	f := newFakeForge(t, githubBody)
	gate := make(chan struct{})
	f.mu.Lock()
	f.released = gate
	f.mu.Unlock()
	t.Cleanup(func() { close(gate) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := githubClient(f, Options{}).Releases(ctx, "https://github.com/o/r")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestMemoSharedAcrossCaseAndSpelling(t *testing.T) {
	f := newFakeForge(t, githubBody)
	c := githubClient(f, Options{})
	for _, source := range []string{"https://github.com/o/r", "https://github.com/O/R/tree/main", "https://www.github.com/o/r.git"} {
		_, err := c.Releases(context.Background(), source)
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), f.hits.Load())
}

func TestConcurrentCallsFetchOnce(t *testing.T) {
	f := newFakeForge(t, githubBody)
	gate := make(chan struct{})
	f.mu.Lock()
	f.released = gate
	f.mu.Unlock()
	c := githubClient(f, Options{})

	const callers = 16
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			releases, err := c.Releases(context.Background(), "https://github.com/o/r")
			if err == nil && len(releases) != 3 {
				err = errors.New("wrong release count")
			}
			errs <- err
		})
	}
	// Let every caller reach the lock before the first answer lands.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), f.hits.Load())
}

func TestDiskCache(t *testing.T) {
	const source = "https://github.com/o/r"

	t.Run("fresh entry is reused by the next run", func(t *testing.T) {
		f := newFakeForge(t, githubBody)
		dir := t.TempDir()

		first, err := githubClient(f, Options{CacheDir: dir, TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)

		f.answer(http.StatusInternalServerError, nil, ``)
		second, err := githubClient(f, Options{CacheDir: dir, TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)
		assert.Equal(t, int64(1), f.hits.Load())
		assert.Equal(t, len(first), len(second))
		assert.Equal(t, first[0].Tag, second[0].Tag)
		assert.True(t, first[0].Published.Equal(second[0].Published))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1, "one file per repository, no temp files left behind")
		assert.Equal(t, ".json", filepath.Ext(entries[0].Name()))
	})

	t.Run("expired entry is fetched again", func(t *testing.T) {
		f := newFakeForge(t, githubBody)
		dir := t.TempDir()
		_, err := githubClient(f, Options{CacheDir: dir, TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)
		ageEntries(t, dir, 2*time.Hour)

		_, err = githubClient(f, Options{CacheDir: dir, TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)
		assert.Equal(t, int64(2), f.hits.Load())
	})

	t.Run("refresh bypasses a fresh entry", func(t *testing.T) {
		f := newFakeForge(t, githubBody)
		dir := t.TempDir()
		_, err := githubClient(f, Options{CacheDir: dir, TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)

		f.answer(http.StatusOK, nil, `[]`)
		releases, err := githubClient(f, Options{CacheDir: dir, TTL: time.Hour, Refresh: true}).Releases(context.Background(), source)
		require.NoError(t, err)
		assert.Empty(t, releases)
		assert.Equal(t, int64(2), f.hits.Load())

		// And what refresh fetched replaced the entry.
		releases, err = githubClient(f, Options{CacheDir: dir, TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)
		assert.Empty(t, releases)
		assert.Equal(t, int64(2), f.hits.Load())
	})

	t.Run("stale entry covers a rate limit", func(t *testing.T) {
		f := newFakeForge(t, githubBody)
		dir := t.TempDir()
		_, err := githubClient(f, Options{CacheDir: dir, TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)
		ageEntries(t, dir, 2*time.Hour)

		f.answer(http.StatusTooManyRequests, nil, `{}`)
		releases, err := githubClient(f, Options{CacheDir: dir, TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)
		assert.Len(t, releases, 3)
	})

	t.Run("corrupt entry is a miss", func(t *testing.T) {
		f := newFakeForge(t, githubBody)
		dir := t.TempDir()
		c := githubClient(f, Options{CacheDir: dir, TTL: time.Hour})
		repo, err := parseRepo(source)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(c.diskCache().path(repo.key()), []byte("{half"), 0o644))

		_, err = c.Releases(context.Background(), source)
		require.NoError(t, err)
		assert.Equal(t, int64(1), f.hits.Load())
	})

	t.Run("unwritable directory is not fatal", func(t *testing.T) {
		f := newFakeForge(t, githubBody)
		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, nil, 0o644))

		releases, err := githubClient(f, Options{CacheDir: filepath.Join(blocker, "sub"), TTL: time.Hour}).Releases(context.Background(), source)
		require.NoError(t, err)
		assert.Len(t, releases, 3)
	})
}

// ageEntries rewrites every entry's fetch time, which is what the TTL is
// measured from; the file's mtime plays no part.
func ageEntries(t *testing.T, dir string, by time.Duration) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		require.NoError(t, err)

		var stored diskEntry
		require.NoError(t, json.Unmarshal(body, &stored))
		stored.Fetched = stored.Fetched.Add(-by)
		body, err = json.Marshal(stored)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, body, 0o644))
	}
}

func TestTokensFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "gh")
	t.Setenv("GITLAB_TOKEN", "gl")
	github, gitlab := TokensFromEnv()
	assert.Equal(t, "gh", github)
	assert.Equal(t, "gl", gitlab)

	t.Setenv("GITHUB_TOKEN", "primary")
	github, _ = TokensFromEnv()
	assert.Equal(t, "primary", github)
}
