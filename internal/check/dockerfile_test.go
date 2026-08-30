package check

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/stretchr/testify/assert"
)

func TestCreateDockerfileUpdateInfos(t *testing.T) {
	t.Parallel()

	t.Run("a multi-stage base image is one update carrying both lines", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "Dockerfile")
		assert.NoError(t, os.WriteFile(path, []byte(`FROM keycloak/keycloak:26.7.2 AS builder
ENV KC_HEALTH_ENABLED=true
RUN /opt/keycloak/bin/kc.sh build --db postgres

FROM keycloak/keycloak:26.7.2
COPY --from=builder /opt/keycloak/ /opt/keycloak/
USER keycloak
`), 0644))

		infos, err := NewDockerfile(path, filepath.Join(dir, "compose.yaml"), "keycloak", nil, policy.Set{}).updates()

		assert.NoError(t, err)
		assert.Len(t, infos, 1)
		assert.Equal(t, "keycloak/keycloak", infos[0].ImageName)
		assert.Equal(t, "26.7.2", infos[0].CurrentTag)
		assert.Equal(t, []string{"keycloak"}, infos[0].Services)
		assert.Equal(t, "FROM keycloak/keycloak:26.7.2 AS builder", infos[0].RawLine)
		assert.Equal(t, []string{"FROM keycloak/keycloak:26.7.2"}, infos[0].ExtraLines)
		assert.True(t, infos[0].IsDockerfile())
		assert.Equal(t, filepath.Join(dir, "compose.yaml"), infos[0].RestartPath())
	})

	t.Run("references no registry can answer are skipped", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "Dockerfile")
		assert.NoError(t, os.WriteFile(path, []byte(`ARG BASE=alpine:3.19
FROM golang:1.22 AS build
FROM scratch
FROM ${BASE}
FROM build AS final
FROM alpine:3.19
`), 0644))

		infos, err := NewDockerfile(path, filepath.Join(dir, "compose.yaml"), "app", nil, policy.Set{}).updates()

		assert.NoError(t, err)
		assert.Len(t, infos, 2)
		assert.Equal(t, "library/golang", infos[0].ImageName)
		assert.Equal(t, "library/alpine", infos[1].ImageName)
	})
}

// TestUpdateRewritesEveryStage is the point of ExtraLines: a builder stage left
// on the old base would have the final image copy artefacts out of a release it
// is no longer built from.

// TestUpdateRewritesEveryStage is the point of ExtraLines: a builder stage left
// on the old base would have the final image copy artefacts out of a release it
// is no longer built from.
func TestUpdateRewritesEveryStage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	content := `FROM keycloak/keycloak:26.7.2 AS builder
RUN /opt/keycloak/bin/kc.sh build --db postgres

FROM keycloak/keycloak:26.7.2
COPY --from=builder /opt/keycloak/ /opt/keycloak/
`
	assert.NoError(t, os.WriteFile(path, []byte(content), 0644))
	t.Cleanup(func() { os.Remove(path + ".ccu") })

	u := Update{
		FilePath:      path,
		ComposePath:   filepath.Join(dir, "compose.yaml"),
		RawLine:       "FROM keycloak/keycloak:26.7.2 AS builder",
		ExtraLines:    []string{"FROM keycloak/keycloak:26.7.2"},
		FullImageName: "keycloak/keycloak:26.7.2",
		ImageName:     "keycloak/keycloak",
		CurrentTag:    "26.7.2",
		LatestTag:     "26.8.0",
	}

	assert.NoError(t, u.Apply())

	written, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.Equal(t, `FROM keycloak/keycloak:26.8.0 AS builder
RUN /opt/keycloak/bin/kc.sh build --db postgres

FROM keycloak/keycloak:26.8.0
COPY --from=builder /opt/keycloak/ /opt/keycloak/
`, string(written))

	backup, err := os.ReadFile(path + ".ccu")
	assert.NoError(t, err)
	assert.Equal(t, content, string(backup))
}

// TestParseFromWithTrailingComment is the case a `\s*$` anchor used to hide: a
// commented FROM line parses like any other, or the stage it declares would be
// left behind on the old base image while its twin moves.

// TestDockerfileCommentedStageStillMoves is the same guard one level up: both
// stages end up in one update even when one of them carries a comment.
func TestDockerfileCommentedStageStillMoves(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	assert.NoError(t, os.WriteFile(path, []byte(`FROM keycloak/keycloak:26.7.2 AS builder
RUN /opt/keycloak/bin/kc.sh build --db postgres

FROM keycloak/keycloak:26.7.2   # the optimized runtime
COPY --from=builder /opt/keycloak/ /opt/keycloak/
`), 0644))

	infos, err := NewDockerfile(path, filepath.Join(dir, "compose.yaml"), "keycloak", nil, policy.Set{}).updates()

	assert.NoError(t, err)
	assert.Len(t, infos, 1)
	assert.Equal(t, []string{"FROM keycloak/keycloak:26.7.2   # the optimized runtime"}, infos[0].ExtraLines)
}

// TestGetBuildTargetsIgnoresNestedKeys covers the two ways a build block used to
// read something that was not its own: a comment where its value would be, and a
// build arg one level deeper that happens to be called `context`.
