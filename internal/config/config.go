// Package config reads ccu's persistent settings from a global file and a
// project-local one. The command line still wins over both.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/versioning"
)

// Config is the on-disk shape of a ccu config file. Absent has to stay
// distinguishable from the zero value, hence the pointers.
type Config struct {
	// Exclude lists directories never to walk into. Entries are unioned across
	// the files and with -exclude rather than replacing each other: the point of
	// writing one down is not having to remember it again.
	Exclude []string `yaml:"exclude"`

	// Images holds the per-image preferences, keyed by image name without tag or
	// digest. These replace rather than union when merged, so a project can raise
	// a cap the global file set.
	Images map[string]policy.Image `yaml:"images"`

	// PinFloating turns on pinning bare floating tags to the digest they resolve
	// to. Absent, so a project file that says nothing leaves the global one be.
	PinFloating *bool `yaml:"pin_floating"`

	// FloatingTags names further tags to treat as moving, for every image. Union
	// rather than replacement, like Exclude: the built-in names are a fact about
	// how registries work, not a preference to be overridden.
	FloatingTags []string `yaml:"floating_tags"`

	// Versioning is the scheme every image's tags are read under unless the image
	// names one of its own. Empty means `semver`; "" is already the "not set
	// here" the merge needs, so no pointer.
	Versioning policy.Versioning `yaml:"versioning"`

	// MinAge is how long a tag has to have been published before any image may be
	// moved to it, written as a duration ("7d", "36h"). Empty means no waiting;
	// "" is already the "not set here" the merge needs, so no pointer.
	MinAge string `yaml:"min_age"`

	// CacheTTL is how long a registry answer that can change — a tag list, a
	// tag's digest — may be reused from the on-disk cache, written as a duration
	// ("10m", "1h"). Empty means DefaultCacheTTL, "0" reads nothing back. Kept
	// deliberately short: a cache that hid a release published a minute ago would
	// be worse than no cache at all.
	CacheTTL string `yaml:"cache_ttl"`

	// Dockerfiles turns off checking the base images of Dockerfiles built by a
	// compose service. Absent means on: it is the only way `build:` is covered.
	Dockerfiles *bool `yaml:"dockerfiles"`
}

// DefaultCacheTTL is how long a mutable registry answer is reused when no
// cache_ttl is written down: long enough that reopening the TUI or running a
// check straight after one is free, short enough that a release published while
// the user was reading is still found by the next run.
const DefaultCacheTTL = 10 * time.Minute

// CacheTTLDuration is the settled cache lifetime for this configuration. A
// value ccu cannot read falls back to the default rather than to zero: Parse
// already refused the file it came from, so anything left here is a value from
// a caller that skipped validation.
func (c Config) CacheTTLDuration() time.Duration {
	if c.CacheTTL == "" {
		return DefaultCacheTTL
	}
	d, err := policy.ParseDuration(c.CacheTTL)
	if err != nil || d < 0 {
		return DefaultCacheTTL
	}
	return d
}

// PinFloatingEnabled reports whether floating tags are to be pinned. Absent
// means off: pinning rewrites a reference the user deliberately left mutable, so
// it only ever happens where it was asked for.
func (c Config) PinFloatingEnabled() bool {
	return c.PinFloating != nil && *c.PinFloating
}

// DockerfilesEnabled reports whether the base images of built Dockerfiles are
// checked. Absent means on: it is the only way a `build:` service is covered,
// and it reads no file the compose file did not already point at.
func (c Config) DockerfilesEnabled() bool {
	return c.Dockerfiles == nil || *c.Dockerfiles
}

// projectNames are the project-local file names, in the order they are tried.
// Both spellings exist because compose files themselves accept both, and a user
// who wrote .ccu.yml should not have to find out which one ccu prefers.
var projectNames = []string{".ccu.yaml", ".ccu.yml"}

// globalNames are the file names tried inside the per-user config directory.
var globalNames = []string{"config.yaml", "config.yml"}

// Loaded is the result of resolving the configuration for a run: the merged
// settings plus where they came from, so `ccu config` can explain itself and an
// error can name the file that caused it.
type Loaded struct {
	Config
	Sources []string // absolute paths actually read, in merge order

	// Global and Project are the two layers before merging. The TUI needs them
	// apart: a pin is added to or removed from one scope.
	Global  Config
	Project Config

	// GlobalPath and ProjectPath are where those layers were read from, empty when
	// the layer is not backed by a file. Sources is one flat list and cannot say
	// which scope a single file belonged to.
	GlobalPath  string
	ProjectPath string
}

// Load resolves the configuration for a scan rooted at root. A non-empty
// explicit path replaces the search, and is an error when missing.
func Load(root, explicit string) (Loaded, error) {
	if explicit != "" {
		cfg, err := readFile(explicit)
		if err != nil {
			return Loaded{}, err
		}
		// A file named by hand belongs to no scope in particular. Treating it as
		// the project layer is the safer of the two: a pin then lands in the file
		// the user pointed at rather than somewhere they did not name.
		return Loaded{Config: cfg, Sources: []string{explicit}, Project: cfg, ProjectPath: explicit}, nil
	}

	var loaded Loaded

	if path := globalFile(); path != "" {
		cfg, err := readFile(path)
		if err != nil {
			return Loaded{}, err
		}
		loaded.Global = cfg
		loaded.GlobalPath = path
		loaded.Config = merge(loaded.Config, cfg)
		loaded.Sources = append(loaded.Sources, path)
	}

	if path := findProjectFile(root); path != "" {
		cfg, err := readFile(path)
		if err != nil {
			return Loaded{}, err
		}
		loaded.Project = cfg
		loaded.ProjectPath = path
		loaded.Config = merge(loaded.Config, cfg)
		loaded.Sources = append(loaded.Sources, path)
	}

	return loaded, nil
}

// globalDirs lists the per-user config directories to try, in preference order.
// ~/.config/ccu comes first — where a CLI's hand-edited dotfile is looked for,
// macOS included. os.UserConfigDir is still accepted, since the update-check
// state lives there. An empty list means there is no home, which is not an error.
func globalDirs() []string {
	// The tests' way in, and an escape hatch for anyone keeping their config
	// somewhere else entirely.
	if dir := os.Getenv("CCU_CONFIG_DIR"); dir != "" {
		return []string{dir}
	}

	var dirs []string

	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		dirs = append(dirs, filepath.Join(dir, "ccu"))
	} else if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "ccu"))
	}

	if dir, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(dir, "ccu"))
	}

	return dedupe(dirs)
}

// globalFile returns the global config to read, or "" when there is none. Only
// the first directory that actually holds one is used: reading a config out of
// two places at once would make it impossible to say which value won.
func globalFile() string {
	for _, dir := range globalDirs() {
		for _, name := range globalNames {
			path := filepath.Join(dir, name)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
		}
	}
	return ""
}

// findProjectFile looks for a project config at root and then in each parent
// directory, so `ccu -d ./services/api` reads the same file a run from the
// repository root does.
func findProjectFile(root string) string {
	dir, err := filepath.Abs(root)
	if err != nil {
		return ""
	}

	// A file named as the root is a scan of a single compose file's directory;
	// the config lives beside it either way.
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	for {
		for _, name := range projectNames {
			path := filepath.Join(dir, name)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// readFile parses one config file. Unknown keys are rejected rather than
// ignored: a silently dropped `excludes:` typo looks exactly like a feature that
// does not work.
func readFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	cfg, err := Parse(f)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes a config from r. An empty document is a valid, empty config.
func Parse(r io.Reader) (Config, error) {
	var cfg Config

	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, nil
		}
		return Config{}, err
	}

	for i, e := range cfg.Exclude {
		cfg.Exclude[i] = strings.TrimSpace(e)
	}
	cfg.Exclude = nonEmpty(cfg.Exclude)

	cfg.FloatingTags = Union(cfg.FloatingTags)
	for _, tag := range cfg.FloatingTags {
		if !validTag(tag) {
			return Config{}, fmt.Errorf("floating_tags: %q is not a valid tag", tag)
		}
	}

	if err := versioning.ValidateDefault(cfg.Versioning); err != nil {
		return Config{}, err
	}

	if err := ValidateMinAge(cfg.MinAge); err != nil {
		return Config{}, err
	}

	if err := ValidateCacheTTL(cfg.CacheTTL); err != nil {
		return Config{}, err
	}

	if err := validateImages(cfg.Images); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// merge layers over onto base. Exclude unions instead of replacing, so a
// project file adds its own directories without having to repeat the ones the
// user already excluded globally.
func merge(base, over Config) Config {
	base.Exclude = Union(base.Exclude, over.Exclude)
	base.FloatingTags = Union(base.FloatingTags, over.FloatingTags)
	base.Images = mergeImages(base.Images, over.Images)
	// Only a file that actually names the key overrides it, which is what makes
	// the project layer able to turn the global setting off as well as on.
	if over.PinFloating != nil {
		base.PinFloating = over.PinFloating
	}
	if over.Dockerfiles != nil {
		base.Dockerfiles = over.Dockerfiles
	}
	if over.Versioning != "" {
		base.Versioning = over.Versioning
	}
	if over.MinAge != "" {
		base.MinAge = over.MinAge
	}
	if over.CacheTTL != "" {
		base.CacheTTL = over.CacheTTL
	}
	return base
}

// Union concatenates the lists, dropping empty entries and duplicates while
// keeping first-seen order, so the resolved exclude list reads the way it was
// written: global entries, then project ones, then the command line's.
func Union(lists ...[]string) []string {
	var out []string
	seen := make(map[string]struct{})

	for _, list := range lists {
		for _, e := range list {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			if _, dup := seen[e]; dup {
				continue
			}
			seen[e] = struct{}{}
			out = append(out, e)
		}
	}

	return out
}

func dedupe(list []string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, e := range list {
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

func nonEmpty(list []string) []string {
	var out []string
	for _, e := range list {
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}
