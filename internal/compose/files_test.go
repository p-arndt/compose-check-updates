package compose

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

func TestGetComposeFilePaths(t *testing.T) {
	t.Parallel()

	expectedPaths := []string{
		"../../tests/docker-compose.yml",
		"../../tests/folder1/compose.yml",
		"../../tests/folder1/compose.yaml",
		"../../tests/folder2/docker-compose.yml",
		"../../tests/folder2/docker-compose.yaml",
		"../../tests/keycloak/compose.yaml",
		"../../tests/sample1/docker-compose.yml",
		"../../tests/sample2/compose.yml",
	}

	result, err := Files("../../tests", []string{})
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(result) != len(expectedPaths) {
		t.Errorf("Files() = %v, want %v", result, expectedPaths)
	}

	// Sort both slices to ensure order does not matter
	sort.Strings(result)
	sort.Strings(expectedPaths)

	for i, path := range result {
		if filepath.Clean(path) != filepath.Clean(expectedPaths[i]) {
			t.Errorf("Files() = %v, want %v", result, expectedPaths)
		}

		if filepath.ToSlash(filepath.Clean(path)) != filepath.ToSlash(filepath.Clean(expectedPaths[i])) {
			t.Errorf("Files() = %v, want %v", result, expectedPaths)
		}
	}
}

func TestGetComposeFilePathsWithExclude(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "ccu-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// tmpDir/
	//   docker-compose.yml
	//   subdir/
	//     docker-compose.yml
	//   excluded/
	//     docker-compose.yml
	//   another_excluded/
	//     docker-compose.yml

	if err := os.WriteFile(filepath.Join(tmpDir, "docker-compose.yml"), []byte("version: '3'"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	subdir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "docker-compose.yml"), []byte("version: '3'"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	excluded := filepath.Join(tmpDir, "excluded")
	if err := os.MkdirAll(excluded, 0755); err != nil {
		t.Fatalf("Failed to create excluded dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(excluded, "docker-compose.yml"), []byte("version: '3'"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	anotherExcluded := filepath.Join(tmpDir, "another_excluded")
	if err := os.MkdirAll(anotherExcluded, 0755); err != nil {
		t.Fatalf("Failed to create another_excluded dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(anotherExcluded, "docker-compose.yml"), []byte("version: '3'"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Test 1: No exclusions - should find all 4 files
	result, err := Files(tmpDir, []string{})
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(result) != 4 {
		t.Errorf("Files() = %v, want 4 files", len(result))
	}

	// Test 2: Exclude 'excluded' directory - should find 3 files
	result, err = Files(tmpDir, []string{"excluded"})
	if err != nil {
		t.Fatalf("GetComposeFilePathsWithExclude() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("GetComposeFilePathsWithExclude(['excluded']) = %v, want 3 files", len(result))
	}

	excludedFile := filepath.Join(excluded, "docker-compose.yml")
	found := false
	for _, path := range result {
		if filepath.Clean(path) == filepath.Clean(excludedFile) {
			found = true
			break
		}
	}
	if found {
		t.Errorf("Excluded file %s was found in results", excludedFile)
	}

	// Test 3: Exclude multiple directories - should find 2 files
	result, err = Files(tmpDir, []string{"excluded", "another_excluded"})
	if err != nil {
		t.Fatalf("GetComposeFilePathsWithMultipleExclude() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("GetComposeFilePathsWithMultipleExclude() = %v, want 2 files", len(result))
	}

	// Test 4: Exclude with relative paths - should work the same
	result, err = Files(tmpDir, []string{"excluded"})
	if err != nil {
		t.Fatalf("GetComposeFilePathsWithRelativeExclude() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("GetComposeFilePathsWithRelativeExclude() = %v, want 3 files", len(result))
	}
}

func TestGetComposeFilePathsIgnoresPermissionDenied(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping permission denied test on Windows")
	}

	tmpDir := t.TempDir()

	subdir := filepath.Join(tmpDir, "restricted")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "docker-compose.yml"), []byte("version: '3'"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Restrict access to the subdir, simulating a permission denied error while walking
	if err := os.Chmod(subdir, 0000); err != nil {
		t.Fatalf("Failed to chmod restricted dir: %v", err)
	}
	defer func() {
		_ = os.Chmod(subdir, 0755)
	}()

	_, err := Files(tmpDir, []string{})
	if err != nil {
		t.Fatalf("Files() should ignore permission errors, got %v", err)
	}
}

// TestGetComposeFilePathsExcludesByName covers the entry a config file is
// written for: a bare name, meant to hold wherever that directory turns up,
// rather than only at the root the scan started from.
func TestGetComposeFilePathsExcludesByName(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	dirs := []string{".", "keep", filepath.Join("keep", "backup"), "backup"}
	for _, dir := range dirs {
		full := filepath.Join(tmpDir, dir)
		if err := os.MkdirAll(full, 0755); err != nil {
			t.Fatalf("Failed to create %s: %v", full, err)
		}
		if err := os.WriteFile(filepath.Join(full, "docker-compose.yml"), []byte("version: '3'"), 0644); err != nil {
			t.Fatalf("Failed to create file in %s: %v", full, err)
		}
	}

	result, err := Files(tmpDir, []string{"backup"})
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}

	expected := []string{
		filepath.Join(tmpDir, "docker-compose.yml"),
		filepath.Join(tmpDir, "keep", "docker-compose.yml"),
	}
	if len(result) != len(expected) {
		t.Fatalf("Files() = %v, want %v", result, expected)
	}

	sort.Strings(result)
	sort.Strings(expected)
	for i := range result {
		if filepath.Clean(result[i]) != filepath.Clean(expected[i]) {
			t.Errorf("Files() = %v, want %v", result, expected)
		}
	}
}

// TestGetComposeFilePathsExcludesAbsolutePath covers the entry a global config
// would use to name a location outright, including the case that makes it easy
// to get wrong: the scan root was reached through a symlinked parent, so the
// paths the walk produces are a different spelling of the ones the entry names.
func TestGetComposeFilePathsExcludesAbsolutePath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink test on Windows")
	}

	parent := t.TempDir()
	real := filepath.Join(parent, "stacks")

	for _, dir := range []string{"keep", "skipme"} {
		full := filepath.Join(real, dir)
		if err := os.MkdirAll(full, 0755); err != nil {
			t.Fatalf("Failed to create %s: %v", full, err)
		}
		if err := os.WriteFile(filepath.Join(full, "docker-compose.yml"), []byte("version: '3'"), 0644); err != nil {
			t.Fatalf("Failed to create file in %s: %v", full, err)
		}
	}

	excluded := filepath.Join(real, "skipme")

	// Walk does not follow a symlink that *is* the root, so the link goes one
	// level up: that is the shape of /tmp -> /private/tmp on macOS, where the
	// spelling mismatch actually bites.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(parent, link); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Once through the real path, once through the symlinked parent: the same
	// entry has to exclude the same directory either way.
	for _, root := range []string{real, filepath.Join(link, "stacks")} {
		result, err := Files(root, []string{excluded})
		if err != nil {
			t.Fatalf("Files(%q) error = %v", root, err)
		}
		if len(result) != 1 {
			t.Fatalf("Files(%q) = %v, want only the kept file", root, result)
		}
		if filepath.Base(filepath.Dir(result[0])) != "keep" {
			t.Errorf("Files(%q) = %v, want the file under keep/", root, result)
		}
	}
}
