package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

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
	assert.Equal(t, 7, events[0].Total)

	expected := []string{
		"docker-compose.yml",
		"folder1/compose.yaml",
		"folder1/compose.yml",
		"folder2/docker-compose.yaml",
		"folder2/docker-compose.yml",
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
	assert.Equal(t, 3, events[0].Total)

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
