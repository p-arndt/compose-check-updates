package policy

import "slices"

// DefaultReferenceTag is the mutable tag digest mode compares against when an
// image names none: images without version tags carry no ordering, so what this
// tag resolves to is what identifies their current release.
const DefaultReferenceTag = "latest"

// Image is what the user recorded about one image. The zero value is the policy
// of an image nobody named.
type Image struct {
	// Max is the highest level this image may be updated to; empty means no cap.
	Max Level `yaml:"max"`

	// Versioning is the scheme this image's tags are read under; empty means the
	// run's default.
	Versioning Versioning `yaml:"versioning"`

	// ReferenceTag is the tag digest mode compares this image against. A
	// repository publishing no "latest" has nothing to be compared against at
	// all, so naming the tag it does publish is the only way ccu can say anything
	// about it.
	ReferenceTag string `yaml:"reference_tag"`

	// FloatingTags names further tags this image floats under, for a repository
	// whose moving tag is spelled "release" or "canary". They are added to the
	// built-in set rather than replacing it — see Floats.
	FloatingTags []string `yaml:"floating_tags"`

	// VersioningPattern is the regex VersioningRegex reads this image's tags
	// with, using Go's named groups: (?P<major>…), (?P<minor>…), (?P<patch>…),
	// (?P<build>…) and (?P<suffix>…). A pattern that fits one repository's tags
	// is meaningless for the next one's, so there is no run-wide key for it.
	VersioningPattern string `yaml:"versioning_pattern"`
}

// Set is every per-image policy in effect, plus the settings that apply to the
// whole run.
type Set struct {
	Images map[string]Image

	// Versioning is the scheme for images that named none; empty means semver.
	// Never VersioningRegex: a pattern belongs to one image.
	Versioning Versioning

	// FloatingTags apply to every image, added to whatever it named for itself.
	FloatingTags []string

	// PinFloating turns on writing down what a bare floating tag resolves to. Off
	// by default: it costs a request per floating image and pins a reference the
	// user left mutable on purpose.
	PinFloating bool
}

// For resolves the policy in effect for one image, keyed by name without tag or
// digest (e.g. "library/traefik"), with the run-wide settings folded in.
func (s Set) For(image string) Image {
	p := s.Images[image]

	if p.Versioning == "" {
		p.Versioning = s.Versioning
	}
	if p.ReferenceTag == "" {
		p.ReferenceTag = DefaultReferenceTag
	}
	if len(s.FloatingTags) > 0 {
		p.FloatingTags = slices.Concat(s.FloatingTags, p.FloatingTags)
	}

	return p
}

// IsZero reports whether the user recorded nothing about this image.
func (i Image) IsZero() bool {
	return i.Max == "" && i.Versioning == "" && i.ReferenceTag == "" &&
		i.VersioningPattern == "" && len(i.FloatingTags) == 0
}
