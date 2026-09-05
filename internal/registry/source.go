package registry

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/regclient/regclient"
	"github.com/regclient/regclient/types/descriptor"
	"github.com/regclient/regclient/types/manifest"
	"github.com/regclient/regclient/types/ref"
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

// maxIndexDepth bounds how far an index is followed. A nested index is legal but
// vanishingly rare, and without a bound a registry could loop a client forever.
const maxIndexDepth = 4

// SourceFetcher is the optional half of a Fetcher: where an image says its own
// source lives. Kept out of Fetcher because every stand-in a test writes would
// otherwise have to answer it, and because it costs requests only a caller with
// an update to explain should pay for — such a caller type-asserts for it.
type SourceFetcher interface {
	// SourceURL returns the source URL an image records, or "" when it records
	// none. A missing label is not an error: most images simply have none.
	SourceURL(image string) (string, error)
}

// sourceResult is one memoised lookup. The error is kept too: a repository that
// refused the config blob once will refuse it again, and re-asking for every
// occurrence of the image would multiply the cost of a failure.
type sourceResult struct {
	url string
	err error
}

// SourceURL reads the source label of one reference. The result is cached per
// repository and tag for the lifetime of the client, since the same image often
// appears in several stacks of one scan.
func (c *Client) SourceURL(image string) (string, error) {
	c.mu.Lock()
	if got, ok := c.sources[image]; ok {
		c.mu.Unlock()
		return got.url, got.err
	}
	c.mu.Unlock()

	sourceURL, err := c.fetchSource(image)

	c.mu.Lock()
	if c.sources == nil {
		c.sources = make(map[string]sourceResult)
	}
	c.sources[image] = sourceResult{url: sourceURL, err: err}
	c.mu.Unlock()

	return sourceURL, err
}

// fetchSource walks from the reference to the labels: the manifest, an index
// followed to one platform manifest where there is one, and finally the config
// blob. Annotations are read on the way down because they are already in hand —
// only when none of them names a source is the config blob worth a request.
func (c *Client) fetchSource(image string) (string, error) {
	rRef, err := ref.New(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	ctx := context.Background()
	m, err := c.rc.ManifestGet(ctx, rRef)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest for %q: %w", image, err)
	}

	for range maxIndexDepth {
		if source := annotationSource(m); source != "" {
			return source, nil
		}
		if !m.IsList() {
			break
		}

		desc, err := platformDescriptor(m)
		if err != nil {
			return "", fmt.Errorf("failed to read the index of %q: %w", image, err)
		}
		if m, err = c.rc.ManifestGet(ctx, rRef, regclient.WithManifestDesc(desc)); err != nil {
			return "", fmt.Errorf("failed to fetch platform manifest for %q: %w", image, err)
		}
	}

	imager, ok := m.(manifest.Imager)
	if !ok {
		// An artifact or a schema1 manifest: no config blob to read labels from.
		return "", nil
	}
	configDesc, err := imager.GetConfig()
	if err != nil {
		return "", fmt.Errorf("failed to read the config descriptor of %q: %w", image, err)
	}

	config, err := c.rc.BlobGetOCIConfig(ctx, rRef, configDesc)
	if err != nil {
		return "", fmt.Errorf("failed to fetch the config blob of %q: %w", image, err)
	}

	return pickSource(config.GetConfig().Config.Labels), nil
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

// platformDescriptor picks the manifest of an index to descend into. Any real
// platform will do — the labels describe the image, not the architecture — so
// the first one is taken rather than matching the host, which would fail for an
// image built for a single foreign platform. Attestation entries are skipped:
// buildx lists them as "unknown/unknown" and they carry no image config.
func platformDescriptor(m manifest.Manifest) (descriptor.Descriptor, error) {
	indexer, ok := m.(manifest.Indexer)
	if !ok {
		return descriptor.Descriptor{}, fmt.Errorf("manifest list of unsupported type %s", m.GetDescriptor().MediaType)
	}

	list, err := indexer.GetManifestList()
	if err != nil {
		return descriptor.Descriptor{}, err
	}

	for _, d := range list {
		if d.Platform != nil && d.Platform.OS == "unknown" {
			continue
		}
		return d, nil
	}

	return descriptor.Descriptor{}, fmt.Errorf("no platform manifest in index")
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

// sourceCache memoises the lookups of one client: the same image often appears
// in several stacks of one scan, and the labels behind a tag do not change while
// it runs. Embedded in Client because nothing else there is shared between the
// goroutines the scanner checks files in.
type sourceCache struct {
	mu      sync.Mutex
	sources map[string]sourceResult
}
