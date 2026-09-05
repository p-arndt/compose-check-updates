package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadEnvReadsTheAssignmentsComposeWould(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	compose := filepath.Join(dir, "docker-compose.yml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"# a comment\n"+
			"PLAIN=1.2.3\n"+
			"export EXPORTED=2.0.0\n"+
			"SPACED =  3.0.0\n"+
			`QUOTED="4.0.0"`+"\n"+
			"SINGLE='5.0.0'\n"+
			"COMMENTED=6.0.0 # why\n"+
			"not an assignment\n"), 0644))

	env, path := readEnv(compose)

	assert.Equal(t, filepath.Join(dir, ".env"), path)
	assert.Equal(t, "1.2.3", env["PLAIN"].Value)
	assert.Equal(t, "2.0.0", env["EXPORTED"].Value)
	assert.Equal(t, "3.0.0", env["SPACED"].Value)
	assert.Equal(t, "4.0.0", env["QUOTED"].Value)
	assert.Equal(t, "5.0.0", env["SINGLE"].Value)
	assert.Equal(t, "6.0.0", env["COMMENTED"].Value)
	assert.Len(t, env, 6)
}

// The recorded range is what a rewrite writes into, so it has to bound the value
// alone: quotes and a trailing comment must survive a new version.
func TestReadEnvBoundsTheValueInsideItsLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	compose := filepath.Join(dir, "docker-compose.yml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(
		`QUOTED="1.2.3" # pinned`+"\n"), 0644))

	env, _ := readEnv(compose)

	entry := env["QUOTED"]
	require.Equal(t, "1.2.3", entry.Value)
	assert.Equal(t, entry.Value, entry.Line[entry.ValueStart:entry.ValueEnd])
	assert.Equal(t, `QUOTED="2.0.0" # pinned`, entry.Line[:entry.ValueStart]+"2.0.0"+entry.Line[entry.ValueEnd:])
}

// Compose reads the last assignment of a variable, and the line number is what
// tells the two apart when their text is identical.
func TestReadEnvKeepsTheLastAssignment(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	compose := filepath.Join(dir, "docker-compose.yml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("TAG=1.0.0\nTAG=2.0.0\n"), 0644))

	env, _ := readEnv(compose)

	assert.Equal(t, "2.0.0", env["TAG"].Value)
	assert.Equal(t, 2, env["TAG"].LineNo)
}

// Most stacks have no .env at all, which is not a reason to stop checking the
// images whose tags are written out in full.
func TestReadEnvIsEmptyWithoutAFile(t *testing.T) {
	t.Parallel()

	env, path := readEnv(filepath.Join(t.TempDir(), "docker-compose.yml"))

	assert.Empty(t, env)
	assert.Empty(t, path)
}
