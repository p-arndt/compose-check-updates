package update

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{name: "patch ordering", a: "1.0.0", b: "1.0.1", want: -1},
		{name: "minor ordering", a: "1.1.0", b: "1.0.9", want: 1},
		{name: "major ordering", a: "2.0.0", b: "10.0.0", want: -1},
		{name: "equal versions", a: "1.2.3", b: "1.2.3", want: 0},

		// GitHub release tags carry a "v"; ccu's own stamped version does not.
		{name: "v prefix on one side", a: "v1.2.3", b: "1.2.3", want: 0},
		{name: "v prefix on both sides", a: "v1.2.4", b: "v1.2.3", want: 1},

		// Missing core fields default to 0.
		{name: "partial version equals padded", a: "1.2", b: "1.2.0", want: 0},
		{name: "partial version ordering", a: "1.2", b: "1.2.1", want: -1},

		// Build metadata is excluded from precedence by the semver spec.
		{name: "build metadata ignored", a: "1.2.3+abc123", b: "1.2.3", want: 0},
		{name: "build metadata differs only", a: "1.2.3+aaa", b: "1.2.3+zzz", want: 0},
		{name: "build metadata on prerelease", a: "1.0.0-rc.1+build.7", b: "1.0.0-rc.1", want: 0},

		// A release outranks any prerelease of the same core.
		{name: "release beats prerelease", a: "1.0.0", b: "1.0.0-rc.1", want: 1},
		{name: "prerelease loses to release", a: "1.0.0-rc.1", b: "1.0.0", want: -1},
		{name: "prerelease still loses to lower release core", a: "1.0.0-rc.1", b: "0.9.9", want: 1},

		// The canonical semver precedence chain.
		{name: "alpha before alpha.1", a: "1.0.0-alpha", b: "1.0.0-alpha.1", want: -1},
		{name: "alpha.1 before beta", a: "1.0.0-alpha.1", b: "1.0.0-beta", want: -1},
		{name: "beta before release", a: "1.0.0-beta", b: "1.0.0", want: -1},
		{name: "numeric identifiers compare numerically", a: "1.0.0-rc.2", b: "1.0.0-rc.10", want: -1},
		{name: "numeric ranks below alphanumeric", a: "1.0.0-1", b: "1.0.0-alpha", want: -1},

		// Garbage off the network must never sort as newer.
		{name: "unparsable is not greater", a: "not-a-version", b: "1.0.0", want: 0},
		{name: "unparsable is not lesser", a: "1.0.0", b: "not-a-version", want: 0},
		{name: "both unparsable", a: "latest", b: "stable", want: 0},
		{name: "empty strings", a: "", b: "", want: 0},
		{name: "escape sequence payload", a: "\x1b[31m1.0.0", b: "1.0.0", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CompareVersions(tt.a, tt.b))
			// Comparison must be antisymmetric, or ordering would depend on
			// argument order.
			assert.Equal(t, -tt.want, CompareVersions(tt.b, tt.a), "reversed")
		})
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{name: "newer patch", latest: "1.0.1", current: "1.0.0", want: true},
		{name: "newer major with v prefix", latest: "v2.0.0", current: "1.9.9", want: true},
		{name: "same version", latest: "1.0.0", current: "1.0.0", want: false},
		{name: "same version modulo build metadata", latest: "1.0.0+ci.9", current: "1.0.0", want: false},

		// Strict-newer-only: an older "latest" is a rollback attempt.
		{name: "downgrade rejected", latest: "0.9.0", current: "1.0.0", want: false},
		{name: "prerelease of current core rejected", latest: "1.0.0-rc.1", current: "1.0.0", want: false},
		{name: "prerelease of higher core accepted", latest: "2.0.0-rc.1", current: "1.0.0", want: true},

		// Dev builds are never behind a release.
		{name: "unstamped current", latest: "9.9.9", current: "", want: false},
		{name: "dev current", latest: "9.9.9", current: "dev", want: false},
		{name: "devel current", latest: "9.9.9", current: "(devel)", want: false},

		{name: "malformed latest never triggers update", latest: "latest", current: "1.0.0", want: false},
		{name: "escape payload never triggers update", latest: "\x1b]0;x\x07", current: "1.0.0", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsNewer(tt.latest, tt.current))
		})
	}
}

func TestIsDevVersion(t *testing.T) {
	tests := []struct {
		name string
		v    string
		want bool
	}{
		{name: "unstamped", v: "", want: true},
		{name: "dev", v: "dev", want: true},
		{name: "devel", v: "(devel)", want: true},
		{name: "surrounding whitespace", v: "  dev  ", want: true},
		{name: "uppercase", v: "DEV", want: true},
		{name: "release version", v: "1.0.0", want: false},
		{name: "prerelease version", v: "1.0.0-dev", want: false},
		{name: "development spelled out", v: "development", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsDevVersion(tt.v))
		})
	}
}

func TestValidVersion(t *testing.T) {
	tests := []struct {
		name string
		v    string
		want bool
	}{
		{name: "plain semver", v: "1.0.0", want: true},
		{name: "prerelease", v: "1.0.0-beta.1", want: true},
		{name: "build metadata", v: "1.0.0+build.7", want: true},
		{name: "partial version", v: "1.2", want: true},
		{name: "exactly 64 chars", v: "1" + strings.Repeat("0", 63), want: true},

		{name: "empty", v: "", want: false},
		{name: "65 chars", v: "1" + strings.Repeat("0", 64), want: false},
		{name: "leading v", v: "v1.0.0", want: false},
		{name: "leading letter", v: "latest", want: false},
		{name: "ansi escape", v: "1.0.0\x1b[31m", want: false},
		{name: "bare escape byte", v: "1.0.0\x1b", want: false},
		{name: "forward slash", v: "1.0.0/etc", want: false},
		{name: "path traversal", v: "1.0.0/../../etc/passwd", want: false},
		{name: "backslash", v: "1.0.0\\x", want: false},
		{name: "space", v: "1.0.0 rc1", want: false},
		{name: "leading space", v: " 1.0.0", want: false},
		{name: "newline", v: "1.0.0\ninjected", want: false},
		{name: "carriage return", v: "1.0.0\r", want: false},
		{name: "nul byte", v: "1.0.0\x00", want: false},
		{name: "shell metacharacters", v: "1.0.0;rm", want: false},
		{name: "non-ascii", v: "1.0.0é", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ValidVersion(tt.v))
		})
	}
}
