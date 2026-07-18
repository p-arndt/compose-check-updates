package internal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
)

type UpdateInfo struct {
	FilePath      string
	RawLine       string
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

	// Tag LatestDigest was resolved for. A digest only ever describes one
	// release, so switching target has to invalidate it.
	digestFor string
}

// TagForTarget returns the tag this image would move to at the given target
// level, degrading gracefully so a caller asking for "major" on an image that
// only has a patch available still gets an answer.
func (u *UpdateInfo) TagForTarget(target string) string {
	// A non-semver image moved by digest alone has no levels to choose between.
	if u.PatchTag == "" && u.MinorTag == "" && u.MajorTag == "" && u.IsDigestUpdate() {
		return u.LatestTag
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
	// A digest change carries no major/minor/patch level, so the level filters
	// cannot apply to it — it is either a different image or it is not.
	if u.IsDigestUpdate() {
		return true
	}

	if u.CurrentTag == "" || u.LatestTag == "" {
		return false
	}

	current, err := semver.NewVersion(u.CurrentTag)
	if err != nil {
		return false
	}

	latest, err := semver.NewVersion(u.LatestTag)
	if err != nil {
		return false
	}

	return latest.GreaterThan(current)
}

// UpdateLevel returns the semantic version increment level between CurrentTag and LatestTag.
// Possible values are "major", "minor", "patch", "digest" for changes that carry
// no version, or empty string when undetermined.
func (u *UpdateInfo) UpdateLevel() string {
	if u.CurrentTag == "" || u.LatestTag == "" {
		if u.IsDigestUpdate() {
			return "digest"
		}
		return ""
	}

	current, err := semver.NewVersion(u.CurrentTag)
	if err != nil {
		if u.IsDigestUpdate() {
			return "digest"
		}
		return ""
	}

	latest, err := semver.NewVersion(u.LatestTag)
	if err != nil {
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
	if latest.Patch() > current.Patch() {
		return "patch"
	}
	return ""
}

// replacement is a single substring rewrite to apply to an image line.
type replacement struct{ old, new string }

// replacements lists what has to change in the image reference. A reference can
// pin both a tag and a digest, in which case both move together so the tag never
// ends up pointing at the digest of the release it replaced.
func (u *UpdateInfo) replacements() []replacement {
	var reps []replacement

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

	// Do a backup of the original file
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

	// check if a backup file exists
	_, err := os.Stat(u.FilePath + ".ccu")
	if err != nil {
		if os.IsNotExist(err) {
			// if the file does not exist, create a backup
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
		if !strings.Contains(line, u.RawLine) {
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

func (u *UpdateInfo) Restart() error {
	dockerComposeCommand := "docker-compose"
	_, err := exec.LookPath(dockerComposeCommand)
	if err != nil {
		dockerComposeCommand = "docker compose"
		_, err = exec.LookPath(dockerComposeCommand)
		if err != nil {
			return err
		}
	}

	var cmd *exec.Cmd
	if dockerComposeCommand == "docker-compose" {
		cmd = exec.Command("docker-compose", "-f", u.FilePath, "up", "-d")
	} else {
		cmd = exec.Command("docker", "compose", "-f", u.FilePath, "up", "-d")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
