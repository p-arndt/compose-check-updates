package registry

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/regclient/regclient/types/manifest"
)

// sourceLabels are the labels naming where an image is built from, in the order
// they are consulted. The OCI one is what every current build tool writes; the
// other two are what images built before it still carry.
var sourceLabels = []string{
	"org.opencontainers.image.source",
	"org.opencontainers.image.url",
	"org.label-schema.vcs-url",
}

// schemePattern matches the "scheme:" a URL opens with.
var schemePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// SourceFetcher is the optional half of a Fetcher: where an image says its own
// source lives. Kept out of Fetcher because every stand-in a test writes would
// otherwise have to answer it, and because it costs requests only a caller with
// an update to explain should pay for — such a caller type-asserts for it.
type SourceFetcher interface {
	// SourceURL returns the source URL an image records, or "" when it records
	// none. A missing label is not an error: most images simply have none.
	SourceURL(image string) (string, error)
}

// SourceURL reads the source label of one reference. It rides on the same walk
// down to the config blob that Created uses, so an update whose date and source
// are both reported costs one manifest chain, not two.
func (c *Client) SourceURL(image string) (string, error) {
	info, err := c.imageInfoFor(image)
	if err != nil {
		return "", err
	}
	return info.Source, nil
}

// annotationSource reads the source out of a manifest's annotations, which an
// index carries in place of the config blob it does not have.
func annotationSource(m manifest.Manifest) string {
	annotator, ok := m.(manifest.Annotator)
	if !ok {
		return ""
	}
	annotations, err := annotator.GetAnnotations()
	if err != nil {
		return ""
	}
	return pickSource(annotations)
}

func pickSource(values map[string]string) string {
	for _, label := range sourceLabels {
		if v := strings.TrimSpace(values[label]); v != "" {
			return v
		}
	}
	return ""
}

// SourceLinks turns the source an image records into the two links ccu reports:
// the repository itself, and where the release notes for tag would be.
//
// The release link is constructed, never verified: checking it would mean a
// request per update against a host that is not even the registry, and a 404
// would not be news anyway — a project may tag its releases differently, or not
// publish them at all. Only the forges whose release URLs are a fixed shape get
// one; anything else is reported as a plain source link.
func SourceLinks(source, tag string) (sourceURL, releaseURL string) {
	sourceURL = normalizeSource(source)
	if sourceURL == "" || tag == "" {
		return sourceURL, ""
	}

	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return sourceURL, ""
	}

	path := strings.Trim(parsed.Path, "/")
	if len(strings.Split(path, "/")) < 2 {
		// Not a repository but a forge's landing page or a bare group.
		return sourceURL, ""
	}
	base := parsed.Scheme + "://" + parsed.Host + "/"

	switch parsed.Host {
	case "github.com", "www.github.com":
		// Only the first two segments name the repository; a label pointing into a
		// subdirectory ("/tree/main/docker") carries more that is not part of it.
		owner, rest, _ := strings.Cut(path, "/")
		repo, _, _ := strings.Cut(rest, "/")
		return sourceURL, base + owner + "/" + repo + "/releases/tag/" + url.PathEscape(tag)
	case "gitlab.com", "www.gitlab.com":
		// The whole path, because a GitLab project may sit any number of subgroups
		// deep and every one of them is part of its address.
		return sourceURL, base + path + "/-/releases/" + url.PathEscape(tag)
	}

	return sourceURL, ""
}

// normalizeSource turns the spellings a source label is written in into a URL a
// browser can open, and drops anything that is neither http nor https — a label
// holding a package name or a mail address is not a link to offer.
func normalizeSource(source string) string {
	s := strings.TrimSpace(source)
	if s == "" {
		return ""
	}

	// Git remotes are copied into this label as they stand, including the scp-like
	// form ("git@github.com:owner/repo.git") that no URL parser accepts.
	s = strings.TrimPrefix(s, "git+")
	if rest, ok := strings.CutPrefix(s, "git@"); ok {
		s = "https://" + strings.Replace(rest, ":", "/", 1)
	}
	for _, scheme := range []string{"ssh://git@", "ssh://", "git://"} {
		if rest, ok := strings.CutPrefix(s, scheme); ok {
			s = "https://" + rest
			break
		}
	}
	// A bare "github.com/owner/repo" is a link with the obvious scheme left off.
	// Anything that already names a scheme is not — "mailto:someone@example.com"
	// would otherwise be turned into a host by prefixing it.
	if !schemePattern.MatchString(s) && strings.Contains(s, ".") {
		s = "https://" + s
	}

	parsed, err := url.Parse(s)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}

	parsed.User = nil
	// Docker's official images write the label as a git URL with the commit and
	// the Dockerfile's directory behind "#" — pointing into the repository, not
	// at a page anyone can open.
	parsed.Fragment, parsed.RawQuery = "", ""
	parsed.Path = strings.TrimSuffix(strings.TrimSuffix(parsed.Path, "/"), ".git")

	return parsed.String()
}
