package config

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

func TestParseMinAge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		yaml      string
		wantRun   string
		wantImage string
		wantErr   string
	}{
		{
			name:    "run-wide only",
			yaml:    "min_age: 7d\n",
			wantRun: "7d",
		},
		{
			name:      "per-image outranks the run-wide value",
			yaml:      "min_age: 7d\nimages:\n  internal/thing:\n    min_age: 3d\n",
			wantRun:   "7d",
			wantImage: "3d",
		},
		{
			name: "nothing set",
			yaml: "images:\n  internal/thing:\n    max: minor\n",
		},
		{
			// The error has to name the image, or a config with a dozen entries says
			// only that one of them is wrong.
			name:    "unparsable per-image value",
			yaml:    "images:\n  internal/thing:\n    min_age: soon\n",
			wantErr: `image "internal/thing": min_age: "soon" is not a duration`,
		},
		{
			name:    "negative per-image value",
			yaml:    "images:\n  internal/thing:\n    min_age: -3d\n",
			wantErr: `image "internal/thing": min_age: "-3d" is negative`,
		},
		{
			name:    "unparsable run-wide value",
			yaml:    "min_age: 7\n",
			wantErr: `min_age: "7" is not a duration`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Parse(strings.NewReader(tt.yaml))

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, tt.wantRun, cfg.MinAge)
			assert.Equal(t, tt.wantImage, cfg.Images["internal/thing"].MinAge)

			// What the checker will actually see, which is the run-wide value
			// wherever the image named none of its own.
			want := tt.wantImage
			if want == "" {
				want = tt.wantRun
			}
			assert.Equal(t, want, cfg.Policies().For("internal/thing").MinAge)
		})
	}
}

func TestValidateMinAge(t *testing.T) {
	t.Parallel()

	assert.NoError(t, ValidateMinAge(""))
	assert.NoError(t, ValidateMinAge("36h"))
	assert.Error(t, ValidateMinAge("-1h"))
	assert.Error(t, ValidateMinAge("a while"))
}

// The project file layers over the global one, the same way every other
// run-wide key does.
func TestMergeMinAge(t *testing.T) {
	t.Parallel()

	merged := merge(Config{MinAge: "14d"}, Config{MinAge: "2d"})
	assert.Equal(t, "2d", merged.MinAge)

	// A file that says nothing about it leaves the other one be.
	assert.Equal(t, "14d", merge(Config{MinAge: "14d"}, Config{}).MinAge)

	d, err := policy.ParseDuration(merged.MinAge)
	require.NoError(t, err)
	assert.Equal(t, 2*24*time.Hour, d)
}

func TestExplainMinAge(t *testing.T) {
	t.Parallel()

	loaded := Loaded{
		Global:      Config{MinAge: "14d"},
		GlobalPath:  "/home/u/.config/ccu/config.yaml",
		Project:     Config{Images: map[string]policy.Image{"redis": {MinAge: "3d"}}},
		ProjectPath: "/srv/.ccu.yaml",
	}
	effective := merge(loaded.Global, loaded.Project)

	var buf bytes.Buffer
	Explain(&buf, loaded, effective, "redis", "", "")
	assert.Contains(t, buf.String(), "min_age: 3d (images.redis.min_age in /srv/.ccu.yaml)")

	buf.Reset()
	Explain(&buf, loaded, effective, "nginx", "", "")
	assert.Contains(t, buf.String(), "min_age: 14d (min_age in /home/u/.config/ccu/config.yaml)")

	// The flag is layered into effective by main, so only the flag itself can say
	// that is where the value came from.
	flagged := effective
	flagged.MinAge = "2d"
	buf.Reset()
	Explain(&buf, loaded, flagged, "nginx", "", "2d")
	assert.Contains(t, buf.String(), "min_age: 2d (-min-age on the command line)")

	buf.Reset()
	Explain(&buf, Loaded{}, Config{}, "nginx", "", "")
	assert.Contains(t, buf.String(), "min_age: none (not set anywhere)")
}
