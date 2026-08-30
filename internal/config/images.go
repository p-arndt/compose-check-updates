package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/versioning"
)

// MaxLevel returns the cap recorded for an image, or "" when there is none.
// Lookup is exact on the image name without tag or digest (e.g.
// "library/traefik").
func (c Config) MaxLevel(image string) policy.Level {
	return c.Images[image].Max
}

// Policies is everything the checker needs to know about the images of a run.
func (c Config) Policies() policy.Set {
	images := make(map[string]policy.Image, len(c.Images))
	for image, p := range c.Images {
		// Union rather than a plain copy: it trims the entries and drops repeats,
		// so a tag named twice across the two config files is probed once.
		p.FloatingTags = Union(p.FloatingTags)
		images[image] = p
	}

	scheme := c.Versioning
	if scheme == "" {
		scheme = policy.VersioningSemver
	}

	return policy.Set{
		Images:       images,
		Versioning:   scheme,
		FloatingTags: c.FloatingTags,
		PinFloating:  c.PinFloatingEnabled(),
	}
}

// mergeImages layers over onto base. Unlike Exclude, which unions, a per-image
// policy *replaces*: the project file has to be able to raise a cap the global
// file set, not only tighten it.
func mergeImages(base, over map[string]policy.Image) map[string]policy.Image {
	if len(base) == 0 {
		return over
	}
	if len(over) == 0 {
		return base
	}

	merged := make(map[string]policy.Image, len(base)+len(over))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range over {
		merged[k] = v
	}
	return merged
}

// validateImages rejects a value that names no level or no scheme, for the same
// reason an unknown key is rejected: a silently ignored `max: mayor` looks
// exactly like a feature that does not work.
func validateImages(images map[string]policy.Image) error {
	for image, p := range images {
		if err := validateImage(p); err != nil {
			return fmt.Errorf("image %q: %w", image, err)
		}
	}
	return nil
}

func validateImage(p policy.Image) error {
	if p.Max != "" && !p.Max.Valid() {
		return fmt.Errorf("max: %q is not one of %s, %s, %s", p.Max, policy.LevelPatch, policy.LevelMinor, policy.LevelMajor)
	}
	if err := versioning.Validate(p.Versioning); err != nil {
		return err
	}
	if p.ReferenceTag != "" && !validTag(p.ReferenceTag) {
		return fmt.Errorf("reference_tag: %q is not a valid tag", p.ReferenceTag)
	}
	for _, tag := range p.FloatingTags {
		// An empty entry is a list item written and left blank: rejected rather
		// than dropped, because the user meant to say something with it.
		if trimmed := strings.TrimSpace(tag); trimmed == "" || !validTag(trimmed) {
			return fmt.Errorf("floating_tags: %q is not a valid tag", tag)
		}
	}
	return versioning.ValidatePattern(p.Versioning, p.VersioningPattern)
}

// tagPattern is the tag grammar registries accept. Checking it here turns a typo
// into an error naming the file it came from, instead of a lookup that quietly
// finds nothing.
var tagPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)

func validTag(s string) bool { return tagPattern.MatchString(s) }
