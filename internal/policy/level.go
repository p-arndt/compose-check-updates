// Package policy is the vocabulary of what a user recorded about an image: how
// far it may move, how its tags are read, and which tags it floats under.
package policy

// Level names how far an image moved, and how far a cap allows it to move.
type Level string

const (
	LevelPatch Level = "patch"
	LevelMinor Level = "minor"
	LevelMajor Level = "major"

	// LevelDigest: the manifest changed under a tag carrying no version.
	LevelDigest Level = "digest"
	// LevelPin: a floating tag written down as the digest it resolves to today,
	// so a later run can tell it has moved on.
	LevelPin Level = "pin"
	// LevelUnreadable: no version and no digest to compare against. Reported
	// rather than dropped, so the user has something to act on.
	LevelUnreadable Level = "unreadable"
)

// capRank orders the levels a cap can be expressed in; the others carry no
// version to be higher or lower than anything.
var capRank = map[Level]int{LevelPatch: 1, LevelMinor: 2, LevelMajor: 3}

// Valid reports whether l is a level a cap may be set to.
func (l Level) Valid() bool {
	_, ok := capRank[l]
	return ok
}

func (l Level) String() string { return string(l) }

// Allows reports whether an update of the given level stays within the cap l.
// A cap or level outside capRank permits everything rather than hiding updates.
func (l Level) Allows(level Level) bool {
	capped, ok := capRank[l]
	if !ok {
		return true
	}
	want, ok := capRank[level]
	if !ok {
		return true
	}
	return want <= capped
}
