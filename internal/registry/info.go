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

// maxIndexDepth bounds how far an index is followed. A nested index is legal but
// vanishingly rare, and without a bound a registry could loop a client forever.
const maxIndexDepth = 4

// imageInfo is everything the config blob of one image answers: when it was
// built and where it says it is built from. The two used to be two walks down
// the same manifest to the same blob; they are one now, because a caller with
// an update to report asks both questions about the same tag.
type imageInfo struct {
	Created time.Time `json:"created"`
	Source  string    `json:"source,omitempty"`
	// HasConfig separates "the image records no build time" from "there is no
	// image config here at all" — an artifact or a schema1 manifest. The first is
	// an image nobody can date, the second is not an image.
	HasConfig bool `json:"has_config"`
}

// infoResult is one memoised walk. The failure is kept as well: a repository
// that refused the config blob once will refuse it again, and re-asking for
// every occurrence of the image would multiply the cost of a failure.
type infoResult struct {
	info imageInfo
	err  error
}

// infoCache memoises the walks of one client for the length of a run. The disk
// cache below it holds successes only, and only where a digest could be
// resolved; this one also stops a failure from being retried per occurrence.
type infoCache struct {
	entries sync.Map // image -> infoResult
}

func (c *infoCache) get(image string) (infoResult, bool) {
	e, ok := c.entries.Load(image)
	if !ok {
		return infoResult{}, false
	}
	return e.(infoResult), true
}

func (c *infoCache) set(image string, r infoResult) {
	c.entries.Store(image, r)
}

// imageInfoFor resolves what the config blob of a reference says, through both
// caches.
//
// The disk cache is keyed by digest, not by reference: what a digest was built
// from is fixed forever, so it is worth keeping for a month. Getting from the
// tag to the digest is a manifest HEAD that is deliberately *not* cached that
// long — that is the step which would otherwise make ccu blind to a new release.
func (c *Client) imageInfoFor(image string) (imageInfo, error) {
	if e, ok := c.info.get(image); ok {
		return e.info, e.err
	}

	info, err := c.resolveInfoCached(image)
	c.info.set(image, infoResult{info: info, err: err})
	return info, err
}

func (c *Client) resolveInfoCached(image string) (imageInfo, error) {
	rRef, err := ref.New(image)
	if err != nil {
		return imageInfo{}, fmt.Errorf("failed to parse image reference %q: %w", image, err)
	}

	// Without a disk cache the digest would be resolved for nothing: the walk
	// fetches the manifest anyway, and the extra HEAD would be pure overhead.
	if c.cache == nil || c.cache.disabled {
		return c.resolveInfo(rRef, image)
	}

	digest := rRef.Digest
	if digest == "" {
		if digest, err = c.Digest(image); err != nil {
			// No digest, no content address to file the answer under. The walk
			// would fail the same way, so its error is the one worth reporting.
			return c.resolveInfo(rRef, image)
		}
	}

	return cached(c.cache, kindImage, repoKey(rRef)+"@"+digest, image, ImmutableTTL, func() (imageInfo, error) {
		return c.resolveInfo(rRef, image)
	})
}

// resolveInfo walks from the reference to the config blob: the manifest, an
// index followed to one platform's manifest, and the blob itself. Annotations
// are read on the way down because they are already in hand, and the blob is
// fetched once for both answers — it is the request that costs, not the parsing.
func (c *Client) resolveInfo(rRef ref.Ref, image string) (imageInfo, error) {
	ctx := context.Background()

	m, err := c.rc.ManifestGet(ctx, rRef)
	if err != nil {
		return imageInfo{}, fmt.Errorf("failed to fetch manifest for %q: %w", image, err)
	}

	var info imageInfo
	// The first annotation that names a source wins, because an index annotates
	// the release while the platform manifest below it annotates one build of it.
	info.Source = annotationSource(m)
	// The build time goes the other way: the deepest manifest is the one that was
	// built, and the config blob below still outranks its annotation.
	annotated := createdFromAnnotations(m)

	for range maxIndexDepth {
		if !m.IsList() {
			break
		}

		desc, err := platformDescriptor(m)
		if err != nil {
			return imageInfo{}, fmt.Errorf("failed to resolve a platform for %q: %w", image, err)
		}
		if m, err = c.rc.ManifestGet(ctx, rRef, regclient.WithManifestDesc(desc)); err != nil {
			return imageInfo{}, fmt.Errorf("failed to fetch platform manifest for %q: %w", image, err)
		}

		if info.Source == "" {
			info.Source = annotationSource(m)
		}
		if t := createdFromAnnotations(m); !t.IsZero() {
			annotated = t
		}
	}

	conf, ok, err := c.imageConfig(ctx, rRef, m, image)
	if err != nil {
		return imageInfo{}, err
	}
	if ok {
		info.HasConfig = true
		info.Created = createdFromConfig(conf)
		if info.Source == "" {
			info.Source = pickSource(conf.Config.Labels)
		}
	}

	if info.Created.IsZero() {
		info.Created = annotated
	}

	return info, nil
}

// imageConfig fetches the config blob a manifest points at. The false return is
// not a failure: an artifact, a schema1 manifest or an index entry that carries
// no image config is something ccu reports nothing about, not something it stops
// over.
func (c *Client) imageConfig(ctx context.Context, rRef ref.Ref, m manifest.Manifest, image string) (v1.Image, bool, error) {
	imager, ok := m.(manifest.Imager)
	if !ok {
		return v1.Image{}, false, nil
	}

	desc, err := imager.GetConfig()
	if err != nil {
		return v1.Image{}, false, nil
	}
	if desc.MediaType != mediatype.OCI1ImageConfig && desc.MediaType != mediatype.Docker2ImageConfig {
		return v1.Image{}, false, nil
	}

	conf, err := c.rc.BlobGetOCIConfig(ctx, rRef, desc)
	if err != nil {
		return v1.Image{}, false, fmt.Errorf("failed to fetch the image config for %q: %w", image, err)
	}

	return conf.GetConfig(), true, nil
}

// platformDescriptor picks the manifest of an index to descend into: the host's
// platform where the index has one, so a registry's own cache is reused, and
// otherwise the first real image. Attestation and signature manifests are
// skipped — buildx lists them as "unknown/unknown" and neither carries a config
// blob.
func platformDescriptor(index manifest.Manifest) (descriptor.Descriptor, error) {
	indexer, ok := index.(manifest.Indexer)
	if !ok {
		return descriptor.Descriptor{}, fmt.Errorf("unsupported index media type %q", index.GetDescriptor().MediaType)
	}

	list, err := indexer.GetManifestList()
	if err != nil {
		return descriptor.Descriptor{}, err
	}

	local := platform.Local()
	if desc, err := descriptor.DescriptorListSearch(list, descriptor.MatchOpt{Platform: &local}); err == nil {
		return desc, nil
	}

	// An index with no manifest for this host still describes a release, and the
	// labels and the build time are the same on every architecture of it.
	return firstImage(list)
}

// annotationsOf reads a manifest's annotations, nil when it carries none or
// refuses to hand them over. A nil map answers every lookup with the zero
// value, so callers need no second branch for it.
func annotationsOf(m manifest.Manifest) map[string]string {
	annotator, ok := m.(manifest.Annotator)
	if !ok {
		return nil
	}
	annotations, err := annotator.GetAnnotations()
	if err != nil {
		return nil
	}
	return annotations
}

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
