package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sum returns the sha256sum-style hex digest of b.
func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// releaseServer stands in for GitHub: it serves /releases/latest plus one asset
// per entry in assets. Asset URLs point back at the same server, which listens
// on loopback — the only case where allowedURL tolerates plain http.
func releaseServer(t *testing.T, tag string, assets map[string][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		var items []string
		for name := range assets {
			items = append(items, fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/dl/%s"}`, name, srv.URL, name))
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, tag, strings.Join(items, ","))
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := assets[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(body)
	})
	return srv
}

// updateClient returns a Client aimed at srv. Named distinctly from notice_test.go's
// clientFor, which takes a base URL string.
func updateClient(srv *httptest.Server) *Client {
	c := NewClient(nil)
	c.APIBase = srv.URL
	return c
}

// fakeExe points executablePath at a throwaway file for the duration of the
// test, so the binary swap can be exercised for real without touching the test
// runner's own executable (which the OS would not let us replace anyway).
func fakeExe(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ccu-fake.exe")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	// EvalSymlinks: on macOS t.TempDir() lives under a symlinked /var, and the
	// production path resolves symlinks, so the expected path must too.
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	orig := executablePath
	executablePath = func() (string, error) { return resolved, nil }
	t.Cleanup(func() { executablePath = orig })
	return resolved
}

func TestBinaryAssetName(t *testing.T) {
	tests := []struct {
		name         string
		goos, goarch string
		want         string
	}{
		{"linux amd64", "linux", "amd64", "ccu-linux-amd64"},
		{"linux arm64", "linux", "arm64", "ccu-linux-arm64"},
		{"darwin arm64", "darwin", "arm64", "ccu-darwin-arm64"},
		{"windows gets .exe", "windows", "amd64", "ccu-windows-amd64.exe"},
		{"windows 386", "windows", "386", "ccu-windows-386.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BinaryAssetName(tt.goos, tt.goarch))
		})
	}
}

func TestChecksumsName(t *testing.T) {
	assert.Equal(t, "ccu_0.4.1_checksums.txt", ChecksumsName("0.4.1"))
	assert.Equal(t, "ccu_1.0.0-beta.1_checksums.txt", ChecksumsName("1.0.0-beta.1"))
}

func TestAllowedURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string // substring; "" means allowed
	}{
		{"github api over https", "https://api.github.com/repos/x/y", ""},
		{"github.com over https", "https://github.com/o/r/releases/download/v1/ccu", ""},
		{"cdn over https", "https://objects.githubusercontent.com/blob", ""},
		{"trailing dot host", "https://github.com./o/r", ""},
		{"loopback http for tests", "http://127.0.0.1:8080/dl", ""},
		{"localhost http for tests", "http://localhost:8080/dl", ""},
		{"ipv6 loopback http", "http://[::1]:8080/dl", ""},
		{"non-github https", "https://evil.example/ccu", "non-GitHub host"},
		{"lookalike suffix", "https://evilgithubusercontent.com/x", "non-GitHub host"},
		{"lookalike prefix", "https://github.com.evil.example/x", "non-GitHub host"},
		{"plain http to github", "http://github.com/o/r", "non-https"},
		{"non-loopback http", "http://192.0.2.1/ccu", "non-https"},
		{"file scheme", "file:///etc/passwd", "non-https"},
		{"unparsable", "://nope", "unparsable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := allowedURL(tt.url)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			// The untrusted URL must never be echoed back to the terminal.
			assert.NotContains(t, err.Error(), "evil")
		})
	}
}

func TestVerifyChecksum(t *testing.T) {
	bin := []byte("binary contents")
	good := sum(bin)

	tests := []struct {
		name      string
		checksums string
		wantErr   string
	}{
		{"match", good + "  ccu-linux-amd64\n", ""},
		{"match with binary-mode star", good + " *ccu-linux-amd64\n", ""},
		{"match uppercase hex", strings.ToUpper(good) + "  ccu-linux-amd64\n", ""},
		{"match among other entries", "0\n" + sum([]byte("x")) + "  ccu-darwin-arm64\n" + good + "  ccu-linux-amd64\n", ""},
		{"mismatch", sum([]byte("other")) + "  ccu-linux-amd64\n", "checksum mismatch"},
		{"name absent", good + "  ccu-darwin-arm64\n", "no checksum listed"},
		{"empty file", "", "no checksum listed"},
		{"malformed lines ignored", "notahash ccu-linux-amd64\nshort  ccu-linux-amd64\n", "no checksum listed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyChecksum(bin, "ccu-linux-amd64", []byte(tt.checksums))
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// A non-hex "hash" must not be able to smuggle terminal escapes into the error.
func TestVerifyChecksum_RejectsEscapeSmuggling(t *testing.T) {
	bin := []byte("binary contents")
	line := "\x1b[31mPWNED\x1b[0m  ccu-linux-amd64\n"
	err := verifyChecksum(bin, "ccu-linux-amd64", []byte(line))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "\x1b")
	assert.NotContains(t, err.Error(), "PWNED")
}

func TestFindAsset(t *testing.T) {
	assets := []Asset{{Name: "ccu-linux-amd64", URL: "u1"}, {Name: "sums.txt", URL: "u2"}}

	got, err := findAsset(assets, "sums.txt")
	require.NoError(t, err)
	assert.Equal(t, "u2", got.URL)

	_, err = findAsset(assets, "ccu-plan9-amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ccu-plan9-amd64")
}

func TestLatestRelease(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		srv := releaseServer(t, "v1.2.3", map[string][]byte{"ccu-linux-amd64": []byte("bin")})
		rel, err := updateClient(srv).LatestRelease(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "v1.2.3", rel.Tag)
		require.Len(t, rel.Assets, 1)
		assert.Equal(t, "ccu-linux-amd64", rel.Assets[0].Name)
		assert.Contains(t, rel.Assets[0].URL, "/dl/ccu-linux-amd64")
	})

	t.Run("sends the API headers github requires", func(t *testing.T) {
		var accept, ua string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			accept, ua = r.Header.Get("Accept"), r.Header.Get("User-Agent")
			fmt.Fprint(w, `{"tag_name":"v1.0.0"}`)
		}))
		defer srv.Close()
		_, err := updateClient(srv).LatestRelease(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "application/vnd.github+json", accept)
		assert.Equal(t, "ccu-updater", ua)
	})

	t.Run("non-200 reports the code, never the server's status text", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A hostile status line: it must not reach the user's terminal.
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()
		_, err := updateClient(srv).LatestRelease(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "403")
		assert.NotContains(t, err.Error(), "Forbidden")
	})

	t.Run("rejects an implausible tag", func(t *testing.T) {
		for _, tag := range []string{
			`v\u001b[31mnope`, // a terminal escape smuggled through the tag
			`../../etc/passwd`,
			`v` + strings.Repeat("9", 200), // absurd length
			``,
		} {
			// The tag goes into the body verbatim rather than through
			// releaseServer's %q, so the escape case can carry a JSON-level
			// unicode escape. %q would render an ESC as a Go hex escape, which
			// JSON does not understand, and a raw ESC byte is rejected by the
			// decoder before ValidVersion ever sees it - either way the test
			// would pass for the wrong reason.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"tag_name":"%s","assets":[]}`, tag)
			}))
			_, err := updateClient(srv).LatestRelease(context.Background())
			srv.Close()
			require.Error(t, err, "tag %q should be refused", tag)
			assert.Contains(t, err.Error(), "not a plausible version")
		}
	})

	t.Run("refuses a non-GitHub API base", func(t *testing.T) {
		c := NewClient(nil)
		c.APIBase = "https://evil.example"
		_, err := c.LatestRelease(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-GitHub host")
	})
}

func TestSelfUpdate_DevVersionRefused(t *testing.T) {
	for _, v := range []string{"", "dev", "(devel)"} {
		t.Run(fmt.Sprintf("version %q", v), func(t *testing.T) {
			// No server: it must refuse before making any request.
			_, err := NewClient(nil).SelfUpdate(context.Background(), v, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not an installed release")
		})
	}
}

func TestSelfUpdate_AlreadyCurrent(t *testing.T) {
	srv := releaseServer(t, "v1.0.0", nil)
	res, err := updateClient(srv).SelfUpdate(context.Background(), "1.0.0", false)
	require.NoError(t, err)
	assert.False(t, res.Updated)
	assert.Equal(t, "1.0.0", res.Current)
	assert.Equal(t, "1.0.0", res.Latest)
	assert.Empty(t, res.ExePath, "nothing should have been located, let alone replaced")
}

func TestSelfUpdate_CheckOnlyDownloadsNothing(t *testing.T) {
	exe := fakeExe(t, "old binary")
	downloads := 0
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v2.0.0","assets":[{"name":"any","browser_download_url":"%s/dl/any"}]}`, srv.URL)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) { downloads++ })

	res, err := updateClient(srv).SelfUpdate(context.Background(), "1.0.0", true)
	require.NoError(t, err)
	assert.False(t, res.Updated)
	assert.Equal(t, "2.0.0", res.Latest)
	assert.Zero(t, downloads, "checkOnly must not fetch any asset")

	data, err := os.ReadFile(exe)
	require.NoError(t, err)
	assert.Equal(t, "old binary", string(data), "checkOnly must not touch the binary")
}

func TestSelfUpdate_ReplacesTheBinary(t *testing.T) {
	exe := fakeExe(t, "old binary")
	newBin := []byte("brand new binary contents")
	binName := BinaryAssetName(runtime.GOOS, runtime.GOARCH)
	srv := releaseServer(t, "v2.0.0", map[string][]byte{
		binName:                newBin,
		ChecksumsName("2.0.0"): []byte(sum(newBin) + "  " + binName + "\n"),
	})

	res, err := updateClient(srv).SelfUpdate(context.Background(), "1.0.0", false)
	require.NoError(t, err)
	assert.True(t, res.Updated)
	assert.Equal(t, "2.0.0", res.Latest)
	assert.Equal(t, exe, res.ExePath)

	got, err := os.ReadFile(exe)
	require.NoError(t, err)
	assert.Equal(t, newBin, got)

	// No temp file may be left behind in the install directory.
	entries, err := os.ReadDir(filepath.Dir(exe))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".ccu-update-"), "leftover temp file %s", e.Name())
	}
}

func TestSelfUpdate_ChecksumMismatchLeavesBinaryAlone(t *testing.T) {
	exe := fakeExe(t, "old binary")
	binName := BinaryAssetName(runtime.GOOS, runtime.GOARCH)
	srv := releaseServer(t, "v2.0.0", map[string][]byte{
		binName: []byte("tampered payload"),
		// A checksum for something else entirely.
		ChecksumsName("2.0.0"): []byte(sum([]byte("the real binary")) + "  " + binName + "\n"),
	})

	res, err := updateClient(srv).SelfUpdate(context.Background(), "1.0.0", false)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "checksum mismatch")

	got, err := os.ReadFile(exe)
	require.NoError(t, err)
	assert.Equal(t, "old binary", string(got), "a failed verification must not touch the installed binary")
}

func TestSelfUpdate_MissingChecksumsRefused(t *testing.T) {
	fakeExe(t, "old binary")
	binName := BinaryAssetName(runtime.GOOS, runtime.GOARCH)
	srv := releaseServer(t, "v2.0.0", map[string][]byte{binName: []byte("payload")})

	_, err := updateClient(srv).SelfUpdate(context.Background(), "1.0.0", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksums")
}

func TestSelfUpdate_MissingPlatformAssetRefused(t *testing.T) {
	fakeExe(t, "old binary")
	srv := releaseServer(t, "v2.0.0", map[string][]byte{"ccu-plan9-mips": []byte("payload")})

	_, err := updateClient(srv).SelfUpdate(context.Background(), "1.0.0", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), BinaryAssetName(runtime.GOOS, runtime.GOARCH))
}

func TestSelfUpdate_OversizeAssetRefused(t *testing.T) {
	exe := fakeExe(t, "old binary")
	// Shrink the cap rather than serving 64 MiB.
	orig := maxAsset
	maxAsset = 8
	t.Cleanup(func() { maxAsset = orig })

	big := []byte("far more than eight bytes")
	binName := BinaryAssetName(runtime.GOOS, runtime.GOARCH)
	srv := releaseServer(t, "v2.0.0", map[string][]byte{
		binName:                big,
		ChecksumsName("2.0.0"): []byte(sum(big) + "  " + binName + "\n"),
	})

	_, err := updateClient(srv).SelfUpdate(context.Background(), "1.0.0", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size limit")
	// The point of erroring instead of truncating: the install survives intact.
	got, err := os.ReadFile(exe)
	require.NoError(t, err)
	assert.Equal(t, "old binary", string(got))
}

func TestSelfUpdate_NonGitHubAssetURLRefused(t *testing.T) {
	fakeExe(t, "old binary")
	binName := BinaryAssetName(runtime.GOOS, runtime.GOARCH)
	// A tampered release whose asset URL points off-host: the metadata is input,
	// not truth, so the download must be refused even though the API response
	// itself came from a "trusted" server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v2.0.0","assets":[{"name":%q,"browser_download_url":"https://evil.example/ccu"},{"name":%q,"browser_download_url":"https://evil.example/sums"}]}`,
			binName, ChecksumsName("2.0.0"))
	}))
	defer srv.Close()

	_, err := updateClient(srv).SelfUpdate(context.Background(), "1.0.0", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-GitHub host")
	assert.NotContains(t, err.Error(), "evil.example")
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ccu.exe")
	require.NoError(t, os.WriteFile(exe, []byte("old"), 0o755))

	require.NoError(t, replaceExecutable(exe, []byte("new")))
	got, err := os.ReadFile(exe)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))

	if runtime.GOOS == "windows" {
		// The old binary is moved aside rather than deleted, because a running
		// .exe cannot be removed on Windows.
		assert.FileExists(t, exe+".old")
	} else {
		// Only meaningful off Windows, where the mode bits are enforced.
		info, err := os.Stat(exe)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	}
}

func TestReplaceExecutable_MissingDirectoryLeavesNothingBehind(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist", "ccu.exe")
	err := replaceExecutable(missing, []byte("new"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permissions")
	assert.NoFileExists(t, missing)
}

func TestCleanupLeftovers_IsSafeToCall(t *testing.T) {
	// The running test binary has no ".old" sibling; this must be a silent
	// no-op rather than a panic or an error the caller has to handle.
	assert.NotPanics(t, CleanupLeftovers)
}
