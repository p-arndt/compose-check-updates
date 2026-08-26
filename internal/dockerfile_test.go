package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// writeStack lays out a directory holding a compose file and, for every further
// entry, a file next to it — the shape a `build:` is resolved against.
func writeStack(t *testing.T, compose string, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	assert.NoError(t, os.WriteFile(path, []byte(compose), 0644))

	for name, content := range files {
		full := filepath.Join(dir, name)
		assert.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		assert.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}

	return path
}

func TestGetBuildTargets(t *testing.T) {
	t.Run("long form with a quoted context", func(t *testing.T) {
		// The shape a real stack takes, commented-out image line included: it
		// must not be read as the service's image.
		path := writeStack(t, `services:
  keycloak:
    # image: quay.io/keycloak/keycloak:latest
    build:
      context: "./"
      dockerfile: Dockerfile
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_started
  postgres:
    image: postgres:16.2
`, map[string]string{"Dockerfile": "FROM keycloak/keycloak:26.7.2\n"})

		targets := GetBuildTargets(path)

		assert.Len(t, targets, 1)
		assert.Equal(t, "keycloak", targets[0].Service)
		assert.Equal(t, filepath.Join(filepath.Dir(path), "Dockerfile"), targets[0].Dockerfile)
	})

	t.Run("short form names the context on the key", func(t *testing.T) {
		path := writeStack(t, `services:
  app:
    build: ./app
`, map[string]string{"app/Dockerfile": "FROM alpine:3.19\n"})

		targets := GetBuildTargets(path)

		assert.Len(t, targets, 1)
		assert.Equal(t, filepath.Join(filepath.Dir(path), "app", "Dockerfile"), targets[0].Dockerfile)
	})

	t.Run("a named dockerfile is resolved against the context", func(t *testing.T) {
		path := writeStack(t, `services:
  app:
    build:
      context: ./app
      dockerfile: Dockerfile.prod
      args:
        FOO: bar
`, map[string]string{"app/Dockerfile.prod": "FROM alpine:3.19\n"})

		targets := GetBuildTargets(path)

		assert.Len(t, targets, 1)
		assert.Equal(t, filepath.Join(filepath.Dir(path), "app", "Dockerfile.prod"), targets[0].Dockerfile)
	})

	t.Run("two services building the same file yield one target", func(t *testing.T) {
		path := writeStack(t, `services:
  web:
    build: .
  worker:
    build: .
`, map[string]string{"Dockerfile": "FROM alpine:3.19\n"})

		assert.Len(t, GetBuildTargets(path), 1)
	})

	t.Run("nothing to read on disk yields nothing", func(t *testing.T) {
		path := writeStack(t, `services:
  missing:
    build: ./nowhere
  remote:
    build:
      context: https://github.com/example/repo.git
  inline:
    build:
      context: ./
      dockerfile_inline: |
        FROM alpine:3.19
  plain:
    image: postgres:16.2
`, map[string]string{"Dockerfile": "FROM alpine:3.19\n"})

		// The inline service does name a context that exists, but its Dockerfile
		// lives in the compose file itself and has no lines of its own to rewrite.
		assert.Empty(t, GetBuildTargets(path))
	})
}

func TestParseFrom(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		image string
		stage string
		ok    bool
	}{
		{"plain", "FROM alpine:3.19", "alpine:3.19", "", true},
		{"with stage", "FROM keycloak/keycloak:26.7.2 AS builder", "keycloak/keycloak:26.7.2", "builder", true},
		{"lowercase", "from alpine:3.19 as build", "alpine:3.19", "build", true},
		{"with platform", "FROM --platform=$BUILDPLATFORM golang:1.22 AS build", "golang:1.22", "build", true},
		{"indented", "  FROM alpine:3.19  ", "alpine:3.19", "", true},
		{"copy from a stage is not a FROM", "COPY --from=builder /opt/keycloak/ /opt/keycloak/", "", "", false},
		{"a comment is not a FROM", "# FROM alpine:3.19", "", "", false},
		{"a word starting with from is not a FROM", "RUN fromage --help", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image, stage, ok := parseFrom(tt.line)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.image, image)
			assert.Equal(t, tt.stage, stage)
		})
	}
}

func TestCreateDockerfileUpdateInfos(t *testing.T) {
	t.Run("a multi-stage base image is one update carrying both lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "Dockerfile")
		assert.NoError(t, os.WriteFile(path, []byte(`FROM keycloak/keycloak:26.7.2 AS builder
ENV KC_HEALTH_ENABLED=true
RUN /opt/keycloak/bin/kc.sh build --db postgres

FROM keycloak/keycloak:26.7.2
COPY --from=builder /opt/keycloak/ /opt/keycloak/
USER keycloak
`), 0644))

		infos, err := NewDockerfileChecker(path, filepath.Join(dir, "compose.yaml"), "keycloak", nil).createUpdateInfos()

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
		dir := t.TempDir()
		path := filepath.Join(dir, "Dockerfile")
		assert.NoError(t, os.WriteFile(path, []byte(`ARG BASE=alpine:3.19
FROM golang:1.22 AS build
FROM scratch
FROM ${BASE}
FROM build AS final
FROM alpine:3.19
`), 0644))

		infos, err := NewDockerfileChecker(path, filepath.Join(dir, "compose.yaml"), "app", nil).createUpdateInfos()

		assert.NoError(t, err)
		assert.Len(t, infos, 2)
		assert.Equal(t, "library/golang", infos[0].ImageName)
		assert.Equal(t, "library/alpine", infos[1].ImageName)
	})
}

// TestUpdateRewritesEveryStage is the point of ExtraLines: a builder stage left
// on the old base would have the final image copy artefacts out of a release it
// is no longer built from.
func TestUpdateRewritesEveryStage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	content := `FROM keycloak/keycloak:26.7.2 AS builder
RUN /opt/keycloak/bin/kc.sh build --db postgres

FROM keycloak/keycloak:26.7.2
COPY --from=builder /opt/keycloak/ /opt/keycloak/
`
	assert.NoError(t, os.WriteFile(path, []byte(content), 0644))
	t.Cleanup(func() { os.Remove(path + ".ccu") })

	u := UpdateInfo{
		FilePath:      path,
		ComposePath:   filepath.Join(dir, "compose.yaml"),
		RawLine:       "FROM keycloak/keycloak:26.7.2 AS builder",
		ExtraLines:    []string{"FROM keycloak/keycloak:26.7.2"},
		FullImageName: "keycloak/keycloak:26.7.2",
		ImageName:     "keycloak/keycloak",
		CurrentTag:    "26.7.2",
		LatestTag:     "26.8.0",
	}

	assert.NoError(t, u.Update())

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
func TestParseFromWithTrailingComment(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		image string
		stage string
	}{
		{"plain", "FROM alpine:3.19 # the runtime", "alpine:3.19", ""},
		{"with stage", "FROM alpine:3.19 AS build # the builder", "alpine:3.19", "build"},
		{"no space before the comment", "FROM alpine:3.19# terse", "alpine:3.19", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image, stage, ok := parseFrom(tt.line)
			assert.True(t, ok)
			assert.Equal(t, tt.image, image)
			assert.Equal(t, tt.stage, stage)
		})
	}
}

// TestDockerfileCommentedStageStillMoves is the same guard one level up: both
// stages end up in one update even when one of them carries a comment.
func TestDockerfileCommentedStageStillMoves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	assert.NoError(t, os.WriteFile(path, []byte(`FROM keycloak/keycloak:26.7.2 AS builder
RUN /opt/keycloak/bin/kc.sh build --db postgres

FROM keycloak/keycloak:26.7.2   # the optimized runtime
COPY --from=builder /opt/keycloak/ /opt/keycloak/
`), 0644))

	infos, err := NewDockerfileChecker(path, filepath.Join(dir, "compose.yaml"), "keycloak", nil).createUpdateInfos()

	assert.NoError(t, err)
	assert.Len(t, infos, 1)
	assert.Equal(t, []string{"FROM keycloak/keycloak:26.7.2   # the optimized runtime"}, infos[0].ExtraLines)
}

// TestGetBuildTargetsIgnoresNestedKeys covers the two ways a build block used to
// read something that was not its own: a comment where its value would be, and a
// build arg one level deeper that happens to be called `context`.
func TestGetBuildTargetsIgnoresNestedKeys(t *testing.T) {
	path := writeStack(t, `services:
  app:
    build:  # built locally, not pulled
      context: ./app
      args:
        context: /somewhere/else
        dockerfile: nonsense
`, map[string]string{"app/Dockerfile": "FROM alpine:3.19\n"})

	targets := GetBuildTargets(path)

	assert.Len(t, targets, 1)
	assert.Equal(t, filepath.Join(filepath.Dir(path), "app", "Dockerfile"), targets[0].Dockerfile)
}
