package forge

import (
	"fmt"
	"net/url"
	"strings"
)

type forgeKind string

const (
	forgeGitHub forgeKind = "github"
	forgeGitLab forgeKind = "gitlab"
)

// repository is one project on one forge, reduced to what its release API
// needs: GitHub addresses a repository as owner/name, GitLab as the full
// namespace path, however many subgroups deep.
type repository struct {
	forge forgeKind
	path  string // "owner/repo" or "group/sub/project"
}

// key identifies the repository in both caches. Lowercased because both
// forges treat paths case-insensitively, so "Owner/Repo" and "owner/repo" are
// one repository and should cost one request.
func (r repository) key() string { return string(r.forge) + ":" + strings.ToLower(r.path) }

// display names the repository in an error the way a user would type it.
func (r repository) display() string { return string(r.forge) + ".com/" + r.path }

// parseRepo reads the repository out of a source URL. Only github.com and
// gitlab.com are read: a self-hosted GitLab or Gitea has an API ccu could talk
// to, but nothing in an image label says which kind of forge a host runs, and
// guessing means sending a GitLab token to a host that is not GitLab.
func parseRepo(sourceURL string) (repository, error) {
	parsed, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil || parsed.Host == "" {
		return repository{}, fmt.Errorf("%q: %w", sourceURL, ErrUnsupported)
	}

	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	segments := splitPath(parsed.Path)

	switch host {
	case "github.com":
		// Only owner and name make the repository; a label pointing into a
		// subdirectory ("/tree/main/docker") carries more that is not part of it.
		if len(segments) < 2 {
			break
		}
		return repository{forge: forgeGitHub, path: segments[0] + "/" + trimGit(segments[1])}, nil
	case "gitlab.com":
		segments = gitlabProject(segments)
		if len(segments) < 2 {
			break
		}
		segments[len(segments)-1] = trimGit(segments[len(segments)-1])
		return repository{forge: forgeGitLab, path: strings.Join(segments, "/")}, nil
	}

	return repository{}, fmt.Errorf("%q: %w", sourceURL, ErrUnsupported)
}

// gitlabProject cuts a GitLab path down to the project. GitLab separates the
// project from what is inside it with a "-" segment ("/-/tree/main"), which
// no group or project may be named. Older links went straight to "/tree/" or
// "/blob/"; those words are legal project names, so they only end the project
// once a group and a project have already been seen.
func gitlabProject(segments []string) []string {
	for i, segment := range segments {
		if segment == "-" {
			return segments[:i]
		}
		if i >= 2 && (segment == "tree" || segment == "blob") {
			return segments[:i]
		}
	}
	return segments
}

func splitPath(path string) []string {
	var segments []string
	for segment := range strings.SplitSeq(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func trimGit(name string) string { return strings.TrimSuffix(name, ".git") }
