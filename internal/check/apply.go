package check

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
	if u.movesTag() {
		switch {
		case u.TagVar == nil:
			// Literal tags are rewritten by position in Apply.
		case u.TagVar.FromDefault:
			// The value lives in the reference's own default, so the image line is
			// still where it is written — just the default rather than the tag.
			if value, ok := u.TagVar.valueFor(u.LatestTag); ok {
				if raw, ok := u.TagVar.rawWithValue(value); ok {
					reps = append(reps, replacement{u.TagVar.Raw, raw})
				}
			}
		}
		// A variable assigned in a .env is rewritten there, by applyEnv.
	}
	if u.CurrentDigest != "" && u.IsDigestUpdate() {
		reps = append(reps, replacement{u.CurrentDigest, u.LatestDigest})
	}
	return reps
}

// movesTag reports whether this update changes the tag, as opposed to moving a
// digest under an unchanged one.
func (u *Update) movesTag() bool {
	return u.CurrentTag != "" && u.LatestTag != "" && u.LatestTag != u.CurrentTag
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

func (u *Update) Backup() error { return backupFile(u.FilePath) }

// Apply rewrites every line of the file carrying this reference, after backing
// the file up once.
func (u *Update) Apply() error {
	if err := u.writable(); err != nil {
		return err
	}

	writeMu.Lock()
	defer writeMu.Unlock()

	// The .env goes first: leaving the compose file rewritten but the variable
	// behind on a failure would point the stack at a release that is only half
	// recorded, while the reverse order leaves both files simply unchanged.
	if err := u.applyEnv(); err != nil {
		return err
	}

	reps := u.replacements()
	var tagPattern *regexp.Regexp
	if u.TagVar == nil && u.movesTag() && !u.PinsFloating {
		// Require both tag boundaries: the same text can occur in a repository
		// name or a registry port (whose following slash must not match).
		tagPattern = regexp.MustCompile(`:` + regexp.QuoteMeta(u.CurrentTag) + `(?:$|[@\s"'])`)
	}
	if len(reps) == 0 && tagPattern == nil {
		return nil
	}

	return rewriteFile(u.FilePath, func(lines []string) {
		for i, line := range lines {
			// The whole line has to match, not merely start with the reference: with
			// `nginx:stable` and `nginx:stable-alpine` in the same file, a substring
			// match rewrote the second into "nginx:stable@sha256:…-alpine".
			if !u.rewrites(line) {
				continue
			}
			if tagPattern != nil {
				if loc := tagPattern.FindStringIndex(line); loc != nil {
					start := loc[0] + 1
					line = line[:start] + u.LatestTag + line[start+len(u.CurrentTag):]
				}
			}
			for _, r := range reps {
				line = strings.Replace(line, r.old, r.new, 1)
			}
			lines[i] = line
		}
	})
}

// applyEnv writes the new version into the .env assignment the tag is
// interpolated from, which for such an image is the only place the release is
// recorded at all.
func (u *Update) applyEnv() error {
	if u.TagVar == nil || u.TagVar.EnvPath == "" || !u.movesTag() {
		return nil
	}

	value, ok := u.TagVar.valueFor(u.LatestTag)
	if !ok {
		return fmt.Errorf("refusing to update %s: %q does not fit around %s", u.FullImageName, u.LatestTag, u.TagVar.Raw)
	}

	return rewriteFile(u.TagVar.EnvPath, func(lines []string) {
		// Sought by position, not by text: a .env may assign the same variable
		// twice, and compose reads the last one.
		i := u.TagVar.Env.LineNo - 1
		if i < 0 || i >= len(lines) || lines[i] != u.TagVar.Env.Line {
			return
		}
		lines[i] = u.TagVar.envLineWithValue(value)
	})
}

// rewriteFile backs a file up once and applies edit to its lines. The backup is
// what makes an update reversible, and one per file per run: a second image of
// the same file must not overwrite the copy taken before the first was written.
func rewriteFile(path string, edit func(lines []string)) error {
	if _, err := os.Stat(path + backupSuffix); os.IsNotExist(err) {
		if err := backupFile(path); err != nil {
			return err
		}
	}

	input, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(input), "\n")
	edit(lines)

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func backupFile(path string) error {
	input, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+backupSuffix, input, 0644)
}

// writable refuses the updates that would write something wrong, rather than
// doing nothing quietly and leaving a backup behind to suggest otherwise.
func (u *Update) writable() error {
	if u.IsUnreadable() {
		return fmt.Errorf("refusing to update %s: %s", u.FullImageName, u.UnreadableMessage)
	}
	// An interpolated tag is only ever written back through its variable. Writing
	// the new tag into the image line instead would drop the variable and with it
	// everything else reading it.
	if u.TagVar != nil && u.movesTag() {
		if u.TagVar.Unwritable != "" {
			return fmt.Errorf("refusing to update %s: %s", u.FullImageName, u.TagVar.Unwritable)
		}
		if _, ok := u.TagVar.valueFor(u.LatestTag); !ok {
			return fmt.Errorf("refusing to update %s: %q does not fit around %s", u.FullImageName, u.LatestTag, u.TagVar.Raw)
		}
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
