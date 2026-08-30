package cli

import (
	"flag"
	"os"
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/policy"
	"github.com/stretchr/testify/assert"
)

func TestSplitSubcommand(t *testing.T) {
	tests := []struct {
		name     string
		argv     []string
		wantSub  string
		wantRest []string
	}{
		{name: "no arguments", argv: nil, wantSub: "", wantRest: nil},
		{name: "check", argv: []string{"check", "-u"}, wantSub: "check", wantRest: []string{"-u"}},
		{name: "help", argv: []string{"help"}, wantSub: "help", wantRest: []string{}},
		{name: "version", argv: []string{"version"}, wantSub: "version", wantRest: []string{}},
		{name: "self-update", argv: []string{"self-update"}, wantSub: "self-update", wantRest: []string{}},
		{name: "check-update", argv: []string{"check-update"}, wantSub: "check-update", wantRest: []string{}},
		{name: "config", argv: []string{"config", "-d", "/srv"}, wantSub: "config", wantRest: []string{"-d", "/srv"}},
		// A leading flag is never a subcommand, and an unknown word is handed
		// back so it keeps being ignored rather than changing the mode.
		{name: "leading flag", argv: []string{"-d", "check"}, wantSub: "", wantRest: []string{"-d", "check"}},
		{name: "unknown word", argv: []string{"nonsense"}, wantSub: "", wantRest: []string{"nonsense"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, rest := splitSubcommand(tt.argv)
			if sub != tt.wantSub {
				t.Errorf("splitSubcommand(%q) sub = %q, expected %q", tt.argv, sub, tt.wantSub)
			}
			if len(rest) != len(tt.wantRest) {
				t.Fatalf("splitSubcommand(%q) rest = %q, expected %q", tt.argv, rest, tt.wantRest)
			}
			for i := range rest {
				if rest[i] != tt.wantRest[i] {
					t.Errorf("splitSubcommand(%q) rest[%d] = %q, expected %q", tt.argv, i, rest[i], tt.wantRest[i])
				}
			}
		})
	}
}

// TestParseMode covers which mode an invocation selects: the TUI (Check false)
// is what a bare run means now, and everything that only the report reads has
// to end up in the report either way.
func TestParseMode(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantCheck       bool
		wantLegacyPlain bool
	}{
		{name: "bare run means the TUI", args: []string{}},
		{name: "-i still means the TUI", args: []string{"-i"}},
		{name: "-d alone stays interactive", args: []string{"-d", "/srv"}},
		{name: "check subcommand", args: []string{"check"}, wantCheck: true},
		{name: "check with flags", args: []string{"check", "-u", "-r"}, wantCheck: true},
		{name: "bare -u infers the report", args: []string{"-u"}, wantCheck: true, wantLegacyPlain: true},
		{name: "bare -f infers the report", args: []string{"-f"}, wantCheck: true, wantLegacyPlain: true},
		{name: "bare -patch infers the report", args: []string{"-patch"}, wantCheck: true, wantLegacyPlain: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = append([]string{"ccu"}, tt.args...)

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			result := Parse("test")
			if result.Check != tt.wantCheck {
				t.Errorf("Parse(%q).Check = %v, expected %v", tt.args, result.Check, tt.wantCheck)
			}
			if result.LegacyPlain != tt.wantLegacyPlain {
				t.Errorf("Parse(%q).LegacyPlain = %v, expected %v", tt.args, result.LegacyPlain, tt.wantLegacyPlain)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected Flags
	}{
		{
			name: "default values",
			args: []string{},
			expected: Flags{
				Help:        false,
				Update:      false,
				Restart:     false,
				Interactive: false,
				Directory:   ".",
				Full:        false,
				Major:       false,
				Minor:       false,
				Patch:       true,
				Exclude:     []string{},
			},
		},
		{
			name: "update flag",
			args: []string{"-u"},
			expected: Flags{
				Update:  true,
				Patch:   true,
				Exclude: []string{},
			},
		},
		{
			name: "full flag",
			args: []string{"-f"},
			expected: Flags{
				Full:    true,
				Major:   true,
				Minor:   true,
				Patch:   true,
				Exclude: []string{},
			},
		},
		{
			name: "directory flag",
			args: []string{"-d", "/path/to/dir"},
			expected: Flags{
				Directory: "/path/to/dir",
				Patch:     true,
				Exclude:   []string{},
			},
		},
		{
			name: "exclude single directory",
			args: []string{"-exclude", "dir1"},
			expected: Flags{
				Patch:   true,
				Exclude: []string{"dir1"},
			},
		},
		{
			name: "exclude multiple directories",
			args: []string{"-exclude", "dir1,dir2,dir3"},
			expected: Flags{
				Patch:   true,
				Exclude: []string{"dir1", "dir2", "dir3"},
			},
		},
		{
			name: "exclude with spaces",
			args: []string{"-exclude", "dir1, dir2 , dir3 "},
			expected: Flags{
				Patch:   true,
				Exclude: []string{"dir1", "dir2", "dir3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save the original command-line arguments and restore them after the test
			origArgs := os.Args
			defer func() { os.Args = origArgs }()

			os.Args = append([]string{"cmd"}, tt.args...)

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			exitCode := 0
			flag.CommandLine.Usage = func() {
				exitCode = 2
			}
			err := flag.CommandLine.Parse(os.Args[1:])
			if err != nil {
				exitCode = 2
			}

			result := Parse("test")
			if exitCode != 0 {
				return
			}

			// Compare the parsed fields individually (avoiding direct struct comparison)
			if result.Help != tt.expected.Help {
				t.Errorf("Parse().Help = %v, expected %v", result.Help, tt.expected.Help)
			}
			if result.Update != tt.expected.Update {
				t.Errorf("Parse().Update = %v, expected %v", result.Update, tt.expected.Update)
			}
			if result.Restart != tt.expected.Restart {
				t.Errorf("Parse().Restart = %v, expected %v", result.Restart, tt.expected.Restart)
			}
			if result.Interactive != tt.expected.Interactive {
				t.Errorf("Parse().Interactive = %v, expected %v", result.Interactive, tt.expected.Interactive)
			}
			if result.Directory != tt.expected.Directory {
				t.Errorf("Parse().Directory = %v, expected %v", result.Directory, tt.expected.Directory)
			}
			if result.Full != tt.expected.Full {
				t.Errorf("Parse().Full = %v, expected %v", result.Full, tt.expected.Full)
			}
			if result.Major != tt.expected.Major {
				t.Errorf("Parse().Major = %v, expected %v", result.Major, tt.expected.Major)
			}
			if result.Minor != tt.expected.Minor {
				t.Errorf("Parse().Minor = %v, expected %v", result.Minor, tt.expected.Minor)
			}
			if result.Patch != tt.expected.Patch {
				t.Errorf("Parse().Patch = %v, expected %v", result.Patch, tt.expected.Patch)
			}
			if result.Version != tt.expected.Version {
				t.Errorf("Parse().Version = %v, expected %v", result.Version, tt.expected.Version)
			}
			// Compare exclude slices
			if len(result.Exclude) != len(tt.expected.Exclude) {
				t.Errorf("Parse().Exclude length = %v, expected %v", len(result.Exclude), len(tt.expected.Exclude))
			} else {
				for i, exclude := range result.Exclude {
					if exclude != tt.expected.Exclude[i] {
						t.Errorf("Parse().Exclude[%d] = %v, expected %v", i, exclude, tt.expected.Exclude[i])
					}
				}
			}
		})
	}
}

// -pin-floating applies to both modes, so naming it must not infer the report,
// and whether it was spelled out at all has to survive parsing: a plain false
// means "the command line said nothing" and leaves the config in charge, while
// -pin-floating=false is a run opting out of `pin_floating: true`.
func TestParsePinFloating(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantOn   bool
		wantSet  bool
		wantMode bool // Check
	}{
		{name: "absent"},
		{name: "on", args: []string{"-pin-floating"}, wantOn: true, wantSet: true},
		{name: "explicitly off", args: []string{"-pin-floating=false"}, wantSet: true},
		// Read outside the mode-inference block, so `check` in front of it is seen.
		{name: "off under check", args: []string{"check", "-pin-floating=false"}, wantSet: true, wantMode: true},
		{name: "on under check", args: []string{"check", "-pin-floating"}, wantOn: true, wantSet: true, wantMode: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = append([]string{"ccu"}, tt.args...)

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			result := Parse("test")
			if result.PinFloating != tt.wantOn {
				t.Errorf("Parse(%q).PinFloating = %v, expected %v", tt.args, result.PinFloating, tt.wantOn)
			}
			if result.PinFloatingSet != tt.wantSet {
				t.Errorf("Parse(%q).PinFloatingSet = %v, expected %v", tt.args, result.PinFloatingSet, tt.wantSet)
			}
			if result.Check != tt.wantMode {
				t.Errorf("Parse(%q).Check = %v, expected %v", tt.args, result.Check, tt.wantMode)
			}
		})
	}
}

// Each subcommand sets exactly one mode flag, and nothing else: `ccu config`
// opening the TUI, or `ccu check-update` running a scan, would be the kind of
// mix-up that only shows up in front of a user.
func TestParseSubcommandModes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Flags
	}{
		{name: "check", args: []string{"check"}, want: Flags{Check: true}},
		{name: "self-update", args: []string{"self-update"}, want: Flags{SelfUpdate: true}},
		{name: "check-update", args: []string{"check-update"}, want: Flags{CheckUpdate: true}},
		{name: "config", args: []string{"config"}, want: Flags{ShowConfig: true}},
		// `config -image nginx` narrows the explanation to one image, so the
		// name has to survive alongside the mode the subcommand picked.
		{name: "config for one image", args: []string{"config", "-image", "nginx"}, want: Flags{ShowConfig: true, Image: "nginx"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = append([]string{"ccu"}, tt.args...)

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			result := Parse("test")
			assert.Equal(t, tt.want.Check, result.Check)
			assert.Equal(t, tt.want.SelfUpdate, result.SelfUpdate)
			assert.Equal(t, tt.want.CheckUpdate, result.CheckUpdate)
			assert.Equal(t, tt.want.ShowConfig, result.ShowConfig)
			assert.Equal(t, tt.want.Image, result.Image)
			// None of these is the legacy spelling main warns about.
			assert.False(t, result.LegacyPlain)
		})
	}
}

// -format only ever describes the report, so naming a real format states the
// mode as plainly as `check` does — but without the legacy warning, since
// there was never an older spelling of it.
func TestParseFormatInfersTheReport(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantFormat      string
		wantCheck       bool
		wantLegacyPlain bool
	}{
		{name: "no format leaves the TUI", args: []string{}, wantFormat: "auto"},
		// "auto" is the default, so spelling it out says nothing about the mode.
		{name: "explicit auto is still the default", args: []string{"-format=auto"}, wantFormat: "auto"},
		{name: "json infers the report", args: []string{"-format=json"}, wantFormat: "json", wantCheck: true},
		{name: "pretty infers the report", args: []string{"-format=pretty"}, wantFormat: "pretty", wantCheck: true},
		// Under `check` the mode was already stated; the format just carries.
		{name: "json under check", args: []string{"check", "-format=json"}, wantFormat: "json", wantCheck: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = append([]string{"ccu"}, tt.args...)

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			result := Parse("test")
			assert.Equal(t, tt.wantFormat, result.Format)
			assert.Equal(t, tt.wantCheck, result.Check)
			assert.Equal(t, tt.wantLegacyPlain, result.LegacyPlain)
		})
	}
}

// -dockerfiles defaults to on, so its value alone cannot say whether the user
// or the default asked for it; only DockerfilesSet can, and that is what lets
// -dockerfiles=false override a config that turned it on.
func TestParseDockerfiles(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantOn    bool
		wantSet   bool
		wantCheck bool
	}{
		{name: "absent keeps the default on", args: []string{}, wantOn: true},
		{name: "explicitly on", args: []string{"-dockerfiles"}, wantOn: true, wantSet: true},
		{name: "explicitly off", args: []string{"-dockerfiles=false"}, wantSet: true},
		// Read outside the mode-inference block, so `check` in front of it is seen.
		{name: "off under check", args: []string{"check", "-dockerfiles=false"}, wantSet: true, wantCheck: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = append([]string{"ccu"}, tt.args...)

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			result := Parse("test")
			assert.Equal(t, tt.wantOn, result.Dockerfiles)
			assert.Equal(t, tt.wantSet, result.DockerfilesSet)
			// Both modes read it, so naming it must not force the report.
			assert.Equal(t, tt.wantCheck, result.Check)
		})
	}
}

// -versioning is passed through unvalidated: Parse cannot report an error, so
// an unknown scheme has to reach the layer that resolves it, and an absent one
// has to stay empty rather than becoming a default that outranks the config.
func TestParseVersioning(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want policy.Versioning
	}{
		{name: "absent leaves the config in charge", args: []string{}, want: ""},
		{name: "semver", args: []string{"-versioning", "semver"}, want: policy.VersioningSemver},
		{name: "loose", args: []string{"-versioning", "loose"}, want: policy.VersioningLoose},
		{name: "unknown scheme is passed on unchanged", args: []string{"-versioning", "nonsense"}, want: policy.Versioning("nonsense")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = append([]string{"ccu"}, tt.args...)

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			result := Parse("test")
			assert.Equal(t, tt.want, result.Versioning)
			// A scheme applies to both modes, so it must not infer the report.
			assert.False(t, result.Check)
		})
	}
}

// -config names a file to read instead of searching, and -exclude with nothing
// in it must not produce a one-element list containing the empty string: that
// would exclude everything under a relative root.
func TestParseConfigAndEmptyExclude(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"ccu", "-config", "/etc/ccu.yaml", "-exclude", ""}

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	result := Parse("test")
	assert.Equal(t, "/etc/ccu.yaml", result.Config)
	assert.Empty(t, result.Exclude)
}
