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
		return Loaded{Config: cfg, Sources: []string{explicit}}, nil
	}

	var loaded Loaded

	for _, path := range candidatePaths(root) {
		cfg, err := readFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Loaded{}, err
		}
		loaded.Config = merge(loaded.Config, cfg)
		loaded.Sources = append(loaded.Sources, path)
	}

	return loaded, nil
}

// candidatePaths lists every file Load would read, global first so the
// project-local one is merged on top of it.
func candidatePaths(root string) []string {
	var paths []string

	if p := globalFile(); p != "" {
		paths = append(paths, p)
	}

	if p := findProjectFile(root); p != "" {
		paths = append(paths, p)
	}

	return paths
}

// globalDirs lists the per-user config directories to try, in preference order.
// ~/.config/ccu comes first because config.yaml is a file people edit by hand,
// and that is where a command line tool's dotfile is looked for — including on
// macOS, where os.UserConfigDir points at Application Support. That location is
// still accepted, because it is where ccu already keeps its update-check state,
// and someone who put their config next to it should not have to move it.
//
// An empty list means there is no home to look in, which is not an error: the
// run simply has no global config.
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

	return cfg, nil
}

// merge layers over onto base. Exclude unions instead of replacing, so a
// project file adds its own directories without having to repeat the ones the
// user already excluded globally.
func merge(base, over Config) Config {
	base.Exclude = Union(base.Exclude, over.Exclude)
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
