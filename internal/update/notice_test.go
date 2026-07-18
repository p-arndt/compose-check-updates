package update

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectState points statePath at a temp file for the duration of the test,
// so nothing here can read or clobber the real user config dir.
func redirectState(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ccu", "update-check.json")
	orig := statePath
	statePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { statePath = orig })
	return path
}

// seedState writes a cache file at the redirected location.
func seedState(t *testing.T, path string, st state) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	data, err := json.Marshal(st)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

// seedRaw writes arbitrary bytes as the cache, for corruption cases.
func seedRaw(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

// notifyOutput runs the notice against a fresh cache (LastCheck=now, so no
// network is involved) and returns whatever it printed.
func notifyOutput(t *testing.T, cached, current string) string {
	t.Helper()
	path := redirectState(t)
	seedState(t, path, state{LastCheck: time.Now(), Latest: cached})
	t.Setenv("CCU_NO_UPDATE_CHECK", "")

	var buf bytes.Buffer
	NotifyIfAvailable(&buf, current)
	return buf.String()
}

func TestNotifyIfAvailable_CachedVersions(t *testing.T) {
	tests := []struct {
		name    string
		cached  string
		current string
		want    string // substring expected in the output; empty means silence
	}{
		{name: "newer cached version prints the notice", cached: "1.4.0", current: "1.3.1", want: "1.4.0"},
		{name: "equal cached version stays silent", cached: "1.3.1", current: "1.3.1"},
		{name: "older cached version stays silent", cached: "1.2.0", current: "1.3.1"},
		{name: "empty cached version stays silent", cached: "", current: "1.3.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := notifyOutput(t, tt.cached, tt.current)
			if tt.want == "" {
				assert.Empty(t, out)
				return
			}
			assert.Contains(t, out, tt.want)
			assert.Contains(t, out, tt.current)
			assert.Contains(t, out, "ccu -self-update")
		})
	}
}

func TestNotifyIfAvailable_KillSwitchSuppressesEverything(t *testing.T) {
	path := redirectState(t)
	// Stale cache: without the kill switch this would both print and refresh.
	seedState(t, path, state{LastCheck: time.Now().Add(-48 * time.Hour), Latest: "1.4.0"})
	t.Setenv("CCU_NO_UPDATE_CHECK", "1")

	var buf bytes.Buffer
	NotifyIfAvailable(&buf, "1.3.1")

	assert.Empty(t, buf.String())
	// The window must not have been claimed either — nothing ran at all.
	before := readState(t, path)
	assert.True(t, before.LastCheck.Before(time.Now().Add(-24*time.Hour)))
}

func TestNotifyIfAvailable_DevVersionSuppressesEverything(t *testing.T) {
	path := redirectState(t)
	seedState(t, path, state{LastCheck: time.Now(), Latest: "1.4.0"})
	t.Setenv("CCU_NO_UPDATE_CHECK", "")

	var buf bytes.Buffer
	NotifyIfAvailable(&buf, "dev")

	assert.Empty(t, buf.String())
}

func TestNotifyIfAvailable_CorruptCacheIsIgnored(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{name: "not json", contents: "this is not json at all"},
		{name: "truncated json", contents: `{"last_check": "2026-01-0`},
		{name: "wrong types", contents: `{"last_check": 42, "latest": ["1.4.0"]}`},
		{name: "empty file", contents: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := redirectState(t)
			seedRaw(t, path, tt.contents)
			t.Setenv("CCU_NO_UPDATE_CHECK", "1") // keep the refresh off the network

			var buf bytes.Buffer
			assert.NotPanics(t, func() { NotifyIfAvailable(&buf, "1.3.1") })
			assert.Empty(t, buf.String())
		})
	}
}

func TestNotifyIfAvailable_RejectsEscapeSequenceInCache(t *testing.T) {
	// A tampered cache must never reach the terminal: the stored version is
	// re-validated on load, so an ANSI payload is dropped rather than printed.
	out := notifyOutput(t, "9.9.9\x1b[31mowned", "1.3.1")
	assert.Empty(t, out)
}

func TestNotifyIfAvailable_FreshCacheSkipsNetwork(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer srv.Close()

	path := redirectState(t)
	seeded := state{LastCheck: time.Now().Add(-time.Hour), Latest: "1.4.0"}
	seedState(t, path, seeded)
	t.Setenv("CCU_NO_UPDATE_CHECK", "")

	var buf bytes.Buffer
	NotifyIfAvailable(&buf, "1.3.1")

	assert.Contains(t, buf.String(), "1.4.0")
	assert.Zero(t, atomic.LoadInt32(&hits), "a check within checkInterval must not touch the network")
	// The window was not re-claimed, so tomorrow's run still refreshes on time.
	assert.WithinDuration(t, seeded.LastCheck, readState(t, path).LastCheck, time.Second)
}

func TestRefresh_StoresLatestOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer srv.Close()

	path := redirectState(t)
	refresh(clientFor(srv.URL), state{})

	st := readState(t, path)
	assert.Equal(t, "9.9.9", st.Latest, "the leading v is stripped before caching")
	assert.WithinDuration(t, time.Now(), st.LastCheck, time.Minute)
}

func TestRefresh_ClaimsWindowWhenServerFails(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "server errors",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
		},
		{
			name: "server hangs past noticeTimeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Unblock on client cancellation so srv.Close() can't deadlock
				// waiting for an in-flight request.
				<-r.Context().Done()
			},
		},
		{
			name: "server returns garbage",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not json"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			path := redirectState(t)
			refresh(clientFor(srv.URL), state{})

			// The whole point of claiming up front: a broken endpoint still costs
			// exactly one attempt per day, not one per invocation.
			st := readState(t, path)
			assert.WithinDuration(t, time.Now(), st.LastCheck, time.Minute)
			assert.Empty(t, st.Latest, "a failed fetch must not cache a version")
		})
	}
}

// clientFor builds a Client aimed at a loopback test server; plain http to
// 127.0.0.1 is permitted by the client's URL policy.
func clientFor(base string) *Client {
	c := NewClient(&http.Client{Timeout: noticeTimeout})
	c.APIBase = base
	return c
}

func readState(t *testing.T, path string) state {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var st state
	require.NoError(t, json.Unmarshal(data, &st))
	return st
}
