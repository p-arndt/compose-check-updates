package tui

import (
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/check"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

// dockerfileEvent is the update a self-built service produces: the row is written
// to the Dockerfile, while the stack it belongs to is the compose file next to it.
func dockerfileEvent() scanEventMsg {
	return scanEventMsg{ev: scanner.Event{
		Kind:  scanner.EventUpdate,
		Path:  "tests/keycloak/compose.yaml",
		Level: "minor",
		Update: check.Update{
			FilePath:      "tests/keycloak/Dockerfile",
			ComposePath:   "tests/keycloak/compose.yaml",
			Services:      []string{"keycloak"},
			ImageName:     "quay.io/keycloak/keycloak",
			FullImageName: "quay.io/keycloak/keycloak:26.0.7",
			RawLine:       "FROM quay.io/keycloak/keycloak:26.0.7 AS builder",
			ExtraLines:    []string{"FROM quay.io/keycloak/keycloak:26.0.7"},
			CurrentTag:    "26.0.7",
			LatestTag:     "26.7.2",
			PatchTag:      "26.0.9",
			MinorTag:      "26.7.2",
		},
	}}
}

func dockerfileModel(t *testing.T) Model {
	t.Helper()

	m := newTestModel()
	return feed(t, m,
		tea.WindowSizeMsg{Width: 110, Height: 24},
		updateEvent("tests/keycloak/compose.yaml", "library/postgres", "16.2", "18.6", "major"),
		dockerfileEvent(),
	)
}

// TestDockerfileRowSitsBesideItsComposeFile is what the tree does with a row
// written to a file that is not a compose file: it becomes a leaf of its own,
// under the directory header the stack already had.
func TestDockerfileRowSitsBesideItsComposeFile(t *testing.T) {
	m := dockerfileModel(t)

	var headers []string
	for _, e := range m.entries {
		if e.kind == entryHeader {
			headers = append(headers, m.groupInfo(e.node, false).Label)
		}
	}
	assert.Equal(t, []string{"tests/keycloak", "Dockerfile", "compose.yaml"}, headers)

	view := m.View()
	assert.Contains(t, view, "▾ Dockerfile  (1 update, 0 selected)")
	assert.Contains(t, view, "quay.io/keycloak/keycloak")
	assert.Contains(t, view, "26.0.7 → 26.7.2")
}

// TestDockerfileSidebarNamesTheFile guards the one thing the row itself cannot
// say: which file the update will be written to.
func TestDockerfileSidebarNamesTheFile(t *testing.T) {
	m := dockerfileModel(t)
	m = feed(t, m, keyMsg("k")) // onto the Dockerfile row

	row := m.currentRow()
	assert.NotNil(t, row)
	assert.True(t, row.Update.IsDockerfile())
	assert.Contains(t, m.View(), "keycloak/Dockerfile")
}

// TestAffectedFilesRestartsTheStackOnce covers the restart the two rows share: a
// stack whose compose file and Dockerfile both changed is one `up`, and it has to
// be the rebuilding one.
func TestAffectedFilesRestartsTheStackOnce(t *testing.T) {
	m := dockerfileModel(t)
	for i := range m.rows {
		m.rows[i].State = RowApplied
	}

	affected := m.affectedFiles()

	assert.Len(t, affected, 1)
	assert.True(t, affected[0].IsDockerfile())
	assert.Equal(t, "tests/keycloak/compose.yaml", affected[0].RestartPath())
}

// TestDuplicateDockerfileRowIsFoldedIn covers the sibling compose files of one
// directory — `compose.yaml` and `compose.yml` — both building the same
// Dockerfile. Two rows would share a rowKey, and rowByKey would then resolve
// both to the first: one apply would write it twice and leave the other pending.
func TestDuplicateDockerfileRowIsFoldedIn(t *testing.T) {
	m := newTestModel()
	m = feed(t, m, tea.WindowSizeMsg{Width: 110, Height: 24}, dockerfileEvent())

	dup := dockerfileEvent()
	dup.ev.Path = "tests/keycloak/compose.yml"
	dup.ev.Update.ComposePath = "tests/keycloak/compose.yml"
	dup.ev.Update.Services = []string{"keycloak-staging"}
	m = feed(t, m, dup)

	assert.Len(t, m.rows, 1)
	assert.Equal(t, []string{"keycloak", "keycloak-staging"}, m.rows[0].Update.Services)
}
