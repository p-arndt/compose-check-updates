// Package registry talks to container registries: listing a repository's tags
// and resolving what a reference points at.
package registry

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/regclient/regclient"
	"github.com/regclient/regclient/config"
	"github.com/regclient/regclient/types/ref"
)

// Fetcher is the part of a registry the checker needs, so a test can stand in
// for one.
type Fetcher interface {
	Tags(image string) ([]string, error)
	Digest(image string) (string, error)
}

// Client is the live Fetcher: a regclient talking to real registries over the
// network. Tests use a stand-in rather than this.
type Client struct {
	rc *regclient.RegClient
}

// New returns a client for the public registries. A non-empty registryURL points
// every lookup at that host over plain HTTP, which is what a test server needs.
func New(registryURL string) *Client {
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

	return &Client{rc: regclient.New(opts...)}
}

func (c *Client) Tags(image string) ([]string, error) {
	// regclient expands official images, e.g. "nginx" -> "docker.io/library/nginx".
	rRef, err := ref.New(image)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

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
func (c *Client) Digest(image string) (string, error) {
	rRef, err := ref.New(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	m, err := c.rc.ManifestHead(context.Background(), rRef)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest for %q: %w", image, err)
	}

	return m.GetDescriptor().Digest.String(), nil
}
