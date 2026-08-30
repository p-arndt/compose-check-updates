package check

import (
	"github.com/p-arndt/compose-check-updates/internal/policy"
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
		versioning policy.Versioning
		expected   bool
	}{
		{"No new version", "1.0.0", "1.0.0", "", false},
		{"New patch version", "1.0.0", "1.0.1", "", true},
		{"New minor version", "1.0.0", "1.1.0", "", true},
		{"New major version", "1.0.0", "2.0.0", "", true},
		{"With suffix", "1.0.0-rc1", "1.0.0-rc2", "", true},
		{"With suffix, no new version", "1.0.0-rc1", "1.0.0-rc1", "", false},
		{"Invalid current tag", "", "1.0.0", "", false},
		{"Invalid latest tag", "1.0.0", "", "", false},
		// The default scheme cannot read a fourth segment at all, so the update
		// it would describe is not one it can confirm.
		{"Calendar tag under semver", "v2026.7.7", "v2026.7.7.2", "", false},
		{"Calendar tag under loose", "v2026.7.7", "v2026.7.7.2", policy.VersioningLoose, true},
		{"Calendar tag under loose, across months", "v2026.7.7.2", "v2026.8.27", policy.VersioningLoose, true},
		{"Calendar tag under loose, no new version", "v2026.8.27", "v2026.8.27", policy.VersioningLoose, false},
		{"Calendar tag under loose, backwards", "v2026.7.7.2", "v2026.7.7", policy.VersioningLoose, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &Update{
				CurrentTag: tt.currentTag,
				LatestTag:  tt.latestTag,
				Versioning: tt.versioning,
			}
			if got := u.HasNewVersion(); got != tt.expected {
				t.Errorf("HasNewVersion() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTagForTarget(t *testing.T) {
	tests := []struct {
		name   string
		info   Update
		target policy.Level
		want   string
	}{
		{"major picks the major tag", Update{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}, "major", "3.7.8"},
		{"minor stays in the major", Update{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}, "minor", "2.11.3"},
		{"patch stays in the minor", Update{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}, "patch", "2.9.4"},
		{"major falls back to minor", Update{PatchTag: "2.9.4", MinorTag: "2.11.3"}, "major", "2.11.3"},
		{"major falls back to patch", Update{PatchTag: "2.9.4"}, "major", "2.9.4"},
		{"minor falls back to patch", Update{PatchTag: "2.9.4"}, "minor", "2.9.4"},
		{"patch never falls up", Update{MinorTag: "2.11.3", MajorTag: "3.7.8"}, "patch", ""},
		{"minor never falls up", Update{MajorTag: "3.7.8"}, "minor", ""},
		{"nothing available", Update{}, "major", ""},
		{"unknown target", Update{PatchTag: "2.9.4"}, "digest", ""},
		{
			"digest-only update returns the latest tag",
			Update{LatestTag: "abc123", CurrentDigest: "sha256:old", LatestDigest: "sha256:new"},
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
		info Update
		want []policy.Level
	}{
		{"all levels in order", Update{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}, []policy.Level{"patch", "minor", "major"}},
		{"missing levels are dropped", Update{MajorTag: "3.7.8"}, []policy.Level{"major"}},
		{"identical tags are not offered twice", Update{MinorTag: "2.11.3", MajorTag: "2.11.3"}, []policy.Level{"minor"}},
		{"nothing available", Update{}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.info.AvailableTargets())
		})
	}
}

func TestSelectTarget(t *testing.T) {
	t.Run("changes the tag", func(t *testing.T) {
		u := Update{LatestTag: "3.7.8", PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}
		assert.True(t, u.SelectTarget("patch"))
		assert.Equal(t, "2.9.4", u.LatestTag)
	})

	t.Run("reports no change when already selected", func(t *testing.T) {
		u := Update{LatestTag: "3.7.8", MajorTag: "3.7.8"}
		assert.False(t, u.SelectTarget("major"))
		assert.Equal(t, "3.7.8", u.LatestTag)
	})

	t.Run("keeps the selection when the level is empty", func(t *testing.T) {
		u := Update{LatestTag: "3.7.8", MajorTag: "3.7.8"}
		assert.False(t, u.SelectTarget("patch"))
		assert.Equal(t, "3.7.8", u.LatestTag)
	})

	t.Run("clears a digest resolved for another tag", func(t *testing.T) {
		u := Update{
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
		u := Update{
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
		u := Update{
			FilePath: path, RawLine: line,
			CurrentTag: "1.0.0", LatestTag: "2.0.0",
			CurrentDigest: "sha256:old",
		}

		assert.Error(t, u.Apply())

		content, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, line, string(content))
	})

	t.Run("stale digest", func(t *testing.T) {
		path := writeComposeFile(t, line)
		u := Update{
			FilePath: path, RawLine: line,
			CurrentTag: "1.0.0", LatestTag: "2.0.0",
			CurrentDigest: "sha256:old", LatestDigest: "sha256:for-1-1-0",
			digestFor: "1.1.0",
		}

		assert.Error(t, u.Apply())

		content, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, line, string(content))
	})

	t.Run("matching digest is written", func(t *testing.T) {
		path := writeComposeFile(t, line)
		u := Update{
			FilePath: path, RawLine: line,
			CurrentTag: "1.0.0", LatestTag: "2.0.0",
			CurrentDigest: "sha256:old", LatestDigest: "sha256:new",
			digestFor: "2.0.0",
		}

		assert.NoError(t, u.Apply())

		content, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, "image: myapp:2.0.0@sha256:new", string(content))
	})
}

// TestResolveDigestNoop guards the cheap exits: references without a digest have
// nothing to rewrite, so no registry call may happen.
func TestResolveDigestNoop(t *testing.T) {
	u := Update{LatestTag: "2.0.0"}
	assert.NoError(t, u.ResolveDigest(nil))
	assert.Empty(t, u.LatestDigest)

	resolved := Update{
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

	infos := []Update{
		{FilePath: path, RawLine: "image: myapp:1.0.0", CurrentTag: "1.0.0", LatestTag: "1.1.0"},
		{FilePath: path, RawLine: "image: other:2.0.0", CurrentTag: "2.0.0", LatestTag: "2.1.0"},
		{FilePath: path, RawLine: "image: third:3.0.0", CurrentTag: "3.0.0", LatestTag: "3.1.0"},
	}

	var wg sync.WaitGroup
	for _, info := range infos {
		wg.Add(1)
		go func(info Update) {
			defer wg.Done()
			if err := info.Apply(); err != nil {
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
	tmpFile, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := []byte("test content")
	if _, err := tmpFile.Write(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	u := &Update{FilePath: tmpFile.Name()}
	if err := u.Backup(); err != nil {
		t.Errorf("Backup() error = %v", err)
	}

	if _, err := os.Stat(tmpFile.Name() + ".ccu"); os.IsNotExist(err) {
		t.Errorf("Backup file does not exist")
	}
}

func TestUpdate(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := []byte("image: myapp:1.0.0")
	if _, err := tmpFile.Write(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	u := &Update{
		FilePath:   tmpFile.Name(),
		RawLine:    "image: myapp:1.0.0",
		CurrentTag: "1.0.0",
		LatestTag:  "1.1.0",
	}

	if err := u.Apply(); err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updatedContent, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	expectedContent := "image: myapp:1.1.0"
	if string(updatedContent) != expectedContent {
		t.Errorf("Update() = %v, want %v", string(updatedContent), expectedContent)
	}
}

// TestCap covers the per-image cap: the user recorded how far this image may
// move, and every question about a target has to respect that.
func TestCap(t *testing.T) {
	t.Run("hides levels above the cap", func(t *testing.T) {
		u := Update{Cap: "minor", PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}
		assert.Equal(t, []policy.Level{"patch", "minor"}, u.AvailableTargets())
	})

	t.Run("degrades a request above the cap", func(t *testing.T) {
		u := Update{Cap: "minor", PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}
		assert.Equal(t, "2.11.3", u.TagForTarget("major"))
		assert.Equal(t, u.TagForTarget("minor"), u.TagForTarget("major"))
	})

	t.Run("SelectTarget cannot select above the cap", func(t *testing.T) {
		u := Update{Cap: "minor", LatestTag: "3.7.8", PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}
		assert.True(t, u.SelectTarget("major"))
		assert.Equal(t, "2.11.3", u.LatestTag)
	})

	t.Run("an update above the cap is not a new version", func(t *testing.T) {
		u := Update{Cap: "patch", CurrentTag: "1.0.0", LatestTag: "2.0.0", MajorTag: "2.0.0"}
		assert.False(t, u.HasNewVersion())
	})

	t.Run("an update within the cap still counts", func(t *testing.T) {
		u := Update{Cap: "minor", CurrentTag: "1.0.0", LatestTag: "1.1.0", MinorTag: "1.1.0"}
		assert.True(t, u.HasNewVersion())
	})

	t.Run("a cap has no say over a digest update", func(t *testing.T) {
		u := Update{
			Cap: "patch", CurrentTag: "stable", LatestTag: "stable",
			CurrentDigest: "sha256:old", LatestDigest: "sha256:new",
		}
		assert.True(t, u.HasNewVersion())
		assert.Equal(t, "stable", u.TagForTarget("major"))
	})

	t.Run("an empty cap changes nothing", func(t *testing.T) {
		u := Update{PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}
		assert.Equal(t, []policy.Level{"patch", "minor", "major"}, u.AvailableTargets())
		assert.Equal(t, "3.7.8", u.TagForTarget("major"))

		v := Update{CurrentTag: "1.0.0", LatestTag: "2.0.0"}
		assert.True(t, v.HasNewVersion())
	})

	t.Run("an unrecognised cap permits everything", func(t *testing.T) {
		u := Update{Cap: "nonsense", PatchTag: "2.9.4", MinorTag: "2.11.3", MajorTag: "3.7.8"}
		assert.Equal(t, []policy.Level{"patch", "minor", "major"}, u.AvailableTargets())
		assert.Equal(t, "3.7.8", u.TagForTarget("major"))

		v := Update{Cap: "nonsense", CurrentTag: "1.0.0", LatestTag: "2.0.0"}
		assert.True(t, v.HasNewVersion())
	})
}

// TestUpdateLevelVersioning pins down the level a fourth segment is reported
// at. It is a patch: "2026.7.7.2" rebuilds the release "2026.7.7" names rather
// than being a release of its own, so it stays inside the three levels the caps
// and the filters already speak.
func TestUpdateLevelVersioning(t *testing.T) {
	tests := []struct {
		name       string
		currentTag string
		latestTag  string
		versioning policy.Versioning
		want       policy.Level
	}{
		{"fourth segment appears", "v2026.7.7", "v2026.7.7.2", policy.VersioningLoose, "patch"},
		{"fourth segment advances", "v2026.7.7.1", "v2026.7.7.2", policy.VersioningLoose, "patch"},
		{"fourth segment disappears", "v2026.7.7.2", "v2026.7.30", policy.VersioningLoose, "patch"},
		{"month moves", "v2026.7.7.2", "v2026.8.16.2", policy.VersioningLoose, "minor"},
		{"year moves", "v2026.8.27", "v2027.1.5", policy.VersioningLoose, "major"},
		// Unreadable under the default scheme, and with no digest to fall back
		// on there is no level to report.
		{"fourth segment under semver", "v2026.7.7", "v2026.7.7.2", "", ""},
		{"ordinary semver is unaffected", "1.2.3", "1.3.0", "", "minor"},
		// The config layer rejects these on load; one that got this far must not
		// hide every update, so it falls back rather than failing.
		{"unknown scheme falls back to the default", "1.2.3", "1.2.4", "calendar", "patch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &Update{
				CurrentTag: tt.currentTag,
				LatestTag:  tt.latestTag,
				Versioning: tt.versioning,
			}
			assert.Equal(t, tt.want, u.Level())
		})
	}
}
