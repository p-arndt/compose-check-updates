package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/check"
)

// The sidebar is the only place a link fits, and it is worth the line: what a
// release changed is the question the list itself cannot answer.
func TestSidebarShowsTheReleaseLink(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m.height = 40
	row := m.currentRow()
	require.NotNil(t, row)
	row.Update.SourceURL = "https://github.com/traefik/traefik"

	view := plainText(m.View())

	// Without the scheme, which is the same on every link and only costs columns,
	// and cut back to the repository because the whole release link does not fit
	// a column this narrow.
	assert.Contains(t, view, "github.com/traefik/traefik")
	assert.NotContains(t, view, "https://")
}

func TestFitLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		link     string
		width    int
		expected string
	}{
		{
			name:     "the whole link where there is room",
			link:     "https://github.com/owner/repo/releases/tag/v1",
			width:    60,
			expected: "github.com/owner/repo/releases/tag/v1",
		},
		{
			name:     "the repository where there is not",
			link:     "https://github.com/owner/repo/releases/tag/v1",
			width:    30,
			expected: "github.com/owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, strings.TrimSpace(fitLink(tt.link, tt.width)))
		})
	}
}

// A forge with no release page ccu can name still tells the user where the image
// comes from, which is more than the row says.
func TestSidebarFallsBackToTheSourceLink(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m.height = 40
	row := m.currentRow()
	require.NotNil(t, row)
	row.Update.SourceURL = "https://git.example.com/team/app"

	view := plainText(m.View())

	assert.Contains(t, view, "git.example.com/team/app")
}

// An image recording no source must not leave a stray blank line behind.
func TestSidebarWithoutASourceShowsNoLink(t *testing.T) {
	t.Parallel()

	m, _ := sidebarModel(t)
	m.height = 40

	assert.Empty(t, notesLink(m.currentRow().Update))
}

func TestNotesLinkPrefersTheRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		update   check.Update
		expected string
	}{
		{
			name:     "release page when the forge has one",
			update:   check.Update{LatestTag: "v1.2.3", SourceURL: "https://github.com/owner/repo"},
			expected: "https://github.com/owner/repo/releases/tag/v1.2.3",
		},
		{
			name:     "the repository otherwise",
			update:   check.Update{LatestTag: "v1.2.3", SourceURL: "https://git.example.com/owner/repo"},
			expected: "https://git.example.com/owner/repo",
		},
		{
			name:     "nothing at all without a source",
			update:   check.Update{LatestTag: "v1.2.3"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, notesLink(tt.update))
		})
	}
}
