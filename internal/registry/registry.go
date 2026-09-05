// Package registry talks to container registries: listing a repository's tags
// and resolving what a reference points at.
package registry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/regclient/regclient"
	"github.com/regclient/regclient/config"
	"github.com/regclient/regclient/types/ref"
)

// Fetcher is the part of a registry the checker needs, so a test can stand in
// for one.
type Fetcher interface {
	Tags(image string) ([]string, error)
	Digest(image string) (string, error)
	// Created is when the image behind a reference was built. It is the one
	// question that needs the config blob rather than a manifest header, so a
	// caller that only wants a tag list never pays for it.
	Created(image string) (time.Time, error)
}

// Client is the live Fetcher: a regclient talking to real registries over the
// network. Tests use a stand-in rather than this.
type Client struct {
	rc *regclient.RegClient
	// cache is the on-disk store shared with every other client of the run, nil
	// when the run asked for none. Shared rather than per-client because the
	// scanner builds one client per compose file, and an image that appears in
	// two stacks should be one lookup, not two.
	cache *Cache
	info  infoCache
}

// New returns a client for the public registries, asking them every time. A
// non-empty registryURL points every lookup at that host over plain HTTP, which
// is what a test server needs.
func New(registryURL string) *Client { return NewWithCache(registryURL, nil) }

// NewWithCache is New with the run's on-disk cache wired in. A nil cache is a
// client that caches nothing beyond the run's own memory.
func NewWithCache(registryURL string, cache *Cache) *Client {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	opts := []regclient.Opt{regclient.WithSlog(logger)}

	switch host := os.Getenv("CCU_REGISTRY_HOST"); {
	case registryURL != "":
		opts = append(opts, regclient.WithConfigHost(config.Host{
			Name:     registryURL,
			Hostname: registryURL,
			TLS:      config.TLSDisabled,
		}))
	case host != "":
		// Demo/integration hook: point Docker Hub lookups at a local registry, so
		// a recording or an offline test run resolves invented tags. Image
		// references keep reading as "nginx:1.2.3" — only where they are fetched
		// changes.
		opts = append(opts, regclient.WithConfigHost(config.Host{
			Name:     config.DockerRegistry,
			Hostname: host,
			TLS:      config.TLSDisabled,
		}))
	default:
		opts = append(opts,
			regclient.WithDockerCreds(),
			regclient.WithDockerCerts(),
			regclient.WithConfigHost(config.Host{
				Name:     config.DockerRegistry,
				Hostname: config.DockerRegistryDNS,
			}),
		)
	}

	return &Client{rc: regclient.New(opts...), cache: cache}
}

// Tags lists a repository's tags. It is cached only briefly: this is the answer
// that must not go stale, since a release published a minute ago is exactly
// what the user opened ccu for.
func (c *Client) Tags(image string) ([]string, error) {
	// regclient expands official images, e.g. "nginx" -> "docker.io/library/nginx".
	rRef, err := ref.New(image)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	repo := repoKey(rRef)
	return cached(c.cache, kindTags, repo, repo, c.cache.TTL(), func() ([]string, error) {
		return c.fetchTags(rRef, image)
	})
}

func (c *Client) fetchTags(rRef ref.Ref, image string) ([]string, error) {
	list, err := c.rc.TagList(context.Background(), rRef)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for %q: %w", image, err)
	}

	tags, err := list.GetTags()
	if err != nil {
		return nil, fmt.Errorf("failed to extract tags for %q: %w", image, err)
	}

	return tags, nil
}

// Digest resolves the manifest digest a reference points at. Only the manifest
// headers are requested, so probing several tags of one image stays cheap.
//
// Cached under the short TTL, never by content: a tag is a moving pointer, and
// keeping this step fresh is what makes the digest-addressed cache behind it
// safe to keep for a month.
func (c *Client) Digest(image string) (string, error) {
	rRef, err := ref.New(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	return cached(c.cache, kindDigest, refKey(rRef), image, c.cache.TTL(), func() (string, error) {
		return c.fetchDigest(rRef, image)
	})
}

func (c *Client) fetchDigest(rRef ref.Ref, image string) (string, error) {
	m, err := c.rc.ManifestHead(context.Background(), rRef)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest for %q: %w", image, err)
	}

	return m.GetDescriptor().Digest.String(), nil
}

// repoKey is the cache key of a repository: the registry it lives on plus the
// path on it, so "nginx" and "docker.io/library/nginx" share one entry while
// two registries serving the same path do not.
func repoKey(r ref.Ref) string {
	return r.Registry + "/" + r.Repository
}

// refKey is repoKey plus what the reference asked for.
func refKey(r ref.Ref) string {
	key := repoKey(r)
	if r.Tag != "" {
		key += ":" + r.Tag
	}
	if r.Digest != "" {
		key += "@" + r.Digest
	}
	return key
}
