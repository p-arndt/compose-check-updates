package buildinfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestString covers the two shapes a released binary and a `go build` binary
// produce, plus the half-stamped cases: an ldflags line that sets only one of
// the two extras must still not print empty parentheses or a stray comma.
func TestString(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		date    string
		want    string
	}{
		{
			// What -ldflags stamps in for a release: all three values present.
			name:    "fully stamped release",
			version: "1.2.3",
			commit:  "abc1234",
			date:    "2026-07-01T12:00:00Z",
			want:    "1.2.3 (abc1234, 2026-07-01T12:00:00Z)",
		},
		{
			// A plain `go build` leaves the extras empty, and the bare word is
			// what `ccu version` then shows.
			name:    "unstamped dev build",
			version: "dev",
			want:    "dev",
		},
		{
			name:    "commit only",
			version: "1.2.3",
			commit:  "abc1234",
			want:    "1.2.3 (abc1234)",
		},
		{
			name:    "date only",
			version: "1.2.3",
			date:    "2026-07-01T12:00:00Z",
			want:    "1.2.3 (2026-07-01T12:00:00Z)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Package-level vars, so every case restores them: a leaked commit
			// would silently change the expectation of the next test.
			origVersion, origCommit, origDate := Version, Commit, Date
			defer func() { Version, Commit, Date = origVersion, origCommit, origDate }()

			Version, Commit, Date = tt.version, tt.commit, tt.date

			assert.Equal(t, tt.want, String())
		})
	}
}

// The declared defaults are what an unstamped build reports, so they are part
// of the contract with the justfile's ldflags rather than an implementation
// detail: "dev" is what tells a user their binary was not released.
func TestDefaults(t *testing.T) {
	assert.Equal(t, "dev", Version)
	assert.Empty(t, Commit)
	assert.Empty(t, Date)
}
