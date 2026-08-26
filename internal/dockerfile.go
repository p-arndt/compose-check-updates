package internal

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// BuildTarget is one Dockerfile a compose file builds, together with the service
// that builds it. Only Dockerfiles reached through a service's `build:` are
// interesting: a Dockerfile nobody builds says nothing about what the stacks
// below the scanned directory are actually running.
type BuildTarget struct {
	Service    string // the compose service whose build produces this image
	Dockerfile string // path to the Dockerfile, resolved against the compose file
}

// GetBuildTargets reads composePath and returns the Dockerfiles its services
// build. A file that cannot be read yields nothing rather than an error: the
// caller is already checking this compose file for image updates and a second
// failure about the same unreadable file would only be noise.
//
// Contexts that are not a local path — a git URL, say — are skipped, as is a
// service using `dockerfile_inline:`: neither has a file on disk whose FROM
// lines could be rewritten.
func GetBuildTargets(composePath string) []BuildTarget {
	file, err := os.Open(composePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	dir := filepath.Dir(composePath)
	tracker := newServiceTracker()

	var targets []BuildTarget
	seen := make(map[string]bool)

	// State of the `build:` block currently being read. buildIndent below zero
	// means the scan is not inside one.
	buildIndent, childIndent := -1, -1
	var buildService, context, dockerfile string
	inline := false

	flush := func() {
		if buildIndent < 0 {
			return
		}
		buildIndent, childIndent = -1, -1

		if inline {
			return
		}
		path := dockerfilePath(dir, context, dockerfile)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		targets = append(targets, BuildTarget{Service: buildService, Dockerfile: path})
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		service := tracker.observe(line)

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		m := yamlKeyPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent, key := len(m[1]), strings.TrimSpace(m[2])

		// Inside a build block: its own keys are read, anything at or above its
		// indentation ends it.
		if buildIndent >= 0 {
			if indent > buildIndent {
				// Only the block's direct children are its keys. Without this a
				// build arg named `context` — nested one level deeper, under
				// `args:` — would decide where the Dockerfile is looked for.
				if childIndent < 0 {
					childIndent = indent
				}
				if indent != childIndent {
					continue
				}
				switch key {
				case "context":
					context = scalarValue(line)
				case "dockerfile":
					dockerfile = scalarValue(line)
				case "dockerfile_inline":
					inline = true
				}
				continue
			}
			flush()
		}

		// `build:` outside a service — in an x- block, say — builds nothing this
		// scan can attribute to a service, so it is left alone.
		if key == "build" && service != "" {
			buildIndent, childIndent, buildService = indent, -1, service
			// The short form carries the context on the key's own line
			// (`build: ./keycloak`); the long form leaves it empty here and fills
			// it in from the nested keys above.
			context, dockerfile, inline = scalarValue(line), "", false
		}
	}
	flush()

	return targets
}

// dockerfilePath resolves a build context and dockerfile name the way compose
// does — the dockerfile relative to the context, the context relative to the
// compose file — and returns "" when the result is not a readable file. Both
// keys are optional and default to "." and "Dockerfile".
func dockerfilePath(dir, context, dockerfile string) string {
	if isRemoteContext(context) {
		return ""
	}
	if context == "" {
		context = "."
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	base := context
	if !filepath.IsAbs(base) {
		base = filepath.Join(dir, base)
	}
	path := dockerfile
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}

// isRemoteContext reports whether a context names something built by the daemon
// from elsewhere rather than a directory next to the compose file.
func isRemoteContext(context string) bool {
	return strings.Contains(context, "://") ||
		strings.HasPrefix(context, "git@") ||
		strings.HasPrefix(context, "github.com/")
}

// scalarValue reads the value written after a mapping key on the same line,
// without the quotes a user may have wrapped it in and without a trailing
// comment. An empty result means the key opened a block instead.
func scalarValue(line string) string {
	_, rest, ok := strings.Cut(line, ":")
	if !ok {
		return ""
	}

	value := strings.TrimSpace(rest)
	// A key carrying nothing but a comment (`build:  # built locally`) opens a
	// block; reading the comment as its value would send the path resolution
	// looking for a directory named after it.
	if strings.HasPrefix(value, "#") {
		return ""
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return strings.Trim(value, `"'`)
}

// fromPattern captures the image of a Dockerfile FROM line, plus the stage name
// it may be given. Flags such as --platform sit between the two and are skipped;
// the reference itself is the first argument that is not one. A trailing comment
// is part of the line and must not hide it: a FROM this fails to match is a
// stage silently left behind on the old base image.
var fromPattern = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--\S+\s+)*(\S+)(?:\s+AS\s+(\S+))?\s*(?:#.*)?$`)

// parseFrom reads one line as a FROM instruction, reporting the image it names
// and the stage it declares. ok is false for every other line.
func parseFrom(line string) (image, stage string, ok bool) {
	m := fromPattern.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
