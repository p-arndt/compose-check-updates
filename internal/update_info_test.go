package internal

import (
	"os"
	"sync"
	"testing"
)

func TestHasNewVersion(t *testing.T) {
	tests := []struct {
		name       string
		currentTag string
		latestTag  string
		expected   bool
	}{
		{"No new version", "1.0.0", "1.0.0", false},
		{"New patch version", "1.0.0", "1.0.1", true},
		{"New minor version", "1.0.0", "1.1.0", true},
		{"New major version", "1.0.0", "2.0.0", true},
		{"With suffix", "1.0.0-rc1", "1.0.0-rc2", true},
		{"With suffix, no new version", "1.0.0-rc1", "1.0.0-rc1", false},
		{"Invalid current tag", "", "1.0.0", false},
		{"Invalid latest tag", "1.0.0", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &UpdateInfo{
				CurrentTag: tt.currentTag,
				LatestTag:  tt.latestTag,
			}
			if got := u.HasNewVersion(true, true, true); got != tt.expected {
				t.Errorf("HasNewVersion() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestUpdateConcurrent guards against images of the same compose file
// overwriting each other's rewrite, which is how they are updated in practice.
func TestUpdateConcurrent(t *testing.T) {
	path := writeComposeFile(t, "image: myapp:1.0.0\nimage: other:2.0.0\nimage: third:3.0.0")

	infos := []UpdateInfo{
		{FilePath: path, RawLine: "image: myapp:1.0.0", CurrentTag: "1.0.0", LatestTag: "1.1.0"},
		{FilePath: path, RawLine: "image: other:2.0.0", CurrentTag: "2.0.0", LatestTag: "2.1.0"},
		{FilePath: path, RawLine: "image: third:3.0.0", CurrentTag: "3.0.0", LatestTag: "3.1.0"},
	}

	var wg sync.WaitGroup
	for _, info := range infos {
		wg.Add(1)
		go func(info UpdateInfo) {
			defer wg.Done()
			if err := info.Update(); err != nil {
				t.Errorf("Update() error = %v", err)
			}
		}(info)
	}
	wg.Wait()

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	expected := "image: myapp:1.1.0\nimage: other:2.1.0\nimage: third:3.1.0"
	if string(updated) != expected {
		t.Errorf("Update() = %q, want %q", string(updated), expected)
	}
}

func TestBackup(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	// Write some content to the file
	content := []byte("test content")
	if _, err := tmpFile.Write(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	u := &UpdateInfo{FilePath: tmpFile.Name()}
	if err := u.Backup(); err != nil {
		t.Errorf("Backup() error = %v", err)
	}

	// Check if backup file exists
	if _, err := os.Stat(tmpFile.Name() + ".ccu"); os.IsNotExist(err) {
		t.Errorf("Backup file does not exist")
	}
}

func TestUpdate(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	// Write some content to the file
	content := []byte("image: myapp:1.0.0")
	if _, err := tmpFile.Write(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	u := &UpdateInfo{
		FilePath:   tmpFile.Name(),
		RawLine:    "image: myapp:1.0.0",
		CurrentTag: "1.0.0",
		LatestTag:  "1.1.0",
	}

	if err := u.Update(); err != nil {
		t.Errorf("Update() error = %v", err)
	}

	// Check if the file content is updated
	updatedContent, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := "image: myapp:1.1.0"
	if string(updatedContent) != expectedContent {
		t.Errorf("Update() = %v, want %v", string(updatedContent), expectedContent)
	}
}
