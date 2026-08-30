package internal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// LevelPin is the level of a floating tag being pinned to the digest it
// currently resolves to. Unlike the semver levels it describes no change to the
// image: it writes down which image "latest" means today, so that every later
// run can tell that it has moved on.
const LevelPin = "pin"

type UpdateInfo struct {
	FilePath string
	RawLine  string
	// ExtraLines are further lines of FilePath carrying this same reference, all
	// of which are rewritten together. A multi-stage Dockerfile names its base
	// image once per stage — `FROM x:1 AS builder` and `FROM x:1` — and moving
	// only one of them would build the next image from two different releases.
	ExtraLines []string
	// ComposePath is the compose file whose service builds FilePath, set only
	// when FilePath is a Dockerfile. Empty means FilePath is the compose file
	// itself. It is what a restart acts on: compose knows the service, not the
	// Dockerfile behind it.
	ComposePath string
	// Services names the compose services that declare this image. It is a list
	// because identical references are collapsed into one entry: two services on
	// the same image are one update to make, but both names are worth reporting.
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

	// Cap is the highest level this image may move to ("patch"/"minor"/"major");
	// empty means no cap. It is kept as a plain string so this package stays
	// independent of wherever the preference was read from.
	Cap string

	// Versioning names the scheme this image's tags are read under ("semver" or
	// "loose"); empty means the default. A plain string for the same reason Cap
	// is one, and carried on the update itself because the level and the cap are
	// re-derived from the tags long after the checker that resolved them is gone.
	Versioning string

	// PinsFloating marks the one update that adds a digest instead of changing
	// one: a bare floating tag ("latest") gaining the digest it resolves to right
	// now. Nothing about the image moved, so it carries no level of its own and
	// no cap has anything to say about it — see LevelPin.
	PinsFloating bool

	// Tag LatestDigest was resolved for. A digest only ever describes one
	// release, so switching target has to invalidate it.
	digestFor string
}

// levelRank orders the levels a cap can be expressed in. A level missing from
// this map is one no cap can speak about — "digest", for one, which carries no
// version to be higher or lower than anything.
var levelRank = map[string]int{"patch": 1, "minor": 2, "major": 3}

// AllowsLevel reports whether an update of the given level stays within Cap, for
// callers deciding whether a selection has to be moved before it is offered.
func (u *UpdateInfo) AllowsLevel(level string) bool { return u.allowsLevel(level) }

// allowsLevel reports whether an update of the given level stays within Cap. An
// empty or unrecognised cap permits everything: a cap nobody can interpret must
// not silently hide every update the user came here to see.
func (u *UpdateInfo) allowsLevel(level string) bool {
	capRank, ok := levelRank[u.Cap]
	if !ok {
		return true
	}
	want, ok := levelRank[level]
	if !ok {
		return true
	}
	return want <= capRank
}

// TagForTarget returns the tag this image would move to at the given target
// level, degrading gracefully so a caller asking for "major" on an image that
// only has a patch available still gets an answer.
func (u *UpdateInfo) TagForTarget(target string) string {
	// A non-semver image moved by digest alone has no levels to choose between.
	if u.PatchTag == "" && u.MinorTag == "" && u.MajorTag == "" && u.IsDigestUpdate() {
		return u.LatestTag
	}

	// The cap clamps the request rather than the answer, so a capped "major"
	// behaves exactly as asking for the cap would, degradation included.
	if !u.allowsLevel(target) {
		target = u.Cap
	}

	switch target {
	case "major":
		if u.MajorTag != "" {
			return u.MajorTag
		}
		fallthrough
	case "minor":
		if u.MinorTag != "" {
			return u.MinorTag
		}
		fallthrough
	case "patch":
		return u.PatchTag
	}

	return ""
}

// AvailableTargets lists which of "patch"/"minor"/"major" have a distinct tag
// available, in that order, so a consumer only offers levels that exist here.
func (u *UpdateInfo) AvailableTargets() []string {
	var targets []string
	seen := make(map[string]struct{})

	for _, t := range []struct{ name, tag string }{
		{"patch", u.PatchTag},
		{"minor", u.MinorTag},
		{"major", u.MajorTag},
	} {
		if t.tag == "" {
			continue
		}
		// A level above the cap is not a choice this image has.
		if !u.allowsLevel(t.name) {
			continue
		}
		if _, dup := seen[t.tag]; dup {
			continue
		}
		seen[t.tag] = struct{}{}
		targets = append(targets, t.name)
	}

	return targets
}

// SelectTarget points LatestTag at the tag for the given level and reports
// whether that changed anything. Nothing is selected when the level has no tag,
// rather than clearing an already valid selection.
func (u *UpdateInfo) SelectTarget(target string) bool {
	tag := u.TagForTarget(target)
	if tag == "" || tag == u.LatestTag {
		u.invalidateStaleDigest()
		return false
	}

	u.LatestTag = tag
	u.invalidateStaleDigest()
	return true
}

// invalidateStaleDigest drops a digest that was resolved for a different tag, so
// a mismatched tag/digest pair can never reach the compose file.
func (u *UpdateInfo) invalidateStaleDigest() {
	if u.digestFor != u.LatestTag {
		u.LatestDigest = ""
	}
}

// ResolveDigest fills in the digest belonging to the currently selected tag.
// Only references that pin a digest need one; for the rest there is nothing to
// rewrite and no registry call to make.
func (u *UpdateInfo) ResolveDigest(reg *Registry) error {
	if u.CurrentDigest == "" || u.LatestTag == "" {
		return nil
	}
	if u.LatestDigest != "" && u.digestFor == u.LatestTag {
		return nil
	}

	digest, err := reg.FetchImageDigest(u.ImageName + ":" + u.LatestTag)
	if err != nil {
		return err
	}

	u.LatestDigest = digest
	u.digestFor = u.LatestTag
	return nil
}

// IsDigestUpdate reports whether the image moved to a different manifest without
// a semantic version to describe the change.
func (u *UpdateInfo) IsDigestUpdate() bool {
	return u.LatestDigest != "" && u.LatestDigest != u.CurrentDigest
}

func (u *UpdateInfo) HasNewVersion(major, minor, patch bool) bool {
	// Pinning a floating tag is something to do regardless of the level filters:
	// they speak about versions, and this one writes a digest.
	if u.PinsFloating {
		return true
	}

	// A digest change carries no major/minor/patch level, so the level filters
	// cannot apply to it — it is either a different image or it is not.
	if u.IsDigestUpdate() {
		return true
	}

	if u.CurrentTag == "" || u.LatestTag == "" {
		return false
	}

	scheme := u.versioning()

	current, ok := scheme.Parse(u.CurrentTag)
	if !ok {
		return false
	}

	latest, ok := scheme.Parse(u.LatestTag)
	if !ok {
		return false
	}

	if !latest.GreaterThan(current) {
		return false
	}

	// An update above the cap is one this image may never take, so it does not
	// count as having a new version at all.
	return u.allowsLevel(u.UpdateLevel())
}

// UpdateLevel returns the semantic version increment level between CurrentTag and LatestTag.
// Possible values are "major", "minor", "patch", "digest" for changes that carry
// no version, LevelPin for a floating tag being given the digest it resolves to,
// or empty string when undetermined.
func (u *UpdateInfo) UpdateLevel() string {
	// Checked before the digest cases below, which would otherwise report a pin
	// as a digest update — the digest is new to the file, but the image behind it
	// is the one the tag already pointed at.
	if u.PinsFloating {
		return LevelPin
	}

	if u.CurrentTag == "" || u.LatestTag == "" {
		if u.IsDigestUpdate() {
			return "digest"
		}
		return ""
	}

	scheme := u.versioning()

	current, ok := scheme.Parse(u.CurrentTag)
	if !ok {
		if u.IsDigestUpdate() {
			return "digest"
		}
		return ""
	}

	latest, ok := scheme.Parse(u.LatestTag)
	if !ok {
		if u.IsDigestUpdate() {
			return "digest"
		}
		return ""
	}

	if latest.Major() > current.Major() {
		return "major"
	}
	if latest.Minor() > current.Minor() {
		return "minor"
	}
	// Anything left that still moved forward is a patch, which is how a fourth
	// segment advancing on its own is reported: "2026.7.7.2" rebuilds the release
	// "2026.7.7" names rather than being a release of its own.
	if latest.GreaterThan(current) {
		return "patch"
	}
	return ""
}

// versioning is the scheme this image's tags are read under. An unrecognised
// name falls back to the default rather than failing: the config layer rejects
// those on load, so one reaching this far is not worth hiding every update over.
func (u *UpdateInfo) versioning() Versioning {
	scheme, ok := VersioningByName(u.Versioning)
	if !ok {
		return DefaultVersioning()
	}
	return scheme
}

// replacement is a single substring rewrite to apply to an image line.
type replacement struct{ old, new string }

// replacements lists what has to change in the image reference. A reference can
// pin both a tag and a digest, in which case both move together so the tag never
// ends up pointing at the digest of the release it replaced.
func (u *UpdateInfo) replacements() []replacement {
	var reps []replacement

	// A pin adds a digest where the reference had none, so there is nothing to
	// substitute: the whole reference is replaced by itself plus the digest. The
	// full reference rather than the bare tag, because "latest" may well appear in
	// the repository name too and only the first occurrence would be rewritten.
	if u.PinsFloating && u.LatestDigest != "" {
		return []replacement{{u.FullImageName, u.FullImageName + "@" + u.LatestDigest}}
	}

	if u.CurrentTag != "" && u.LatestTag != "" && u.LatestTag != u.CurrentTag {
		reps = append(reps, replacement{u.CurrentTag, u.LatestTag})
	}
	if u.CurrentDigest != "" && u.IsDigestUpdate() {
		reps = append(reps, replacement{u.CurrentDigest, u.LatestDigest})
	}

	return reps
}

func (u *UpdateInfo) Backup() error {
	input, err := os.ReadFile(u.FilePath)
	if err != nil {
		return err
	}

	err = os.WriteFile(u.FilePath+".ccu", input, 0644)
	if err != nil {
		return err
	}

	return nil
}

// updateMu serializes the read-modify-write below. Every image of a compose
// file is updated in its own goroutine, so without this their writes overwrite
// each other and only the last image to finish keeps its new version.
var updateMu sync.Mutex

// sameImageLine reports whether line is the one RawLine was scanned from.
// Trailing blanks are ignored along with the \r, so a line differing from the
// scanned one only in invisible characters is still rewritten.
func sameImageLine(line, raw string) bool {
	const blanks = " \t\r"
	return strings.TrimRight(line, blanks) == strings.TrimRight(raw, blanks)
}

// rewrites reports whether line is one this update has to change: the line it
// was scanned from, or one of the further lines carrying the same reference.
func (u *UpdateInfo) rewrites(line string) bool {
	if sameImageLine(line, u.RawLine) {
		return true
	}
	for _, extra := range u.ExtraLines {
		if sameImageLine(line, extra) {
			return true
		}
	}
	return false
}

// IsDockerfile reports whether this update sits in a Dockerfile built by a
// compose service rather than in a compose file.
func (u *UpdateInfo) IsDockerfile() bool { return u.ComposePath != "" }

// RestartPath is the compose file to hand `docker compose -f` for this update:
// the Dockerfile's owning compose file, or the file the update itself sits in.
func (u *UpdateInfo) RestartPath() string {
	if u.ComposePath != "" {
		return u.ComposePath
	}
	return u.FilePath
}

func (u *UpdateInfo) Update() error {
	// A reference that pins a digest gets both tag and digest rewritten. Writing
	// a tag next to the digest of some other release would silently pin the wrong
	// image, which is worse than refusing the update.
	if u.CurrentDigest != "" {
		if u.LatestDigest == "" {
			return fmt.Errorf("refusing to update %s: no digest resolved for tag %q", u.FullImageName, u.LatestTag)
		}
		if u.digestFor != u.LatestTag {
			return fmt.Errorf("refusing to update %s: digest was resolved for tag %q, not %q", u.FullImageName, u.digestFor, u.LatestTag)
		}
	}

	updateMu.Lock()
	defer updateMu.Unlock()

	_, err := os.Stat(u.FilePath + ".ccu")
	if err != nil {
		if os.IsNotExist(err) {
			err = u.Backup()
			if err != nil {
				return err
			}
		}
	}

	input, err := os.ReadFile(u.FilePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(input), "\n")
	for i, line := range lines {
		// The whole line has to match, not merely start with the reference: with
		// `nginx:stable` and `nginx:stable-alpine` in the same file, a substring
		// match rewrote the second one into "nginx:stable@sha256:…-alpine". The \r
		// is what a CRLF file leaves behind once the content is split on \n; the
		// scanner that produced RawLine had already dropped it.
		if !u.rewrites(line) {
			continue
		}
		for _, r := range u.replacements() {
			line = strings.Replace(line, r.old, r.new, 1)
		}
		lines[i] = line
	}

	output := strings.Join(lines, "\n")
	err = os.WriteFile(u.FilePath, []byte(output), 0644)
	if err != nil {
		return err
	}

	return nil
}

// composeCommand returns the argv prefix for the compose CLI available on this
// host, preferring the `docker compose` plugin over the legacy `docker-compose`
// binary.
func composeCommand() ([]string, error) {
	if _, err := exec.LookPath("docker"); err == nil {
		// `docker` exists, but the compose plugin may not be installed.
		if err := exec.Command("docker", "compose", "version").Run(); err == nil {
			return []string{"docker", "compose"}, nil
		}
	}

	if _, err := exec.LookPath("docker-compose"); err == nil {
		return []string{"docker-compose"}, nil
	}

	return nil, fmt.Errorf("neither `docker compose` nor `docker-compose` is available in $PATH")
}

func (u *UpdateInfo) Restart() error {
	compose, err := composeCommand()
	if err != nil {
		return err
	}

	args := append(append([]string{}, compose[1:]...), "-f", u.RestartPath(), "up", "-d")

	// A new base image only reaches the running container once the image is
	// built again — without this, restarting a self-built service would come
	// back up on exactly the image it was already running.
	if u.IsDockerfile() {
		args = append(args, "--build")
	}
	cmd := exec.Command(compose[0], args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
