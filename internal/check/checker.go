package check

import (
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/p-arndt/compose-check-updates/internal/compose"
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/versioning"
)

// Checker resolves the images of one file against the registries.
type Checker struct {
	path     string
	registry registry.Fetcher
	policies policy.Set

	// composePath is the compose file that builds path, set only when path is a
	// Dockerfile reached through a service's `build:`. A restart acts on it:
	// `docker compose up` knows the service, not the Dockerfile behind it.
	composePath string
	service     string

	// images narrows the run to the references it matches, for `-image`. nil
	// selects everything.
	images *compose.ImageMatcher
}

// Only restricts the checker to the images m matches. The filter bites while
// the file is being read, not on the way out: the point of naming an image is
// that the registries are never asked about the others.
func (c *Checker) Only(m *compose.ImageMatcher) *Checker {
	c.images = m
	return c
}

// New returns a checker for a compose file.
func New(path string, reg registry.Fetcher, policies policy.Set) *Checker {
	if reg == nil {
		reg = registry.New("")
	}
	return &Checker{path: path, registry: reg, policies: policies}
}

// NewDockerfile returns a checker for the Dockerfile a compose service builds.
// Everything past parsing is the work a compose file gets: the base image of a
// self-built image moves like any other, it just lives on a FROM line.
func NewDockerfile(dockerfile, composePath, service string, reg registry.Fetcher, policies policy.Set) *Checker {
	c := New(dockerfile, reg, policies)
	c.composePath, c.service = composePath, service
	return c
}

// Check resolves every image of the file.
func (c *Checker) Check(major, minor, patch bool) ([]Update, error) {
	updates, err := c.updates()
	if err != nil {
		return nil, err
	}

	for i := range updates {
		c.resolve(&updates[i], major, minor, patch)
		c.resolveSource(&updates[i])
	}
	return updates, nil
}

// CheckImage re-checks the single image the file spells as reference, reporting
// false when the file no longer names it. For the caller that changed one
// image's settings, where a full Check would re-fetch every other tag list too.
func (c *Checker) CheckImage(reference string, major, minor, patch bool) (Update, bool, error) {
	updates, err := c.updates()
	if err != nil {
		return Update{}, false, err
	}

	for i := range updates {
		if updates[i].FullImageName != reference {
			continue
		}
		c.resolve(&updates[i], major, minor, patch)
		c.resolveSource(&updates[i])
		return updates[i], true, nil
	}
	return Update{}, false, nil
}

// CheckPins resolves nothing but the digests bare floating tags point at, and
// returns only the images it could pin: one manifest head per floating image and
// no tag lists at all.
func (c *Checker) CheckPins() ([]Update, error) {
	updates, err := c.updates()
	if err != nil {
		return nil, err
	}

	var pins []Update
	for i := range updates {
		u := &updates[i]

		// A reference already carrying a digest is not waiting to be pinned; the
		// ordinary scan reports its drift.
		if u.CurrentDigest != "" || !c.policyFor(u).Floats(u.CurrentTag) {
			continue
		}

		c.pinFloatingTag(u)
		if u.PinsFloating {
			pins = append(pins, *u)
		}
	}
	return pins, nil
}

func (c *Checker) policyFor(u *Update) policy.Image {
	return c.policies.For(u.ImageName)
}

// resolve fills in one update, shared by the full scan and the single re-check
// so the two can never drift apart.
func (c *Checker) resolve(u *Update, major, minor, patch bool) {
	p := c.policyFor(u)

	// Recorded on the update itself, because the level and the cap are worked out
	// again from the tags after this checker is gone.
	u.Cap = p.Max
	u.Versioning = p.Versioning
	u.VersioningPattern = p.VersioningPattern

	// A variable nothing defines leaves the reference with no tag at all. Saying
	// so by name beats the generic "no tag" message: the file does spell a tag,
	// it is the .env next to it that is missing the line.
	if u.TagVar != nil && u.TagVar.Unset && u.CurrentDigest == "" {
		u.MarkUnreadable(ReasonUnresolvedVariable,
			fmt.Sprintf("this image's tag comes from %s, which nothing defines — set %s in %s", u.TagVar.Raw, u.TagVar.Name, compose.EnvFile(u.FilePath)))
		slog.Warn("Skipping (unresolved variable)", "image", u.ImageName, "variable", u.TagVar.Name, "path", u.FilePath)
		return
	}

	scheme, ok := versioning.ByName(u.Versioning, u.VersioningPattern)
	if !ok {
		// The config layer rejects an unknown name and an unusable pattern alike
		// on load, so one reaching here means the two sides have drifted apart.
		slog.Warn("Unusable versioning scheme, falling back to the default", "scheme", u.Versioning, "pattern", u.VersioningPattern, "image", u.ImageName, "path", u.FilePath)
		scheme = versioning.Default()
	}

	if _, readable := scheme.Parse(u.CurrentTag); !readable {
		// No version this scheme can read. Comparing manifest digests is then the
		// only way to tell whether the image moved on, which covers digest-pinned
		// images and repositories publishing commit tags instead of versions.
		c.checkDigest(u, p)
		return
	}

	tags, err := c.registry.Tags(u.ImageName)
	if err != nil {
		slog.Error("Skipping (failed fetching tags)", "image", u.ImageName, "path", u.FilePath)
		return
	}

	// One filter for both calls below, so the two walks share its budget and its
	// answers: they run over the same candidates, newest first.
	oldEnough := c.oldEnoughFilter(u, p.MinAgeDuration())

	// Every level is resolved so an interactive caller can offer a choice of
	// target; LatestTag stays the highest tag the requested flags allow.
	u.PatchTag, u.MinorTag, u.MajorTag = versioning.LatestPerLevelFunc(scheme, u.CurrentTag, tags, oldEnough)

	latest := versioning.LatestFunc(scheme, u.CurrentTag, tags, major, minor, patch, oldEnough)
	if latest == "" {
		// No tag to move to at any level, so a pinned reference that drifted where
		// it stands is the only thing left worth saying about this image. Gated on
		// there being no candidate at all rather than on the requested levels: with
		// one available, that update is the stronger news and an interactive caller
		// can still switch target to it.
		if u.PatchTag == "" && u.MinorTag == "" && u.MajorTag == "" && c.checkPinDrift(u) {
			return
		}

		// Ordinarily the image is simply on its newest release — unless nothing
		// here could ever be compared with the current tag, in which case no run
		// will offer this image anything and the user deserves to hear so.
		if !versioning.HasComparableTag(scheme, u.CurrentTag, tags) && !soleRelease(p, u.CurrentTag, tags) {
			u.MarkUnreadable(ReasonNoComparableTag, hint(p,
				fmt.Sprintf("%q reads as a version under %s, but no other tag of this image can be compared with it", u.CurrentTag, u.Versioning)))
		}
		return
	}
	u.LatestTag = latest

	// A reference pinning both a version and a digest (nginx:1.2.3@sha256:...)
	// must have its digest moved along with the tag, or the rewritten line would
	// point the new tag at the old image.
	if u.CurrentDigest != "" {
		digest, err := c.registry.Digest(u.ImageName + ":" + latest)
		if err != nil {
			slog.Error("Skipping (failed resolving digest for new tag)", "image", u.ImageName, "tag", latest, "path", u.FilePath)
			u.LatestTag = ""
			return
		}
		u.LatestDigest, u.digestFor = digest, latest
	}

	c.clampToCap(u)
	c.resolvePublished(u)
}

// maxAgeProbes caps how many candidate tags one image may have their build date
// fetched for. Each answer costs a manifest and a config blob, and a repository
// that publishes a burst of releases in one afternoon would otherwise have every
// one of them probed before min_age finds a tag old enough. Ten is well past the
// number of releases a settling window realistically spans; beyond it ccu offers
// nothing rather than spending the requests.
const maxAgeProbes = 10

// oldEnoughFilter is the rule min_age puts on a candidate tag, or nil when no
// minimum age applies to this image. Nil rather than a predicate that always
// says yes: versioning skips a nil filter entirely, which is what keeps a run
// without min_age from fetching a single build date for a candidate.
func (c *Checker) oldEnoughFilter(u *Update, minAge time.Duration) versioning.Eligible {
	if minAge <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-minAge)
	// Answers are remembered per tag as well as in the registry client, because
	// the budget must count tags asked about, not questions asked.
	answers := map[string]bool{}

	return func(tag string) bool {
		if answer, asked := answers[tag]; asked {
			return answer
		}
		if len(answers) >= maxAgeProbes {
			return false
		}

		published, err := c.registry.Created(u.ImageName + ":" + tag)
		if err != nil {
			// An age nobody can read is no reason to hide a release: min_age asks
			// for tags known to be young to be skipped, not for unknown ones to be.
			slog.Debug("Failed reading the build date, treating the tag as old enough",
				"image", u.ImageName, "tag", tag, "error", err)
			answers[tag] = true
			return true
		}

		answers[tag] = !published.After(cutoff)
		return answers[tag]
	}
}

// resolvePublished reads the build date of the tag this run settled on, for the
// report to show beside it. One request set per update, and only for the tag
// actually offered: it is display, not a decision.
func (c *Checker) resolvePublished(u *Update) {
	if u.LatestTag == "" {
		return
	}

	published, err := c.registry.Created(u.ImageName + ":" + u.LatestTag)
	if err != nil {
		slog.Debug("Failed reading the build date", "image", u.ImageName, "tag", u.LatestTag, "error", err)
		return
	}
	u.SetPublished(u.LatestTag, published)
}

// resolveSource reads where the image is built from, so the report can point at
// what changed. Only for an image that actually moves: this is a manifest and a
// config blob on top of the lookups the check itself needs, and paying that for
// every up-to-date image would double the traffic of a quiet run for nothing.
//
// A failure is not one: the label is metadata about an update ccu has already
// resolved, so anything that goes wrong here leaves the fields empty and the
// update itself untouched.
func (c *Checker) resolveSource(u *Update) {
	if u.IsUnreadable() || !u.HasNewVersion() {
		return
	}

	// Not part of Fetcher, so the stand-ins a test writes need not answer it.
	fetcher, ok := c.registry.(registry.SourceFetcher)
	if !ok {
		return
	}

	// The tag being moved to is the one whose labels describe the new release; a
	// drifted pin has none, and falls back to the tag the file names.
	tag := u.LatestTag
	if tag == "" {
		tag = u.CurrentTag
	}
	if tag == "" {
		return
	}

	source, err := fetcher.SourceURL(u.ImageName + ":" + tag)
	if err != nil {
		slog.Debug("Failed reading the source label", "image", u.ImageName, "tag", tag, "error", err)
		return
	}

	// Stored normalised, so every consumer sees the same spelling of a link that
	// registries carry in half a dozen forms.
	u.SourceURL, _ = registry.SourceLinks(source, "")
}

// clampToCap moves a selection the cap forbids down to it, rather than letting
// the update be dropped: an image capped at minor still has its minor update
// offered.
func (c *Checker) clampToCap(u *Update) {
	if u.LatestTag == "" || u.Cap.Allows(u.Level()) {
		return
	}

	u.SelectTarget(u.Cap)

	// SelectTarget drops a digest resolved for the tag it replaced, and a
	// reference that pins one cannot be written without it.
	if u.CurrentDigest != "" && u.LatestTag != "" {
		if err := u.ResolveDigest(c.registry); err != nil {
			slog.Warn("Skipping (failed resolving digest for capped tag)", "image", u.ImageName, "tag", u.LatestTag, "path", u.FilePath)
			u.LatestTag = ""
		}
	}
}

// checkPinDrift compares what the pinned tag resolves to now against the digest
// the file records for it, and reports whether the two have parted. A tag
// re-pushed under the same name is invisible to the version comparison — "0.1.0"
// stays "0.1.0" — so for a version tag this is the only place its drift can
// surface. Home-built images are the case: their version is bumped far less
// often than the image behind it is rebuilt.
//
// Only a reference pinning a digest can be checked. Without one the file records
// nothing to compare against, and ccu keeps no state between runs; pinning the
// digest is what buys the image this check.
func (c *Checker) checkPinDrift(u *Update) bool {
	if u.CurrentDigest == "" {
		return false
	}

	digest, err := c.registry.Digest(u.ImageName + ":" + u.CurrentTag)
	if err != nil {
		slog.Warn("Skipping (failed resolving digest for pinned tag)", "image", u.ImageName, "tag", u.CurrentTag, "path", u.FilePath)
		return false
	}
	if digest == u.CurrentDigest {
		return false
	}

	// The tag stays exactly as it is, only the digest under it moved, so there is
	// no LatestTag to set — and digestFor tracks that empty tag so the digest is
	// dropped should a caller later select a real target.
	u.LatestDigest, u.digestFor = digest, u.LatestTag
	return true
}

// soleRelease reports whether currentTag is the only release the repository
// publishes, its floating and reference tags aside. There is nothing to compare
// it with then — not because the tags cannot be read, but because nothing else
// has been released yet, which is the normal state of a private registry or a
// GHCR package holding one home-built image. That image is up to date, so it
// must not be reported as unreadable.
//
// The current tag has to be among the listed ones: a repository not even
// admitting to the tag the file names is a lookup gone wrong, and staying quiet
// about that would hide it.
func soleRelease(p policy.Image, currentTag string, tags []string) bool {
	if !slices.Contains(tags, currentTag) {
		return false
	}

	for _, tag := range tags {
		if tag == currentTag || tag == p.ReferenceTag || p.Floats(tag) {
			continue
		}
		return false
	}

	return true
}

// hint appends the way out of an unreadable image, worth naming only when the
// image is not already on the loose scheme — where loose could not read the tags
// either and suggesting it again would be no help at all.
func hint(p policy.Image, message string) string {
	if p.Versioning == policy.VersioningLoose {
		return message
	}
	return message + "; if this image's tags are versions, try `versioning: " + policy.VersioningLoose.String() + "` for it"
}

// updates reads the file and collects its image references, folding repeated
// ones into a single update: two services on the same image are one update, and
// a multi-stage Dockerfile must move both of its FROM lines together.
func (c *Checker) updates() ([]Update, error) {
	occurrences, err := c.occurrences()
	if err != nil {
		return nil, err
	}

	var updates []Update
	byImage := make(map[string]int)

	for _, occ := range occurrences {
		name, tag, digest := registry.ParseRef(occ.Reference)
		if !c.images.Match(name) {
			continue
		}
		key := name + ":" + tag + "@" + digest

		// Two spellings of one image are the same update — `nginx:1.2` and
		// `docker.io/library/nginx:1.2` move together. Two interpolated ones are
		// not: they resolve alike today but are written back to different
		// variables, so the spelling has to keep them apart.
		tagVar := tagVarFor(occ)
		if tagVar != nil {
			key = occ.Raw + "\x00" + key
		}

		service := occ.Service
		if c.composePath != "" {
			// A Dockerfile is only ever reached through the service that builds it.
			service = c.service
		}

		if i, seen := byImage[key]; seen {
			updates[i].Services = AppendService(updates[i].Services, service)
			updates[i].ExtraLines = appendLine(updates[i].ExtraLines, updates[i].RawLine, occ.Line)
			continue
		}

		byImage[key] = len(updates)
		updates = append(updates, Update{
			FilePath:    c.path,
			ComposePath: c.composePath,
			RawLine:     occ.Line,
			Services:    AppendService(nil, service),
			// The raw spelling, because this is what the user reads in the report
			// and what has to be found again in the line to rewrite it.
			FullImageName: occ.Raw,
			ImageName:     name,
			CurrentTag:    tag,
			CurrentDigest: digest,
			TagVar:        tagVar,
		})
	}

	return updates, nil
}

func (c *Checker) occurrences() ([]compose.Occurrence, error) {
	if c.composePath != "" {
		return compose.DockerfileImages(c.path)
	}
	return compose.Images(c.path)
}

// AppendService adds name unless it is empty or already there. An image declared
// outside any service contributes no name rather than an empty one.
func AppendService(services []string, name string) []string {
	if name == "" {
		return services
	}
	if slices.Contains(services, name) {
		return services
	}
	return append(services, name)
}

// appendLine adds line unless it is already covered by the update's own RawLine
// or by a line collected before it — Apply rewrites every matching line anyway.
func appendLine(extra []string, raw, line string) []string {
	if sameImageLine(line, raw) {
		return extra
	}
	for _, e := range extra {
		if sameImageLine(line, e) {
			return extra
		}
	}
	return append(extra, line)
}
