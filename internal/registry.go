package internal

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/regclient/regclient"
	"github.com/regclient/regclient/config"
	"github.com/regclient/regclient/types/ref"
)

type IRegistry interface {
	FetchImageTags(image string) ([]string, error)
	FetchImageDigest(image string) (string, error)
}

type Registry struct {
	rc *regclient.RegClient
}

func NewRegistry(registryURL string) *Registry {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	opts := []regclient.Opt{
		regclient.WithSlog(logger),
	}

	if registryURL != "" {
		// A test server is reachable over plain HTTP only.
		opts = append(opts, regclient.WithConfigHost(config.Host{
			Name:     registryURL,
			Hostname: registryURL,
			TLS:      config.TLSDisabled,
		}))
	} else if host := os.Getenv("CCU_REGISTRY_HOST"); host != "" {
		// Demo/integration hook: point Docker Hub lookups at a local registry
		// over plain HTTP, so a recording or an offline test run resolves
		// invented tags instead of depending on the real hub. Image references
		// keep reading as "nginx:1.2.3" — only where they are fetched changes.
		opts = append(opts, regclient.WithConfigHost(config.Host{
			Name:     config.DockerRegistry,
			Hostname: host,
			TLS:      config.TLSDisabled,
		}))
	} else {
		opts = append(opts,
			regclient.WithDockerCreds(),
			regclient.WithDockerCerts(),
			regclient.WithConfigHost(config.Host{
				Name:     config.DockerRegistry,
				Hostname: config.DockerRegistryDNS,
			}),
		)
	}

	rc := regclient.New(opts...)
	return &Registry{rc: rc}
}

func (r *Registry) FetchImageTags(image string) ([]string, error) {
	ctx := context.Background()

	// regclient expands official images, e.g. "nginx" -> "docker.io/library/nginx".
	rRef, err := ref.New(image)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	tl, err := r.rc.TagList(ctx, rRef)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for %q: %w", image, err)
	}

	tags, err := tl.GetTags()
	if err != nil {
		return nil, fmt.Errorf("failed to extract tags for %q: %w", image, err)
	}

	return tags, nil
}

// FetchImageDigest resolves the manifest digest an image reference currently
// points to. Only the manifest headers are requested, so this stays cheap
// enough to probe several tags of the same image.
func (r *Registry) FetchImageDigest(image string) (string, error) {
	ctx := context.Background()

	rRef, err := ref.New(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	m, err := r.rc.ManifestHead(ctx, rRef)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest for %q: %w", image, err)
	}

	return m.GetDescriptor().Digest.String(), nil
}
