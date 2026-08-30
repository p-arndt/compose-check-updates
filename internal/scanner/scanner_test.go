package scanner

import (
	"context"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/p-arndt/compose-check-updates/internal/check"
	composepkg "github.com/p-arndt/compose-check-updates/internal/compose"
	"github.com/p-arndt/compose-check-updates/internal/policy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mirrorTestFixtures recreates the layout of the repository's tests/ directory
// as empty files. The real fixtures reference real images, so checking them
// would hit registries; empty compose files exercise discovery and the event
// stream without any network access.
func mirrorTestFixtures(t *testing.T) string {
	t.Helper()

	src := filepath.Join("..", "..", "tests")
	dst := t.TempDir()

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return os.WriteFile(target, nil, 0644)
	})
	require.NoError(t, err)

	return dst
}

// collect drains every event of a scan, failing if the channel is not closed.
func collect(t *testing.T, ch <-chan Event) []Event {
	t.Helper()

	var events []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("scan did not finish in time")
		}
	}
}

func kindPaths(events []Event, kind EventKind) []string {
	var paths []string
	for _, ev := range events {
		if ev.Kind == kind {
			paths = append(paths, filepath.ToSlash(ev.Path))
		}
	}
	sort.Strings(paths)
	return paths
}

func TestScanEmptyDirectory(t *testing.T) {
	ch, err := Scan(context.Background(), Options{Root: t.TempDir()})
	require.NoError(t, err)

	events := collect(t, ch)
	require.Len(t, events, 1)
	assert.Equal(t, EventDiscovered, events[0].Kind)
	assert.Equal(t, 0, events[0].Total)
	assert.Empty(t, events[0].Path)
}

func TestScanDiscoversComposeFiles(t *testing.T) {
	root := mirrorTestFixtures(t)

	ch, err := Scan(context.Background(), Options{Root: root})
	require.NoError(t, err)

	events := collect(t, ch)
	require.NotEmpty(t, events)
	require.Equal(t, EventDiscovered, events[0].Kind, "EventDiscovered must be first")
	assert.Equal(t, 8, events[0].Total)

	expected := []string{
		"docker-compose.yml",
		"folder1/compose.yaml",
		"folder1/compose.yml",
		"folder2/docker-compose.yaml",
		"folder2/docker-compose.yml",
		"keycloak/compose.yaml",
		"sample1/docker-compose.yml",
		"sample2/compose.yml",
	}
	for i := range expected {
		expected[i] = filepath.ToSlash(filepath.Join(root, expected[i]))
	}
	sort.Strings(expected)

	assert.Equal(t, expected, kindPaths(events, EventFileStart))
	// Empty fixtures declare no images, so every file completes cleanly.
	assert.Equal(t, expected, kindPaths(events, EventFileDone))
	assert.Empty(t, kindPaths(events, EventError))
	assert.Empty(t, kindPaths(events, EventUpdate))
}

func TestScanHonoursExclude(t *testing.T) {
	root := mirrorTestFixtures(t)

	ch, err := Scan(context.Background(), Options{Root: root, Exclude: []string{"folder1", "folder2"}})
	require.NoError(t, err)

	events := collect(t, ch)
	require.NotEmpty(t, events)
	assert.Equal(t, 4, events[0].Total)

	for _, path := range kindPaths(events, EventFileStart) {
		assert.NotContains(t, path, "/folder1/")
		assert.NotContains(t, path, "/folder2/")
	}
}

func TestScanCancelledContext(t *testing.T) {
	root := mirrorTestFixtures(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch, err := Scan(ctx, Options{Root: root})
	require.NoError(t, err)

	// The channel must still close, and nothing may be emitted once cancelled.
	events := collect(t, ch)
	assert.Empty(t, events)
}

func TestScanCancelDuringConsumption(t *testing.T) {
	root := mirrorTestFixtures(t)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := Scan(ctx, Options{Root: root, Concurrency: 1})
	require.NoError(t, err)

	first, ok := <-ch
	require.True(t, ok)
	require.Equal(t, EventDiscovered, first.Kind)

	cancel()
	collect(t, ch) // must terminate rather than deadlock
}

// TestDockerfileCheckers covers the wiring only: whether the Dockerfiles a
// compose file builds are picked up at all, and that the option switches them
// off. What the checkers then find needs a registry and is tested there.
func TestDockerfileCheckers(t *testing.T) {
	dir := t.TempDir()
	compose := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(compose, []byte(`services:
  keycloak:
    build:
      context: "./"
      dockerfile: Dockerfile
  postgres:
    image: postgres:16.2
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM library/postgres:latest\n"), 0644))
	require.Len(t, composepkg.BuildTargets(compose), 1)

	const digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	server := registrytest.Server(t, "library/postgres", []string{"16.2", "latest"}, map[string]string{"latest": digest})
	t.Setenv("CCU_REGISTRY_HOST", strings.TrimPrefix(server.URL, "http://"))

	// Only the Dockerfile's FROM floats, so a pin appearing at all is proof the
	// Dockerfile was read.
	withDockerfiles, err := checkAll(Options{Dockerfiles: true}, compose, (*check.Checker).CheckPins)
	require.NoError(t, err)
	require.Len(t, withDockerfiles, 1)
	assert.Equal(t, "keycloak", withDockerfiles[0].Services[0])

	without, err := checkAll(Options{}, compose, (*check.Checker).CheckPins)
	require.NoError(t, err)
	assert.Empty(t, without)
}

func TestScanEmitsUnreadableImages(t *testing.T) {
	const (
		digestOld = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		digestNew = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	)

	server := registrytest.Server(t, "library/myimage",
		[]string{"latest", "sha-e1c83ba"},
		map[string]string{"latest": digestNew, "sha-e1c83ba": digestOld})

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv("CCU_REGISTRY_HOST", serverURL.Host)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "compose.yaml"),
		[]byte("services:\n  app:\n    image: library/myimage:sha-e1c83ba\n"), 0644))

	events, err := Scan(context.Background(), Options{Root: root, Major: true, Minor: true, Patch: true})
	require.NoError(t, err)

	var updates []Event
	for _, ev := range collect(t, events) {
		if ev.Kind == EventUpdate {
			updates = append(updates, ev)
		}
	}

	require.Len(t, updates, 1)
	assert.Equal(t, policy.LevelUnreadable, updates[0].Level)
	assert.True(t, updates[0].Update.IsUnreadable())
	assert.Equal(t, check.ReasonNoTagForDigest, updates[0].Update.UnreadableReason)
}
