// Package buildinfo exposes the binary's version metadata.
//
// The values default to an unstamped "dev" build and are overridden at release
// time via the Go linker, e.g.
//
//	go build -ldflags "-X github.com/padi2312/compose-check-updates/internal/buildinfo.Version=1.2.3 \
//	                   -X github.com/padi2312/compose-check-updates/internal/buildinfo.Commit=abc1234 \
//	                   -X github.com/padi2312/compose-check-updates/internal/buildinfo.Date=2026-07-01T12:00:00Z"
//
// The release pipeline reads the version from the repo-root VERSION file (the
// single source of truth) and injects it here. See .github/workflows/release.yml
// and the `build-release` recipe in the justfile.
//
// Note: this package is about *ccu's own* version. internal/version.go is
// unrelated — that one compares semver tags of Docker images.
package buildinfo

var (
	// Version is the semantic version, without a leading "v" (e.g. "1.2.3").
	// "dev" when the binary was built without stamping.
	Version = "dev"
	// Commit is the short git SHA the binary was built from; empty when unstamped.
	Commit = ""
	// Date is the RFC 3339 UTC build timestamp; empty when unstamped.
	Date = ""
)

// String renders a one-line version string. A stamped build reports
// "1.2.3 (abc1234, 2026-07-01T12:00:00Z)"; an unstamped one degrades to just
// "dev" rather than printing empty parentheses.
func String() string {
	switch {
	case Commit != "" && Date != "":
		return Version + " (" + Commit + ", " + Date + ")"
	case Commit != "":
		return Version + " (" + Commit + ")"
	case Date != "":
		return Version + " (" + Date + ")"
	default:
		return Version
	}
}
