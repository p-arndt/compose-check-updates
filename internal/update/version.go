package update

import (
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// maxVersionLen bounds how much untrusted version text we are willing to carry
// around. It mirrors the `^[0-9][0-9A-Za-z.+-]{0,63}$` bound the release
// workflow applies (see .github/workflows/release.yml), so a string this
// binary accepts is exactly a string the release pipeline could have produced.
const maxVersionLen = 64

// devVersions are the values the version stamp takes when the binary was not
// built by the release pipeline: "" when -ldflags -X was never applied, and
// "dev"/"(devel)" for local `go build`/`go install` builds.
var devVersions = map[string]struct{}{
	"":        {},
	"dev":     {},
	"(devel)": {},
}

// IsNewer reports whether latest is strictly newer than current.
//
// Two deliberate refusals live here:
//
// A dev build is never considered "behind" a release. Someone running a
// locally built binary has a working tree that is usually *ahead* of the last
// tag, and nagging them to "update" would replace their own build with an
// older released one.
//
// Only a strictly greater version qualifies, which makes downgrades
// impossible. That is the mitigation for a rollback/freeze attack: an attacker
// who controls the network can withhold new releases and stall a user on the
// version they already run, but cannot serve an older, known-vulnerable
// release and have this code offer it as an "update".
func IsNewer(latest, current string) bool {
	if IsDevVersion(current) {
		return false
	}
	return CompareVersions(latest, current) > 0
}

// CompareVersions returns -1 if a sorts before b, +1 if a sorts after b, and 0
// if they have equal precedence.
//
// Precedence is standard semver: a leading "v" is tolerated, "+build" metadata
// is ignored, missing core fields default to 0 ("1.2" == "1.2.0"), a release
// outranks any pre-release of the same core, and pre-release identifiers are
// compared dot-wise.
//
// The comparison is delegated to Masterminds/semver, the same library
// internal/version.go already uses for Docker image tags, so ccu's own version
// ordering cannot drift from the ordering it reports for everything else.
// Inputs the library rejects fall back to a tolerant hand-rolled comparison,
// and anything that fallback cannot make sense of compares equal rather than
// greater — a malformed tag arriving off the network must never be able to
// trigger an update, and must never panic.
func CompareVersions(a, b string) int {
	va, errA := semver.NewVersion(normalize(a))
	vb, errB := semver.NewVersion(normalize(b))
	if errA == nil && errB == nil {
		return va.Compare(vb)
	}
	return compareLoose(a, b)
}

// IsDevVersion reports whether v is an unstamped or locally built binary
// rather than a released one. Callers use it to stay silent instead of
// comparing a meaningless version against the latest release.
func IsDevVersion(v string) bool {
	_, ok := devVersions[strings.ToLower(strings.TrimSpace(v))]
	return ok
}

// ValidVersion reports whether v is a plausible version string.
//
// This is an ingress guard, not a semver parser. Version strings reach us from
// the GitHub API — untrusted input — and are then printed to a terminal,
// written into a cache file, and spliced into downloaded asset file names. The
// charset below is what makes each of those safe:
//
//   - a leading digit and a [0-9A-Za-z.+-] body exclude ESC, so a crafted tag
//     cannot smuggle ANSI/terminal escape sequences into our output (which can
//     rewrite the screen or, on some terminals, stuff the input buffer);
//   - excluding "/" and "\" keeps path separators — and therefore "../"
//     traversal — out of any file name we build from a version;
//   - the length bound keeps a multi-megabyte "version" out of the cache file
//     and off the screen.
//
// It intentionally rejects a leading "v": callers normalize the prefix away
// before validating, so exactly one representation is ever stored or rendered.
// This mirrors the `^[0-9][0-9A-Za-z.+-]{0,63}$` check the release workflow
// applies when cutting a release, so both ends of the pipeline agree on what a
// version may look like.
func ValidVersion(v string) bool {
	if v == "" || len(v) > maxVersionLen {
		return false
	}
	if v[0] < '0' || v[0] > '9' {
		return false
	}
	// Byte-wise on purpose: any multi-byte rune has bytes >= 0x80 and is
	// rejected, so no non-ASCII (including bidi/homoglyph) content survives.
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= '0' && c <= '9',
			c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c == '.', c == '+', c == '-':
		default:
			return false
		}
	}
	return true
}

// normalize strips the surrounding whitespace and optional "v" prefix that
// release tags carry, leaving something semver.NewVersion can accept.
func normalize(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		v = v[1:]
	}
	return v
}

// looseVersion is the minimal shape needed to order versions the library
// refuses to parse.
type looseVersion struct {
	core [3]uint64
	pre  string
}

// parseLoose accepts the same grammar as semver but with every core field
// optional and no rejection of leading zeros. ok is false when the core is not
// purely numeric, which is the signal to give up rather than guess.
func parseLoose(v string) (looseVersion, bool) {
	var out looseVersion

	v = normalize(v)
	// Build metadata is explicitly excluded from precedence by semver.
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		out.pre = v[i+1:]
		v = v[:i]
	}
	if v == "" {
		return out, false
	}

	fields := strings.Split(v, ".")
	if len(fields) > 3 {
		return out, false
	}
	for i, f := range fields {
		n, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return out, false
		}
		out.core[i] = n
	}
	return out, true
}

// compareLoose orders versions that Masterminds/semver rejected. Anything it
// cannot parse either compares equal (0), so an unparsable input is never
// reported as newer than a real version.
func compareLoose(a, b string) int {
	va, okA := parseLoose(a)
	vb, okB := parseLoose(b)
	if !okA || !okB {
		return 0
	}

	for i := range va.core {
		if va.core[i] != vb.core[i] {
			if va.core[i] < vb.core[i] {
				return -1
			}
			return 1
		}
	}
	return comparePrerelease(va.pre, vb.pre)
}

// comparePrerelease implements semver's pre-release precedence: a release
// (empty pre-release) outranks any pre-release of the same core, identifiers
// are compared dot-wise, numeric identifiers compare numerically and rank
// below alphanumeric ones, and when all shared identifiers are equal the
// shorter set ranks lower.
func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}

	fa := strings.Split(a, ".")
	fb := strings.Split(b, ".")
	for i := 0; i < len(fa) && i < len(fb); i++ {
		if c := compareIdentifier(fa[i], fb[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(fa) < len(fb):
		return -1
	case len(fa) > len(fb):
		return 1
	}
	return 0
}

// compareIdentifier orders a single pre-release identifier pair.
func compareIdentifier(a, b string) int {
	na, errA := strconv.ParseUint(a, 10, 64)
	nb, errB := strconv.ParseUint(b, 10, 64)

	switch {
	case errA == nil && errB == nil:
		switch {
		case na < nb:
			return -1
		case na > nb:
			return 1
		}
		return 0
	// Numeric identifiers always have lower precedence than alphanumeric ones.
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}
