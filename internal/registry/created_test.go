package registry

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

func TestCreated(t *testing.T) {
	t.Parallel()

	built := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		tag     string
		wantErr bool
	}{
		{name: "tag with a config blob", tag: "1.2.4"},
		// A manifest without a config blob is what a repository serving anything
		// but an image looks like; it is an error, never a zero time passed off as
		// a build date.
		{name: "tag without a config blob", tag: "1.2.3", wantErr: true},
		{name: "tag the registry does not have", tag: "9.9.9", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := registrytest.ServerWithCreated(t,
				"library/myimage",
				[]string{"1.2.3", "1.2.4"},
				map[string]string{"1.2.3": registrytest.DigestOld, "1.2.4": registrytest.DigestNew},
				map[string]time.Time{"1.2.4": built},
			)

			serverURL, err := url.Parse(server.URL)
			require.NoError(t, err)

			client := New(serverURL.Host)
			got, err := client.Created(serverURL.Host + "/library/myimage:" + tt.tag)

			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, got.IsZero())
				return
			}
			require.NoError(t, err)
			assert.True(t, built.Equal(got), "want %s, got %s", built, got)
		})
	}
}

// The answer is worth remembering: min_age asks about the same candidate from
// the level walk and from the display lookup, and a repository charging a rate
// limit for each would notice.
func TestCreatedIsCachedPerReference(t *testing.T) {
	t.Parallel()

	built := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	server := registrytest.ServerWithCreated(t,
		"library/myimage",
		[]string{"1.2.4"},
		map[string]string{"1.2.4": registrytest.DigestNew},
		map[string]time.Time{"1.2.4": built},
	)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	client := New(serverURL.Host)
	reference := serverURL.Host + "/library/myimage:1.2.4"

	first, err := client.Created(reference)
	require.NoError(t, err)

	// With the registry gone, only a cached answer can still be given.
	server.Close()

	second, err := client.Created(reference)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}
