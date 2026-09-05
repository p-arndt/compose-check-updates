package policy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{value: "", want: 0},
		{value: "7d", want: 7 * 24 * time.Hour},
		{value: "0d", want: 0},
		{value: "1.5d", want: 36 * time.Hour},
		{value: " 3d ", want: 3 * 24 * time.Hour},
		{value: "36h", want: 36 * time.Hour},
		{value: "90m", want: 90 * time.Minute},
		{value: "-2d", want: -2 * 24 * time.Hour},
		// A bare number names no unit, and guessing one would be a guess the user
		// never gets told about.
		{value: "7", wantErr: true},
		{value: "soon", wantErr: true},
		// Go rejects the mixed form, and so does ccu rather than reading half of it.
		{value: "1d12h", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			got, err := ParseDuration(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMinAgeDuration(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 7*24*time.Hour, Image{MinAge: "7d"}.MinAgeDuration())
	assert.Zero(t, Image{}.MinAgeDuration())
	// Neither an unreadable nor a negative value may hide every update; the
	// config layer is where those are refused.
	assert.Zero(t, Image{MinAge: "soon"}.MinAgeDuration())
	assert.Zero(t, Image{MinAge: "-2d"}.MinAgeDuration())
}

func TestSetForMinAge(t *testing.T) {
	t.Parallel()

	set := Set{
		MinAge: "7d",
		Images: map[string]Image{
			"library/nginx": {MinAge: "3d"},
			"library/redis": {Max: LevelMinor},
		},
	}

	// The per-image value outranks the run-wide one; an image that named none
	// inherits it, named or not in the map at all.
	assert.Equal(t, "3d", set.For("library/nginx").MinAge)
	assert.Equal(t, "7d", set.For("library/redis").MinAge)
	assert.Equal(t, "7d", set.For("library/postgres").MinAge)
	assert.Zero(t, Set{}.For("library/nginx").MinAgeDuration())
}
