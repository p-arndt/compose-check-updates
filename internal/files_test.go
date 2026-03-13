package internal

import (
	"path/filepath"
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

	result, err := GetComposeFilePaths("../tests")
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
