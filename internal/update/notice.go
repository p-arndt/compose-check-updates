package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// checkInterval is how often the passive notice refreshes its cached "latest
// version" — at most once per this window, so ordinary scans never repeatedly
// hit the network.
const checkInterval = 24 * time.Hour

// noticeTimeout bounds the refresh so a slow or unreachable GitHub can't delay
// the user's command by more than this.
const noticeTimeout = 1500 * time.Millisecond

// state is the cached result of the last update check.
type state struct {
	LastCheck time.Time `json:"last_check"`
	Latest    string    `json:"latest"` // latest version seen, without a leading "v"
}

// statePath returns the cache file location. It's a var so tests can redirect it
// instead of mutating the process environment for os.UserConfigDir.
//
// UserConfigDir is the natural cross-platform home for this: %AppData% on
// Windows (a first-class CI target here), ~/.config elsewhere — preferable to
// hand-rolling ~/.ccu.
var statePath = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ccu", "update-check.json"), nil
}

func loadState() state {
	var st state
	path, err := statePath()
	if err != nil {
		return st
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st) // a corrupt cache is not a user-facing error, it just forces a fresh check
	// The cached version is printed straight to a terminal, and the cache file is
	// attacker-influencable at rest. Re-validate on load so terminal escape
	// sequences can't survive a round trip through the cache.
	if !ValidVersion(st.Latest) {
		st.Latest = ""
	}
	return st
}

func saveState(st state) {
	path, err := statePath()
	if err != nil {
		return
	}
	// 0o700/0o600: the file is per-user state, nothing else needs to read it.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600) // best effort; a failed write only costs an extra check
}

// NotifyIfAvailable prints a one-line "newer version available" hint to w when
// the cached latest version is newer than current, then refreshes the cache if
// it's stale. The caller passes os.Stderr so piped stdout stays
// machine-readable.
//
// The notice always reflects the *previously* cached check: the refresh below
// only updates the cache for the next run. That keeps the common path off a
// blocking network round trip while still converging within a day.
//
// Set CCU_NO_UPDATE_CHECK to any non-empty value to disable it entirely.
func NotifyIfAvailable(w io.Writer, current string) {
	// Dev builds have no meaningful "newer" release to point at, and the opt-out
	// must short-circuit before any file or network access.
	if os.Getenv("CCU_NO_UPDATE_CHECK") != "" || IsDevVersion(current) {
		return
	}

	st := loadState()

	// Print first, refresh second — see the doc comment.
	if st.Latest != "" && IsNewer(st.Latest, current) {
		fmt.Fprintf(w, "\nA newer ccu is available: %s (you have %s). Run `ccu -self-update` to upgrade.\n", st.Latest, current)
	}

	if time.Since(st.LastCheck) < checkInterval {
		return
	}
	refresh(NewClient(&http.Client{Timeout: noticeTimeout}), st)
}

// refresh claims the 24h window before touching the network, so a hanging or
// erroring endpoint doesn't make every single invocation retry the fetch. Then
// it records the latest version for the next run.
//
// This still blocks the caller for up to noticeTimeout in the worst case; that
// is the deliberate ceiling on the once-a-day path.
func refresh(c *Client, st state) {
	st.LastCheck = time.Now()
	saveState(st) // claim the window up front, even if the fetch below fails

	ctx, cancel := context.WithTimeout(context.Background(), noticeTimeout)
	defer cancel()

	// The fetch runs in a goroutine so ctx.Done() bounds the wait even if the
	// client ignores the context; the buffered channel keeps it from leaking.
	done := make(chan string, 1)
	go func() {
		rel, err := c.LatestRelease(ctx)
		if err != nil {
			close(done)
			return
		}
		done <- strings.TrimPrefix(rel.Tag, "v")
	}()

	select {
	case v, ok := <-done:
		if ok && v != "" {
			st.Latest = v
			saveState(st)
		}
	case <-ctx.Done():
		// Timed out; the claimed window stands and we try again tomorrow.
	}
}
