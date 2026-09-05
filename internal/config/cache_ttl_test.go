package config

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCacheTTL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		want    time.Duration
		wantErr string
	}{
		{name: "nothing set falls back to the default", want: DefaultCacheTTL},
		{name: "a duration", yaml: "cache_ttl: 30m\n", want: 30 * time.Minute},
		{name: "the day suffix min_age accepts", yaml: "cache_ttl: 1d\n", want: 24 * time.Hour},
		// Zero is how a user says "write the cache but never read it back", which
		// is a setting, not an error.
		{name: "zero reads nothing back", yaml: "cache_ttl: 0\n", want: 0},
		{name: "unreadable", yaml: "cache_ttl: soon\n", wantErr: `cache_ttl: "soon" is not a duration`},
		{name: "negative", yaml: "cache_ttl: -10m\n", wantErr: `cache_ttl: "-10m" is negative`},
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
			assert.Equal(t, tt.want, cfg.CacheTTLDuration())
		})
	}
}

// A project file may shorten or lengthen the TTL its global counterpart set,
// like every other scalar key.
func TestCacheTTLMerges(t *testing.T) {
	t.Parallel()

	merged := merge(Config{CacheTTL: "30m"}, Config{CacheTTL: "1m"})
	assert.Equal(t, time.Minute, merged.CacheTTLDuration())

	untouched := merge(Config{CacheTTL: "30m"}, Config{})
	assert.Equal(t, 30*time.Minute, untouched.CacheTTLDuration())
}

// `ccu config` has to say where the cache is and how long it is trusted for:
// both are otherwise invisible, and the first question about a cache is always
// "is it even on".
func TestShowReportsTheCache(t *testing.T) {
	t.Parallel()

	var on bytes.Buffer
	Show(&on, Loaded{}, Config{CacheTTL: "5m"}, "/tmp/ccu-cache")
	assert.Contains(t, on.String(), "cache_ttl: 5m0s")
	assert.Contains(t, on.String(), "cache: /tmp/ccu-cache")

	var off bytes.Buffer
	Show(&off, Loaded{}, Config{}, "")
	assert.Contains(t, off.String(), "cache_ttl: 10m0s")
	assert.Contains(t, off.String(), "cache: off")
}
