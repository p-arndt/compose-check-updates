package internal

import "testing"

// TestExcludeMatcher covers the three ways an entry can be written, and the
// property that matters most for the walk: an excluded directory takes
// everything below it with it.
func TestExcludeMatcher(t *testing.T) {
	tests := []struct {
		name    string
		exclude []string
		relPath string
		want    bool
	}{
		{name: "no entries matches nothing", relPath: "services/api"},

		// A bare name is the config case: written once, meant to hold wherever
		// the directory turns up.
		{name: "bare name at the root", exclude: []string{"backup"}, relPath: "backup", want: true},
		{name: "bare name nested", exclude: []string{"backup"}, relPath: "srv/app/backup", want: true},
		{name: "bare name covers children", exclude: []string{"backup"}, relPath: "srv/backup/docker-compose.yml", want: true},
		{name: "bare name is not a substring match", exclude: []string{"backup"}, relPath: "backups"},

		// A rooted entry is the -exclude case, anchored at the scan root.
		{name: "rooted path", exclude: []string{"services/legacy"}, relPath: "services/legacy", want: true},
		{name: "rooted path covers children", exclude: []string{"services/legacy"}, relPath: "services/legacy/compose.yml", want: true},
		{name: "rooted path elsewhere", exclude: []string{"services/legacy"}, relPath: "other/services/legacy"},

		// Spelling variants of the same directory must not be told apart.
		{name: "trailing slash", exclude: []string{"backup/"}, relPath: "backup", want: true},
		{name: "leading dot slash", exclude: []string{"./services/legacy"}, relPath: "services/legacy", want: true},

		{name: "wildcard name", exclude: []string{"*.bak"}, relPath: "srv/old.bak/compose.yml", want: true},
		{name: "wildcard path", exclude: []string{"services/*-old"}, relPath: "services/api-old", want: true},

		// The root itself is never excluded; an entry that appeared to say so
		// would silence the whole scan.
		{name: "root", exclude: []string{"."}, relPath: "."},

		{name: "blank entries are ignored", exclude: []string{"", "   "}, relPath: "services/api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewExcludeMatcher(tt.exclude)
			if got := m.Match(tt.relPath, ""); got != tt.want {
				t.Errorf("Match(%q) with exclude %q = %v, expected %v", tt.relPath, tt.exclude, got, tt.want)
			}
		})
	}
}

// TestExcludeMatcherAbsolute checks the entry a global config would use to name
// a location that has nothing to do with the current scan root.
func TestExcludeMatcherAbsolute(t *testing.T) {
	m := NewExcludeMatcher([]string{"/mnt/backups"})

	if !m.Match("data", "/mnt/backups/data") {
		t.Error("expected a path below the absolute entry to be excluded")
	}
	if !m.Match("backups", "/mnt/backups") {
		t.Error("expected the absolute entry itself to be excluded")
	}
	if m.Match("backups2", "/mnt/backups2") {
		t.Error("expected an absolute entry not to match by prefix of a name")
	}
	// Without an absolute path to compare against there is nothing to match, and
	// guessing one from the relative path would exclude the wrong tree.
	if m.Match("mnt/backups", "") {
		t.Error("expected no match when the caller supplied no absolute path")
	}
}

func TestExcludeMatcherEmpty(t *testing.T) {
	if !NewExcludeMatcher(nil).Empty() {
		t.Error("expected a matcher with no entries to be empty")
	}
	if !NewExcludeMatcher([]string{"", " ", "."}).Empty() {
		t.Error("expected entries that name nothing to leave the matcher empty")
	}
	if NewExcludeMatcher([]string{"backup"}).Empty() {
		t.Error("expected a matcher with an entry not to be empty")
	}
}
