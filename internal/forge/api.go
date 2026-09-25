package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGitHubAPI = "https://api.github.com"
	defaultGitLabAPI = "https://gitlab.com/api/v4"
)

// perPage is the one page ccu asks for, and the most either forge serves in
// one. Both list newest first, and the notes a user wants are the ones between
// the tag they run and the latest; a hundred releases back is far past any
// update ccu offers. Following the Link header for the rest would spend the
// unauthenticated budget of 60 requests an hour on history nobody scrolls to.
const perPage = 100

// maxBody caps what is read of one response. A hundred releases with long
// notes run to a few megabytes; anything far past that is not a release list.
const maxBody = 32 << 20

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
}

func (c *Client) fetchGitHub(ctx context.Context, repo repository) ([]Release, error) {
	endpoint := strings.TrimSuffix(c.opts.GitHubAPI, "/") + "/repos/" + repo.path +
		"/releases?per_page=" + strconv.Itoa(perPage)

	headers := http.Header{}
	headers.Set("Accept", "application/vnd.github+json")
	headers.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.opts.GitHubToken != "" {
		headers.Set("Authorization", "Bearer "+c.opts.GitHubToken)
	}

	var raw []githubRelease
	if err := c.getJSON(ctx, repo, endpoint, headers, &raw); err != nil {
		return nil, err
	}

	releases := make([]Release, 0, len(raw))
	for _, r := range raw {
		// A draft is visible to a token with push access and to nobody else; it
		// is not something the image the user runs could have been built from.
		if r.Draft {
			continue
		}
		releases = append(releases, Release{
			Tag:        r.TagName,
			Name:       r.Name,
			Body:       r.Body,
			URL:        r.HTMLURL,
			Published:  r.PublishedAt,
			Prerelease: r.Prerelease,
		})
	}
	return newestFirst(releases), nil
}

type gitlabRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ReleasedAt  time.Time `json:"released_at"`
	Upcoming    bool      `json:"upcoming_release"`
	Links       struct {
		Self string `json:"self"`
	} `json:"_links"`
}

func (c *Client) fetchGitLab(ctx context.Context, repo repository) ([]Release, error) {
	// The project is addressed by its whole path in one segment, slashes
	// escaped, so that "group/sub/project" is not read as three.
	endpoint := strings.TrimSuffix(c.opts.GitLabAPI, "/") + "/projects/" + url.PathEscape(repo.path) +
		"/releases?per_page=" + strconv.Itoa(perPage)

	headers := http.Header{}
	headers.Set("Accept", "application/json")
	if c.opts.GitLabToken != "" {
		headers.Set("PRIVATE-TOKEN", c.opts.GitLabToken)
	}

	var raw []gitlabRelease
	if err := c.getJSON(ctx, repo, endpoint, headers, &raw); err != nil {
		return nil, err
	}

	releases := make([]Release, 0, len(raw))
	for _, r := range raw {
		releases = append(releases, Release{
			Tag:       r.TagName,
			Name:      r.Name,
			Body:      r.Description,
			URL:       r.Links.Self,
			Published: r.ReleasedAt,
			// GitLab has no prerelease flag. An upcoming release — one dated in
			// the future — is the nearest thing: announced, not yet out.
			Prerelease: r.Upcoming,
		})
	}
	return newestFirst(releases), nil
}

// newestFirst orders by publication date. Both forges already list newest
// first, but by creation date, and a release drafted early and published late
// would otherwise sit below the ones that came out before it. Undated entries
// sink to the end rather than claiming to be the newest.
func newestFirst(releases []Release) []Release {
	slices.SortStableFunc(releases, func(a, b Release) int { return b.Published.Compare(a.Published) })
	return releases
}

// getJSON performs one GET and decodes the answer into out, turning the status
// codes that mean something to a user into errors that say what to do.
func (c *Client) getJSON(ctx context.Context, repo repository, endpoint string, headers http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", repo.display(), err)
	}
	req.Header = headers
	req.Header.Set("User-Agent", "ccu (compose-check-updates)")

	resp, err := c.opts.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s: fetching releases: %w", repo.display(), err)
	}
	defer resp.Body.Close()

	if err := statusError(repo, resp); err != nil {
		return err
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return fmt.Errorf("%s: reading releases: %w", repo.display(), err)
	}
	return nil
}

func statusError(repo repository, resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case isRateLimited(resp):
		return rateLimitError(repo, resp, headersHaveToken(resp.Request))
	case resp.StatusCode == http.StatusNotFound:
		// GitHub answers 404 rather than 403 for a private repository, so as not
		// to confirm it exists; a public one without releases answers an empty
		// list, not this.
		return fmt.Errorf("%s: %w (private, renamed or deleted)", repo.display(), errNotFound)
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%s: the forge rejected the token (%s)", repo.display(), resp.Status)
	}
	return fmt.Errorf("%s: unexpected answer from the forge: %s", repo.display(), resp.Status)
}

// isRateLimited tells a rate limit from any other refusal. GitHub signals its
// primary limit with a 403 and X-RateLimit-Remaining: 0, and its secondary
// ("abuse") limit with a 403 and Retry-After; GitLab uses 429. A bare 403
// without either header is a permission problem, not something a token of the
// same scope would lift.
func isRateLimited(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""
	}
	return false
}

// headersHaveToken reports whether the refused request was authenticated,
// which decides whether "set a token" is advice or noise.
func headersHaveToken(req *http.Request) bool {
	return req != nil && (req.Header.Get("Authorization") != "" || req.Header.Get("PRIVATE-TOKEN") != "")
}

func rateLimitError(repo repository, resp *http.Response, authenticated bool) error {
	hint := "set GITHUB_TOKEN or GH_TOKEN to lift it"
	if repo.forge == forgeGitLab {
		hint = "set GITLAB_TOKEN to lift it"
	}
	if authenticated {
		hint = "the token's quota is spent too"
	}

	when := ""
	// GitHub names the reset X-RateLimit-Reset, GitLab RateLimit-Reset; both
	// hold a Unix timestamp.
	for _, name := range []string{"X-RateLimit-Reset", "RateLimit-Reset"} {
		if seconds, err := strconv.ParseInt(resp.Header.Get(name), 10, 64); err == nil && seconds > 0 {
			when = ", resets at " + time.Unix(seconds, 0).Format("15:04")
			break
		}
	}

	return fmt.Errorf("%s: %w%s; %s", repo.display(), ErrRateLimited, when, hint)
}
