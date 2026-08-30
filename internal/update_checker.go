package internal

import (
	"bufio"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/regclient/regclient/types/ref"
)

type UpdateChecker struct {
	path     string
	registry *Registry

	// pinFloating turns on writing down what a bare floating tag resolves to.
	// Off by default: it costs one request per floating image and turns a
	// reference the user deliberately left mutable into a pinned one, so it has
	// to be asked for.
	pinFloating bool

	// versionings is the scheme to read each image's tags under, keyed by image
	// name without tag or digest, and defaultVersioning is what an image with no
	// entry gets. Held as names rather than resolved schemes so this stays a
	// plain pass-through of what the config said, resolved once per image below.
	versionings       map[string]string
	defaultVersioning string

	// composePath is the compose file that builds path, set only when path is a
	// Dockerfile reached through a service's `build:`. It is what tells the two
	// kinds of file apart here — and what a restart has to act on later, since
	// `docker compose up` knows about the service, not about its Dockerfile.
	composePath string
	// service is the compose service that builds path. Empty for a compose file,
	// whose services are read from the file itself.
	service string
}

func NewUpdateChecker(path string, registry *Registry) *UpdateChecker {
	if registry == nil {
		registry = NewRegistry("")
	}
	return &UpdateChecker{path: path, registry: registry}
}

// NewDockerfileChecker returns a checker for the Dockerfile a compose service
// builds. Everything past parsing is the same work a compose file gets — the
// base image of a self-built image moves exactly like any other image, it just
// lives on a FROM line — so the two share the whole checker below.
func NewDockerfileChecker(dockerfile, composePath, service string, registry *Registry) *UpdateChecker {
	checker := NewUpdateChecker(dockerfile, registry)
	checker.composePath, checker.service = composePath, service
	return checker
}

// WithPinFloating enables pinning bare floating tags to the digest they resolve
// to. See UpdateChecker.pinFloating.
func (u *UpdateChecker) WithPinFloating(on bool) *UpdateChecker {
	u.pinFloating = on
	return u
}

// WithVersioning sets the per-image versioning schemes and the default for
// images without one. See UpdateChecker.versionings.
func (u *UpdateChecker) WithVersioning(perImage map[string]string, def string) *UpdateChecker {
	u.versionings, u.defaultVersioning = perImage, def
	return u
}

// versioningFor returns the scheme name recorded for an image, falling back to
// the run's default. See ResolveVersioning.
func (u *UpdateChecker) versioningFor(image string) string {
	return ResolveVersioning(u.versionings, u.defaultVersioning, image)
}

func (u *UpdateChecker) Check(major, minor, patch bool) ([]UpdateInfo, error) {
	updateInfos, err := u.createUpdateInfos()
	if err != nil {
		return nil, err
	}

	for i := range updateInfos {
		info := &updateInfos[i]

		// Recorded on the update itself, because the level and the cap are worked
		// out again from the tags after this checker is gone.
		info.Versioning = u.versioningFor(info.ImageName)
		scheme, ok := VersioningByName(info.Versioning)
		if !ok {
			// The config layer rejects these on load, so one reaching here means
			// the two lists of scheme names have drifted apart. Falling back is
			// still the safe move, but doing it quietly would accept the user's
			// setting and then ignore it.
			slog.Warn("Unknown versioning scheme, falling back to the default", "scheme", info.Versioning, "image", info.ImageName, "path", info.FilePath)
			scheme = DefaultVersioning()
		}

		if _, readable := scheme.Parse(info.CurrentTag); !readable {
			// No version this scheme can read. Comparing manifest digests is then
			// the only way to tell whether the image moved on, which covers both
			// digest-pinned images and repositories that publish commit tags
			// instead of versions (e.g. ghcr.io/vert-sh/vert).
			u.checkDigest(info)
			continue
		}

		tags, err := u.registry.FetchImageTags(info.ImageName)
		if err != nil {
			slog.Error("Skipping (failed fetching tags)", "image", info.ImageName, "path", info.FilePath)
			continue
		}

		// Every level is resolved so an interactive caller can offer a choice of
		// target; LatestTag stays the highest tag the requested flags allow.
		info.PatchTag, info.MinorTag, info.MajorTag = FindLatestPerLevel(scheme, info.CurrentTag, tags)

		latestVersion := FindLatestVersion(scheme, info.CurrentTag, tags, major, minor, patch)
		if latestVersion == "" {
			continue
		}
		info.LatestTag = latestVersion

		// A reference that pins both a version and a digest (nginx:1.2.3@sha256:...)
		// must have its digest moved along with the tag, otherwise the rewritten
		// line would point the new tag at the old image.
		if info.CurrentDigest != "" {
			latestDigest, err := u.registry.FetchImageDigest(info.ImageName + ":" + latestVersion)
			if err != nil {
				slog.Error("Skipping (failed resolving digest for new tag)", "image", info.ImageName, "tag", latestVersion, "path", info.FilePath)
				info.LatestTag = ""
				continue
			}
			info.LatestDigest = latestDigest
			info.digestFor = latestVersion
		}
	}

	return updateInfos, nil
}

// checkDigest fills in the update fields for images whose tag is not a semantic
// version, by comparing the digest the reference currently resolves to against
// the digest of the repository's floating reference tag.
func (u *UpdateChecker) checkDigest(info *UpdateInfo) {
	// Whether the compose file itself pins a digest decides what gets rewritten
	// later: the digest in place, or the tag that now carries it.
	pinnedByDigest := info.CurrentDigest != ""

	// A bare floating tag records no digest in the compose file, so there is
	// nothing to compare it against — it already resolves to whatever is newest.
	// All that can be done for it is to write down what it resolves to today.
	if _, floating := mutableTags[info.CurrentTag]; floating && !pinnedByDigest {
		if !u.pinFloating {
			slog.Debug("Skipping (floating tag without digest)", "image", info.ImageName, "tag", info.CurrentTag, "path", info.FilePath)
			return
		}
		u.pinFloatingTag(info)
		return
	}
	if info.CurrentTag == "" && !pinnedByDigest {
		slog.Warn("Skipping (no tag or digest)", "image", info.ImageName, "path", info.FilePath)
		return
	}

	latestDigest, err := u.registry.FetchImageDigest(info.ImageName + ":" + referenceTag)
	if err != nil {
		slog.Warn("Skipping (no "+referenceTag+" tag to compare against)", "image", info.ImageName, "path", info.FilePath)
		return
	}

	// Digest-pinned references carry their current digest in the file itself.
	currentDigest := info.CurrentDigest
	if !pinnedByDigest {
		currentDigest, err = u.registry.FetchImageDigest(info.ImageName + ":" + info.CurrentTag)
		if err != nil {
			slog.Warn("Skipping (failed resolving current digest)", "image", info.ImageName, "tag", info.CurrentTag, "path", info.FilePath)
			return
		}
		info.CurrentDigest = currentDigest
	}

	if currentDigest == latestDigest {
		return
	}
	info.LatestDigest = latestDigest
	info.digestFor = info.LatestTag

	// A pinned digest is rewritten in place and the tag, if any, stays as it is.
	if pinnedByDigest {
		return
	}

	// Otherwise the tag is all there is to rewrite, so the tag now carrying the
	// new digest has to be found.
	tags, err := u.registry.FetchImageTags(info.ImageName)
	if err != nil {
		slog.Error("Skipping (failed fetching tags)", "image", info.ImageName, "path", info.FilePath)
		info.LatestDigest = ""
		return
	}

	candidates, dropped := digestCandidates(tags, info.CurrentTag)
	if dropped > 0 {
		slog.Warn("Only probing a subset of tags", "image", info.ImageName, "probed", len(candidates), "skipped", dropped)
	}

	latestTag := findTagForDigest(u.registry, info.ImageName, candidates, latestDigest)
	if latestTag == "" {
		// This is where an image with version-shaped tags ccu cannot read ends up,
		// so the way out is named rather than left to be discovered — unless the
		// image is already on that scheme, in which case loose could not read the
		// tags either and suggesting it again would be no help at all.
		message := "Skipping (no tag matches the newest digest)"
		if u.versioningFor(info.ImageName) != VersioningLoose {
			message += "; if this image's tags are versions, try `versioning: loose` for it"
		}
		slog.Warn(message, "image", info.ImageName, "tag", info.CurrentTag, "path", info.FilePath)
		info.LatestDigest = ""
		return
	}
	info.LatestTag = latestTag
	info.digestFor = latestTag
}

// CheckPins resolves nothing but the digests bare floating tags resolve to, and
// returns only the images it could pin. It exists for the caller that wants the
// pins after the fact — the TUI, once the user asks for them — without paying for
// a second full scan: no tag lists are fetched, one manifest head per floating
// image and nothing else.
func (u *UpdateChecker) CheckPins() ([]UpdateInfo, error) {
	infos, err := u.createUpdateInfos()
	if err != nil {
		return nil, err
	}

	var pins []UpdateInfo
	for i := range infos {
		info := &infos[i]

		// A reference that already carries a digest is not waiting to be pinned;
		// the ordinary scan reports its drift.
		if info.CurrentDigest != "" {
			continue
		}
		if _, floating := mutableTags[info.CurrentTag]; !floating {
			continue
		}

		u.pinFloatingTag(info)
		if info.PinsFloating {
			pins = append(pins, *info)
		}
	}

	return pins, nil
}

// pinFloatingTag records the digest a bare floating tag currently resolves to,
// so the reference can be rewritten as "latest@sha256:…". This is not an update
// — the image is the one the tag already pointed at — but the one-off pin that
// gives every later run something to compare against.
func (u *UpdateChecker) pinFloatingTag(info *UpdateInfo) {
	digest, err := u.registry.FetchImageDigest(info.ImageName + ":" + info.CurrentTag)
	if err != nil {
		slog.Warn("Skipping (failed resolving digest for floating tag)", "image", info.ImageName, "tag", info.CurrentTag, "path", info.FilePath)
		return
	}

	// The tag stays exactly as it is; only the digest is new. LatestTag is set
	// all the same, so the consumers that read it — the report, the TUI's row —
	// have the tag the pin belongs to rather than an empty field.
	info.LatestTag = info.CurrentTag
	info.LatestDigest = digest
	info.digestFor = info.CurrentTag
	info.PinsFloating = true
}

func (u *UpdateChecker) createUpdateInfos() ([]UpdateInfo, error) {
	if u.composePath != "" {
		return u.createDockerfileUpdateInfos()
	}

	var updateInfos []UpdateInfo
	// Index rather than a set: a repeated reference has to find the entry it
	// duplicates so its service name can be added to it.
	byImage := make(map[string]int)

	file, err := os.Open(u.path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	tracker := newServiceTracker()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		service := tracker.observe(line)

		matches := imageNamePattern.FindStringSubmatch(line)
		if len(matches) <= 1 {
			continue
		}

		imageName := matches[1]
		name, tag, dgst := u.getNameTagAndDigest(imageName)
		imageKey := name + ":" + tag + "@" + dgst

		if i, exists := byImage[imageKey]; exists {
			updateInfos[i].Services = AppendService(updateInfos[i].Services, service)
			continue
		}

		byImage[imageKey] = len(updateInfos)
		updateInfos = append(updateInfos, UpdateInfo{
			FilePath:      u.path,
			RawLine:       line,
			Services:      AppendService(nil, service),
			FullImageName: imageName,
			ImageName:     name,
			CurrentTag:    tag,
			CurrentDigest: dgst,
		})
	}

	return updateInfos, nil
}

// createDockerfileUpdateInfos is createUpdateInfos for a Dockerfile: the base
// images its FROM lines name, minus the ones no registry can answer for.
//
// A multi-stage build usually names the same base twice — once as the builder,
// once as the runtime — and both lines have to move together, or the next build
// copies artefacts from one release into another. So the duplicates are folded
// into a single update carrying every line it has to rewrite.
func (u *UpdateChecker) createDockerfileUpdateInfos() ([]UpdateInfo, error) {
	var updateInfos []UpdateInfo
	byImage := make(map[string]int)
	// Stage names declared by earlier FROM lines. A later FROM naming one of
	// them builds on this file, not on anything a registry has heard of.
	stages := make(map[string]struct{})

	file, err := os.Open(u.path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		imageName, stage, ok := parseFrom(line)
		if !ok {
			continue
		}
		// A stage declared by this very line is recorded after it has been
		// checked itself: `FROM x AS x` still names the image x.
		skip := false
		if _, isStage := stages[strings.ToLower(imageName)]; isStage {
			skip = true
		}
		// The empty image of `FROM scratch` has no repository behind it, and an
		// ARG-interpolated reference names a tag only the build knows.
		if strings.EqualFold(imageName, "scratch") || strings.Contains(imageName, "$") {
			skip = true
		}
		if stage != "" {
			stages[strings.ToLower(stage)] = struct{}{}
		}
		if skip {
			continue
		}

		name, tag, dgst := u.getNameTagAndDigest(imageName)
		imageKey := name + ":" + tag + "@" + dgst

		if i, exists := byImage[imageKey]; exists {
			updateInfos[i].ExtraLines = appendLine(updateInfos[i].ExtraLines, updateInfos[i].RawLine, line)
			continue
		}

		byImage[imageKey] = len(updateInfos)
		updateInfos = append(updateInfos, UpdateInfo{
			FilePath:      u.path,
			ComposePath:   u.composePath,
			RawLine:       line,
			Services:      AppendService(nil, u.service),
			FullImageName: imageName,
			ImageName:     name,
			CurrentTag:    tag,
			CurrentDigest: dgst,
		})
	}

	return updateInfos, nil
}

// appendLine adds line to extra unless it is already covered — by the update's
// own RawLine or by a line collected before it. Update() rewrites every matching
// line in the file, so a repeated spelling needs no second entry.
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

// AppendService adds name to services unless it is empty or already there. An
// image declared outside any service — a top-level x- block, say — simply
// contributes no name rather than an empty one.
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

var (
	imageNamePattern = regexp.MustCompile(`^\s*image:\s*(\S+)\s*$`)
	// A mapping key, with whatever it may carry on the same line. Only the
	// indentation and the key itself matter here.
	yamlKeyPattern = regexp.MustCompile(`^(\s*)([^\s#][^:]*):(\s|$)`)
	servicesKey    = regexp.MustCompile(`^(\s*)services:\s*$`)
)

// serviceTracker follows which service block the scanner is currently inside,
// by indentation alone. Parsing the file as YAML would answer this properly, but
// the rest of the checker works line by line so it can rewrite the exact line it
// read; this keeps that property and is enough for the shapes compose files
// actually take.
type serviceTracker struct {
	servicesIndent int // indentation of the `services:` key, -1 when outside it
	serviceIndent  int // indentation of the service names below it, -1 until seen
	current        string
}

func newServiceTracker() *serviceTracker {
	return &serviceTracker{servicesIndent: -1, serviceIndent: -1}
}

// observe feeds one line to the tracker and returns the service the line belongs
// to, empty when it belongs to none.
func (t *serviceTracker) observe(line string) string {
	if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
		return t.current
	}

	if m := servicesKey.FindStringSubmatch(line); m != nil {
		t.servicesIndent, t.serviceIndent, t.current = len(m[1]), -1, ""
		return ""
	}

	m := yamlKeyPattern.FindStringSubmatch(line)
	if m == nil {
		return t.current
	}
	indent := len(m[1])

	// Nothing below `services:` has been seen yet, or the block has ended.
	if t.servicesIndent < 0 {
		return ""
	}
	if indent <= t.servicesIndent {
		t.servicesIndent, t.serviceIndent, t.current = -1, -1, ""
		return ""
	}

	// The first key inside the block fixes the depth service names live at.
	if t.serviceIndent < 0 {
		t.serviceIndent = indent
	}
	if indent == t.serviceIndent {
		t.current = strings.TrimSpace(m[2])
	}

	return t.current
}

func (u *UpdateChecker) naiveParsing(imageName string) (string, string) {
	parts := strings.Split(imageName, ":")
	if len(parts) < 2 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func (u *UpdateChecker) getNameTagAndDigest(imageName string) (string, string, string) {
	// Split off an explicit digest first, so the colon inside "@sha256:..."
	// cannot be mistaken for the separator introducing a tag.
	remainder := imageName
	dgst := ""
	if at := strings.LastIndex(imageName, "@"); at != -1 {
		remainder = imageName[:at]
		if candidate := imageName[at+1:]; IsDigest(candidate) {
			dgst = candidate
		}
	}

	// A colon after the last slash introduces a tag; one before it is a port.
	lastSlash := strings.LastIndex(remainder, "/")
	lastColon := strings.LastIndex(remainder, ":")
	hasTag := lastColon > lastSlash

	rRef, err := ref.New(imageName)
	if err != nil {
		name, tag := u.naiveParsing(remainder)
		return name, tag, dgst
	}

	name := rRef.Repository
	if rRef.Registry != "" && rRef.Registry != "docker.io" && rRef.Registry != "index.docker.io" {
		name = rRef.Registry + "/" + rRef.Repository
	}

	tag := ""
	if hasTag {
		tag = rRef.Tag
	}

	return name, tag, dgst
}
