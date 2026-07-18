package internal

import (
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestTagForTarget(t *testing.T) {
	tests := []struct {
		name   string
		info   UpdateInfo
		target string
		want   string
	}{
		{"major picks the major tag", UpdateInfo{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}, "major", "3.7.8"},
		{"minor stays in the major", UpdateInfo{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}, "minor", "2.11.3"},
		{"patch stays in the minor", UpdateInfo{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}, "patch", "2.9.4"},
		{"major falls back to minor", UpdateInfo{PatchTag: "2.9.4", MinorTag: "2.11.3"}, "major", "2.11.3"},
		{"major falls back to patch", UpdateInfo{PatchTag: "2.9.4"}, "major", "2.9.4"},
		{"minor falls back to patch", UpdateInfo{PatchTag: "2.9.4"}, "minor", "2.9.4"},
		{"patch never falls up", UpdateInfo{MinorTag: "2.11.3", MajorTag: "3.7.8"}, "patch", ""},
		{"minor never falls up", UpdateInfo{MajorTag: "3.7.8"}, "minor", ""},
		{"nothing available", UpdateInfo{}, "major", ""},
		{"unknown target", UpdateInfo{PatchTag: "2.9.4"}, "digest", ""},
		{
			"digest-only update returns the latest tag",
			UpdateInfo{LatestTag: "abc123", CurrentDigest: "sha256:old", LatestDigest: "sha256:new"},
			"major", "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.info.TagForTarget(tt.target))
		})
	}
}

func TestAvailableTargets(t *testing.T) {
	tests := []struct {
		name string
		info UpdateInfo
		want []string
	}{
		{"all levels in order", UpdateInfo{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}, []string{"patch", "minor", "major"}},
		{"missing levels are dropped", UpdateInfo{MajorTag: "3.7.8"}, []string{"major"}},
		{"identical tags are not offered twice", UpdateInfo{MinorTag: "2.11.3", MajorTag: "2.11.3"}, []string{"minor"}},
		{"nothing available", UpdateInfo{}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.info.AvailableTargets())
		})
	}
}

func TestSelectTarget(t *testing.T) {
	t.Run("changes the tag", func(t *testing.T) {
		u := UpdateInfo{LatestTag: "3.7.8", PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}
		assert.True(t, u.SelectTarget("patch"))
		assert.Equal(t, "2.9.4", u.LatestTag)
	})

	t.Run("reports no change when already selected", func(t *testing.T) {
		u := UpdateInfo{LatestTag: "3.7.8", MajorTag: "3.7.8"}
		assert.False(t, u.SelectTarget("major"))
		assert.Equal(t, "3.7.8", u.LatestTag)
	})

	t.Run("keeps the selection when the level is empty", func(t *testing.T) {
		u := UpdateInfo{LatestTag: "3.7.8", MajorTag: "3.7.8"}
		assert.False(t, u.SelectTarget("patch"))
		assert.Equal(t, "3.7.8", u.LatestTag)
	})

	t.Run("clears a digest resolved for another tag", func(t *testing.T) {
		u := UpdateInfo{
			LatestTag:     "3.7.8",
			PatchTag:      "2.9.4",
			MajorTag:      "3.7.8",
			CurrentDigest: "sha256:old",
			LatestDigest:  "sha256:major",
			digestFor:     "3.7.8",
		}

		assert.True(t, u.SelectTarget("patch"))
		assert.Equal(t, "2.9.4", u.LatestTag)
		assert.Empty(t, u.LatestDigest, "digest of the major release must not survive")
	})

	t.Run("keeps a digest that still matches", func(t *testing.T) {
		u := UpdateInfo{
			LatestTag:     "3.7.8",
			MajorTag:      "3.7.8",
			CurrentDigest: "sha256:old",
			LatestDigest:  "sha256:major",
			digestFor:     "3.7.8",
		}

		assert.False(t, u.SelectTarget("major"))
		assert.Equal(t, "sha256:major", u.LatestDigest)
	})
}

// TestUpdateRefusesDigestMismatch covers the case the guard exists for: a target
// switch left the digest behind, and writing it would pin the wrong image.
func TestUpdateRefusesDigestMismatch(t *testing.T) {
	line := "image: myapp:1.0.0@sha256:old"

	t.Run("missing digest", func(t *testing.T) {
		path := writeComposeFile(t, line)
		u := UpdateInfo{
			FilePath: path, RawLine: line,
			CurrentTag: "1.0.0", LatestTag: "2.0.0",
			CurrentDigest: "sha256:old",
		}

		assert.Error(t, u.Update())

		content, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, line, string(content))
	})

	t.Run("stale digest", func(t *testing.T) {
		path := writeComposeFile(t, line)
		u := UpdateInfo{
			FilePath: path, RawLine: line,
			CurrentTag: "1.0.0", LatestTag: "2.0.0",
			CurrentDigest: "sha256:old", LatestDigest: "sha256:for-1-1-0",
			digestFor: "1.1.0",
		}

		assert.Error(t, u.Update())

		content, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, line, string(content))
	})

	t.Run("matching digest is written", func(t *testing.T) {
		path := writeComposeFile(t, line)
		u := UpdateInfo{
			FilePath: path, RawLine: line,
			CurrentTag: "1.0.0", LatestTag: "2.0.0",
			CurrentDigest: "sha256:old", LatestDigest: "sha256:new",
			digestFor: "2.0.0",
		}

		assert.NoError(t, u.Update())

		content, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, "image: myapp:2.0.0@sha256:new", string(content))
	})
}

// TestResolveDigestNoop guards the cheap exits: references without a digest have
// nothing to rewrite, so no registry call may happen.
func TestResolveDigestNoop(t *testing.T) {
	u := UpdateInfo{LatestTag: "2.0.0"}
	assert.NoError(t, u.ResolveDigest(nil))
	assert.Empty(t, u.LatestDigest)

	resolved := UpdateInfo{
		LatestTag: "2.0.0", CurrentDigest: "sha256:old",
		LatestDigest: "sha256:new", digestFor: "2.0.0",
	}
	assert.NoError(t, resolved.ResolveDigest(nil))
	assert.Equal(t, "sha256:new", resolved.LatestDigest)
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
