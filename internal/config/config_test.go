package config

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    []string
		wantErr string
	}{
		{name: "empty document", yaml: ""},
		{name: "comments only", yaml: "# nothing here\n"},
		{
			name: "exclude list",
			yaml: "exclude:\n  - node_modules\n  - services/legacy\n",
			want: []string{"node_modules", "services/legacy"},
		},
		{
			// Entries a hand-edited file collects; neither should reach the
			// matcher, where an empty one would look like a pattern.
			name: "blank and padded entries",
			yaml: "exclude:\n  - \"  backup  \"\n  - \"\"\n",
			want: []string{"backup"},
		},
		{
			// A dropped typo looks exactly like a feature that does not work, so
			// it is named instead.
			name:    "unknown key",
			yaml:    "excludes:\n  - backup\n",
			wantErr: "excludes",
		},
		{
			name:    "wrong type",
			yaml:    "exclude: backup\n",
			wantErr: "cannot unmarshal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse(strings.NewReader(tt.yaml))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Parse(%q) expected an error mentioning %q, got none", tt.yaml, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse(%q) error = %v, expected it to mention %q", tt.yaml, err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.yaml, err)
			}
			assertEqual(t, "exclude", cfg.Exclude, tt.want)
		})
	}
}

func TestUnion(t *testing.T) {
	got := Union(
		[]string{"node_modules", "backup"},
		[]string{"backup", " cache "},
		[]string{"", "tmp"},
	)
	// First-seen order, so the resolved list reads the way it was written:
	// global entries, then project ones, then the command line's.
	assertEqual(t, "union", got, []string{"node_modules", "backup", "cache", "tmp"})

	if got := Union(); got != nil {
		t.Errorf("Union() = %q, expected nil", got)
	}
}

// TestLoadMerges checks the layering that gives the feature its point: the
// global file holds a preference across projects, the project file adds to it
// rather than replacing it.
func TestLoadMerges(t *testing.T) {
	global := t.TempDir()
	write(t, filepath.Join(global, "config.yaml"), "exclude:\n  - node_modules\n")
	t.Setenv("CCU_CONFIG_DIR", global)

	root := t.TempDir()
	write(t, filepath.Join(root, ".ccu.yaml"), "exclude:\n  - services/legacy\n")

	loaded, err := Load(root, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertEqual(t, "exclude", loaded.Exclude, []string{"node_modules", "services/legacy"})
	if len(loaded.Sources) != 2 {
		t.Fatalf("Sources = %q, expected the global and the project file", loaded.Sources)
	}
	if !strings.HasPrefix(loaded.Sources[0], global) {
		t.Errorf("Sources[0] = %q, expected the global file to be merged first", loaded.Sources[0])
	}
}

// TestLoadWalksUp covers running ccu from a subdirectory: the config belongs to
// the project, not to the directory the scan happened to start in.
func TestLoadWalksUp(t *testing.T) {
	t.Setenv("CCU_CONFIG_DIR", t.TempDir())

	root := t.TempDir()
	write(t, filepath.Join(root, ".ccu.yml"), "exclude:\n  - backup\n")

	sub := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sub, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertEqual(t, "exclude", loaded.Exclude, []string{"backup"})
}

func TestLoadNoFiles(t *testing.T) {
	t.Setenv("CCU_CONFIG_DIR", t.TempDir())

	loaded, err := Load(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Sources) != 0 || len(loaded.Exclude) != 0 {
		t.Errorf("Load with no files = %+v, expected an empty result", loaded)
	}
}

// TestLoadExplicit covers -config: the search is replaced, and a file the user
// named by hand that is not there is an error rather than a silent skip.
func TestLoadExplicit(t *testing.T) {
	global := t.TempDir()
	write(t, filepath.Join(global, "config.yaml"), "exclude:\n  - node_modules\n")
	t.Setenv("CCU_CONFIG_DIR", global)

	explicit := filepath.Join(t.TempDir(), "custom.yaml")
	write(t, explicit, "exclude:\n  - only-this\n")

	loaded, err := Load(t.TempDir(), explicit)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertEqual(t, "exclude", loaded.Exclude, []string{"only-this"})

	if _, err := Load(t.TempDir(), filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("expected an error for a -config file that does not exist")
	}
}

// TestLoadBrokenFile checks that a malformed file stops the run and names
// itself: scanning with settings the user believes are in effect is worse than
// failing.
func TestLoadBrokenFile(t *testing.T) {
	t.Setenv("CCU_CONFIG_DIR", t.TempDir())

	root := t.TempDir()
	write(t, filepath.Join(root, ".ccu.yaml"), "exclude:\n  - [unclosed\n")

	_, err := Load(root, "")
	if err == nil {
		t.Fatal("expected an error for a malformed config file")
	}
	if !strings.Contains(err.Error(), ".ccu.yaml") {
		t.Errorf("error = %v, expected it to name the file", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertEqual(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %q, expected %q", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %q, expected %q", what, got, want)
		}
	}
}

// pin_floating is a plain switch, but an absent one must not read as "off":
// that is what lets a project file turn the global preference off as well as on.
func TestPinFloatingLayers(t *testing.T) {
	tests := []struct {
		name           string
		global, projct string
		want           bool
	}{
		{name: "neither file says anything", want: false},
		{name: "global on", global: "pin_floating: true\n", want: true},
		{name: "global on, project silent", global: "pin_floating: true\n", projct: "exclude:\n  - tmp\n", want: true},
		{name: "project turns it off again", global: "pin_floating: true\n", projct: "pin_floating: false\n", want: false},
		{name: "project alone turns it on", projct: "pin_floating: true\n", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			global := t.TempDir()
			if tt.global != "" {
				write(t, filepath.Join(global, "config.yaml"), tt.global)
			}
			t.Setenv("CCU_CONFIG_DIR", global)

			root := t.TempDir()
			if tt.projct != "" {
				write(t, filepath.Join(root, ".ccu.yaml"), tt.projct)
			}

			loaded, err := Load(root, "")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := loaded.PinFloatingEnabled(); got != tt.want {
				t.Errorf("PinFloatingEnabled() = %t, expected %t", got, tt.want)
			}
		})
	}
}

// A file that never mentions the key leaves it unset, so a later layer can still
// decide it. Only reading the pointer can tell that apart from an explicit false.
func TestPinFloatingAbsentIsUnset(t *testing.T) {
	cfg, err := Parse(strings.NewReader("exclude:\n  - tmp\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.PinFloating != nil {
		t.Errorf("PinFloating = %v, expected it to stay unset", *cfg.PinFloating)
	}

	cfg, err = Parse(strings.NewReader("pin_floating: false\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.PinFloating == nil || *cfg.PinFloating {
		t.Errorf("PinFloating = %v, expected an explicit false", cfg.PinFloating)
	}
}

// The merged map has to be the caller's own: the TUI is handed the merged config
// and the two layers it came from, and it edits a layer's map in place. A result
// aliasing a layer would turn an edit meant for one scope into an edit of both.
func TestMergeImagesReturnsAMapTheCallerOwns(t *testing.T) {
	tests := []struct {
		name string
		base map[string]policy.Image
		over map[string]policy.Image
	}{
		{
			name: "base empty",
			over: map[string]policy.Image{"library/traefik": {Max: policy.LevelMinor}},
		},
		{
			name: "over empty",
			base: map[string]policy.Image{"library/traefik": {Max: policy.LevelMinor}},
		},
		{
			name: "both filled",
			base: map[string]policy.Image{"library/traefik": {Max: policy.LevelMinor}},
			over: map[string]policy.Image{"library/redis": {Max: policy.LevelPatch}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseBefore := maps.Clone(tt.base)
			overBefore := maps.Clone(tt.over)

			merged := mergeImages(tt.base, tt.over)
			merged["library/nginx"] = policy.Image{Max: policy.LevelMajor}
			delete(merged, "library/traefik")

			assert.Equal(t, baseBefore, tt.base, "base was written through")
			assert.Equal(t, overBefore, tt.over, "over was written through")
		})
	}
}

// Two empty layers give nil rather than an empty map, so a merged config still
// compares equal to one parsed straight out of a file that named no image.
func TestMergeImagesEmptyLayersStayNil(t *testing.T) {
	assert.Nil(t, mergeImages(nil, nil))
	assert.Nil(t, mergeImages(map[string]policy.Image{}, nil))
}
