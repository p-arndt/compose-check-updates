package check

import (
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// newStableOnlyServer serves a repository that publishes no "latest" at all —
// its moving tag is "stable" — which is the whole reason the reference tag is a
// setting rather than a constant.
func newStableOnlyServer(t *testing.T) (host, image string) {
	t.Helper()

	server := registrytest.Server(t, "internal/thing",
		[]string{"stable", "sha-49821e5", "sha-e1c83ba"},
		map[string]string{
			"stable":      registrytest.DigestNew,
			"sha-49821e5": registrytest.DigestNew,
			"sha-e1c83ba": registrytest.DigestOld,
		})
	t.Cleanup(server.Close)

	serverURL, _ := url.Parse(server.URL)
	return serverURL.Host, serverURL.Host + "/internal/thing"
}

// The named tag is what "newest" means for this image, so the commit tag
// carrying that digest is the update.
func TestReferenceTagMovesTagToTheNamedReference(t *testing.T) {
	t.Parallel()

	host, image := newStableOnlyServer(t)
	file := writeComposeFile(t, "image: "+image+":sha-e1c83ba")

	checker := New(file, registry.New(host), policy.Set{
		Images: map[string]policy.Image{image: {ReferenceTag: "stable"}},
	})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "sha-49821e5", info.LatestTag)
	assert.Equal(t, registrytest.DigestOld, info.CurrentDigest)
	assert.Equal(t, registrytest.DigestNew, info.LatestDigest)
	assert.Equal(t, policy.Level("digest"), info.Level())

	assertUpdateWrites(t, info, file, "image: "+image+":sha-49821e5")
}

// A digest-pinned reference is rewritten in place, against the same tag.
func TestReferenceTagRefreshesAPinnedDigest(t *testing.T) {
	t.Parallel()

	host, image := newStableOnlyServer(t)
	file := writeComposeFile(t, "image: "+image+"@"+registrytest.DigestOld)

	checker := New(file, registry.New(host), policy.Set{
		Images: map[string]policy.Image{image: {ReferenceTag: "stable"}},
	})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.Equal(t, registrytest.DigestNew, infos[0].LatestDigest)
	assertUpdateWrites(t, infos[0], file, "image: "+image+"@"+registrytest.DigestNew)
}

// The same image without the setting: there is no "latest" to compare against,
// so ccu has nothing to report — which is exactly what it did before, and what
// the setting exists to fix.
func TestWithoutAReferenceTagAnImageWithoutLatestIsSkipped(t *testing.T) {
	t.Parallel()

	host, image := newStableOnlyServer(t)
	file := writeComposeFile(t, "image: "+image+":sha-e1c83ba")

	infos, err := New(file, registry.New(host), policy.Set{}).Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)
	assert.Empty(t, infos[0].LatestDigest)
	assert.False(t, infos[0].HasNewVersion())
}

func TestDigestCandidatesDropsTheReferenceTag(t *testing.T) {
	t.Parallel()

	tags := []string{"stable", "stable-49821e5", "stable-e1c83ba"}

	candidates, dropped := digestCandidates(tags, "stable-e1c83ba", policy.Image{ReferenceTag: "stable"})

	assert.Equal(t, []string{"stable-49821e5"}, candidates)
	assert.Zero(t, dropped)
}
