package internal

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// newStableOnlyServer serves a repository that publishes no "latest" at all —
// its moving tag is "stable" — which is the whole reason the reference tag is a
// setting rather than a constant.
func newStableOnlyServer(t *testing.T) (host, image string) {
	t.Helper()

	server := newRegistryTestServer(t, "internal/thing",
		[]string{"stable", "sha-49821e5", "sha-e1c83ba"},
		map[string]string{
			"stable":      digestNew,
			"sha-49821e5": digestNew,
			"sha-e1c83ba": digestOld,
		})
	t.Cleanup(server.Close)

	serverURL, _ := url.Parse(server.URL)
	return serverURL.Host, serverURL.Host + "/internal/thing"
}

// The named tag is what "newest" means for this image, so the commit tag
// carrying that digest is the update.
func TestReferenceTagMovesTagToTheNamedReference(t *testing.T) {
	host, image := newStableOnlyServer(t)
	file := writeComposeFile(t, "image: "+image+":sha-e1c83ba")

	checker := NewUpdateChecker(file, NewRegistry(host)).
		WithReferenceTags(map[string]string{image: "stable"})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "sha-49821e5", info.LatestTag)
	assert.Equal(t, digestOld, info.CurrentDigest)
	assert.Equal(t, digestNew, info.LatestDigest)
	assert.Equal(t, "digest", info.UpdateLevel())

	assertUpdateWrites(t, info, file, "image: "+image+":sha-49821e5")
}

// A digest-pinned reference is rewritten in place, against the same tag.
func TestReferenceTagRefreshesAPinnedDigest(t *testing.T) {
	host, image := newStableOnlyServer(t)
	file := writeComposeFile(t, "image: "+image+"@"+digestOld)

	checker := NewUpdateChecker(file, NewRegistry(host)).
		WithReferenceTags(map[string]string{image: "stable"})
	infos, err := checker.Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)

	assert.Equal(t, digestNew, infos[0].LatestDigest)
	assertUpdateWrites(t, infos[0], file, "image: "+image+"@"+digestNew)
}

// The same image without the setting: there is no "latest" to compare against,
// so ccu has nothing to report — which is exactly what it did before, and what
// the setting exists to fix.
func TestWithoutAReferenceTagAnImageWithoutLatestIsSkipped(t *testing.T) {
	host, image := newStableOnlyServer(t)
	file := writeComposeFile(t, "image: "+image+":sha-e1c83ba")

	infos, err := NewUpdateChecker(file, NewRegistry(host)).Check(true, true, true)
	assert.NoError(t, err)
	assert.Len(t, infos, 1)
	assert.Empty(t, infos[0].LatestDigest)
	assert.False(t, infos[0].HasNewVersion(true, true, true))
}

// An image nobody named keeps comparing against "latest", so one entry cannot
// change what every other image is checked against.
func TestReferenceTagLookupIsExact(t *testing.T) {
	checker := (&UpdateChecker{}).WithReferenceTags(map[string]string{
		"internal/thing": "stable",
		// A key written with nothing after it is not a tag; it takes the default
		// rather than asking the registry for the empty one.
		"internal/other": "",
	})

	assert.Equal(t, "stable", checker.referenceTagFor("internal/thing"))
	assert.Equal(t, defaultReferenceTag, checker.referenceTagFor("internal/other"))
	assert.Equal(t, defaultReferenceTag, checker.referenceTagFor("library/redis"))
	assert.Equal(t, defaultReferenceTag, (&UpdateChecker{}).referenceTagFor("internal/thing"))
}

// The reference is the tag whose digest is being chased, so it can never be
// offered as the tag now carrying it.
func TestDigestCandidatesDropsTheReferenceTag(t *testing.T) {
	tags := []string{"stable", "stable-49821e5", "stable-e1c83ba"}

	candidates, dropped := digestCandidates(tags, "stable-e1c83ba", "stable")

	assert.Equal(t, []string{"stable-49821e5"}, candidates)
	assert.Zero(t, dropped)
}
