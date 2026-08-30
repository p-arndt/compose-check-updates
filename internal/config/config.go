// Package config reads ccu's persistent settings: the options a user does not
// want to retype on every run. Two files are read, a global one for personal
// preferences that hold across projects and a project-local one that travels
// with the compose files, and the command line still wins over both.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk shape of a ccu config file. Fields are pointers or
// slices that can be absent, because merging has to tell "not set here" apart
// from "set to the zero value here".
type Config struct {
	// Exclude lists directories never to walk into. Entries are unioned across
	// the files and with -exclude rather than replacing each other: the point of
	// writing one down is not having to remember it again.
	Exclude []string `yaml:"exclude"`

	// Images holds the per-image preferences, keyed by image name without tag or
	// digest. Unlike Exclude these replace rather than union when merged: a
	// project has to be able to raise a cap the global file set, not only
	// tighten it.
	Images map[string]ImagePolicy `yaml:"images"`

	// PinFloating turns on pinning bare floating tags to the digest they resolve
	// to. A pointer because absent and `false` have to stay distinguishable: a
	// project file that says nothing must not switch off what the global one
	// turned on.
	PinFloating *bool `yaml:"pin_floating"`

	// Versioning is the scheme every image's tags are read under unless the image
	// names one of its own. Empty means `semver`. A plain string rather than a
	// pointer because "" is already the "not set here" the merge below needs, and
	// there is no false to tell apart from absent.
	Versioning Versioning `yaml:"versioning"`

	// Dockerfiles turns off checking the base images of Dockerfiles built by a
	// compose service. A pointer for the same reason PinFloating is one, and
	// absent means on: a service built from a Dockerfile has no image tag of its
	// own, so leaving it out would mean ccu has nothing to say about it at all.
	Dockerfiles *bool `yaml:"dockerfiles"`
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

	// Global and Project are the two layers before merging. A caller that only
	// wants the resolved settings reads the embedded Config; the TUI needs the
	// layers apart, because a pin is added to or removed from one scope and must
	// not be confused by what the other one says.
	Global  Config
	Project Config
}

// Load resolves the configuration for a scan rooted at root. explicit, when
// non-empty, is a path named on the command line: it replaces the search
// entirely, and a missing file is then an error rather than a silent skip —
// the user pointed at something specific.
func Load(root, explicit string) (Loaded, error) {
	if explicit != "" {
		cfg, err := readFile(explicit)
		if err != nil {
			return Loaded{}, err
		}
		// A file named by hand belongs to no scope in particular. Treating it as
		// the project layer is the safer of the two: a pin then lands in the file
		// the user pointed at rather than somewhere they did not name.
		return Loaded{Config: cfg, Sources: []string{explicit}, Project: cfg}, nil
	}

	var loaded Loaded

	if path := globalFile(); path != "" {
		cfg, err := readFile(path)
		if err != nil {
			return Loaded{}, err
		}
		loaded.Global = cfg
		loaded.Config = merge(loaded.Config, cfg)
		loaded.Sources = append(loaded.Sources, path)
	}

	if path := findProjectFile(root); path != "" {
		cfg, err := readFile(path)
		if err != nil {
			return Loaded{}, err
		}
		loaded.Project = cfg
		loaded.Config = merge(loaded.Config, cfg)
		loaded.Sources = append(loaded.Sources, path)
	}

	return loaded, nil
}

// globalDirs lists the per-user config directories to try, in preference order.
// ~/.config/ccu comes first, that being where a CLI's hand-edited dotfile is
// looked for — including on macOS, where os.UserConfigDir points at Application
// Support. That location is still accepted, since ccu keeps its update-check
// state there. An empty list means there is no home, which is not an error.
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
// directory. Walking up is what makes `ccu -d ./services/api` behave the same as
// a run from the repository root: the file belongs to the project, not to the
// directory the scan happened to start in.
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

	if err := ValidateVersioning(cfg.Versioning); err != nil {
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
