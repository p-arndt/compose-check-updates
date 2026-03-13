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
}

type Registry struct {
	rc *regclient.RegClient
}

func NewRegistry(registryURL string) *Registry {
	// Create a logger that discards output by default
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	opts := []regclient.Opt{
		regclient.WithSlog(logger),
	}

	// If a custom registry URL is provided (for testing), configure it
	if registryURL != "" {
		// For test servers, we configure a custom host
		// The URL format is typically host:port (from httptest.Server)
		// We need to configure this host to use HTTP (not HTTPS)
		opts = append(opts, regclient.WithConfigHost(config.Host{
			Name:     registryURL,
			Hostname: registryURL,
			TLS:      config.TLSDisabled,
		}))
	} else {
		// Default: use Docker Hub with credentials
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

	// Parse the image reference
	// regclient handles Docker Hub official images (e.g., "nginx" -> "docker.io/library/nginx")
	rRef, err := ref.New(image)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	// Fetch tag list using regclient
	tl, err := r.rc.TagList(ctx, rRef)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for %q: %w", image, err)
	}

	// Extract tags from the response
	tags, err := tl.GetTags()
	if err != nil {
		return nil, fmt.Errorf("failed to extract tags for %q: %w", image, err)
	}

	return tags, nil
}
