package compose

import (
	"slices"
	"strings"
)

// ImageMatcher decides whether an image is part of the run at all, for the
// `-image` selection. How a pattern is written picks what it is compared with:
//
//   - one without a separator ("traefik") matches the last element of the name,
//     so it finds "library/traefik" and "ghcr.io/immich-app/traefik" alike;
//   - one with a separator ("ghcr.io/immich-app/*") matches the whole name as
//     ccu reports it.
//
// Both forms take `*` as a wildcard, and unlike filepath.Match's it spans "/":
// an image name is one string to the user, and "ghcr.io/*" has to reach the
// repositories two levels down as well. Matching is case-sensitive, because so
// is a repository name.
type ImageMatcher struct {
	names []string // compared with the last element of the image name
	full  []string // compared with the whole image name
}

// NewImageMatcher builds a matcher from the patterns as the user wrote them. A
// matcher with no pattern selects everything, so an empty list means "no
// selection was made" rather than "select nothing".
func NewImageMatcher(patterns []string) *ImageMatcher {
	m := &ImageMatcher{}

	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		// ccu reports a Docker Hub image without its registry ("library/nginx"),
		// so a pattern spelling the host out would otherwise match nothing.
		p = strings.TrimPrefix(p, "index.docker.io/")
		p = strings.TrimPrefix(p, "docker.io/")
		if p == "" {
			continue
		}

		if strings.Contains(p, "/") {
			m.full = append(m.full, p)
		} else {
			m.names = append(m.names, p)
		}
	}

	return m
}

// Empty reports whether the matcher selects everything, so a caller can skip
// the work of asking it about each image.
func (m *ImageMatcher) Empty() bool {
	return m == nil || len(m.names)+len(m.full) == 0
}

// Match reports whether image — the name without tag or digest, as ccu reports
// it — was selected. Without a pattern every image is.
func (m *ImageMatcher) Match(image string) bool {
	if m.Empty() {
		return true
	}

	for _, pattern := range m.full {
		if globMatch(pattern, image) {
			return true
		}
	}

	last := image
	if i := strings.LastIndex(image, "/"); i >= 0 {
		last = image[i+1:]
	}
	for _, pattern := range m.names {
		if globMatch(pattern, last) {
			return true
		}
	}

	return false
}

// Patterns returns the patterns the matcher was built from, for the caller that
// has to name them in a message when none of them matched anything.
func (m *ImageMatcher) Patterns() []string {
	if m == nil {
		return nil
	}
	return slices.Concat(m.full, m.names)
}

// globMatch reports whether pattern matches s, with `*` standing for any run of
// characters — separators included, which is where this parts ways with
// filepath.Match.
func globMatch(pattern, s string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}

	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]

	// The middle parts have to appear in order; the earliest occurrence of each
	// is always the one to take, since leaving more of s over can only help the
	// parts still to come.
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(s, part)
		if i < 0 {
			return false
		}
		s = s[i+len(part):]
	}

	return strings.HasSuffix(s, parts[len(parts)-1])
}
