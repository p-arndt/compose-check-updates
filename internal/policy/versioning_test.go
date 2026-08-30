package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A scheme is typed into a config file by hand, so anything outside the three
// known names has to be caught rather than falling through to a default that
// reads the user's tags differently than they asked.
func TestVersioningValid(t *testing.T) {
	tests := []struct {
		name       string
		versioning Versioning
		want       bool
	}{
		{name: "semver", versioning: VersioningSemver, want: true},
		{name: "loose", versioning: VersioningLoose, want: true},
		{name: "regex", versioning: VersioningRegex, want: true},
		// Empty means "the run's default", which is resolved elsewhere; it is
		// not itself a scheme a config may name.
		{name: "the zero value is not a scheme", versioning: Versioning(""), want: false},
		{name: "unknown text", versioning: Versioning("calver"), want: false},
		{name: "casing is not normalised", versioning: Versioning("Semver"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.versioning.Valid())
		})
	}
}

func TestVersioningString(t *testing.T) {
	tests := []struct {
		name       string
		versioning Versioning
		want       string
	}{
		{name: "semver", versioning: VersioningSemver, want: "semver"},
		{name: "loose", versioning: VersioningLoose, want: "loose"},
		{name: "regex", versioning: VersioningRegex, want: "regex"},
		{name: "the zero value prints as empty", versioning: Versioning(""), want: ""},
		{name: "an unknown scheme prints itself back", versioning: Versioning("calver"), want: "calver"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.versioning.String())
		})
	}
}

// Versionings is what error messages and the config docs enumerate, so it has to
// agree with Valid in both directions: a scheme listed but rejected, or accepted
// but never listed, leaves the user with no way to guess the right spelling.
func TestVersioningsAgreesWithValid(t *testing.T) {
	listed := Versionings()

	assert.Equal(t, []Versioning{VersioningSemver, VersioningLoose, VersioningRegex}, listed)
	for _, v := range listed {
		assert.True(t, v.Valid(), v.String())
	}
}
