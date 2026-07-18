package update

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeUpdater returns a canned result, so every reporting branch can be driven
// without reaching the network or touching the running binary.
type fakeUpdater struct {
	res *Result
	err error

	gotCurrent   string
	gotCheckOnly bool
}

func (f *fakeUpdater) SelfUpdate(_ context.Context, current string, checkOnly bool) (*Result, error) {
	f.gotCurrent, f.gotCheckOnly = current, checkOnly
	return f.res, f.err
}

func TestRunUpToDate(t *testing.T) {
	var buf bytes.Buffer
	u := &fakeUpdater{res: &Result{Current: "1.2.3", Latest: "1.2.3"}}

	require.NoError(t, run(context.Background(), &buf, u, "1.2.3", false))

	assert.Contains(t, buf.String(), "Current version: 1.2.3. Checking for updates…")
	assert.Contains(t, buf.String(), "You're on the latest version (1.2.3).")
}

func TestRunCheckOnlyNewer(t *testing.T) {
	var buf bytes.Buffer
	u := &fakeUpdater{res: &Result{Current: "1.2.3", Latest: "1.3.0"}}

	require.NoError(t, run(context.Background(), &buf, u, "1.2.3", true))

	assert.True(t, u.gotCheckOnly, "checkOnly must reach SelfUpdate, or a check would install")
	// A check announces nothing up front — the verdict is the entire output.
	assert.Equal(t, "A newer version is available: 1.3.0 (you have 1.2.3). Run `ccu -self-update` to upgrade.\n", buf.String())
}

func TestRunUpdated(t *testing.T) {
	var buf bytes.Buffer
	u := &fakeUpdater{res: &Result{Current: "1.2.3", Latest: "1.3.0", Updated: true}}

	require.NoError(t, run(context.Background(), &buf, u, "1.2.3", false))

	assert.False(t, u.gotCheckOnly)
	assert.Equal(t, "1.2.3", u.gotCurrent)
	assert.Contains(t, buf.String(), "Updated ccu 1.2.3 → 1.3.0.")
}

// A failed lookup must surface unchanged: main turns it into the exit code, so
// wrapping or swallowing it here would hide why the update did not happen.
func TestRunPropagatesError(t *testing.T) {
	var buf bytes.Buffer
	want := errors.New("release lookup failed")

	err := run(context.Background(), &buf, &fakeUpdater{err: want}, "1.2.3", false)

	assert.Same(t, want, err)
	assert.NotContains(t, buf.String(), "Updated ccu")
}
