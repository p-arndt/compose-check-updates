package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// updateTimeout bounds the whole check-download-verify-install cycle. Generous
// compared to the passive notice, since the user explicitly asked to update and
// is waiting on the result rather than being interrupted by it.
const updateTimeout = 60 * time.Second

// selfUpdater is the slice of *Client that Run needs. Naming it lets the tests
// drive every output branch without a network round-trip.
type selfUpdater interface {
	SelfUpdate(ctx context.Context, current string, checkOnly bool) (*Result, error)
}

// Run performs (or checks for) a self-update, writing progress to w.
func Run(w io.Writer, current string, checkOnly bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	return run(ctx, w, NewClient(&http.Client{Timeout: updateTimeout}), current, checkOnly)
}

// run holds the reporting logic, separated from client construction so the
// caller of the tests can supply an updater. Errors travel back unwrapped: main
// owns the exit code, this only decides what the user reads.
func run(ctx context.Context, w io.Writer, u selfUpdater, current string, checkOnly bool) error {
	// A plain check is expected to be quiet enough to script around; only the
	// installing path announces itself before the (possibly slow) download.
	if !checkOnly {
		fmt.Fprintf(w, "Current version: %s. Checking for updates…\n", current)
	}

	res, err := u.SelfUpdate(ctx, current, checkOnly)
	if err != nil {
		return err
	}

	// SelfUpdate reports what it found even when it installed nothing, so the
	// comparison — not res.Updated — is what tells the two apart.
	if !IsNewer(res.Latest, res.Current) {
		fmt.Fprintf(w, "You're on the latest version (%s).\n", res.Current)
		return nil
	}
	if checkOnly {
		fmt.Fprintf(w, "A newer version is available: %s (you have %s). Run `ccu -self-update` to upgrade.\n", res.Latest, res.Current)
		return nil
	}
	fmt.Fprintf(w, "Updated ccu %s → %s.\n", res.Current, res.Latest)
	return nil
}
