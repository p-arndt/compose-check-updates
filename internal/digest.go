package internal

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

const (
	// referenceTag is the mutable tag consulted to find out what "newest" means
	// for images that do not publish semver tags. Their commit tags (for example
	// "sha-e1c83ba") carry no ordering, so the digest this tag resolves to is
	// what identifies the current release.
	referenceTag = "latest"

	// maxDigestCandidates bounds how many sibling tags are probed while looking
	// for the one matching the reference digest. Each probe is a request, so an
	// image with thousands of commit tags would otherwise exhaust rate limits.
	// Registries return tags in lexical order, which for commit tags is
	// unrelated to age, so a low cap would drop the very tag being looked for.
	maxDigestCandidates = 250

	// digestProbeWorkers limits how many of those probes run at once. The probes
	// are latency-bound, so this is well above the core count.
	digestProbeWorkers = 24
)

// digestPattern matches a fully qualified digest such as "sha256:ab12...".
var digestPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[0-9a-fA-F]{32,}$`)

// mutableTags are tags that float to whatever is newest. They are never
// proposed as an update target: moving from one floating tag to another says
// nothing about the image having changed.
var mutableTags = map[string]struct{}{
	"latest":  {},
	"main":    {},
	"master":  {},
	"edge":    {},
	"stable":  {},
	"nightly": {},
	"dev":     {},
	"develop": {},
}

// IsDigest reports whether s is a digest reference rather than a tag.
func IsDigest(s string) bool {
	return digestPattern.MatchString(s)
}

// tagFamily returns the leading, non-varying part of a tag — everything up to
// and including its last separator. "sha-e1c83ba" yields "sha-" and
// "sha256-ab12" yields "sha256-". It is used to keep the search for a newer tag
// within the same naming scheme, so a "sha-" tag is never replaced by an
// unrelated "v2-beta" one.
func tagFamily(tag string) string {
	if i := strings.LastIndexAny(tag, "-_."); i != -1 {
		return tag[:i+1]
	}
	return ""
}

// digestCandidates narrows a repository's tag list to the tags that could
// plausibly be a newer spelling of currentTag. It returns the candidates and
// how many were dropped by maxDigestCandidates.
func digestCandidates(tags []string, currentTag string) (candidates []string, dropped int) {
	family := tagFamily(currentTag)

	for _, tag := range tags {
		if tag == currentTag {
			continue
		}
		if _, floating := mutableTags[tag]; floating {
			continue
		}
		if tagFamily(tag) != family {
			continue
		}
		candidates = append(candidates, tag)
	}

	if len(candidates) > maxDigestCandidates {
		dropped = len(candidates) - maxDigestCandidates
		candidates = candidates[:maxDigestCandidates]
	}

	return candidates, dropped
}

// findTagForDigest probes candidate tags of image until one resolves to digest,
// returning "" when none matches. Probes run concurrently and stop early once a
// match is found.
func findTagForDigest(registry IRegistry, image string, candidates []string, digest string) string {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		match   string
		queue   = make(chan string)
		workers = min(digestProbeWorkers, len(candidates))
	)

	found := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return match != ""
	}

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tag := range queue {
				if found() {
					continue
				}

				candidateDigest, err := registry.FetchImageDigest(image + ":" + tag)
				if err != nil {
					slog.Debug("Failed probing tag", "image", image, "tag", tag, "error", err)
					continue
				}
				if candidateDigest != digest {
					continue
				}

				mu.Lock()
				if match == "" {
					match = tag
				}
				mu.Unlock()
			}
		}()
	}

	for _, tag := range candidates {
		if found() {
			break
		}
		queue <- tag
	}
	close(queue)
	wg.Wait()

	return match
}
