package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// writeConfig drops a config file into dir and returns its path.
func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, ".ccu.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(data)
}

func parseConfig(t *testing.T, path string) Config {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open config: %v", err)
	}
	defer f.Close()

	cfg, err := Parse(f)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return cfg
}

func TestSetImageMaxKeepsCommentsAndOtherKeys(t *testing.T) {
	const original = `# a comment the user wrote
exclude:
  - node_modules # trailing note
  - vendor
`
	path := writeConfig(t, t.TempDir(), original)

	if err := SetImageMax(path, "library/traefik", policy.LevelMinor); err != nil {
		t.Fatalf("SetImageMax: %v", err)
	}

	got := readConfig(t, path)
	for _, want := range []string{
		"# a comment the user wrote",
		"exclude:",
		"- node_modules",
		"# trailing note",
		"- vendor",
		"library/traefik:",
		"max: minor",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config lost %q:\n%s", want, got)
		}
	}

	cfg := parseConfig(t, path)
	if len(cfg.Exclude) != 2 || cfg.Exclude[0] != "node_modules" || cfg.Exclude[1] != "vendor" {
		t.Errorf("exclude = %v, want [node_modules vendor]", cfg.Exclude)
	}
	if got := cfg.MaxLevel("library/traefik"); got != policy.LevelMinor {
		t.Errorf("MaxLevel = %q, want %q", got, policy.LevelMinor)
	}
}

func TestSetImageMaxCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "config.yaml")

	if err := SetImageMax(path, "library/redis", policy.LevelPatch); err != nil {
		t.Fatalf("SetImageMax: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Windows has no Unix permission bits: Go reports 0666 for any file that is
	// not read-only, whatever mode it was created with. There is nothing for the
	// assertion to check there, and checking it anyway only ever fails.
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o644 {
			t.Errorf("mode = %v, want 0644", got)
		}
	}

	if got := parseConfig(t, path).MaxLevel("library/redis"); got != policy.LevelPatch {
		t.Errorf("MaxLevel = %q, want %q", got, policy.LevelPatch)
	}
}

func TestSetImageMaxUpdatesInPlace(t *testing.T) {
	path := writeConfig(t, t.TempDir(), `images:
  library/traefik:
    max: minor
  library/redis:
    max: patch
`)

	if err := SetImageMax(path, "library/traefik", policy.LevelMajor); err != nil {
		t.Fatalf("SetImageMax: %v", err)
	}

	got := readConfig(t, path)
	if n := strings.Count(got, "library/traefik:"); n != 1 {
		t.Errorf("library/traefik appears %d times:\n%s", n, got)
	}
	if strings.Contains(got, "max: minor") {
		t.Errorf("old cap still present:\n%s", got)
	}

	cfg := parseConfig(t, path)
	if cfg.MaxLevel("library/traefik") != policy.LevelMajor {
		t.Errorf("traefik = %q, want %q", cfg.MaxLevel("library/traefik"), policy.LevelMajor)
	}
	if cfg.MaxLevel("library/redis") != policy.LevelPatch {
		t.Errorf("redis = %q, want %q", cfg.MaxLevel("library/redis"), policy.LevelPatch)
	}
}

func TestClearImageMaxRemovesEntryAndEmptyImages(t *testing.T) {
	path := writeConfig(t, t.TempDir(), `# keep me
exclude:
  - vendor

images:
  library/traefik:
    max: minor
  library/redis:
    max: patch
`)

	if err := ClearImageMax(path, "library/redis"); err != nil {
		t.Fatalf("ClearImageMax: %v", err)
	}

	got := readConfig(t, path)
	if strings.Contains(got, "library/redis") {
		t.Errorf("redis entry survived:\n%s", got)
	}
	if !strings.Contains(got, "library/traefik:") || !strings.Contains(got, "# keep me") {
		t.Errorf("removal took too much with it:\n%s", got)
	}

	if err := ClearImageMax(path, "library/traefik"); err != nil {
		t.Fatalf("ClearImageMax: %v", err)
	}

	got = readConfig(t, path)
	if strings.Contains(got, "images:") {
		t.Errorf("empty images map survived:\n%s", got)
	}
	if !strings.Contains(got, "vendor") {
		t.Errorf("unrelated keys lost:\n%s", got)
	}

	cfg := parseConfig(t, path)
	if cfg.MaxLevel("library/traefik") != "" {
		t.Errorf("cap survived: %q", cfg.MaxLevel("library/traefik"))
	}
}

func TestClearImageMaxUnknownImageIsNoOp(t *testing.T) {
	const original = `# untouched
images:
  library/traefik:
    max: minor
`
	dir := t.TempDir()
	path := writeConfig(t, dir, original)

	if err := ClearImageMax(path, "library/redis"); err != nil {
		t.Fatalf("ClearImageMax: %v", err)
	}
	if got := readConfig(t, path); got != original {
		t.Errorf("file was rewritten:\n%s", got)
	}

	missing := filepath.Join(dir, "does-not-exist.yaml")
	if err := ClearImageMax(missing, "library/redis"); err != nil {
		t.Fatalf("ClearImageMax on missing file: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("missing config was created")
	}
}

func TestSetImageMaxRejectsInvalidLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := SetImageMax(path, "library/traefik", policy.Level("mayor")); err == nil {
		t.Fatal("SetImageMax accepted an invalid level")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("invalid level still wrote a file")
	}
}

func TestClearImageMaxRemovesFileItCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ccu.yaml")

	if err := SetImageMax(path, "library/traefik", policy.LevelMinor); err != nil {
		t.Fatalf("SetImageMax: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config was not written: %v", err)
	}

	if err := ClearImageMax(path, "library/traefik"); err != nil {
		t.Fatalf("ClearImageMax: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("empty config file survived the last cap being cleared: %v", err)
	}
}
