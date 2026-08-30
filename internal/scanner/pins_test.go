package scanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

// writeFile puts one file into dir and returns its path, so a test can spell out
// the stack it needs instead of leaning on the shared fixtures.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

// updateEvents narrows a drained scan to the rows a consumer would render.
func updateEvents(events []Event) []Event {
	var updates []Event
	for _, ev := range events {
		if ev.Kind == EventUpdate {
			updates = append(updates, ev)
		}
	}
	return updates
}

// The point of ScanPins: a bare floating tag has nothing to compare against, so
// the scan writes down what it resolves to right now.
func TestScanPinsRecordsTheDigestAFloatingTagResolvesTo(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": registrytest.DigestNew})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	root := t.TempDir()
	compose := writeFile(t, root, "compose.yaml",
		"services:\n  app:\n    image: library/myimage:latest\n")

	ch, err := ScanPins(context.Background(), Options{Root: root})
	require.NoError(t, err)
	events := collect(t, ch)

	require.Len(t, events, 1, "the pin scan carries no progress events of its own")
	assert.Equal(t, EventUpdate, events[0].Kind)
	assert.Equal(t, compose, events[0].Path)
	assert.Equal(t, policy.LevelPin, events[0].Level)
	assert.True(t, events[0].Update.PinsFloating)
	assert.Equal(t, "latest", events[0].Update.LatestTag)
	assert.Equal(t, registrytest.DigestNew, events[0].Update.LatestDigest)
}

// Neither a reference that already carries a digest nor a fixed version tag is
// waiting to be pinned, and neither may cost a request.
func TestScanPinsLeavesPinnedAndVersionedReferencesAlone(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"latest", "1.0"},
		map[string]string{"latest": registrytest.DigestNew, "1.0": registrytest.DigestOld})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	root := t.TempDir()
	writeFile(t, root, "compose.yaml", "services:\n"+
		"  pinned:\n    image: library/myimage:latest@"+registrytest.DigestOld+"\n"+
		"  versioned:\n    image: library/myimage:1.0\n")

	ch, err := ScanPins(context.Background(), Options{Root: root})
	require.NoError(t, err)

	assert.Empty(t, collect(t, ch))
}

// A floating tag the registry cannot answer for is left alone rather than
// reported as half-pinned: there is no digest to write into the file.
func TestScanPinsSkipsAFloatingTagTheRegistryDoesNotKnow(t *testing.T) {
	server := registrytest.Server(t, "library/myimage", []string{"latest"}, map[string]string{})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	root := t.TempDir()
	writeFile(t, root, "compose.yaml",
		"services:\n  app:\n    image: library/myimage:latest\n")

	ch, err := ScanPins(context.Background(), Options{Root: root})
	require.NoError(t, err)

	assert.Empty(t, collect(t, ch))
}

// A file that cannot be read fails that file and nothing else, as an event the
// consumer can show next to the stack it belongs to.
func TestScanPinsEmitsAnErrorForAFileItCannotRead(t *testing.T) {
	// Windows maps Chmod onto the read-only bit alone, so the file stays
	// readable and there would be no error to assert.
	if runtime.GOOS == "windows" {
		t.Skip("Skipping permission denied test on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root reads a file whatever its mode says")
	}

	root := t.TempDir()
	compose := writeFile(t, root, "compose.yaml", "services:\n  app:\n    image: library/myimage:latest\n")
	require.NoError(t, os.Chmod(compose, 0000))

	ch, err := ScanPins(context.Background(), Options{Root: root})
	require.NoError(t, err)
	events := collect(t, ch)

	require.Len(t, events, 1)
	assert.Equal(t, EventError, events[0].Kind)
	assert.Equal(t, compose, events[0].Path)
	assert.Error(t, events[0].Err)
}

// A pinned digest that still matches what the floating tag resolves to is no
// news, so the ordinary scan says nothing about it.
func TestScanReportsNothingForAPinnedDigestThatIsStillCurrent(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": registrytest.DigestNew})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	root := t.TempDir()
	writeFile(t, root, "compose.yaml",
		"services:\n  app:\n    image: library/myimage:latest@"+registrytest.DigestNew+"\n")

	ch, err := Scan(context.Background(), Options{Root: root, Major: true, Minor: true, Patch: true})
	require.NoError(t, err)

	assert.Empty(t, updateEvents(collect(t, ch)))
}

// Once the digest is in the file, the run after it can tell that the tag has
// moved under it — the whole reason for pinning in the first place.
func TestScanReportsAPinnedDigestThatHasMoved(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"latest"},
		map[string]string{"latest": registrytest.DigestNew})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	root := t.TempDir()
	writeFile(t, root, "compose.yaml",
		"services:\n  app:\n    image: library/myimage:latest@"+registrytest.DigestOld+"\n")

	ch, err := Scan(context.Background(), Options{Root: root, Major: true, Minor: true, Patch: true})
	require.NoError(t, err)

	updates := updateEvents(collect(t, ch))
	require.Len(t, updates, 1)
	assert.Equal(t, policy.Level("digest"), updates[0].Level)
	assert.Equal(t, registrytest.DigestOld, updates[0].Update.CurrentDigest)
	assert.Equal(t, registrytest.DigestNew, updates[0].Update.LatestDigest)
}

// CheckImage stands in for a whole scan of one line, so it has to hand back the
// event that scan would have emitted.
func TestCheckImageReturnsTheEventAScanWouldEmit(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"1.0", "1.1"},
		map[string]string{"1.0": registrytest.DigestOld, "1.1": registrytest.DigestNew})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	compose := writeFile(t, t.TempDir(), "compose.yaml",
		"services:\n  app:\n    image: library/myimage:1.0\n")

	event, err := CheckImage(
		Options{Major: true, Minor: true, Patch: true},
		check.Update{FilePath: compose, FullImageName: "library/myimage:1.0"})
	require.NoError(t, err)

	assert.Equal(t, EventUpdate, event.Kind)
	assert.Equal(t, compose, event.Path)
	assert.Equal(t, "1.1", event.Update.LatestTag)
	assert.Equal(t, policy.LevelMinor, event.Level)
}

// A Dockerfile is only ever reached through the service that builds it, so its
// row has to come back under the compose file a restart would act on.
func TestCheckImageForADockerfileGroupsUnderTheComposeFile(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"1.0", "1.1"},
		map[string]string{"1.0": registrytest.DigestOld, "1.1": registrytest.DigestNew})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	dir := t.TempDir()
	compose := writeFile(t, dir, "compose.yaml",
		"services:\n  app:\n    build:\n      context: \"./\"\n      dockerfile: Dockerfile\n")
	dockerfile := writeFile(t, dir, "Dockerfile", "FROM library/myimage:1.0\n")

	event, err := CheckImage(
		Options{Major: true, Minor: true, Patch: true},
		check.Update{
			FilePath:      dockerfile,
			ComposePath:   compose,
			Services:      []string{"app"},
			FullImageName: "library/myimage:1.0",
		})
	require.NoError(t, err)

	// Path is the compose file, while the update still points at the Dockerfile
	// the line lives on.
	assert.Equal(t, compose, event.Path)
	assert.Equal(t, dockerfile, event.Update.FilePath)
	assert.Equal(t, compose, event.Update.ComposePath)
	assert.Equal(t, []string{"app"}, event.Update.Services)
	assert.Equal(t, "1.1", event.Update.LatestTag)
}

// A caller that lost track of the service still gets its row; only the service
// name is missing, which is the one thing the update did not carry.
func TestCheckImageForADockerfileWithoutAService(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"1.0", "1.1"},
		map[string]string{"1.0": registrytest.DigestOld, "1.1": registrytest.DigestNew})
	t.Setenv("CCU_REGISTRY_HOST", registrytest.Host(server))

	dir := t.TempDir()
	compose := writeFile(t, dir, "compose.yaml",
		"services:\n  app:\n    build:\n      context: \"./\"\n      dockerfile: Dockerfile\n")
	dockerfile := writeFile(t, dir, "Dockerfile", "FROM library/myimage:1.0\n")

	event, err := CheckImage(
		Options{Major: true, Minor: true, Patch: true},
		check.Update{FilePath: dockerfile, ComposePath: compose, FullImageName: "library/myimage:1.0"})
	require.NoError(t, err)

	assert.Equal(t, compose, event.Path)
	assert.Equal(t, "1.1", event.Update.LatestTag)
}

// The update an earlier scan reported is what identifies the line, so a file
// edited in between has to be reported rather than silently re-checked.
func TestCheckImageFailsWhenTheFileNoLongerNamesTheImage(t *testing.T) {
	compose := writeFile(t, t.TempDir(), "compose.yaml",
		"services:\n  app:\n    image: library/other:1.0\n")

	_, err := CheckImage(Options{}, check.Update{FilePath: compose, FullImageName: "library/myimage:1.0"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer names")
}

func TestCheckImageReportsAFileItCannotRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "compose.yaml")

	_, err := CheckImage(Options{}, check.Update{FilePath: missing, FullImageName: "library/myimage:1.0"})

	assert.Error(t, err)
}
