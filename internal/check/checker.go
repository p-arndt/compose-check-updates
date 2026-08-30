package check

import (
	"fmt"
	"log/slog"

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

	// Every level is resolved so an interactive caller can offer a choice of
	// target; LatestTag stays the highest tag the requested flags allow.
	u.PatchTag, u.MinorTag, u.MajorTag = versioning.LatestPerLevel(scheme, u.CurrentTag, tags)

	latest := versioning.Latest(scheme, u.CurrentTag, tags, major, minor, patch)
	if latest == "" {
		// Ordinarily the image is simply on its newest release — unless nothing
		// here could ever be compared with the current tag, in which case no run
		// will offer this image anything and the user deserves to hear so.
		if !versioning.HasComparableTag(scheme, u.CurrentTag, tags) {
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
		key := name + ":" + tag + "@" + digest

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
			FilePath:      c.path,
			ComposePath:   c.composePath,
			RawLine:       occ.Line,
			Services:      AppendService(nil, service),
			FullImageName: occ.Reference,
			ImageName:     name,
			CurrentTag:    tag,
			CurrentDigest: digest,
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
	for _, s := range services {
		if s == name {
			return services
		}
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
