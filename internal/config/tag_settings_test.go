package config

import (
	"bytes"
	"strings"
	"testing"
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

// ReferenceTags is what reaches the scanner, and an image that named no tag has
// to stay out of it: a lookup miss is how the checker is told to compare against
// "latest" as before.
func TestReferenceTagsFlattening(t *testing.T) {
	cfg := Config{Images: map[string]ImagePolicy{
		"internal/thing": {ReferenceTag: "stable"},
		"library/redis":  {Max: LevelMinor},
	}}

	tags := cfg.ReferenceTags()
	if len(tags) != 1 || tags["internal/thing"] != "stable" {
		t.Errorf("want only internal/thing mapped to stable, got %v", tags)
	}

	if got := (Config{}).ReferenceTags(); got != nil {
		t.Errorf("want nil for a config naming no image, got %v", got)
	}
	if got := (Config{Images: map[string]ImagePolicy{"redis": {Max: LevelMinor}}}).ReferenceTags(); got != nil {
		t.Errorf("want nil when no image named a tag, got %v", got)
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
func TestFloatingTagsFlattening(t *testing.T) {
	cfg := Config{Images: map[string]ImagePolicy{
		"internal/thing": {FloatingTags: []string{" release ", "canary", "release"}},
		"library/redis":  {Max: LevelMinor},
	}}

	tags := cfg.FloatingTags()
	if len(tags) != 1 {
		t.Fatalf("want only internal/thing listed, got %v", tags)
	}
	if got := strings.Join(tags["internal/thing"], ","); got != "release,canary" {
		t.Errorf("want release,canary — trimmed and deduplicated — got %q", got)
	}

	if got := (Config{}).FloatingTags(); got != nil {
		t.Errorf("want nil for a config naming no image, got %v", got)
	}
}

// A project file replacing the global entry for an image is the layering rule
// caps and schemes follow, and the tag settings ride along on it rather than
// merging list by list: within one entry the list is complete.
func TestTagSettingsLayerLikeTheOtherPolicies(t *testing.T) {
	merged := mergeImages(
		map[string]ImagePolicy{
			"internal/thing": {ReferenceTag: "stable", FloatingTags: []string{"release"}},
			"library/redis":  {Max: LevelMinor},
		},
		map[string]ImagePolicy{
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
	if merged["library/redis"].Max != LevelMinor {
		t.Errorf("want an image only the global file names to keep its policy")
	}
}

// One listing per image, whatever mix of settings it carries: two listings would
// mean reading an image's settings in two places.
func TestShowListsTagSettingsPerImage(t *testing.T) {
	var out bytes.Buffer
	Show(&out, Loaded{}, Config{Images: map[string]ImagePolicy{
		"internal/thing": {Max: LevelMinor, ReferenceTag: "stable"},
		"internal/other": {FloatingTags: []string{"release", "canary"}},
		"library/redis":  {Versioning: VersioningLoose},
	}})

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
