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
			wantErr: `"lose" is not one of semver, loose, regex`,
		},
		{
			name:    "unknown per-image scheme",
			yaml:    "images:\n  redis:\n    versioning: calendar\n",
			wantErr: `image "redis": versioning: "calendar" is not one of semver, loose, regex`,
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
		// `regex` reads nothing without one, and a config naming that scheme is
		// required to carry one, so the seam is only honest with a pattern here.
		pattern := ""
		if name == VersioningRegex {
			pattern = calendarPattern
		}

		scheme, ok := internal.VersioningByName(string(name), pattern)
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

// calendarPattern is the shape the regex scheme was added for: a dashed date,
// which every other scheme reads as release 2024 with the day and the month
// mistaken for a prerelease.
const calendarPattern = `^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})$`

// TestParseVersioningPattern covers every way a scheme and a pattern can fail to
// agree. All of them are load-time errors on purpose: a pattern first noticed
// while tags are being read would leave the image quietly compared by digest, in
// the middle of a report about something else, while the line to fix is right
// here and can be named.
func TestParseVersioningPattern(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    string
		wantErr string
	}{
		{
			name: "regex with a pattern",
			yaml: "images:\n  acme/dated:\n    versioning: regex\n    versioning_pattern: '" + calendarPattern + "'\n",
			want: calendarPattern,
		},
		{
			name:    "regex without a pattern",
			yaml:    "images:\n  acme/dated:\n    versioning: regex\n",
			wantErr: `image "acme/dated": versioning: "regex" needs a versioning_pattern`,
		},
		{
			name:    "pattern under another scheme",
			yaml:    "images:\n  acme/dated:\n    versioning: loose\n    versioning_pattern: '" + calendarPattern + "'\n",
			wantErr: `image "acme/dated": versioning_pattern: only "regex" reads a pattern, and this image is on loose`,
		},
		{
			name:    "pattern under no scheme at all",
			yaml:    "images:\n  acme/dated:\n    versioning_pattern: '" + calendarPattern + "'\n",
			wantErr: `image "acme/dated": versioning_pattern: only "regex" reads a pattern, and this image is on semver, the default`,
		},
		{
			name:    "pattern that does not compile",
			yaml:    "images:\n  acme/dated:\n    versioning: regex\n    versioning_pattern: '^(?P<major>\\d+$'\n",
			wantErr: `image "acme/dated": versioning_pattern: "^(?P<major>\\d+$" is not a valid regular expression`,
		},
		{
			name:    "pattern naming no group",
			yaml:    "images:\n  acme/dated:\n    versioning: regex\n    versioning_pattern: '^\\d{4}-\\d{2}-\\d{2}$'\n",
			wantErr: `names no group, so there is nothing to read a version out of`,
		},
		{
			// A default reaches every image at once and there is no one image to
			// take a pattern from, so accepting it would mean a scheme that reads
			// no tag at all: every image silently dropped to comparing digests.
			name:    "regex as the global default",
			yaml:    "versioning: regex\n",
			wantErr: `versioning: "regex" is a per-image scheme`,
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

			if got := cfg.VersioningPatterns()["acme/dated"]; got != tt.want {
				t.Errorf("want pattern %q, got %q", tt.want, got)
			}
		})
	}
}

// TestVersioningPatternsResolve guards the same seam TestVersioningNamesResolve
// does, one level down: this package decides which patterns a config may carry,
// internal is what actually reads tags with them. A pattern accepted here but
// unusable there would be a setting taken and then ignored.
func TestVersioningPatternsResolve(t *testing.T) {
	patterns := []string{
		calendarPattern,
		`(?P<major>\d+)`,
		`^r(?P<major>\d+)_(?P<minor>\d+)(?P<suffix>-.*)?$`,
	}

	for _, pattern := range patterns {
		if err := validateVersioningPattern(VersioningRegex, pattern); err != nil {
			t.Errorf("%q: rejected here: %v", pattern, err)
			continue
		}
		if _, ok := internal.VersioningByName(string(VersioningRegex), pattern); !ok {
			t.Errorf("%q passes validation here but internal cannot read tags with it", pattern)
		}
	}
}

func TestVersioningPatternsMap(t *testing.T) {
	cfg := Config{Images: map[string]ImagePolicy{
		"acme/dated": {Versioning: VersioningRegex, VersioningPattern: calendarPattern},
		"redis":      {Max: LevelMinor},
	}}

	patterns := cfg.VersioningPatterns()
	if len(patterns) != 1 || patterns["acme/dated"] != calendarPattern {
		t.Errorf("want only the image that named a pattern, got %v", patterns)
	}

	// A lookup miss and "this image needs no pattern" are the same thing, so an
	// image without one contributes no entry at all.
	if got := (Config{}).VersioningPatterns(); got != nil {
		t.Errorf("want no patterns, got %v", got)
	}
}
