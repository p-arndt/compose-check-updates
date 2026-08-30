// Package buildinfo exposes ccu's own version, stamped in at release time with
// -ldflags from the repo-root VERSION file. See the `build-release` recipe in
// the justfile.
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

// String reports "1.2.3 (abc1234, 2026-07-01T12:00:00Z)" for a stamped build,
// degrading to bare "dev" rather than printing empty parentheses.
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
