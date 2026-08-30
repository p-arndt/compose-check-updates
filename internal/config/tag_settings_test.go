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

// One listing per image, whatever mix of settings it carries: two listings would
// mean reading an image's settings in two places.
func TestShowListsTagSettingsPerImage(t *testing.T) {
	var out bytes.Buffer
	Show(&out, Loaded{}, Config{Images: map[string]ImagePolicy{
		"internal/thing": {Max: LevelMinor, ReferenceTag: "stable"},
		"library/redis":  {Versioning: VersioningLoose},
	}})

	want := []string{
		"    internal/thing: max minor, reference_tag stable\n",
		"    library/redis: versioning loose\n",
	}
	for _, line := range want {
		if !strings.Contains(out.String(), line) {
			t.Errorf("want a line %q, got:\n%s", line, out.String())
		}
	}
}
