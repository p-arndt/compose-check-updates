package config

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// explain runs the command the way main does: load the two layers, fold
// -versioning into the effective config, then report on one image. Going through
// Load rather than hand-building a Loaded is the point — the provenance lines
// name files, and a test that invented the paths would not catch them being
// recorded wrong.
func explain(t *testing.T, global, project, image, flagVersioning string) string {
	t.Helper()

	globalDir := t.TempDir()
	if global != "" {
		write(t, filepath.Join(globalDir, "config.yaml"), global)
	}
	t.Setenv("CCU_CONFIG_DIR", globalDir)

	root := t.TempDir()
	if project != "" {
		write(t, filepath.Join(root, ".ccu.yaml"), project)
	}

	loaded, err := Load(root, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	effective := loaded.Config
	if flagVersioning != "" {
		effective.Versioning = Versioning(flagVersioning)
	}

	var buf bytes.Buffer
	Explain(&buf, loaded, effective, image, flagVersioning)
	return buf.String()
}

// TestExplainProvenance walks every layer a value can come from. The value alone
// is what `ccu config` already showed; naming the layer is the whole feature, so
// each case asserts on the line that says where it was decided.
func TestExplainProvenance(t *testing.T) {
	tests := []struct {
		name            string
		global, project string
		image, flag     string
		wantVersioning  string
		wantMax         string
		wantGlobalFile  bool // the versioning line names the global config file
		wantProjectFile bool // ... or the project one
		wantMaxGlobal   bool
		wantMaxProject  bool
		reason          string
	}{
		{
			name:           "nothing set anywhere",
			image:          "redis",
			wantVersioning: "versioning: semver (built-in default)",
			wantMax:        "max: no cap (no entry for this image)",
			reason:         "an image nobody wrote anything about takes the built-in default",
		},
		{
			name:           "global file default",
			global:         "versioning: loose\n",
			image:          "redis",
			wantVersioning: "versioning: loose (versioning in ",
			wantGlobalFile: true,
			reason:         "a default in the global file reaches an image with no entry",
		},
		{
			name:            "project file default outranks the global one",
			global:          "versioning: semver\n",
			project:         "versioning: loose\n",
			image:           "redis",
			wantVersioning:  "versioning: loose (versioning in ",
			wantProjectFile: true,
			reason:          "the project layer is merged last, so it is the one that decided",
		},
		{
			name:           "the flag outranks both files",
			global:         "versioning: semver\n",
			project:        "versioning: semver\n",
			image:          "redis",
			flag:           "loose",
			wantVersioning: "versioning: loose (-versioning on the command line)",
			reason:         "a flag is what a run said, and no file was consulted for it",
		},
		{
			name:           "a per-image entry outranks the flag",
			global:         "images:\n  redis:\n    versioning: semver\n",
			image:          "redis",
			flag:           "loose",
			wantVersioning: "versioning: semver (images.redis.versioning in ",
			wantGlobalFile: true,
			reason:         "naming an image is the more specific statement, flag or not",
		},
		{
			name:            "a per-image entry in the project file",
			global:          "images:\n  redis:\n    versioning: semver\n",
			project:         "images:\n  redis:\n    versioning: loose\n",
			image:           "redis",
			wantVersioning:  "versioning: loose (images.redis.versioning in ",
			wantProjectFile: true,
			reason:          "a project entry replaces the global one for that image outright",
		},
		{
			name:          "a cap in the global file",
			global:        "images:\n  redis:\n    max: minor\n",
			image:         "redis",
			wantMax:       "max: minor (images.redis.max in ",
			wantMaxGlobal: true,
			reason:        "a preference across projects is where a cap usually lives",
		},
		{
			name:           "a cap in the project file",
			global:         "images:\n  redis:\n    max: major\n",
			project:        "images:\n  redis:\n    max: patch\n",
			image:          "redis",
			wantMax:        "max: patch (images.redis.max in ",
			wantMaxProject: true,
			reason:         "the project file may tighten or raise what the global one set",
		},
		{
			name:    "an entry that sets no cap",
			project: "images:\n  redis:\n    versioning: loose\n",
			image:   "redis",
			wantMax: "max: no cap (no max set in images.redis in ",
			reason:  "an entry with only a scheme is worth telling apart from no entry at all",
		},
		{
			// The trap: a project entry replaces the global one whole, so a cap
			// written globally does not survive an entry that forgot it.
			name:    "a project entry drops the global cap",
			global:  "images:\n  redis:\n    max: minor\n",
			project: "images:\n  redis:\n    versioning: loose\n",
			image:   "redis",
			wantMax: "max: no cap (no max set in images.redis in ",
			reason:  "per-image policies replace rather than merge, which is invisible otherwise",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := explain(t, tt.global, tt.project, tt.image, tt.flag)

			if tt.wantVersioning != "" && !strings.Contains(out, tt.wantVersioning) {
				t.Errorf("expected %q in the report — %s\ngot:\n%s", tt.wantVersioning, tt.reason, out)
			}
			if tt.wantMax != "" && !strings.Contains(out, tt.wantMax) {
				t.Errorf("expected %q in the report — %s\ngot:\n%s", tt.wantMax, tt.reason, out)
			}
			if (tt.wantGlobalFile || tt.wantMaxGlobal) && !strings.Contains(out, "config.yaml") {
				t.Errorf("expected the global file to be named — %s\ngot:\n%s", tt.reason, out)
			}
			if (tt.wantProjectFile || tt.wantMaxProject) && !strings.Contains(out, ".ccu.yaml") {
				t.Errorf("expected the project file to be named — %s\ngot:\n%s", tt.reason, out)
			}
		})
	}
}

// TestExplainNoMatch covers the line the command exists for: an entry whose key
// never matched looks exactly like no entry at all once merged.
func TestExplainNoMatch(t *testing.T) {
	out := explain(t, "", "images:\n  library/traefik:\n    max: minor\n", "redis", "")

	if !strings.Contains(out, `No config entry names "redis".`) {
		t.Errorf("expected the report to say no entry matched, got:\n%s", out)
	}
	if !strings.Contains(out, "without tag or digest") {
		t.Errorf("expected the report to explain how the key is spelled, got:\n%s", out)
	}
	if strings.Contains(out, "Did you mean") {
		t.Errorf("library/traefik is nothing like redis, so no hint should be offered, got:\n%s", out)
	}
}

// TestExplainMatch is the other half: an image that did match must not be told
// its key is wrong.
func TestExplainMatch(t *testing.T) {
	out := explain(t, "", "images:\n  redis:\n    max: minor\n", "redis", "")

	if strings.Contains(out, "No config entry") {
		t.Errorf("redis is configured, so nothing should claim otherwise, got:\n%s", out)
	}
}

// TestExplainNearMiss is the most valuable part of the feature: a key that is
// almost right is ignored in silence, and the user has no way to see the
// difference between "ccu does not work" and "the key has a tag on it".
func TestExplainNearMiss(t *testing.T) {
	tests := []struct {
		name   string
		images string
		image  string
		want   string
		reason string
	}{
		{
			name:   "the user typed a tag",
			images: "images:\n  library/traefik:\n    max: minor\n",
			image:  "library/traefik:1.2",
			want:   `Did you mean "library/traefik"?`,
			reason: "a reference is what the compose file says, the key is the name alone",
		},
		{
			name:   "the user left off the namespace",
			images: "images:\n  library/traefik:\n    max: minor\n",
			image:  "traefik",
			want:   `Did you mean "library/traefik"?`,
			reason: "ccu reports Docker Hub official images under library/",
		},
		{
			name:   "the config left off the namespace",
			images: "images:\n  traefik:\n    max: minor\n",
			image:  "library/traefik",
			want:   `Did you mean "traefik"?`,
			reason: "the same mistake seen from the config's side",
		},
		{
			name:   "a tag and a namespace at once",
			images: "images:\n  traefik:\n    max: minor\n",
			image:  "library/traefik:1.2",
			want:   `Did you mean "traefik"?`,
			reason: "both mistakes together are still the same entry",
		},
		{
			name:   "a digest instead of a name",
			images: "images:\n  library/traefik:\n    max: minor\n",
			image:  "library/traefik@sha256:abc",
			want:   `Did you mean "library/traefik"?`,
			reason: "a digest-pinned reference names the image just as a tag does",
		},
		{
			name:   "a different registry",
			images: "images:\n  ghcr.io/home-assistant/home-assistant:\n    versioning: loose\n",
			image:  "home-assistant/home-assistant",
			want:   `Did you mean "ghcr.io/home-assistant/home-assistant"?`,
			reason: "the registry prefix is part of the key ccu reports",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := explain(t, "", tt.images, tt.image, "")
			if !strings.Contains(out, tt.want) {
				t.Errorf("expected %q — %s\ngot:\n%s", tt.want, tt.reason, out)
			}
			if !strings.Contains(out, ".ccu.yaml") {
				t.Errorf("expected the hint to name the file the entry is in, got:\n%s", out)
			}
		})
	}
}

// A hint listing several candidates has to come out in the same order twice, or
// the report is one nobody can diff.
func TestExplainNearMissOrder(t *testing.T) {
	images := "images:\n  zzz/traefik:\n    max: minor\n  aaa/traefik:\n    max: minor\n  library/traefik:\n    max: minor\n"

	out := explain(t, "", images, "traefik", "")

	// Matched on the whole hint line: the explanation above the hints quotes
	// "library/traefik" as its example, and a bare name search would find that.
	aaa := strings.Index(out, `Did you mean "aaa/traefik"`)
	library := strings.Index(out, `Did you mean "library/traefik"`)
	zzz := strings.Index(out, `Did you mean "zzz/traefik"`)
	if aaa < 0 || library < 0 || zzz < 0 {
		t.Fatalf("expected all three candidates to be offered, got:\n%s", out)
	}
	if !(aaa < library && library < zzz) {
		t.Errorf("expected the hints in sorted order, got:\n%s", out)
	}
}

// The explicit -config file belongs to no scope, and Load files it under the
// project layer. The report must still name it rather than fall back to the
// pathless phrasing.
func TestExplainExplicitFile(t *testing.T) {
	t.Setenv("CCU_CONFIG_DIR", t.TempDir())

	path := filepath.Join(t.TempDir(), "custom.yaml")
	write(t, path, "images:\n  redis:\n    max: minor\n")

	loaded, err := Load(t.TempDir(), path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	Explain(&buf, loaded, loaded.Config, "redis", "")

	if !strings.Contains(buf.String(), "images.redis.max in "+path) {
		t.Errorf("expected the named file to be credited, got:\n%s", buf.String())
	}
}

// A Config assembled in memory has no file behind it, which the TUI and any
// future caller can produce. Naming the key without a path beats claiming one.
func TestExplainWithoutFiles(t *testing.T) {
	effective := Config{Images: map[string]ImagePolicy{"redis": {Max: LevelMinor}}}

	var buf bytes.Buffer
	Explain(&buf, Loaded{}, effective, "redis", "")

	out := buf.String()
	if !strings.Contains(out, "Config files: none found") {
		t.Errorf("expected the report to say no file was read, got:\n%s", out)
	}
	if !strings.Contains(out, "max: minor (images.redis.max)") {
		t.Errorf("expected the key without a file, got:\n%s", out)
	}
}
