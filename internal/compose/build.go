package compose

import (
	"os"
	"path/filepath"
	"strings"
)

// BuildTarget is one Dockerfile a compose file builds, with the service that
// builds it. A Dockerfile nobody builds says nothing about what the stacks below
// the scanned directory are running.
type BuildTarget struct {
	Service    string
	Dockerfile string // resolved against the compose file
}

// BuildTargets reads composePath and returns the Dockerfiles its services build.
// An unreadable file yields nothing rather than an error, the caller already
// reporting one for it. A remote context or `dockerfile_inline:` is skipped:
// neither has a file on disk whose FROM lines could be rewritten.
func BuildTargets(composePath string) []BuildTarget {
	b := builds{dir: filepath.Dir(composePath), seen: map[string]bool{}}
	b.reset()

	tracker := newServiceTracker()
	// The read error is the unreadable file the caller already reports; a file
	// that ends early keeps the targets read out of it, as it did when this
	// scanned the lines itself.
	_ = eachLine(composePath, func(line string) {
		service := tracker.observe(line)

		// Blank and comment lines are already no key: keyPattern requires a
		// non-"#" character where the key starts.
		m := keyPattern.FindStringSubmatch(line)
		if m == nil {
			return
		}
		indent, key := len(m[1]), strings.TrimSpace(m[2])

		if b.indent >= 0 {
			if indent > b.indent {
				b.readKey(indent, key, line)
				return
			}
			b.flush()
		}

		// `build:` outside a service — in an x- block, say — builds nothing this
		// scan can attribute to a service.
		if key == "build" && service != "" {
			b.open(indent, service, scalarValue(line))
		}
	})
	b.flush()

	return b.targets
}

// builds is the `build:` block currently being read. indent below zero means the
// scan is not inside one.
type builds struct {
	dir  string
	seen map[string]bool

	indent      int
	childIndent int
	service     string
	context     string
	dockerfile  string
	inline      bool

	targets []BuildTarget
}

func (b *builds) reset() { b.indent, b.childIndent = -1, -1 }

// open starts a block. The short form carries the context on the key's own line
// (`build: ./keycloak`); the long form leaves it empty and fills it in from the
// nested keys.
func (b *builds) open(indent int, service, context string) {
	b.indent, b.childIndent = indent, -1
	b.service, b.context, b.dockerfile, b.inline = service, context, "", false
}

func (b *builds) readKey(indent int, key, line string) {
	// Only the block's direct children are its keys. Without this a build arg
	// named `context`, nested under `args:`, would decide where to look.
	if b.childIndent < 0 {
		b.childIndent = indent
	}
	if indent != b.childIndent {
		return
	}

	switch key {
	case "context":
		b.context = scalarValue(line)
	case "dockerfile":
		b.dockerfile = scalarValue(line)
	case "dockerfile_inline":
		b.inline = true
	}
}

func (b *builds) flush() {
	if b.indent < 0 {
		return
	}
	b.reset()

	if b.inline {
		return
	}
	path := dockerfilePath(b.dir, b.context, b.dockerfile)
	if path == "" || b.seen[path] {
		return
	}
	b.seen[path] = true
	b.targets = append(b.targets, BuildTarget{Service: b.service, Dockerfile: path})
}

// dockerfilePath resolves a build context and dockerfile name the way compose
// does — the dockerfile relative to the context, the context relative to the
// compose file — and returns "" when the result is not a readable file.
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

// isRemoteContext reports whether a context names something the daemon builds
// from elsewhere rather than a directory next to the compose file.
func isRemoteContext(context string) bool {
	return strings.Contains(context, "://") ||
		strings.HasPrefix(context, "git@") ||
		strings.HasPrefix(context, "github.com/")
}
