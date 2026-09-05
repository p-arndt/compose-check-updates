package check

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/p-arndt/compose-check-updates/internal/registry"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
)

// writeStack lays out a compose file and, when envContent is non-empty, the
// .env beside it — the pair being what interpolation is about. Its own directory
// per test, because the .env is found by position rather than by name.
func writeStack(t *testing.T, composeContent, envContent string) string {
	t.Helper()

	dir := t.TempDir()
	compose := filepath.Join(dir, "docker-compose.yml")
	require.NoError(t, os.WriteFile(compose, []byte(composeContent), 0644))
	if envContent != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644))
	}
	return compose
}

// versionedServer serves one repository holding 1.2.3 and 1.2.4.
func versionedServer(t *testing.T) (image string, reg registry.Fetcher) {
	t.Helper()

	server := registrytest.Server(t, "library/myimage",
		[]string{"1.2.3", "1.2.4"},
		map[string]string{"1.2.3": registrytest.DigestOld, "1.2.4": registrytest.DigestNew})

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	return serverURL.Host + "/library/myimage", registry.New(serverURL.Host)
}

// The tag an image is checked on is the one the stack runs on, which for an
// interpolated reference is written in the .env and not in the compose file.
func TestTagFromEnvFileIsCheckedAndWrittenBackToTheEnvFile(t *testing.T) {
	image, reg := versionedServer(t)

	composeContent := "services:\n  app:\n    image: " + image + ":${APP_VERSION}\n"
	compose := writeStack(t, composeContent, "# the release we run\nAPP_VERSION=1.2.3\n")

	updates, err := New(compose, reg, policy.Set{}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 1)

	u := updates[0]
	assert.Equal(t, "1.2.3", u.CurrentTag)
	assert.Equal(t, "1.2.4", u.LatestTag)
	// The report shows the reference as the file spells it, variable and all.
	assert.Equal(t, image+":${APP_VERSION}", u.FullImageName)
	require.NotNil(t, u.TagVar)
	assert.Equal(t, "APP_VERSION", u.TagVar.Name)
	assert.Empty(t, u.TagVar.Unwritable)

	require.NoError(t, u.Apply())

	env, err := os.ReadFile(filepath.Join(filepath.Dir(compose), ".env"))
	require.NoError(t, err)
	assert.Equal(t, "# the release we run\nAPP_VERSION=1.2.4\n", string(env))

	// The compose file names the variable, not the version, so it must not move.
	after, err := os.ReadFile(compose)
	require.NoError(t, err)
	assert.Equal(t, composeContent, string(after))

	// The .env is rewritten, so it is the file that needs the backup.
	backup, err := os.ReadFile(filepath.Join(filepath.Dir(compose), ".env"+backupSuffix))
	require.NoError(t, err)
	assert.Equal(t, "# the release we run\nAPP_VERSION=1.2.3\n", string(backup))
}

// A value the reference carries itself has no .env to go to, so it is rewritten
// where it stands — as the default, not as a tag replacing the variable.
func TestTagFromInlineDefaultIsWrittenBackIntoTheImageLine(t *testing.T) {
	image, reg := versionedServer(t)

	compose := writeStack(t, "services:\n  app:\n    image: "+image+":${APP_VERSION:-1.2.3}\n", "")

	updates, err := New(compose, reg, policy.Set{}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 1)

	u := updates[0]
	assert.Equal(t, "1.2.3", u.CurrentTag)
	assert.Equal(t, "1.2.4", u.LatestTag)
	require.NotNil(t, u.TagVar)
	assert.True(t, u.TagVar.FromDefault)

	require.NoError(t, u.Apply())

	after, err := os.ReadFile(compose)
	require.NoError(t, err)
	assert.Equal(t, "services:\n  app:\n    image: "+image+":${APP_VERSION:-1.2.4}\n", string(after))
}

// The literal text around a variable belongs to the image line; only the version
// itself goes back into the .env, or the variable would end up holding a whole
// tag and the suffix would be written twice.
func TestOnlyTheVariablesShareOfTheTagIsWrittenBack(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"1.2.3-alpine", "1.2.4-alpine"},
		map[string]string{"1.2.3-alpine": registrytest.DigestOld, "1.2.4-alpine": registrytest.DigestNew})
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	image := serverURL.Host + "/library/myimage"

	compose := writeStack(t, "services:\n  app:\n    image: "+image+":${APP_VERSION}-alpine\n", "APP_VERSION=1.2.3\n")

	updates, err := New(compose, registry.New(serverURL.Host), policy.Set{}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 1)

	u := updates[0]
	assert.Equal(t, "1.2.3-alpine", u.CurrentTag)
	assert.Equal(t, "1.2.4-alpine", u.LatestTag)
	require.NotNil(t, u.TagVar)
	assert.Equal(t, "-alpine", u.TagVar.Suffix)

	require.NoError(t, u.Apply())

	env, err := os.ReadFile(filepath.Join(filepath.Dir(compose), ".env"))
	require.NoError(t, err)
	assert.Equal(t, "APP_VERSION=1.2.4\n", string(env))
}

// A variable defined in ccu's own environment is what compose would use too, so
// the check runs on it — but there is no file to write the new version into, and
// silently rewriting the compose line would drop the variable.
func TestTagFromProcessEnvironmentIsCheckedButNotWritten(t *testing.T) {
	t.Setenv("CCU_TEST_APP_VERSION", "1.2.3")
	image, reg := versionedServer(t)

	compose := writeStack(t, "services:\n  app:\n    image: "+image+":${CCU_TEST_APP_VERSION}\n", "")

	updates, err := New(compose, reg, policy.Set{}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 1)

	u := updates[0]
	assert.Equal(t, "1.2.4", u.LatestTag)
	require.NotNil(t, u.TagVar)
	assert.NotEmpty(t, u.TagVar.Unwritable)

	err = u.Apply()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CCU_TEST_APP_VERSION")

	// Refusing has to leave nothing behind, backup included.
	after, err := os.ReadFile(compose)
	require.NoError(t, err)
	assert.Contains(t, string(after), "${CCU_TEST_APP_VERSION}")
	assert.NoFileExists(t, compose+backupSuffix)
}

// The immich case: a reference naming a variable no .env defines. The image is
// not merely unresolvable, the file is missing a line — so the report says which
// variable and where it belongs.
func TestUnresolvedVariableIsReportedWithTheVariableItNeeds(t *testing.T) {
	image, reg := versionedServer(t)

	compose := writeStack(t, "services:\n  app:\n    image: "+image+":${APP_VERSION}\n", "")

	updates, err := New(compose, reg, policy.Set{}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 1)

	u := updates[0]
	assert.True(t, u.IsUnreadable())
	assert.Equal(t, ReasonUnresolvedVariable, u.UnreadableReason)
	assert.Contains(t, u.UnreadableMessage, "APP_VERSION")
	assert.Contains(t, u.UnreadableMessage, ".env")
	assert.False(t, u.HasNewVersion())
}

// Two services sharing one variable are one update, so the .env line is rewritten
// once and both services follow it.
func TestServicesSharingOneVariableAreOneUpdate(t *testing.T) {
	image, reg := versionedServer(t)

	compose := writeStack(t,
		"services:\n  app:\n    image: "+image+":${APP_VERSION}\n  worker:\n    image: "+image+":${APP_VERSION}\n",
		"APP_VERSION=1.2.3\n")

	updates, err := New(compose, reg, policy.Set{}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.Equal(t, []string{"app", "worker"}, updates[0].Services)
}

// Two variables holding the same version today are still two variables: folding
// them into one update would write one of them and leave the other behind.
func TestDifferentVariablesOnTheSameTagStaySeparateUpdates(t *testing.T) {
	image, reg := versionedServer(t)

	compose := writeStack(t,
		"services:\n  app:\n    image: "+image+":${APP_VERSION}\n  worker:\n    image: "+image+":${WORKER_VERSION}\n",
		"APP_VERSION=1.2.3\nWORKER_VERSION=1.2.3\n")

	updates, err := New(compose, reg, policy.Set{}).Check(true, true, true)
	require.NoError(t, err)
	require.Len(t, updates, 2)

	for _, u := range updates {
		require.NoError(t, u.Apply())
	}

	env, err := os.ReadFile(filepath.Join(filepath.Dir(compose), ".env"))
	require.NoError(t, err)
	assert.Equal(t, "APP_VERSION=1.2.4\nWORKER_VERSION=1.2.4\n", string(env))
}
