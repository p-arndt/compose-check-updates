package registry

import (
	"regexp"
	"strings"

	"github.com/regclient/regclient/types/ref"
)

var digestPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[0-9a-fA-F]{32,}$`)

// IsDigest reports whether s is a digest reference rather than a tag.
func IsDigest(s string) bool { return digestPattern.MatchString(s) }

// ParseRef splits an image reference into the repository name, its tag and its
// digest, any of which may be empty. Docker Hub is left off the name, so
// "nginx:1.2" and "docker.io/library/nginx:1.2" resolve to the same key.
func ParseRef(reference string) (name, tag, digest string) {
	// Split off an explicit digest first, so the colon inside "@sha256:..."
	// cannot be mistaken for the separator introducing a tag.
	remainder := reference
	if at := strings.LastIndex(reference, "@"); at != -1 {
		remainder = reference[:at]
		if candidate := reference[at+1:]; IsDigest(candidate) {
			digest = candidate
		}
	}

	// A colon after the last slash introduces a tag; one before it is a port.
	hasTag := strings.LastIndex(remainder, ":") > strings.LastIndex(remainder, "/")

	parsed, err := ref.New(reference)
	if err != nil {
		name, tag = splitNaively(remainder)
		return name, tag, digest
	}

	name = parsed.Repository
	if parsed.Registry != "" && parsed.Registry != "docker.io" && parsed.Registry != "index.docker.io" {
		name = parsed.Registry + "/" + parsed.Repository
	}
	if hasTag {
		tag = parsed.Tag
	}

	return name, tag, digest
}

func splitNaively(reference string) (name, tag string) {
	name, tag, _ = strings.Cut(reference, ":")
	return name, tag
}
