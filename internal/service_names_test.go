package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// infosFor writes a compose file and returns what the parser made of it, without
// touching a registry: only the parsing is under test here.
func infosFor(t *testing.T, body string) []UpdateInfo {
	t.Helper()

	path := filepath.Join(t.TempDir(), "compose.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))

	infos, err := NewUpdateChecker(path, nil).createUpdateInfos()
	require.NoError(t, err)
	return infos
}

func TestServiceNamesAreRecorded(t *testing.T) {
	infos := infosFor(t, `services:
  web:
    image: nginx:1.25.0
    ports:
      - "80:80"
  db:
    image: postgres:16.1
`)

	require.Len(t, infos, 2)
	assert.Equal(t, []string{"web"}, infos[0].Services)
	assert.Equal(t, []string{"db"}, infos[1].Services)
}

// Identical references collapse into one entry, so both service names have to
// end up on the entry that survived.
func TestSharedImageCollectsEveryService(t *testing.T) {
	infos := infosFor(t, `services:
  worker:
    image: redis:7.2.0
  scheduler:
    image: redis:7.2.0
`)

	require.Len(t, infos, 1)
	assert.Equal(t, []string{"worker", "scheduler"}, infos[0].Services)
}

// Keys nested below a service — build args, an image key inside x- blocks —
// must not be mistaken for service names.
func TestNestedKeysAreNotServices(t *testing.T) {
	infos := infosFor(t, `version: "3"
services:
  app:
    build:
      context: .
    deploy:
      replicas: 2
    image: myrepo/app:1.0.0
volumes:
  data:
`)

	require.Len(t, infos, 1)
	assert.Equal(t, []string{"app"}, infos[0].Services)
}

// A block ending returns the tracker to no service at all, so an image declared
// outside services carries no name rather than the last one seen.
func TestImageOutsideServicesHasNoService(t *testing.T) {
	infos := infosFor(t, `services:
  web:
    image: nginx:1.25.0
x-templates:
  base:
    image: alpine:3.19.0
`)

	require.Len(t, infos, 2)
	assert.Equal(t, []string{"web"}, infos[0].Services)
	assert.Empty(t, infos[1].Services)
}

// Compose files in the wild are indented four spaces as often as two, and the
// tracker takes its depth from the file rather than assuming one of them.
func TestFourSpaceIndentation(t *testing.T) {
	infos := infosFor(t, `services:
    web:
        image: nginx:1.25.0
    cache:
        image: memcached:1.6.0
`)

	require.Len(t, infos, 2)
	assert.Equal(t, []string{"web"}, infos[0].Services)
	assert.Equal(t, []string{"cache"}, infos[1].Services)
}

// A file with no services block at all still parses; there is simply no name to
// report for what it declares.
func TestNoServicesBlock(t *testing.T) {
	infos := infosFor(t, `image: nginx:1.25.0
`)

	require.Len(t, infos, 1)
	assert.Empty(t, infos[0].Services)
}

func TestCommentsAndBlankLinesDoNotEndTheBlock(t *testing.T) {
	infos := infosFor(t, `services:
  web:
    # the public entrypoint

    image: nginx:1.25.0
`)

	require.Len(t, infos, 1)
	assert.Equal(t, []string{"web"}, infos[0].Services)
}
