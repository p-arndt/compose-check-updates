package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

func TestSetImageVersioningKeepsCommentsAndOtherKeys(t *testing.T) {
	const original = `# a comment the user wrote
images:
  library/traefik:
    max: minor # the ceiling
`
	path := writeConfig(t, t.TempDir(), original)

	if err := SetImageVersioning(path, "library/traefik", policy.VersioningLoose); err != nil {
		t.Fatalf("SetImageVersioning: %v", err)
	}

	got := readConfig(t, path)
	for _, want := range []string{
		"# a comment the user wrote",
		"max: minor",
		"# the ceiling",
		"versioning: loose",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config lost %q:\n%s", want, got)
		}
	}

	cfg := parseConfig(t, path)
	if got := cfg.Images["library/traefik"].Versioning; got != policy.VersioningLoose {
		t.Errorf("policy.Versioning = %q, want %q", got, policy.VersioningLoose)
	}
	if got := cfg.MaxLevel("library/traefik"); got != policy.LevelMinor {
		t.Errorf("MaxLevel = %q, want %q — the cap must survive the other key", got, policy.LevelMinor)
	}
}

func TestSetImageVersioningCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".ccu.yaml")

	if err := SetImageVersioning(path, "library/traefik", policy.VersioningSemver); err != nil {
		t.Fatalf("SetImageVersioning: %v", err)
	}

	if got := parseConfig(t, path).Images["library/traefik"].Versioning; got != policy.VersioningSemver {
		t.Errorf("policy.Versioning = %q, want %q", got, policy.VersioningSemver)
	}
}

func TestSetImageVersioningRejectsAnUnknownScheme(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")

	if err := SetImageVersioning(path, "library/traefik", policy.Versioning("calver")); err == nil {
		t.Fatal("SetImageVersioning accepted a scheme no run could read back")
	}
	if got := readConfig(t, path); got != "" {
		t.Errorf("the file was touched anyway:\n%s", got)
	}
}

// An empty scheme is the absence of one, and the absence is written by removing
// the key rather than by writing something the next load would have to read.
func TestSetImageVersioningWithNoSchemeClearsIt(t *testing.T) {
	path := writeConfig(t, t.TempDir(), `images:
  library/traefik:
    versioning: loose
    max: minor
`)

	if err := SetImageVersioning(path, "library/traefik", ""); err != nil {
		t.Fatalf("SetImageVersioning: %v", err)
	}

	got := readConfig(t, path)
	if strings.Contains(got, "versioning") {
		t.Errorf("the scheme is still there:\n%s", got)
	}
	if !strings.Contains(got, "max: minor") {
		t.Errorf("the cap went with it:\n%s", got)
	}
}

// The entry, and then the whole file, goes when nothing is left in it: an empty
// config sitting in a project still looks as though it configured something.
func TestClearImageVersioningRemovesTheEmptiedFile(t *testing.T) {
	path := writeConfig(t, t.TempDir(), `images:
  library/traefik:
    versioning: loose
`)

	if err := ClearImageVersioning(path, "library/traefik"); err != nil {
		t.Fatalf("ClearImageVersioning: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the config survived with nothing in it: %v", err)
	}
}

// Clearing what was never set is not an error, and must not rewrite a file it
// had no work to do in.
func TestClearImageVersioningLeavesAnUntouchedFileAlone(t *testing.T) {
	const original = `images:
  library/traefik:
    max: minor
`
	path := writeConfig(t, t.TempDir(), original)

	if err := ClearImageVersioning(path, "library/traefik"); err != nil {
		t.Fatalf("ClearImageVersioning: %v", err)
	}
	if err := ClearImageVersioning(path, "library/redis"); err != nil {
		t.Fatalf("ClearImageVersioning: %v", err)
	}

	if got := readConfig(t, path); got != original {
		t.Errorf("the file was rewritten:\n%s", got)
	}
}
