package cli

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderUsage registers the flags the way Parse does and captures the help text
// it produces. usage reads the global flag set, so the registration has to have
// happened first — otherwise the flag section comes out empty and every
// assertion below would pass for the wrong reason.
func renderUsage(t *testing.T, invocation string) string {
	t.Helper()

	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{invocation}

	flag.CommandLine = flag.NewFlagSet(invocation, flag.ContinueOnError)
	Parse("test")

	var buf bytes.Buffer
	usage(&buf)
	return buf.String()
}

// TestUsageDocumentsEveryVisibleFlag is the point of testing the help text: a
// flag added to Parse but left out of the usage output is a feature nobody can
// find. Asserting per flag rather than against a golden blob keeps a reworded
// description from failing the test.
func TestUsageDocumentsEveryVisibleFlag(t *testing.T) {
	out := renderUsage(t, "ccu")

	var documented, hidden []string
	flag.VisitAll(func(f *flag.Flag) {
		// An empty usage string is how Parse marks a flag kept only so old
		// invocations keep working; those are deliberately not taught.
		if f.Usage == "" {
			hidden = append(hidden, f.Name)
			return
		}
		documented = append(documented, f.Name)
	})
	require.NotEmpty(t, documented, "no flags were registered, so the assertions below prove nothing")

	for _, name := range documented {
		t.Run("documents -"+name, func(t *testing.T) {
			// Anchored to a line start so -i cannot be satisfied by -image.
			assert.Regexp(t, regexp.MustCompile(`(?m)^\s+-`+regexp.QuoteMeta(name)+`\b`), out)
		})
	}

	for _, name := range hidden {
		t.Run("omits -"+name, func(t *testing.T) {
			assert.NotRegexp(t, regexp.MustCompile(`(?m)^\s+-`+regexp.QuoteMeta(name)+`\b`), out)
		})
	}
}

// Every word splitSubcommand accepts picks a mode of its own, so leaving one
// out of the Commands section hides a whole mode from the user.
func TestUsageDocumentsEverySubcommand(t *testing.T) {
	tests := []struct {
		name string
		sub  string
	}{
		{name: "check", sub: "check"},
		{name: "self-update", sub: "self-update"},
		{name: "check-update", sub: "check-update"},
		{name: "config", sub: "config"},
		{name: "help", sub: "help"},
		{name: "version", sub: "version"},
	}

	out := renderUsage(t, "ccu")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The subcommand is listed on a line of its own, without a dash:
			// the flag section must not be what satisfies this.
			assert.Regexp(t, regexp.MustCompile(`(?m)^\s+`+regexp.QuoteMeta(tt.sub)+`\b`), out)

			// And the word really is a subcommand, not just documentation.
			sub, _ := splitSubcommand([]string{tt.sub})
			assert.Equal(t, tt.sub, sub)
		})
	}
}

// The help text teaches a command the reader can type, so it names the binary
// as invoked and never the path it happened to be started from.
func TestUsageNamesTheInvocationOnly(t *testing.T) {
	out := renderUsage(t, "/tmp/build/output/ccu")

	assert.NotContains(t, out, "/tmp/build/output")
	assert.Contains(t, out, "ccu <command>")
}

// A default worth knowing about is printed; "false" is not, because an absent
// boolean flag already reads as off and "(default false)" is only noise.
func TestUsageShowsMeaningfulDefaults(t *testing.T) {
	out := renderUsage(t, "ccu")

	assert.Regexp(t, regexp.MustCompile(`(?m)^\s+-d\b.*\(default \.\)`), out)
	assert.Regexp(t, regexp.MustCompile(`(?m)^\s+-format\b.*\(default auto\)`), out)
	assert.NotContains(t, out, "(default false)")
}

// subprocessEnv carries the argument list into the re-executed test binary.
// Parse ends these paths with os.Exit, which a normal test cannot survive.
const subprocessEnv = "CCU_CLI_TEST_ARGV"

// TestParseExitPaths covers the three invocations Parse answers by exiting:
// they are the ones a user hits first, and the exit code is what scripts read.
func TestParseExitPaths(t *testing.T) {
	if argv := os.Getenv(subprocessEnv); argv != "" {
		os.Args = append([]string{"ccu"}, strings.Fields(argv)...)
		flag.CommandLine = flag.NewFlagSet("ccu", flag.ContinueOnError)
		Parse("9.9.9")
		return
	}

	tests := []struct {
		name         string
		argv         string
		wantExitCode int
		wantOutput   []string
	}{
		{
			// Without this, `ccu nonsense -d /srv` would drop the -d and scan
			// the working directory instead of the one that was asked for.
			name:         "unknown command",
			argv:         "nonsense -d /srv",
			wantExitCode: 2,
			wantOutput:   []string{`unknown command "nonsense"`, "Usage:", "Commands:"},
		},
		{
			name:         "version subcommand",
			argv:         "version",
			wantExitCode: 0,
			wantOutput:   []string{"9.9.9"},
		},
		{
			name:         "help subcommand",
			argv:         "help",
			wantExitCode: 0,
			wantOutput:   []string{"Usage:", "Commands:", "Flags"},
		},
		{
			name:         "-h prints the same help",
			argv:         "-h",
			wantExitCode: 0,
			wantOutput:   []string{"Usage:", "Commands:", "Flags"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestParseExitPaths$")
			cmd.Env = append(os.Environ(), subprocessEnv+"="+tt.argv)
			out, err := cmd.CombinedOutput()

			if tt.wantExitCode == 0 {
				require.NoError(t, err, "output: %s", out)
			} else {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, "output: %s", out)
				assert.Equal(t, tt.wantExitCode, exitErr.ExitCode())
			}
			for _, want := range tt.wantOutput {
				assert.Contains(t, string(out), want)
			}
		})
	}
}
