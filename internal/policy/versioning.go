package policy

import "slices"

// Versioning names the scheme an image's tags are read as versions under.
type Versioning string

const (
	// VersioningSemver is the default: at most three segments, no leading zeros.
	VersioningSemver Versioning = "semver"
	// VersioningLoose reads up to six numeric segments and tolerates the leading
	// zeros a calendar tag brings with it: "2026.7.7.2" orders after "2026.7.7".
	VersioningLoose Versioning = "loose"
	// VersioningRegex reads tags with a pattern written beside it, for tags
	// neither of the above can read — "2024-01-01" above all, which loose reads
	// as release 2024 with the date as a suffix and so orders by month name.
	VersioningRegex Versioning = "regex"
)

var versionings = []Versioning{VersioningSemver, VersioningLoose, VersioningRegex}

func (v Versioning) Valid() bool { return slices.Contains(versionings, v) }

func (v Versioning) String() string { return string(v) }

// Versionings lists every scheme a config may name. The result is a copy:
// handing out the backing slice would let a caller that sorts or writes into it
// change what Valid accepts.
func Versionings() []Versioning { return slices.Clone(versionings) }
