package config

import (
	"fmt"
	"strings"
)

// Level names an update level a cap can be set to. It is the same vocabulary
// internal.UpdateInfo speaks, so a cap read from a config file can be compared
// against an UpdateLevel without translation.
type Level string

const (
	LevelPatch Level = "patch"
	LevelMinor Level = "minor"
	LevelMajor Level = "major"
)

// rank orders the levels so a cap can be compared against an update. A level
// missing from this map is one no cap can express — "digest", for one, which
// carries no version to be higher or lower than anything.
var rank = map[Level]int{LevelPatch: 1, LevelMinor: 2, LevelMajor: 3}

// Valid reports whether l is a level a cap may be set to.
func (l Level) Valid() bool {
	_, ok := rank[l]
	return ok
}

// ImagePolicy is what the user recorded about one image. Today that is a cap on
// how far it may move; the struct exists so the next preference does not have to
// change the shape of the config file.
type ImagePolicy struct {
	// Max is the highest level this image may be updated to. Empty means no cap,
	// which is also what an image with no entry at all gets.
	Max Level `yaml:"max"`
}

// MaxLevel returns the cap recorded for an image, or "" when there is none.
// Lookup is exact: the key is the image name without tag or digest, as
// internal.UpdateInfo.ImageName reports it (e.g. "library/traefik").
func (c Config) MaxLevel(image string) Level {
	if c.Images == nil {
		return ""
	}
	return c.Images[image].Max
}

// Caps flattens the per-image policies into the map the scanner reads: image
// name to cap level, with uncapped images left out entirely so a lookup miss and
// "no cap" are the same thing.
func (c Config) Caps() map[string]string {
	if len(c.Images) == 0 {
		return nil
	}

	caps := make(map[string]string, len(c.Images))
	for image, policy := range c.Images {
		if policy.Max == "" {
			continue
		}
		caps[image] = string(policy.Max)
	}
	if len(caps) == 0 {
		return nil
	}
	return caps
}

// Allows reports whether an update of the given level is permitted under cap.
// An empty cap permits everything, and so does a level that carries no version
// to compare — a digest update is either a different image or it is not, and a
// cap has nothing to say about that.
func Allows(cap Level, level string) bool {
	if cap == "" {
		return true
	}
	want, ok := rank[Level(level)]
	if !ok {
		return true
	}
	return want <= rank[cap]
}

// mergeImages layers over onto base. Unlike Exclude, which unions, a per-image
// policy *replaces*: the project file has to be able to raise a cap the global
// file set, not only tighten it. An image only the base names keeps its policy.
func mergeImages(base, over map[string]ImagePolicy) map[string]ImagePolicy {
	if len(base) == 0 {
		return over
	}
	if len(over) == 0 {
		return base
	}

	merged := make(map[string]ImagePolicy, len(base)+len(over))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range over {
		merged[k] = v
	}
	return merged
}

// validateImages rejects a cap that names no level, for the same reason an
// unknown key is rejected: a silently ignored `max: mayor` looks exactly like a
// feature that does not work.
func validateImages(images map[string]ImagePolicy) error {
	for image, policy := range images {
		if policy.Max == "" {
			continue
		}
		if !policy.Max.Valid() {
			return fmt.Errorf("image %q: max: %q is not one of %s", image, policy.Max, strings.Join([]string{
				string(LevelPatch), string(LevelMinor), string(LevelMajor),
			}, ", "))
		}
	}
	return nil
}
