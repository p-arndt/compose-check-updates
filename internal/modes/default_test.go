package modes

import (
	"bytes"
	"context"
	"github.com/p-arndt/compose-check-updates/internal/check"
	"github.com/p-arndt/compose-check-updates/internal/cli"
	"github.com/p-arndt/compose-check-updates/internal/registrytest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal/report"
	"github.com/p-arndt/compose-check-updates/internal/scanner"
)

const (
	digestOld = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	digestNew = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestUnreadableImageIsReportedButNeverPending(t *testing.T) {
	server := registrytest.Server(t, "library/myimage",
		[]string{"latest", "sha-e1c83ba"},
		map[string]string{"latest": digestNew, "sha-e1c83ba": digestOld})

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv("CCU_REGISTRY_HOST", serverURL.Host)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "compose.yaml"),
		[]byte("services:\n  app:\n    image: library/myimage:sha-e1c83ba\n"), 0644))

	var buf bytes.Buffer
	outcome, err := Default(context.Background(),
		scanner.Options{Root: root, Major: true, Minor: true, Patch: true},
		cli.Flags{},
		report.New(report.FormatJSONL, &buf))
	require.NoError(t, err)

	assert.Zero(t, outcome.Updates)
	assert.Zero(t, outcome.Pending)
	assert.False(t, outcome.Failed)
	assert.Equal(t, 1, outcome.Unreadable)

	// Reported all the same, under a kind a consumer can tell from an update.
	line := strings.TrimSpace(buf.String())
	assert.Contains(t, line, `"kind":"unreadable"`)
	assert.Contains(t, line, check.ReasonNoTagForDigest)
}
