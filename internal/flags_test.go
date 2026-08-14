package internal

import (
	"flag"
	"os"
	"testing"
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
		expected CCUFlags
	}{
		{
			name: "default values",
			args: []string{},
			expected: CCUFlags{
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
			expected: CCUFlags{
				Update:  true,
				Patch:   true,
				Exclude: []string{},
			},
		},
		{
			name: "full flag",
			args: []string{"-f"},
			expected: CCUFlags{
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
			expected: CCUFlags{
				Directory: "/path/to/dir",
				Patch:     true,
				Exclude:   []string{},
			},
		},
		{
			name: "exclude single directory",
			args: []string{"-exclude", "dir1"},
			expected: CCUFlags{
				Patch:   true,
				Exclude: []string{"dir1"},
			},
		},
		{
			name: "exclude multiple directories",
			args: []string{"-exclude", "dir1,dir2,dir3"},
			expected: CCUFlags{
				Patch:   true,
				Exclude: []string{"dir1", "dir2", "dir3"},
			},
		},
		{
			name: "exclude with spaces",
			args: []string{"-exclude", "dir1, dir2 , dir3 "},
			expected: CCUFlags{
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
