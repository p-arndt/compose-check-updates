package config

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

func TestParseReferenceTag(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    string
		wantErr string
	}{
		{
			name: "per-image reference tag",
			yaml: "images:\n  internal/thing:\n    reference_tag: stable\n",
			want: "stable",
		},
		{
			name: "beside a cap and a scheme",
			yaml: "images:\n  internal/thing:\n    max: minor\n    versioning: loose\n    reference_tag: stable\n",
			want: "stable",
		},
		{
			name: "nothing set",
			yaml: "images:\n  internal/thing:\n    max: minor\n",
		},
		{
			// A tag no registry could serve is a typo, and a typo that is taken and
			// then quietly finds nothing is indistinguishable from a broken feature.
			name:    "not a tag a registry could serve",
			yaml:    "images:\n  internal/thing:\n    reference_tag: \"not a tag\"\n",
			wantErr: `image "internal/thing": reference_tag: "not a tag" is not a valid tag`,
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

			if got := cfg.Images["internal/thing"].ReferenceTag; got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}

// An image that named no reference tag is compared against the default, which is
// what every image got before the setting existed.
func TestReferenceTagResolution(t *testing.T) {
	policies := Config{Images: map[string]policy.Image{
		"internal/thing": {ReferenceTag: "stable"},
		"library/redis":  {Max: policy.LevelMinor},
	}}.Policies()

	if got := policies.For("internal/thing").ReferenceTag; got != "stable" {
		t.Errorf("want stable, got %q", got)
	}
	for _, image := range []string{"library/redis", "never/named"} {
		if got := policies.For(image).ReferenceTag; got != policy.DefaultReferenceTag {
			t.Errorf("%s: want the default %q, got %q", image, policy.DefaultReferenceTag, got)
		}
	}
}

func TestParseFloatingTags(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    []string
		wantErr string
	}{
		{
			name: "per-image floating tags",
			yaml: "images:\n  internal/thing:\n    floating_tags: [release, canary]\n",
			want: []string{"release", "canary"},
		},
		{
			name: "beside the other settings",
			yaml: "images:\n  internal/thing:\n    reference_tag: release\n    floating_tags:\n      - release\n",
			want: []string{"release"},
		},
		{
			name:    "an entry that names no tag",
			yaml:    "images:\n  internal/thing:\n    floating_tags: [\"\"]\n",
			wantErr: `image "internal/thing": floating_tags: "" is not a valid tag`,
		},
		{
			name:    "an entry no registry could serve",
			yaml:    "images:\n  internal/thing:\n    floating_tags: [\"not a tag\"]\n",
			wantErr: `image "internal/thing": floating_tags: "not a tag" is not a valid tag`,
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

			got := cfg.Images["internal/thing"].FloatingTags
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("want %v, got %v", tt.want, got)
			}
		})
	}
}

// The list reaching the scanner is trimmed and deduplicated, and an image that
// named none stays out of the map: a lookup miss is how the checker is told to
// use nothing but the tags it already knows.
func TestFloatingTagsResolution(t *testing.T) {
	cfg := Config{Images: map[string]policy.Image{
		"internal/thing": {FloatingTags: []string{" release ", "canary", "release"}},
		"library/redis":  {Max: policy.LevelMinor},
	}}

	policies := cfg.Policies()
	if got := strings.Join(policies.For("internal/thing").FloatingTags, ","); got != "release,canary" {
		t.Errorf("want release,canary — trimmed and deduplicated — got %q", got)
	}
	if got := policies.For("library/redis").FloatingTags; len(got) != 0 {
		t.Errorf("want no extra floating tags, got %v", got)
	}
}

// A project file replacing the global entry for an image is the layering rule
// caps and schemes follow, and the tag settings ride along on it rather than
// merging list by list: within one entry the list is complete.
func TestTagSettingsLayerLikeTheOtherPolicies(t *testing.T) {
	merged := mergeImages(
		map[string]policy.Image{
			"internal/thing": {ReferenceTag: "stable", FloatingTags: []string{"release"}},
			"library/redis":  {Max: policy.LevelMinor},
		},
		map[string]policy.Image{
			"internal/thing": {ReferenceTag: "edge"},
		},
	)

	thing := merged["internal/thing"]
	if thing.ReferenceTag != "edge" {
		t.Errorf("want the project file's reference tag, got %q", thing.ReferenceTag)
	}
	if len(thing.FloatingTags) != 0 {
		t.Errorf("want the project entry to replace the global one whole, got %v", thing.FloatingTags)
	}
	if merged["library/redis"].Max != policy.LevelMinor {
		t.Errorf("want an image only the global file names to keep its policy")
	}
}

// One listing per image, whatever mix of settings it carries: two listings would
// mean reading an image's settings in two places.
func TestShowListsTagSettingsPerImage(t *testing.T) {
	var out bytes.Buffer
	Show(&out, Loaded{}, Config{Images: map[string]policy.Image{
		"internal/thing": {Max: policy.LevelMinor, ReferenceTag: "stable"},
		"internal/other": {FloatingTags: []string{"release", "canary"}},
		"library/redis":  {Versioning: policy.VersioningLoose},
	}}, "")

	want := []string{
		"    internal/thing: max minor, reference_tag stable\n",
		"    internal/other: floating_tags release canary\n",
		"    library/redis: versioning loose\n",
	}
	for _, line := range want {
		if !strings.Contains(out.String(), line) {
			t.Errorf("want a line %q, got:\n%s", line, out.String())
		}
	}
}

// TestGlobalFloatingTags covers the list that applies to every image. It unions
// across the layers the way Exclude does rather than replacing, because a tag
// name written down once is meant to stay written down: the built-in names are a
// fact about how registries work, not a preference, and nothing here ever takes
// one away.
func TestGlobalFloatingTags(t *testing.T) {
	tests := []struct {
		name    string
		global  string
		project string
		want    []string
	}{
		{name: "neither", want: nil},
		{name: "global only", global: "floating_tags: [release]\n", want: []string{"release"}},
		{name: "project only", project: "floating_tags: [canary]\n", want: []string{"canary"}},
		{
			name:    "both union rather than replace",
			global:  "floating_tags: [release]\n",
			project: "floating_tags: [canary]\n",
			want:    []string{"release", "canary"},
		},
		{
			name:    "a name in both is kept once",
			global:  "floating_tags: [release, prod]\n",
			project: "floating_tags: [release, canary]\n",
			want:    []string{"release", "prod", "canary"},
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
			assertEqual(t, "floating_tags", loaded.FloatingTags, tt.want)
		})
	}
}

func TestGlobalFloatingTagsRejectsBadName(t *testing.T) {
	_, err := Parse(strings.NewReader("floating_tags: [\"not a tag!\"]\n"))
	if err == nil {
		t.Fatal("want an error naming the bad tag, got none")
	}
	if !strings.Contains(err.Error(), `floating_tags: "not a tag!" is not a valid tag`) {
		t.Errorf("unexpected error: %v", err)
	}
}
