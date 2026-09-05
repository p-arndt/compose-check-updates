// Package check resolves what an image reference found in a file could move to,
// and applies that move.
package check

import (
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/versioning"
)

// The reasons an image ends up unreadable. They are short and stable because a
// consumer of the JSON report dispatches on them; UnreadableMessage is what a
// person reads.
const (
	// ReasonNoTagForDigest: the newest manifest matches none of the tags ccu
	// probed. This is where an image with version-shaped tags the scheme cannot
	// read comes out.
	ReasonNoTagForDigest = "no-tag-for-digest"
	// ReasonNoComparableTag: the current tag reads as a version, but no other tag
	// can be compared with it, so no run will ever offer one.
	ReasonNoComparableTag = "no-comparable-tag"
	// ReasonNoReferenceTag: the repository has no floating reference tag, which
	// is the only thing an unreadable tag can be compared against.
	ReasonNoReferenceTag = "no-reference-tag"
	// ReasonNoCurrentDigest: the tag in the file resolves to no manifest at all.
	ReasonNoCurrentDigest = "current-digest-unresolved"
	// ReasonNoTagOrDigest: the reference names neither, so there is nothing to
	// look up in the first place.
	ReasonNoTagOrDigest = "no-tag-or-digest"
	// ReasonNoFloatingDigest: the floating tag to pin resolves to no manifest.
	ReasonNoFloatingDigest = "floating-digest-unresolved"
	// ReasonUnresolvedVariable: the tag is interpolated from a variable nothing
	// defines, so not even the tag the stack runs on is known.
	ReasonUnresolvedVariable = "unresolved-variable"
)

// Update is one image reference and what it could move to.
type Update struct {
	FilePath string
	RawLine  string
	// ExtraLines are further lines of FilePath carrying this same reference, all
	// rewritten together. A multi-stage Dockerfile names its base image once per
	// stage, and moving only one would build from two different releases.
	ExtraLines []string
	// ComposePath is the compose file whose service builds FilePath, set only
	// when FilePath is a Dockerfile. A restart acts on it: compose knows the
	// service, not the Dockerfile behind it.
	ComposePath string
	// Services names the compose services declaring this image. A list because
	// identical references collapse into one entry.
	Services      []string
	ImageName     string
	FullImageName string
	CurrentTag    string
	LatestTag     string
	CurrentDigest string
	LatestDigest  string

	// Best candidate at each upgrade level, so a consumer can offer a choice of
	// target instead of only the highest tag available.
	PatchTag string
	MinorTag string
	MajorTag string

	// Cap is the highest level this image may move to; empty means no cap.
	Cap policy.Level

	// Versioning and VersioningPattern travel with the update because the level
	// is re-derived from the tags long after the checker that resolved them is
	// gone, and a level derived under the wrong scheme is worse than none.
	Versioning        policy.Versioning
	VersioningPattern string

	// TagVar is set when CurrentTag was interpolated from a variable rather than
	// written in the image line, and says where a new tag has to be written
	// instead. Nil for the ordinary reference that spells its own tag.
	TagVar *TagVar

	// PinsFloating marks the one update that adds a digest instead of changing
	// one: a bare floating tag gaining the digest it resolves to right now.
	PinsFloating bool

	// UnreadableReason names why this image could not be resolved, empty when it
	// could. Carried on the update rather than logged and forgotten: a warning on
	// stderr is not something a user can act on.
	UnreadableReason string
	// UnreadableMessage says the same in a sentence, naming the tag and scheme
	// actually in play and the way out where there is one.
	UnreadableMessage string

	// digestFor is the tag LatestDigest was resolved for. A digest only ever
	// describes one release, so switching target has to invalidate it.
	digestFor string
}

// MarkUnreadable records that this image could not be resolved. The
// half-resolved target fields go with it: a digest fetched for a tag that was
// then never found would sit there looking like an update to apply.
func (u *Update) MarkUnreadable(reason, message string) {
	u.UnreadableReason, u.UnreadableMessage = reason, message
	u.LatestTag, u.LatestDigest, u.digestFor = "", "", ""
	u.PatchTag, u.MinorTag, u.MajorTag = "", "", ""
}

func (u *Update) IsUnreadable() bool { return u.UnreadableReason != "" }

// IsDockerfile reports whether this update sits in a Dockerfile built by a
// compose service rather than in a compose file.
func (u *Update) IsDockerfile() bool { return u.ComposePath != "" }

// RestartPath is the compose file to hand `docker compose -f`.
func (u *Update) RestartPath() string {
	if u.ComposePath != "" {
		return u.ComposePath
	}
	return u.FilePath
}

// scheme is how this image's tags are read. An unrecognised name falls back to
// the default rather than failing: the config layer rejects those on load, so
// one reaching this far is not worth hiding every update over.
func (u *Update) scheme() versioning.Scheme {
	scheme, ok := versioning.ByName(u.Versioning, u.VersioningPattern)
	if !ok {
		return versioning.Default()
	}
	return scheme
}

// TagForTarget returns the tag this image would move to at the given level,
// degrading so a caller asking for "major" on an image that only has a patch
// still gets an answer.
func (u *Update) TagForTarget(target policy.Level) string {
	// An image moved by digest alone has no levels to choose between.
	if u.PatchTag == "" && u.MinorTag == "" && u.MajorTag == "" && u.IsDigestUpdate() {
		return u.LatestTag
	}

	// The cap clamps the request rather than the answer, so a capped "major"
	// behaves exactly as asking for the cap would, degradation included.
	if !u.Cap.Allows(target) {
		target = u.Cap
	}

	switch target {
	case policy.LevelMajor:
		if u.MajorTag != "" {
			return u.MajorTag
		}
		fallthrough
	case policy.LevelMinor:
		if u.MinorTag != "" {
			return u.MinorTag
		}
		fallthrough
	case policy.LevelPatch:
		return u.PatchTag
	}

	return ""
}

// AvailableTargets lists which levels have a distinct tag available, lowest
// first, so a consumer only offers levels that exist here.
func (u *Update) AvailableTargets() []policy.Level {
	var targets []policy.Level
	seen := make(map[string]struct{})

	for _, t := range []struct {
		level policy.Level
		tag   string
	}{
		{policy.LevelPatch, u.PatchTag},
		{policy.LevelMinor, u.MinorTag},
		{policy.LevelMajor, u.MajorTag},
	} {
		if t.tag == "" || !u.Cap.Allows(t.level) {
			continue
		}
		if _, dup := seen[t.tag]; dup {
			continue
		}
		seen[t.tag] = struct{}{}
		targets = append(targets, t.level)
	}

	return targets
}

// SelectTarget points LatestTag at the tag for the given level and reports
// whether that changed anything. A level with no tag selects nothing rather than
// clearing an already valid selection.
func (u *Update) SelectTarget(target policy.Level) bool {
	tag := u.TagForTarget(target)
	if tag == "" || tag == u.LatestTag {
		u.invalidateStaleDigest()
		return false
	}

	u.LatestTag = tag
	u.invalidateStaleDigest()
	return true
}

// invalidateStaleDigest drops a digest resolved for a different tag, so a
// mismatched tag/digest pair can never reach the file.
func (u *Update) invalidateStaleDigest() {
	if u.digestFor != u.LatestTag {
		u.LatestDigest = ""
	}
}

// ResolveDigest fills in the digest belonging to the selected tag. Only
// references that pin a digest need one.
func (u *Update) ResolveDigest(reg registry.Fetcher) error {
	if u.CurrentDigest == "" || u.LatestTag == "" {
		return nil
	}
	if u.LatestDigest != "" && u.digestFor == u.LatestTag {
		return nil
	}

	digest, err := reg.Digest(u.ImageName + ":" + u.LatestTag)
	if err != nil {
		return err
	}

	u.LatestDigest = digest
	u.digestFor = u.LatestTag
	return nil
}

// IsDigestUpdate reports whether the image moved to a different manifest without
// a version to describe the change.
func (u *Update) IsDigestUpdate() bool {
	return u.LatestDigest != "" && u.LatestDigest != u.CurrentDigest
}

// HasNewVersion reports whether this update is one to act on. The level filters
// are not consulted: LatestTag was already resolved under them.
func (u *Update) HasNewVersion() bool {
	switch {
	case u.IsUnreadable():
		// Nothing was resolved, so there is no version to write.
		return false
	case u.PinsFloating:
		// The level filters speak about versions; a pin writes a digest.
		return true
	case u.IsDigestUpdate():
		// A digest change carries no level: it is either a different image or not.
		return true
	}

	level := u.Level()
	return level != "" && u.Cap.Allows(level)
}

// Level names how far this update moves the image.
func (u *Update) Level() policy.Level {
	switch {
	case u.IsUnreadable():
		// First, because every case below reads fields MarkUnreadable cleared.
		return policy.LevelUnreadable
	case u.PinsFloating:
		// Before the digest cases: the digest is new to the file, but the image
		// behind it is the one the tag already pointed at.
		return policy.LevelPin
	}

	scheme := u.scheme()
	current, currentOK := scheme.Parse(u.CurrentTag)
	latest, latestOK := scheme.Parse(u.LatestTag)

	if u.CurrentTag == "" || u.LatestTag == "" || !currentOK || !latestOK {
		if u.IsDigestUpdate() {
			return policy.LevelDigest
		}
		return ""
	}

	return versioning.Diff(current, latest)
}
