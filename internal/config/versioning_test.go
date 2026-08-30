package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal"
)

func TestParseVersioning(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantGlobal Versioning
		wantImage  Versioning
		wantErr    string
	}{
		{
			name: "nothing set",
			yaml: "exclude:\n  - backup\n",
		},
		{
			name:       "global scheme",
			yaml:       "versioning: loose\n",
			wantGlobal: VersioningLoose,
		},
		{
			name:      "per-image scheme",
			yaml:      "images:\n  nousresearch/hermes-agent:\n    versioning: loose\n",
			wantImage: VersioningLoose,
		},
		{
			name:      "per-image scheme beside a cap",
			yaml:      "images:\n  nousresearch/hermes-agent:\n    max: minor\n    versioning: loose\n",
			wantImage: VersioningLoose,
		},
		{
			// A silently ignored typo looks exactly like a feature that does not
			// work, so the load fails and names what was allowed instead.
			name:    "unknown global scheme",
			yaml:    "versioning: lose\n",
			wantErr: `"lose" is not one of semver, loose`,
		},
		{
			name:    "unknown per-image scheme",
			yaml:    "images:\n  redis:\n    versioning: calendar\n",
			wantErr: `image "redis": versioning: "calendar" is not one of semver, loose`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse(strings.NewReader(tt.yaml))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got none", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %q", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.Versioning != tt.wantGlobal {
				t.Errorf("global: want %q, got %q", tt.wantGlobal, cfg.Versioning)
			}
			if tt.wantImage != "" {
				for image, policy := range cfg.Images {
					if policy.Versioning != tt.wantImage {
						t.Errorf("image %s: want %q, got %q", image, tt.wantImage, policy.Versioning)
					}
				}
			}
		})
	}
}

// TestVersioningPrecedence is the rule the feature is built on: an entry naming
// an image outranks every default. It runs the config's own output through
// internal.ResolveVersioning, the same function the checker uses, so the two can
// never agree in the test and disagree in production.
//
// The ordering between -versioning and a global `versioning:` is settled in main
// before any of this, which is why only one default reaches here.
func TestVersioningPrecedence(t *testing.T) {
	tests := []struct {
		name   string
		cfg    Config
		image  string
		want   string
		reason string
	}{
		{
			name:   "nothing set anywhere",
			cfg:    Config{},
			image:  "redis",
			want:   string(VersioningSemver),
			reason: "an image nobody said anything about takes the default",
		},
		{
			name:   "global only",
			cfg:    Config{Versioning: VersioningLoose},
			image:  "redis",
			want:   string(VersioningLoose),
			reason: "the global default reaches an image with no entry",
		},
		{
			name: "per-image only",
			cfg: Config{Images: map[string]ImagePolicy{
				"nousresearch/hermes-agent": {Versioning: VersioningLoose},
			}},
			image:  "nousresearch/hermes-agent",
			want:   string(VersioningLoose),
			reason: "the entry applies without any global default",
		},
		{
			name: "per-image beats the default",
			cfg: Config{
				Versioning: VersioningLoose,
				Images:     map[string]ImagePolicy{"redis": {Versioning: VersioningSemver}},
			},
			image:  "redis",
			want:   string(VersioningSemver),
			reason: "an image may be pulled back to semver out of a loose default",
		},
		{
			name: "an entry with only a cap takes the default",
			cfg: Config{
				Versioning: VersioningLoose,
				Images:     map[string]ImagePolicy{"redis": {Max: LevelMinor}},
			},
			image:  "redis",
			want:   string(VersioningLoose),
			reason: "capping an image says nothing about how its tags are read",
		},
		{
			name: "another image's entry does not leak",
			cfg: Config{Images: map[string]ImagePolicy{
				"nousresearch/hermes-agent": {Versioning: VersioningLoose},
			}},
			image:  "redis",
			want:   string(VersioningSemver),
			reason: "the lookup is exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := internal.ResolveVersioning(
				tt.cfg.Versionings(), tt.cfg.DefaultVersioning(), tt.image)
			if got != tt.want {
				t.Errorf("want %q, got %q — %s", tt.want, got, tt.reason)
			}
		})
	}
}

// TestVersioningNamesResolve guards the seam between the two packages: this one
// validates the scheme names a config may write, internal is what actually reads
// tags under them. A name accepted here but unknown there would be a setting
// taken and then ignored, so every name this package allows has to resolve.
func TestVersioningNamesResolve(t *testing.T) {
	for _, name := range versionings {
		scheme, ok := internal.VersioningByName(string(name))
		if !ok {
			t.Errorf("%q passes validation here but internal cannot resolve it", name)
			continue
		}
		if scheme.Name() != string(name) {
			t.Errorf("%q resolves to a scheme calling itself %q", name, scheme.Name())
		}
	}
}

// TestVersioningLayers checks the two config files layer the way the caps do:
// the project file overrides the global one, and a file that says nothing
// leaves what the other decided alone.
func TestVersioningLayers(t *testing.T) {
	tests := []struct {
		name    string
		global  string
		project string
		want    Versioning
	}{
		{name: "neither", want: ""},
		{name: "global only", global: "versioning: loose\n", want: VersioningLoose},
		{name: "project only", project: "versioning: loose\n", want: VersioningLoose},
		{
			name:    "project overrides global",
			global:  "versioning: loose\n",
			project: "versioning: semver\n",
			want:    VersioningSemver,
		},
		{
			// The whole reason absent and "set to the default" have to stay
			// distinguishable: a project file saying nothing must not switch off
			// what the global one turned on.
			name:    "silent project leaves global alone",
			global:  "versioning: loose\n",
			project: "exclude:\n  - backup\n",
			want:    VersioningLoose,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalDir := t.TempDir()
			root := t.TempDir()
			t.Setenv("CCU_CONFIG_DIR", globalDir)

			if tt.global != "" {
				write(t, filepath.Join(globalDir, "config.yaml"), tt.global)
			}
			if tt.project != "" {
				write(t, filepath.Join(root, ".ccu.yaml"), tt.project)
			}

			loaded, err := Load(root, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if loaded.Versioning != tt.want {
				t.Errorf("want %q, got %q", tt.want, loaded.Versioning)
			}
		})
	}
}

func TestVersioningsAndDefault(t *testing.T) {
	cfg := Config{
		Versioning: VersioningLoose,
		Images: map[string]ImagePolicy{
			"nousresearch/hermes-agent": {Versioning: VersioningLoose},
			"redis":                     {Max: LevelMinor},
		},
	}

	schemes := cfg.Versionings()
	if len(schemes) != 1 || schemes["nousresearch/hermes-agent"] != "loose" {
		t.Errorf("want only the image that named a scheme, got %v", schemes)
	}
	if got := cfg.DefaultVersioning(); got != "loose" {
		t.Errorf("default: want loose, got %q", got)
	}

	// An empty global default reports the scheme by name rather than "", so the
	// scanner and `ccu config` never have to know what "" stood for.
	if got := (Config{}).DefaultVersioning(); got != "semver" {
		t.Errorf("default: want semver, got %q", got)
	}
	if got := (Config{}).Versionings(); got != nil {
		t.Errorf("want no per-image schemes, got %v", got)
	}
}
