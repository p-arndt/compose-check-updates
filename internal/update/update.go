// Package update handles self-updating the ccu binary from GitHub Releases.
//
// The release pipeline publishes one RAW, UNCOMPRESSED binary per platform,
// named ccu-<goos>-<goarch>(.exe), plus a sha256sum-format checksums file
// ccu_<version>_checksums.txt (see .github/workflows/release.yml). There is no
// archive to unpack: the downloaded bytes are the executable, so the download is
// verified against the checksums file and then swapped straight into place.
//
// The trust chain is therefore three links, and every one of them has to hold:
// https to a pinned GitHub host authenticates the channel, the checksums file
// authenticates the binary, and an atomic same-directory rename installs it.
// ccu publishes no detached signature, which makes the host pinning load-bearing
// rather than defence in depth — a checksum fetched over an attacker-controlled
// channel vouches for nothing.
//
// Everything that touches the network goes through Client so tests can point at
// a local httptest server; checksum verification and asset naming are pure
// functions.
//
// Note: internal/version.go at the repo root is UNRELATED to this package's
// version helpers — it compares Docker image tags, not ccu's own release
// versions.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Repo coordinates for the published releases, kept here so every caller points
// at the same place.
const (
	repoOwner = "Padi2312"
	repoName  = "compose-check-updates"
)

// userAgent identifies this client to GitHub; the API rejects requests without
// a User-Agent outright.
const userAgent = "ccu-updater"

// maxAsset caps how many bytes we accept for the release binary so a hostile or
// corrupt response can't exhaust memory. Exceeding it is always an error, never
// a silent truncation: a truncated binary written over the user's working ccu
// would leave them with an install that cannot run. Release binaries are a few
// MB. A var only so tests can shrink it.
var maxAsset = int64(64 << 20) // 64 MiB

// maxChecksums is far tighter than maxAsset: a genuine checksums file is a few
// hundred bytes, and there is no reason to buffer megabytes of "checksums" a
// hostile asset serves up.
const maxChecksums = int64(1 << 20) // 1 MiB

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release is the slice of the GitHub release payload we care about.
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// Result reports the outcome of an update attempt.
type Result struct {
	Current string // version before the attempt
	Latest  string // latest available version
	Updated bool   // whether the binary was actually replaced
	ExePath string // the binary that was (or would be) replaced
}

// Client talks to the GitHub Releases API. The zero value is not usable; use
// NewClient. APIBase and HTTP are overridable in tests.
type Client struct {
	HTTP    *http.Client
	APIBase string // e.g. "https://api.github.com"
	Owner   string
	Repo    string
}

// NewClient returns a Client pointed at github.com with the given HTTP client
// (nil gets a fresh client with a sane timeout — never http.DefaultClient, which
// has no timeout and is a shared global we must not mutate).
//
// The client gets a redirect policy that re-applies allowedURL on every hop.
// Checking only the initial URL would be pointless: GitHub's asset URLs redirect
// to the CDN, so an attacker who can influence a hop could walk the download off
// a GitHub host entirely while the first URL still looked fine.
func NewClient(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	if hc.CheckRedirect == nil {
		hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return allowedURL(req.URL.String())
		}
	}
	return &Client{HTTP: hc, APIBase: "https://api.github.com", Owner: repoOwner, Repo: repoName}
}

// allowedURL rejects any URL that isn't https to a GitHub-controlled host.
// Release metadata — including asset URLs — is input, not truth: following a
// plain-http URL (or a redirect hop onto one) would let an on-path attacker
// substitute both the binary and the checksums file that vouches for it, making
// verification meaningless. Pinning the host matters independently: a tampered
// release must not be able to point every updating client at an arbitrary
// server, which would leak who runs ccu to an attacker-chosen host and turn
// clients into traffic generators. Plain http is tolerated only for loopback so
// tests can run a local httptest server, which never listens on a routable
// address. The error deliberately doesn't echo the URL: it's untrusted bytes
// that would otherwise land on the user's terminal.
func allowedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("release asset URL is unparsable")
	}
	switch {
	case u.Scheme == "https" && isGitHubHost(u.Hostname()):
		return nil
	case u.Scheme == "https":
		return fmt.Errorf("refusing to download a release asset from a non-GitHub host")
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	}
	return fmt.Errorf("refusing to download a release asset over a non-https URL")
}

// isGitHubHost reports whether host is one GitHub serves releases from: the API,
// the release download path on github.com, and the *.githubusercontent.com CDN
// hosts those downloads redirect to. Matching is on whole labels — a lookalike
// like "evilgithubusercontent.com" or "github.com.evil.example" must not pass.
func isGitHubHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	switch host {
	case "github.com", "api.github.com":
		return true
	}
	return strings.HasSuffix(host, ".githubusercontent.com")
}

// isLoopback reports whether host is localhost or a loopback IP.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LatestRelease fetches the latest non-prerelease, non-draft release. GitHub's
// /releases/latest endpoint already excludes both.
func (c *Client) LatestRelease(ctx context.Context) (*Release, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/releases/latest", strings.TrimRight(c.APIBase, "/"), c.Owner, c.Repo)
	// APIBase is overridable, so it gets the same scrutiny as an asset URL.
	if err := allowedURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// StatusCode, not Status: the status line carries server-supplied text
		// and this message is printed to the user's terminal.
		return nil, fmt.Errorf("github returned HTTP %d", resp.StatusCode)
	}
	var rel Release
	// Capped even though this is GitHub: an unbounded decode off the network is
	// a memory-exhaustion primitive regardless of who is on the other end.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	// The tag is untrusted input that flows into terminal output and into asset
	// file names we then match against — reject anything that isn't a clean
	// version (terminal escapes, path separators, absurd lengths) at ingress,
	// before it can go anywhere.
	if !ValidVersion(strings.TrimPrefix(rel.Tag, "v")) {
		return nil, fmt.Errorf("release tag is not a plausible version — refusing it")
	}
	return &rel, nil
}

// download fetches an asset into memory. It refuses URLs that fail allowedURL
// (the asset URL comes from untrusted release metadata) and errors — rather than
// silently truncating — when the body exceeds limit. what names the asset in
// errors, since the URL itself is untrusted bytes we won't echo to the terminal.
func (c *Client) download(ctx context.Context, rawURL, what string, limit int64) ([]byte, error) {
	if err := allowedURL(rawURL); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", what, resp.StatusCode)
	}
	// limit+1 so an over-limit body is detectable: reading exactly limit bytes
	// can't distinguish "fits exactly" from "was cut off".
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds its %d-byte size limit — refusing a truncated install", what, limit)
	}
	return data, nil
}

// BinaryAssetName is the release asset name for a platform, matching what the
// release workflow uploads: raw binaries, not archives, because the README's
// install one-liners tell users to download exactly these names.
func BinaryAssetName(goos, goarch string) string {
	name := fmt.Sprintf("ccu-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// ChecksumsName is the checksums file name for a version.
func ChecksumsName(version string) string {
	return fmt.Sprintf("ccu_%s_checksums.txt", version)
}

// findAsset returns the asset with the given name, or an error naming what was
// looked for (which is our own string, not release-supplied text). A miss most
// often means the release simply didn't build for this platform.
func findAsset(assets []Asset, name string) (Asset, error) {
	for _, a := range assets {
		if a.Name == name {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("release has no asset %q (this platform may not be published)", name)
}

// verifyChecksum confirms bin's SHA-256 appears against name in a
// sha256sum-format checksums file ("<hex>  <name>" per line).
func verifyChecksum(bin []byte, name string, checksums []byte) error {
	sum := sha256.Sum256(bin)
	want := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		// Only well-formed lines (64 hex chars + name) can match at all. This is
		// also what keeps the mismatch message below safe to print: the checksums
		// file is untrusted, and a non-hex "hash" could otherwise smuggle
		// terminal escape sequences into the error output.
		if len(fields) != 2 || !isHex64(fields[0]) {
			continue
		}
		// sha256sum prefixes the name with "*" in binary mode; tolerate it.
		if strings.TrimPrefix(fields[1], "*") == name {
			// EqualFold: sha256sum emits lowercase, but other tools emit upper.
			if strings.EqualFold(fields[0], want) {
				return nil
			}
			// Distinct from the "not listed" case below: a mismatch means the
			// bytes are wrong (tampering or corruption), while a missing entry
			// means the release is incomplete. Different causes, different fixes.
			return fmt.Errorf("checksum mismatch for %q: got %s, want %s", name, want, strings.ToLower(fields[0]))
		}
	}
	return fmt.Errorf("no checksum listed for %q", name)
}

// isHex64 reports whether s is exactly 64 hexadecimal characters (a SHA-256).
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// executablePath resolves the running binary to a real, symlink-free path so the
// swap targets the actual file rather than a symlink pointing at it — replacing
// the symlink would leave the real binary stale and the link dangling. It's a
// var so tests can point the swap at a throwaway file.
var executablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// replaceExecutable atomically swaps the file at exePath for newBin.
//
// The new binary is written to a temp file in the SAME directory as the target
// so the final rename stays on one filesystem: a cross-device rename is not
// atomic (it degrades to copy-then-delete, or fails outright), and a partially
// copied executable is exactly the outcome this whole function exists to avoid.
//
// On Windows a running .exe cannot be deleted or overwritten, but it CAN be
// renamed, so the current file is moved aside to "<exe>.old" first; that
// leftover is cleaned up on the next run (see CleanupLeftovers).
func replaceExecutable(exePath string, newBin []byte) error {
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".ccu-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %q (%w) — reinstall ccu or run with sufficient permissions", dir, err)
	}
	tmpName := tmp.Name()
	// Clean up the temp file on every failure path before the final rename
	// succeeds, so a failed update doesn't litter the install directory.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(newBin); err != nil {
		tmp.Close()
		return fmt.Errorf("writing the new binary: %w", err)
	}
	// Close before chmod/rename: on Windows an open handle blocks the rename,
	// and the write isn't guaranteed flushed until Close returns.
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing the new binary: %w", err)
	}
	// CreateTemp makes the file 0600; without this the installed binary would
	// not be executable (and would not be usable by other users on Unix).
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("making the new binary executable: %w", err)
	}

	if runtime.GOOS == "windows" {
		old := exePath + ".old"
		os.Remove(old) // a stale one from a previous update would block the rename
		if err := os.Rename(exePath, old); err != nil {
			return fmt.Errorf("cannot move the current binary aside (%w) — is another ccu running, or is %q read-only?", err, exePath)
		}
		if err := os.Rename(tmpName, exePath); err != nil {
			// Roll back: without this the user is left with NO binary at all —
			// strictly worse than a failed update.
			os.Rename(old, exePath)
			return fmt.Errorf("cannot install the new binary (%w) — reinstall ccu or run with sufficient permissions", err)
		}
	} else if err := os.Rename(tmpName, exePath); err != nil {
		return fmt.Errorf("cannot replace %q (%w) — reinstall ccu or run with sufficient permissions", exePath, err)
	}

	success = true
	return nil
}

// CleanupLeftovers best-effort removes the "<exe>.old" file left behind by a
// prior Windows self-update. Called once at startup; every error is ignored on
// purpose — there is nothing the user could do about it, and a leftover file is
// harmless (the next update retries the removal anyway).
func CleanupLeftovers() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return
	}
	os.Remove(exe + ".old")
}

// SelfUpdate checks for a newer release and, unless checkOnly, downloads,
// verifies, and installs it in place of the running binary. current is the
// running version (without a leading "v").
//
// The updated binary is NOT re-executed: the caller's process keeps running the
// old code it already loaded, which is both simpler and safer than re-exec'ing
// mid-command with arguments that may no longer mean the same thing.
func (c *Client) SelfUpdate(ctx context.Context, current string, checkOnly bool) (*Result, error) {
	if IsDevVersion(current) {
		// A source build has no release to compare against, and silently
		// overwriting it with a published binary would discard the user's build.
		return nil, fmt.Errorf("this is a %q build, not an installed release — build from source or download a release binary to get updates", current)
	}

	rel, err := c.LatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	latest := strings.TrimPrefix(rel.Tag, "v")
	res := &Result{Current: current, Latest: latest}

	if !IsNewer(latest, current) {
		return res, nil
	}
	// Return before touching the network again: --check must never download,
	// let alone write anything.
	if checkOnly {
		return res, nil
	}

	exe, err := executablePath()
	if err != nil {
		return nil, fmt.Errorf("cannot locate the running binary: %w", err)
	}
	res.ExePath = exe

	binName := BinaryAssetName(runtime.GOOS, runtime.GOARCH)
	binAsset, err := findAsset(rel.Assets, binName)
	if err != nil {
		return nil, err
	}
	sumsAsset, err := findAsset(rel.Assets, ChecksumsName(latest))
	if err != nil {
		// Without checksums nothing authenticates the binary, so this is fatal
		// rather than a warning we could shrug off.
		return nil, fmt.Errorf("release has no checksums file: %w — refusing to update", err)
	}

	// The asset is the executable itself — no archive to unpack.
	bin, err := c.download(ctx, binAsset.URL, "release binary", maxAsset)
	if err != nil {
		return nil, err
	}
	sums, err := c.download(ctx, sumsAsset.URL, "checksums file", maxChecksums)
	if err != nil {
		return nil, err
	}
	if err := verifyChecksum(bin, binName, sums); err != nil {
		return nil, err
	}
	// A zero-byte asset would checksum fine against a matching entry, yet
	// installing it bricks the user's ccu just as thoroughly as a truncated one.
	if len(bin) == 0 {
		return nil, fmt.Errorf("release binary %q is empty — refusing to install it", binName)
	}
	if err := replaceExecutable(exe, bin); err != nil {
		return nil, err
	}

	res.Updated = true
	return res, nil
}
