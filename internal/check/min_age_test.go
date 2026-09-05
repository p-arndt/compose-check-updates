package check

import (
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

// countingFetcher wraps a real client to record how often a build date was
// asked for, which is the whole cost of min_age and therefore the thing worth
// asserting about.
type countingFetcher struct {
	registry.Fetcher
	created atomic.Int64
}

func (f *countingFetcher) Created(image string) (time.Time, error) {
	f.created.Add(1)
	return f.Fetcher.Created(image)
}

// agedServer serves one repository whose tags were published at the given
// times, along with a compose file naming its 1.2.3.
func agedServer(t *testing.T, published map[string]time.Time) (compose, image string, reg *countingFetcher) {
	t.Helper()

	tags := make([]string, 0, len(published))
	digests := map[string]string{}
	for tag := range published {
		tags = append(tags, tag)
		digests[tag] = registrytest.DigestNew
	}

	server := registrytest.ServerWithCreated(t, "library/myimage", tags, digests, published)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	image = serverURL.Host + "/library/myimage"
	compose = filepath.Join(t.TempDir(), "docker-compose.yml")
	require.NoError(t, os.WriteFile(compose, []byte("services:\n  app:\n    image: "+image+":1.2.3\n"), 0644))

	return compose, image, &countingFetcher{Fetcher: registry.New(serverURL.Host)}
}

func TestMinAgeFallsBackToATagThatIsOldEnough(t *testing.T) {
	t.Parallel()

	now := time.Now()
	published := map[string]time.Time{
		"1.2.3": now.Add(-90 * 24 * time.Hour),
		"1.2.4": now.Add(-30 * 24 * time.Hour),
		"1.2.5": now.Add(-10 * 24 * time.Hour),
		"1.2.6": now.Add(-2 * time.Hour),
	}

	tests := []struct {
		name   string
		minAge string
		want   string
	}{
		{name: "no minimum age takes the newest tag", minAge: "", want: "1.2.6"},
		{name: "the newest tag is too young", minAge: "7d", want: "1.2.5"},
		{name: "two tags are too young", minAge: "14d", want: "1.2.4"},
		// Nothing is old enough, so nothing is offered — a settling time the
		// repository cannot satisfy means staying put, not moving anyway.
		{name: "nothing is old enough", minAge: "365d", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			compose, _, reg := agedServer(t, published)
			policies := policy.Set{MinAge: tt.minAge}

			updates, err := New(compose, reg, policies).Check(true, true, true)
			require.NoError(t, err)
			require.Len(t, updates, 1)

			assert.Equal(t, tt.want, updates[0].LatestTag)
		})
	}
}

// The per-image entry is the more specific statement, so it decides even when
// the run-wide key says something else.
func TestMinAgePerImageOutranksTheRunWideOne(t *testing.T) {
	t.Parallel()

	now := time.Now()
	compose, image, reg := agedServer(t, map[string]time.Time{
		"1.2.3": now.Add(-90 * 24 * time.Hour),
		"1.2.4": now.Add(-30 * 24 * time.Hour),
		"1.2.5": now.Add(-2 * time.Hour),
	})

	updates, err := New(compose, reg, policy.Set{
		MinAge: "365d",
		Images: map[string]policy.Image{image: {MinAge: "7d"}},
	}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 1)

	assert.Equal(t, "1.2.4", updates[0].LatestTag)
}

// Without a minimum age configured anywhere, the only build date worth a
// request is the one shown beside the tag that was picked.
func TestWithoutMinAgeOnlyTheChosenTagIsDated(t *testing.T) {
	t.Parallel()

	now := time.Now()
	compose, _, reg := agedServer(t, map[string]time.Time{
		"1.2.3": now.Add(-90 * 24 * time.Hour),
		"1.2.4": now.Add(-30 * 24 * time.Hour),
		"1.2.5": now.Add(-2 * time.Hour),
	})

	updates, err := New(compose, reg, policy.Set{}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 1)

	assert.Equal(t, int64(1), reg.created.Load())
	assert.False(t, updates[0].PublishedAt().IsZero())
	assert.Equal(t, "2h ago", updates[0].Age())
}
