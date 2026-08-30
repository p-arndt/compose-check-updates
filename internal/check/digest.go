package check

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
)

// maxDigestCandidates bounds how many sibling tags are probed while looking for
// the one matching the reference digest. Each probe is a request, so an image
// with thousands of commit tags would otherwise exhaust rate limits. Registries
// return tags in lexical order, which for commit tags is unrelated to age, so a
// low cap would drop the very tag being looked for.
const maxDigestCandidates = 250

// checkDigest fills in the update for an image whose tag is not a version, by
// comparing what the reference resolves to against the repository's floating
// reference tag.
func (c *Checker) checkDigest(u *Update, p policy.Image) {
	// Whether the file itself pins a digest decides what gets rewritten later:
	// the digest in place, or the tag that now carries it.
	pinned := u.CurrentDigest != ""

	// A bare floating tag records no digest in the file, so there is nothing to
	// compare it against — it already resolves to whatever is newest. All that
	// can be done is to write down what it resolves to today.
	if p.Floats(u.CurrentTag) && !pinned {
		if !c.policies.PinFloating {
			slog.Debug("Skipping (floating tag without digest)", "image", u.ImageName, "tag", u.CurrentTag, "path", u.FilePath)
			return
		}
		c.pinFloatingTag(u)
		return
	}
	if u.CurrentTag == "" && !pinned {
		u.MarkUnreadable(ReasonNoTagOrDigest, "this reference names neither a tag nor a digest, so there is nothing to look up")
		slog.Warn("Skipping (no tag or digest)", "image", u.ImageName, "path", u.FilePath)
		return
	}

	latestDigest, err := c.registry.Digest(u.ImageName + ":" + p.ReferenceTag)
	if err != nil {
		u.MarkUnreadable(ReasonNoReferenceTag, "no "+p.ReferenceTag+" tag to compare against, and this image's tag names no version ccu can read; if this image publishes its newest build under another tag, name it with `reference_tag`")
		slog.Warn("Skipping (no "+p.ReferenceTag+" tag to compare against); if this image publishes its newest build under another tag, name it with `reference_tag`", "image", u.ImageName, "path", u.FilePath)
		return
	}

	// A digest-pinned reference carries its current digest in the file itself.
	currentDigest := u.CurrentDigest
	if !pinned {
		currentDigest, err = c.registry.Digest(u.ImageName + ":" + u.CurrentTag)
		if err != nil {
			u.MarkUnreadable(ReasonNoCurrentDigest, fmt.Sprintf("the tag %q in this file resolves to no image in the registry", u.CurrentTag))
			slog.Warn("Skipping (failed resolving current digest)", "image", u.ImageName, "tag", u.CurrentTag, "path", u.FilePath)
			return
		}
		u.CurrentDigest = currentDigest
	}

	if currentDigest == latestDigest {
		return
	}
	u.LatestDigest, u.digestFor = latestDigest, u.LatestTag

	// A pinned digest is rewritten in place and the tag, if any, stays as it is.
	if pinned {
		return
	}

	// Otherwise the tag is all there is to rewrite, so the tag now carrying the
	// new digest has to be found.
	tags, err := c.registry.Tags(u.ImageName)
	if err != nil {
		slog.Error("Skipping (failed fetching tags)", "image", u.ImageName, "path", u.FilePath)
		u.LatestDigest = ""
		return
	}

	candidates, dropped := digestCandidates(tags, u.CurrentTag, p)
	if dropped > 0 {
		slog.Warn("Only probing a subset of tags", "image", u.ImageName, "probed", len(candidates), "skipped", dropped)
	}

	latestTag := registry.TagForDigest(c.registry, u.ImageName, candidates, latestDigest)
	if latestTag == "" {
		// This is where an image with version-shaped tags ccu cannot read ends up,
		// so the way out is named rather than left to be discovered.
		u.MarkUnreadable(ReasonNoTagForDigest, hint(p,
			"none of this image's tags matches its newest digest, and none of them reads as a version"))
		slog.Warn(hint(p, "Skipping (no tag matches the newest digest)"), "image", u.ImageName, "tag", u.CurrentTag, "path", u.FilePath)
		return
	}
	u.LatestTag, u.digestFor = latestTag, latestTag
}

// pinFloatingTag records the digest a bare floating tag resolves to, so the
// reference can be rewritten as "latest@sha256:…". Not an update — the image is
// the one the tag already pointed at — but the one-off pin that gives every
// later run something to compare against.
func (c *Checker) pinFloatingTag(u *Update) {
	digest, err := c.registry.Digest(u.ImageName + ":" + u.CurrentTag)
	if err != nil {
		u.MarkUnreadable(ReasonNoFloatingDigest, fmt.Sprintf("the floating tag %q resolves to no image, so there is nothing to pin it to", u.CurrentTag))
		slog.Warn("Skipping (failed resolving digest for floating tag)", "image", u.ImageName, "tag", u.CurrentTag, "path", u.FilePath)
		return
	}

	// The tag stays exactly as it is; only the digest is new. LatestTag is set all
	// the same, so the report and the TUI's row have the tag the pin belongs to.
	u.LatestTag = u.CurrentTag
	u.LatestDigest, u.digestFor = digest, u.CurrentTag
	u.PinsFloating = true
}

// digestCandidates narrows a repository's tag list to the tags that could
// plausibly be a newer spelling of currentTag, and reports how many were dropped
// by maxDigestCandidates.
//
// The reference tag is dropped along with the floating ones: what is being
// looked for is the tag that *carries* the newest digest, and rewriting a commit
// tag to "stable" would trade a fixed reference for a moving one.
func digestCandidates(tags []string, currentTag string, p policy.Image) (candidates []string, dropped int) {
	family := tagFamily(currentTag)

	for _, tag := range tags {
		if tag == currentTag || tag == p.ReferenceTag || p.Floats(tag) {
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

// tagFamily is everything up to and including a tag's last separator, so the
// search for a newer tag stays within one naming scheme: "sha-e1c83ba" yields
// "sha-", and a "sha-" tag is never replaced by an unrelated "v2-beta".
func tagFamily(tag string) string {
	if i := strings.LastIndexAny(tag, "-_."); i != -1 {
		return tag[:i+1]
	}
	return ""
}
