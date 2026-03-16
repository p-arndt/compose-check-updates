package internal

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

func TestGetComposeFilePaths(t *testing.T) {
	expectedPaths := []string{
		"../tests/docker-compose.yml",
		"../tests/folder1/compose.yml",
		"../tests/folder1/compose.yaml",
		"../tests/folder2/docker-compose.yml",
		"../tests/folder2/docker-compose.yaml",
		"../tests/sample1/docker-compose.yml",
		"../tests/sample2/compose.yml",
	}

	result, err := GetComposeFilePaths("../tests", []string{})
	if err != nil {
		t.Fatalf("GetComposeFilePaths() error = %v", err)
	}
	if len(result) != len(expectedPaths) {
		t.Errorf("GetComposeFilePaths() = %v, want %v", result, expectedPaths)
	}

	// Sort both slices to ensure order does not matter
	sort.Strings(result)
	sort.Strings(expectedPaths)

	for i, path := range result {
		if filepath.Clean(path) != filepath.Clean(expectedPaths[i]) {
			t.Errorf("GetComposeFilePaths() = %v, want %v", result, expectedPaths)
		}

		if filepath.ToSlash(filepath.Clean(path)) != filepath.ToSlash(filepath.Clean(expectedPaths[i])) {
			t.Errorf("GetComposeFilePaths() = %v, want %v", result, expectedPaths)
		}
	}
}

func TestGetComposeFilePathsWithExclude(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir, err := os.MkdirTemp("", "ccu-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test structure
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
	result, err := GetComposeFilePaths(tmpDir, []string{})
	if err != nil {
		t.Fatalf("GetComposeFilePaths() error = %v", err)
	}
	if len(result) != 4 {
		t.Errorf("GetComposeFilePaths() = %v, want 4 files", len(result))
	}

	// Test 2: Exclude 'excluded' directory - should find 3 files
	result, err = GetComposeFilePaths(tmpDir, []string{"excluded"})
	if err != nil {
		t.Fatalf("GetComposeFilePathsWithExclude() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("GetComposeFilePathsWithExclude(['excluded']) = %v, want 3 files", len(result))
	}

	// Verify the excluded file is not in the results
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
	result, err = GetComposeFilePaths(tmpDir, []string{"excluded", "another_excluded"})
	if err != nil {
		t.Fatalf("GetComposeFilePathsWithMultipleExclude() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("GetComposeFilePathsWithMultipleExclude() = %v, want 2 files", len(result))
	}

	// Test 4: Exclude with relative paths - should work the same
	result, err = GetComposeFilePaths(tmpDir, []string{"excluded"})
	if err != nil {
		t.Fatalf("GetComposeFilePathsWithRelativeExclude() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("GetComposeFilePathsWithRelativeExclude() = %v, want 3 files", len(result))
	}
}

func TestGetComposeFilePathsIgnoresPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping permission denied test on Windows")
	}

	tmpDir := t.TempDir()

	subdir := filepath.Join(tmpDir, "restricted")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Create a compose file that should be found when permissions are normal
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

	_, err := GetComposeFilePaths(tmpDir, []string{})
	if err != nil {
		t.Fatalf("GetComposeFilePaths() should ignore permission errors, got %v", err)
	}
}
