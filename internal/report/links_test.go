package report

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// The release link is derived from the source the image records and the tag the
// line reports, so a consumer can send a reader to the notes without ccu having
// checked that they exist.
func TestJSONLReleaseAndSourceLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		update      check.Update
		wantRelease any
		wantSource  any
	}{
		{
			name: "github source becomes a release link",
			update: check.Update{
				ImageName: "traefik", CurrentTag: "v2.9.3", LatestTag: "v3.2.0",
				SourceURL: "https://github.com/traefik/traefik",
			},
			wantRelease: "https://github.com/traefik/traefik/releases/tag/v3.2.0",
			wantSource:  "https://github.com/traefik/traefik",
		},
		{
			name: "gitlab source becomes a release link",
			update: check.Update{
				ImageName: "gitlab-runner", CurrentTag: "16.0.0", LatestTag: "16.1.0",
				SourceURL: "https://gitlab.com/gitlab-org/gitlab-runner",
			},
			wantRelease: "https://gitlab.com/gitlab-org/gitlab-runner/-/releases/16.1.0",
			wantSource:  "https://gitlab.com/gitlab-org/gitlab-runner",
		},
		{
			name: "another forge keeps the source only",
			update: check.Update{
				ImageName: "internal/app", CurrentTag: "1.0.0", LatestTag: "1.1.0",
				SourceURL: "https://git.example.com/team/app",
			},
			wantRelease: nil,
			wantSource:  "https://git.example.com/team/app",
		},
		{
			name: "an image recording no source carries neither key",
			update: check.Update{
				ImageName: "nginx", CurrentTag: "1.2.3", LatestTag: "1.2.4",
			},
			wantRelease: nil,
			wantSource:  nil,
		},
		{
			name: "the link follows the tag the run selected",
			update: check.Update{
				ImageName: "traefik", CurrentTag: "v2.9.3", LatestTag: "v2.11.4",
				SourceURL: "https://github.com/traefik/traefik",
			},
			wantRelease: "https://github.com/traefik/traefik/releases/tag/v2.11.4",
			wantSource:  "https://github.com/traefik/traefik",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			w := New(FormatJSONL, &buf)
			w.Update(tt.update, policy.LevelMinor, Result{})
			require.NoError(t, w.Close())

			rec := decode(t, &buf)[0]
			if tt.wantRelease == nil {
				assert.NotContains(t, rec, "release_url")
			} else {
				assert.Equal(t, tt.wantRelease, rec["release_url"])
			}
			if tt.wantSource == nil {
				assert.NotContains(t, rec, "source_url")
			} else {
				assert.Equal(t, tt.wantSource, rec["source_url"])
			}
		})
	}
}

// An image nothing could be read about is reported under its own kind, where the
// links have no place: there is no release to point at.
func TestJSONLUnreadableCarriesNoLinks(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	u := check.Update{ImageName: "ghcr.io/vert-sh/vert", CurrentTag: "sha-e1c83ba"}
	u.MarkUnreadable(check.ReasonNoTagForDigest, "none of this image's tags matches its newest digest")
	w.Update(u, policy.LevelUnreadable, Result{})
	require.NoError(t, w.Close())

	rec := decode(t, &buf)[0]
	assert.Equal(t, "unreadable", rec["kind"])
	assert.NotContains(t, rec, "release_url")
	assert.NotContains(t, rec, "source_url")
}
