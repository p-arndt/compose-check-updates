package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A cap is only ever written by a user, so every value that is not one of the
// three ordered levels has to be rejected at the door rather than silently
// behaving like one of them.
func TestLevelValid(t *testing.T) {
	tests := []struct {
		name  string
		level Level
		want  bool
	}{
		{name: "patch", level: LevelPatch, want: true},
		{name: "minor", level: LevelMinor, want: true},
		{name: "major", level: LevelMajor, want: true},
		// These three describe what happened to an image, not how far it may
		// move; there is no ordering to cap them at.
		{name: "digest is a finding, not a cap", level: LevelDigest, want: false},
		{name: "pin is a finding, not a cap", level: LevelPin, want: false},
		{name: "unreadable is a finding, not a cap", level: LevelUnreadable, want: false},
		{name: "the zero value means no cap was set", level: Level(""), want: false},
		{name: "unknown text", level: Level("huge"), want: false},
		{name: "casing is not normalised", level: Level("Patch"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.level.Valid())
		})
	}
}

// The string form is what lands in config files and in the report, so it stays
// the bare word with nothing added around it.
func TestLevelString(t *testing.T) {
	tests := []struct {
		name  string
		level Level
		want  string
	}{
		{name: "patch", level: LevelPatch, want: "patch"},
		{name: "minor", level: LevelMinor, want: "minor"},
		{name: "major", level: LevelMajor, want: "major"},
		{name: "digest", level: LevelDigest, want: "digest"},
		{name: "pin", level: LevelPin, want: "pin"},
		{name: "unreadable", level: LevelUnreadable, want: "unreadable"},
		{name: "the zero value prints as empty", level: Level(""), want: ""},
		{name: "an unknown level prints itself back", level: Level("huge"), want: "huge"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.level.String())
		})
	}
}

// Allows decides which updates a user is shown at all, so the whole cap-against-
// level matrix is covered: a wrong cell here hides a real update or offers one
// the user asked never to see.
func TestLevelAllows(t *testing.T) {
	tests := []struct {
		name  string
		cap   Level
		level Level
		want  bool
	}{
		{name: "patch cap takes a patch", cap: LevelPatch, level: LevelPatch, want: true},
		{name: "patch cap refuses a minor", cap: LevelPatch, level: LevelMinor, want: false},
		{name: "patch cap refuses a major", cap: LevelPatch, level: LevelMajor, want: false},

		{name: "minor cap takes a patch", cap: LevelMinor, level: LevelPatch, want: true},
		{name: "minor cap takes a minor", cap: LevelMinor, level: LevelMinor, want: true},
		{name: "minor cap refuses a major", cap: LevelMinor, level: LevelMajor, want: false},

		{name: "major cap takes a patch", cap: LevelMajor, level: LevelPatch, want: true},
		{name: "major cap takes a minor", cap: LevelMajor, level: LevelMinor, want: true},
		{name: "major cap takes a major", cap: LevelMajor, level: LevelMajor, want: true},

		// An unset cap is the common case — most images name no policy at all —
		// and it must not quietly filter anything out.
		{name: "no cap takes a patch", cap: Level(""), level: LevelPatch, want: true},
		{name: "no cap takes a minor", cap: Level(""), level: LevelMinor, want: true},
		{name: "no cap takes a major", cap: Level(""), level: LevelMajor, want: true},
		{name: "no cap takes a digest change", cap: Level(""), level: LevelDigest, want: true},

		// A cap that survived validation as nonsense still must not swallow
		// updates; showing too much is recoverable, hiding is not.
		{name: "an unknown cap permits everything", cap: Level("huge"), level: LevelMajor, want: true},
		{name: "a finding used as a cap permits everything", cap: LevelDigest, level: LevelMajor, want: true},

		// Findings carry no version, so no cap can rank them below or above
		// itself; they are reported whatever the cap says.
		{name: "the tightest cap still reports a digest change", cap: LevelPatch, level: LevelDigest, want: true},
		{name: "the tightest cap still reports a pin", cap: LevelPatch, level: LevelPin, want: true},
		{name: "the tightest cap still reports an unreadable image", cap: LevelPatch, level: LevelUnreadable, want: true},
		{name: "the tightest cap still reports an empty level", cap: LevelPatch, level: Level(""), want: true},
		{name: "the tightest cap still reports an unknown level", cap: LevelPatch, level: Level("huge"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cap.Allows(tt.level))
		})
	}
}
