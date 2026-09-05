package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/regclient/regclient"
	"github.com/regclient/regclient/types/descriptor"
	"github.com/regclient/regclient/types/manifest"
	"github.com/regclient/regclient/types/mediatype"
	v1 "github.com/regclient/regclient/types/oci/v1"
	"github.com/regclient/regclient/types/platform"
	"github.com/regclient/regclient/types/ref"
)

// createdCache remembers what one repository+tag resolved to. The scanner
// resolves files from several goroutines and min_age asks about the same
// candidates repeatedly, so the map is both shared and worth having.
type createdCache struct {
	mu      sync.Mutex
	entries map[string]createdEntry
}

type createdEntry struct {
	at  time.Time
	err error
}

// get returns the remembered entry, and whether there is one at all — a miss
// and a remembered failure are different answers.
func (c *createdCache) get(image string) (createdEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[image]
	return e, ok
}

func (c *createdCache) set(image string, at time.Time, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]createdEntry{}
	}
	c.entries[image] = createdEntry{at: at, err: err}
}

// annotationCreated is where an image records its build time when the config
// blob does not, e.g. anything built with BuildKit's provenance defaults.
const annotationCreated = "org.opencontainers.image.created"

// Created resolves when the image behind a reference was built. It costs one
// manifest request, a second one when the reference resolves to an index, and
// the config blob — nothing else has the build time in it, since a registry
// reports neither when a tag was pushed nor when a manifest was uploaded.
//
// Results are cached per reference for the lifetime of the client, failures
// included: min_age asks about the same candidate tag from several places, and
// a repository that answers with a 404 once will do so again.
func (c *Client) Created(image string) (time.Time, error) {
	if e, ok := c.created.get(image); ok {
		return e.at, e.err
	}

	t, err := c.resolveCreated(image)
	c.created.set(image, t, err)
	return t, err
}

func (c *Client) resolveCreated(image string) (time.Time, error) {
	rRef, err := ref.New(image)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	ctx := context.Background()

	m, err := c.rc.ManifestGet(ctx, rRef)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to fetch manifest for %q: %w", image, err)
	}

	// An index says nothing about when it was built, so one platform's manifest
	// has to be followed. Which one hardly matters: the release is built once and
	// its architectures are pushed together.
	if m.IsList() {
		if m, err = c.platformManifest(ctx, rRef, m); err != nil {
			return time.Time{}, fmt.Errorf("failed to resolve a platform for %q: %w", image, err)
		}
	}

	imager, ok := m.(manifest.Imager)
	if !ok {
		return time.Time{}, fmt.Errorf("manifest for %q carries no image config", image)
	}

	confDesc, err := imager.GetConfig()
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read the config descriptor for %q: %w", image, err)
	}
	if confDesc.MediaType != mediatype.OCI1ImageConfig && confDesc.MediaType != mediatype.Docker2ImageConfig {
		return time.Time{}, fmt.Errorf("unsupported config media type %q for %q", confDesc.MediaType, image)
	}

	conf, err := c.rc.BlobGetOCIConfig(ctx, rRef, confDesc)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to fetch the image config for %q: %w", image, err)
	}

	if t := createdFromConfig(conf.GetConfig()); !t.IsZero() {
		return t, nil
	}
	if t := createdFromAnnotations(m); !t.IsZero() {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("no creation time recorded for %q", image)
}

// platformManifest follows an index to one platform's manifest, preferring the
// platform ccu runs on so a locally cached registry answer is reused, and
// falling back to the first real image in the list — an index that has no
// manifest for this host still has a build time.
func (c *Client) platformManifest(ctx context.Context, rRef ref.Ref, index manifest.Manifest) (manifest.Manifest, error) {
	indexer, ok := index.(manifest.Indexer)
	if !ok {
		return nil, fmt.Errorf("unsupported index media type %q", index.GetDescriptor().MediaType)
	}

	list, err := indexer.GetManifestList()
	if err != nil {
		return nil, err
	}

	local := platform.Local()
	desc, err := descriptor.DescriptorListSearch(list, descriptor.MatchOpt{Platform: &local})
	if err != nil {
		if desc, err = firstImage(list); err != nil {
			return nil, err
		}
	}

	return c.rc.ManifestGet(ctx, rRef, regclient.WithManifestDesc(desc))
}

// firstImage picks the first entry of an index that is an image at all.
// Attestation and signature manifests sit in the same list under the "unknown"
// platform, and neither carries the config blob the build time lives in.
func firstImage(list []descriptor.Descriptor) (descriptor.Descriptor, error) {
	for _, d := range list {
		if d.Platform != nil && d.Platform.OS == "unknown" {
			continue
		}
		if d.MediaType != mediatype.OCI1Manifest && d.MediaType != mediatype.Docker2Manifest {
			continue
		}
		return d, nil
	}
	return descriptor.Descriptor{}, fmt.Errorf("index holds no image manifest")
}

// createdFromConfig reads the build time out of the image config: the `created`
// field first, then the label that carries the same thing for images built
// without one.
func createdFromConfig(conf v1.Image) time.Time {
	if conf.Created != nil && !conf.Created.IsZero() {
		return *conf.Created
	}
	if t, ok := parseCreated(conf.Config.Labels[annotationCreated]); ok {
		return t
	}
	return time.Time{}
}

// createdFromAnnotations is the last resort: the annotation on the manifest
// itself, which is where a build that pushes no config metadata records it.
func createdFromAnnotations(m manifest.Manifest) time.Time {
	annotator, ok := m.(manifest.Annotator)
	if !ok {
		return time.Time{}
	}
	annotations, err := annotator.GetAnnotations()
	if err != nil {
		return time.Time{}
	}
	if t, ok := parseCreated(annotations[annotationCreated]); ok {
		return t
	}
	return time.Time{}
}

func parseCreated(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil || t.IsZero() {
		return time.Time{}, false
	}
	return t, true
}
