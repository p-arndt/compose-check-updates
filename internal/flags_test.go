package internal

import (
	"flag"
	"os"
	"testing"
)

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

			// Set the command-line arguments for the test
			os.Args = append([]string{"cmd"}, tt.args...)

			// Reset the flags to their default state
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			// Parse the flags
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
