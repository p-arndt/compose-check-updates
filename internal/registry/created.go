package registry

import (
	"fmt"
	"time"

	"github.com/regclient/regclient/types/manifest"
	v1 "github.com/regclient/regclient/types/oci/v1"
)

// annotationCreated is where an image records its build time when the config
// blob does not, e.g. anything built with BuildKit's provenance defaults.
const annotationCreated = "org.opencontainers.image.created"

// Created resolves when the image behind a reference was built. It costs one
// manifest request, a second one when the reference resolves to an index, and
// the config blob — nothing else has the build time in it, since a registry
// reports neither when a tag was pushed nor when a manifest was uploaded.
//
// The walk is shared with SourceURL and its result is remembered per reference,
// failures included: min_age asks about the same candidate tag from several
// places, and a repository that answers with a 404 once will do so again.
func (c *Client) Created(image string) (time.Time, error) {
	info, err := c.imageInfoFor(image)
	if err != nil {
		return time.Time{}, err
	}
	if info.Created.IsZero() {
		if !info.HasConfig {
			return time.Time{}, fmt.Errorf("manifest for %q carries no image config", image)
		}
		return time.Time{}, fmt.Errorf("no creation time recorded for %q", image)
	}
	return info.Created, nil
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
