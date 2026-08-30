package check

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const backupSuffix = ".ccu"

// writeMu serializes the read-modify-write below. Every image of a file is
// updated in its own goroutine, so without this their writes overwrite each
// other and only the last image to finish keeps its new version.
var writeMu sync.Mutex

// execCommand and lookPath are the only two points where this package reaches
// out to the host. They are variables so tests can cover the compose shell-out
// without a Docker daemon; production never reassigns them.
var (
	execCommand = exec.Command
	lookPath    = exec.LookPath
)

// replacement is a single substring rewrite to apply to an image line.
type replacement struct{ old, new string }

// replacements lists what has to change in the reference. A reference can pin
// both a tag and a digest, in which case both move together so the tag never
// ends up pointing at the digest of the release it replaced.
func (u *Update) replacements() []replacement {
	// A pin adds a digest where there was none, so there is nothing to
	// substitute. The full reference rather than the bare tag, because "latest"
	// may appear in the repository name too.
	if u.PinsFloating && u.LatestDigest != "" {
		return []replacement{{u.FullImageName, u.FullImageName + "@" + u.LatestDigest}}
	}

	var reps []replacement
	if u.CurrentTag != "" && u.LatestTag != "" && u.LatestTag != u.CurrentTag {
		reps = append(reps, replacement{u.CurrentTag, u.LatestTag})
	}
	if u.CurrentDigest != "" && u.IsDigestUpdate() {
		reps = append(reps, replacement{u.CurrentDigest, u.LatestDigest})
	}
	return reps
}

// sameImageLine reports whether line is the one raw was scanned from. Trailing
// blanks and the \r a CRLF file leaves behind are ignored.
func sameImageLine(line, raw string) bool {
	const blanks = " \t\r"
	return strings.TrimRight(line, blanks) == strings.TrimRight(raw, blanks)
}

// rewrites reports whether line is one this update has to change.
func (u *Update) rewrites(line string) bool {
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

func (u *Update) Backup() error {
	input, err := os.ReadFile(u.FilePath)
	if err != nil {
		return err
	}
	return os.WriteFile(u.FilePath+backupSuffix, input, 0644)
}

// Apply rewrites every line of the file carrying this reference, after backing
// the file up once.
func (u *Update) Apply() error {
	if err := u.writable(); err != nil {
		return err
	}

	writeMu.Lock()
	defer writeMu.Unlock()

	if _, err := os.Stat(u.FilePath + backupSuffix); os.IsNotExist(err) {
		if err := u.Backup(); err != nil {
			return err
		}
	}

	input, err := os.ReadFile(u.FilePath)
	if err != nil {
		return err
	}

	reps := u.replacements()
	lines := strings.Split(string(input), "\n")
	for i, line := range lines {
		// The whole line has to match, not merely start with the reference: with
		// `nginx:stable` and `nginx:stable-alpine` in the same file, a substring
		// match rewrote the second into "nginx:stable@sha256:…-alpine".
		if !u.rewrites(line) {
			continue
		}
		for _, r := range reps {
			line = strings.Replace(line, r.old, r.new, 1)
		}
		lines[i] = line
	}

	return os.WriteFile(u.FilePath, []byte(strings.Join(lines, "\n")), 0644)
}

// writable refuses the updates that would write something wrong, rather than
// doing nothing quietly and leaving a backup behind to suggest otherwise.
func (u *Update) writable() error {
	if u.IsUnreadable() {
		return fmt.Errorf("refusing to update %s: %s", u.FullImageName, u.UnreadableMessage)
	}
	if u.CurrentDigest == "" {
		return nil
	}
	// Writing a tag next to the digest of some other release would silently pin
	// the wrong image, which is worse than refusing.
	if u.LatestDigest == "" {
		return fmt.Errorf("refusing to update %s: no digest resolved for tag %q", u.FullImageName, u.LatestTag)
	}
	if u.digestFor != u.LatestTag {
		return fmt.Errorf("refusing to update %s: digest was resolved for tag %q, not %q", u.FullImageName, u.digestFor, u.LatestTag)
	}
	return nil
}

// Restart brings the stack this update belongs to back up on the new image.
func (u *Update) Restart() error {
	compose, err := composeCommand()
	if err != nil {
		return err
	}

	args := append(append([]string{}, compose[1:]...), "-f", u.RestartPath(), "up", "-d")
	// A new base image only reaches the running container once the image is built
	// again.
	if u.IsDockerfile() {
		args = append(args, "--build")
	}

	cmd := execCommand(compose[0], args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// composeCommand returns the argv prefix for the compose CLI on this host,
// preferring the plugin over the legacy binary.
func composeCommand() ([]string, error) {
	if _, err := lookPath("docker"); err == nil {
		// `docker` exists, but the compose plugin may not be installed.
		if err := execCommand("docker", "compose", "version").Run(); err == nil {
			return []string{"docker", "compose"}, nil
		}
	}
	if _, err := lookPath("docker-compose"); err == nil {
		return []string{"docker-compose"}, nil
	}
	return nil, fmt.Errorf("neither `docker compose` nor `docker-compose` is available in $PATH")
}
