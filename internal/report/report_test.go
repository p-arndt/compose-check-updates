package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFormat(t *testing.T) {
	for in, want := range map[string]Format{
		"":       FormatAuto,
		"auto":   FormatAuto,
		"pretty": FormatPretty,
		"text":   FormatPretty,
		"json":   FormatJSONL,
		"jsonl":  FormatJSONL,
		"  JSON": FormatJSONL,
	} {
		got, err := ParseFormat(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	_, err := ParseFormat("yaml")
	assert.Error(t, err)
}

func TestFormatResolve(t *testing.T) {
	assert.Equal(t, FormatPretty, FormatAuto.Resolve(true))
	assert.Equal(t, FormatJSONL, FormatAuto.Resolve(false))
	// An explicit choice survives whatever stdout turns out to be, which is the
	// point of being able to state it.
	assert.Equal(t, FormatPretty, FormatPretty.Resolve(false))
	assert.Equal(t, FormatJSONL, FormatJSONL.Resolve(true))
}

// decode reads the JSONL a writer produced back into generic maps, which is how
// a consumer sees it — absent keys have to stay absent.
func decode(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), line)
		out = append(out, rec)
	}
	return out
}

func TestJSONLUpdate(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	u := internal.UpdateInfo{
		FilePath:      "stacks/web/compose.yaml",
		Services:      []string{"proxy", "proxy-internal"},
		ImageName:     "traefik",
		FullImageName: "traefik:v2.9.3",
		CurrentTag:    "v2.9.3",
		LatestTag:     "2.11.0",
		PatchTag:      "2.9.10",
		MinorTag:      "2.11.0",
	}
	w.Update(u, "minor", Result{})
	require.NoError(t, w.Close())

	recs := decode(t, &buf)
	require.Len(t, recs, 1)
	rec := recs[0]

	assert.Equal(t, "update", rec["kind"])
	assert.Equal(t, "traefik", rec["image"])
	assert.Equal(t, "traefik:v2.9.3", rec["reference"])
	assert.Equal(t, []any{"proxy", "proxy-internal"}, rec["services"])
	assert.Equal(t, "stacks/web/compose.yaml", rec["file"])
	assert.Equal(t, "v2.9.3", rec["current"])
	assert.Equal(t, "2.11.0", rec["latest"])
	assert.Equal(t, "minor", rec["level"])
	assert.Equal(t, map[string]any{"patch": "2.9.10", "minor": "2.11.0"}, rec["targets"])

	// Nothing was asked to be applied, so claiming either way would be a lie.
	assert.NotContains(t, rec, "applied")
	assert.NotContains(t, rec, "restarted")
	assert.NotContains(t, rec, "current_digest")
	assert.NotContains(t, rec, "latest_digest")
	assert.NotContains(t, rec, "cap")
}

func TestJSONLDigestUpdate(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	w.Update(internal.UpdateInfo{
		ImageName:     "ghcr.io/vert-sh/vert",
		CurrentTag:    "latest",
		LatestTag:     "latest",
		CurrentDigest: "sha256:aaa",
		LatestDigest:  "sha256:bbb",
	}, "digest", Result{})
	require.NoError(t, w.Close())

	rec := decode(t, &buf)[0]
	assert.Equal(t, "digest", rec["level"])
	assert.Equal(t, "sha256:aaa", rec["current_digest"])
	assert.Equal(t, "sha256:bbb", rec["latest_digest"])
}

// A digest that did not move must not be reported as the "latest" one: it would
// read as a change that never happened.
func TestJSONLUnchangedDigestIsNotReportedAsLatest(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	w.Update(internal.UpdateInfo{
		ImageName:     "nginx",
		CurrentTag:    "1.2.3",
		LatestTag:     "1.2.4",
		CurrentDigest: "sha256:aaa",
		LatestDigest:  "sha256:aaa",
	}, "patch", Result{})
	require.NoError(t, w.Close())

	rec := decode(t, &buf)[0]
	assert.Equal(t, "sha256:aaa", rec["current_digest"])
	assert.NotContains(t, rec, "latest_digest")
}

func TestJSONLAppliedAndFailedApply(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	w.Update(internal.UpdateInfo{ImageName: "redis"}, "minor", Result{ApplyRequested: true, Applied: true})
	w.Update(internal.UpdateInfo{ImageName: "postgres"}, "minor", Result{ApplyRequested: true})
	w.Update(internal.UpdateInfo{ImageName: "caddy"}, "minor", Result{ApplyRequested: true, Applied: true, RestartRequested: true, Restarted: true})
	require.NoError(t, w.Close())

	recs := decode(t, &buf)
	require.Len(t, recs, 3)
	assert.Equal(t, true, recs[0]["applied"])
	assert.NotContains(t, recs[0], "restarted")
	// The one that failed still says so, rather than dropping out of the stream.
	assert.Equal(t, false, recs[1]["applied"])
	assert.Equal(t, true, recs[2]["restarted"])
}

func TestJSONLError(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	w.Error("stacks/db/compose.yaml", errors.New("fetching tags: 429"))
	require.NoError(t, w.Close())

	rec := decode(t, &buf)[0]
	assert.Equal(t, "error", rec["kind"])
	assert.Equal(t, "stacks/db/compose.yaml", rec["file"])
	assert.Equal(t, "fetching tags: 429", rec["error"])
}

// Every line has to stand on its own: a consumer reading line by line must be
// able to parse each without the ones around it.
func TestJSONLIsOneObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	w.Update(internal.UpdateInfo{ImageName: "redis", CurrentTag: "7.0", LatestTag: "7.2"}, "minor", Result{})
	w.Error("compose.yaml", errors.New("boom"))
	require.NoError(t, w.Close())

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		assert.NotContains(t, line, "\n")
		var rec map[string]any
		assert.NoError(t, json.Unmarshal([]byte(line), &rec))
	}
}

// The pretty writer must not write to the stream the machine format owns; it
// goes through slog, which main points at stderr in that case.
func TestPrettyWriterWritesNothingToTheStream(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatPretty, &buf)

	w.Update(internal.UpdateInfo{ImageName: "redis"}, "minor", Result{})
	require.NoError(t, w.Close())

	assert.Empty(t, buf.String())
}

// TestJSONLDockerfileUpdate covers what a consumer of the report needs in order
// to act on a Dockerfile update: `file` is the file that was rewritten, and
// `compose_file` is the one to hand `docker compose -f`. A compose-file update
// carries no `compose_file` at all, since `file` already is one.
func TestJSONLDockerfileUpdate(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	w.Update(internal.UpdateInfo{
		FilePath:      "stacks/keycloak/Dockerfile",
		ComposePath:   "stacks/keycloak/compose.yaml",
		Services:      []string{"keycloak"},
		ImageName:     "quay.io/keycloak/keycloak",
		FullImageName: "quay.io/keycloak/keycloak:26.0.7",
		CurrentTag:    "26.0.7",
		LatestTag:     "26.7.2",
		MinorTag:      "26.7.2",
	}, "minor", Result{})
	w.Update(internal.UpdateInfo{
		FilePath:      "stacks/keycloak/compose.yaml",
		Services:      []string{"postgres"},
		ImageName:     "library/postgres",
		FullImageName: "postgres:16.2",
		CurrentTag:    "16.2",
		LatestTag:     "18.6",
		MajorTag:      "18.6",
	}, "major", Result{})
	require.NoError(t, w.Close())

	recs := decode(t, &buf)
	require.Len(t, recs, 2)

	assert.Equal(t, "stacks/keycloak/Dockerfile", recs[0]["file"])
	assert.Equal(t, "stacks/keycloak/compose.yaml", recs[0]["compose_file"])
	assert.NotContains(t, recs[1], "compose_file")
}

// An image ccu could not read gets a kind of its own: a consumer counting
// "update" lines is being told what to change, and this line changes nothing.
func TestJSONLUnreadable(t *testing.T) {
	var buf bytes.Buffer
	w := New(FormatJSONL, &buf)

	u := internal.UpdateInfo{
		ImageName:     "library/myimage",
		FullImageName: "library/myimage:sha-e1c83ba",
		FilePath:      "/tmp/compose.yaml",
		CurrentTag:    "sha-e1c83ba",
	}
	u.MarkUnreadable(internal.ReasonNoTagForDigest, "none of this image's tags matches its newest digest")

	w.Update(u, u.UpdateLevel(), Result{})
	require.NoError(t, w.Close())

	recs := decode(t, &buf)
	require.Len(t, recs, 1)

	assert.Equal(t, "unreadable", recs[0]["kind"])
	assert.Equal(t, internal.ReasonNoTagForDigest, recs[0]["reason"])
	assert.Equal(t, internal.LevelUnreadable, recs[0]["level"])
	assert.Contains(t, recs[0]["message"], "newest digest")
	// There is no target and no new digest, so neither key may be there to be
	// mistaken for one.
	assert.NotContains(t, recs[0], "latest")
	assert.NotContains(t, recs[0], "targets")
}
