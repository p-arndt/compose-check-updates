package internal

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type CCUFlags struct {
	Help        bool     // Show help message
	Update      bool     // Update the Docker Compose files with the new image tags
	Restart     bool     // Restart the services after updating the Docker Compose files
	Interactive bool     // Interactively choose which docker images to update
	Directory   string   // Root directory to search for Docker Compose files
	Full        bool     // Update to the latest semver version
	Major       bool     // Update to the latest major version
	Minor       bool     // Update to the latest minor version
	Patch       bool     // Update to the latest patch version
	Version     bool     // Version of ccu
	SelfUpdate  bool     // Download and install the latest version of ccu
	CheckUpdate bool     // Check whether a newer version of ccu is available, without installing it
	Exclude     []string // Directories to exclude from search
	ExcludeStr  string   // Comma-separated list of directories to exclude from search (flag only)
}

// splitSubcommand pulls a leading subcommand off the argument list. These two
// actions operate on ccu itself rather than on the user's compose files, and
// they ignore every scan option — `ccu self-update -d /srv` never had a
// meaning — so a subcommand states the shape of the invocation better than a
// flag that silently coexists with flags it will not read.
//
// Anything else is handed back untouched, which keeps an unrecognised bare
// argument doing exactly what it did before: nothing.
func splitSubcommand(argv []string) (sub string, rest []string) {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		return "", argv
	}
	switch argv[0] {
	case "self-update", "check-update":
		return argv[0], argv[1:]
	}
	return "", argv
}

func Parse(version string) CCUFlags {
	args := CCUFlags{}

	sub, rest := splitSubcommand(os.Args[1:])

	flag.BoolVar(&args.Help, "h", false, "Show help message")
	flag.BoolVar(&args.Update, "u", false, "Update the Docker Compose files with the new image tags")
	flag.BoolVar(&args.Restart, "r", false, "Restart the services after updating the Docker Compose files")
	flag.BoolVar(&args.Interactive, "i", false, "Interactively choose which docker images to update")
	flag.StringVar(&args.Directory, "d", ".", "Root directory to search for Docker Compose files")
	flag.BoolVar(&args.Full, "f", false, "Update to the latest major version")
	flag.BoolVar(&args.Major, "major", false, "Update to the latest semver version")
	flag.BoolVar(&args.Minor, "minor", false, "Update to the latest minor version")
	flag.BoolVar(&args.Patch, "patch", true, "Update to the latest patch version")
	flag.BoolVar(&args.Version, "v", false, "Show version information")
	// The subcommand spellings are the documented ones; these two flags stay
	// registered so the invocations in existing scripts and cron entries keep
	// working, and are hidden from the usage text so only one form is taught.
	// Unlike -v and -h below, neither is handled here: they talk to the network
	// and can fail, and Parse has no way to report that.
	flag.BoolVar(&args.SelfUpdate, "self-update", false, "")
	flag.BoolVar(&args.CheckUpdate, "check-update", false, "")
	flag.StringVar(&args.ExcludeStr, "exclude", "", "Comma-separated list of directories to exclude from search")

	flag.Usage = func() { usage(flag.CommandLine.Output()) }
	flag.CommandLine.Parse(rest)

	switch sub {
	case "self-update":
		args.SelfUpdate = true
	case "check-update":
		args.CheckUpdate = true
	}

	if args.Version {
		println("Version:", version)
		os.Exit(0)
	}

	if args.Help {
		flag.Usage()
		os.Exit(0)
	}

	if args.Full {
		args.Major = true
		args.Minor = true
		args.Patch = true
	}

	// Process exclude flag - split comma-separated string into slice
	if args.ExcludeStr != "" {
		args.Exclude = strings.Split(args.ExcludeStr, ",")
		// Trim whitespace from each exclude path
		for i := range args.Exclude {
			args.Exclude[i] = strings.TrimSpace(args.Exclude[i])
		}
	}

	return args
}

// usage replaces flag.PrintDefaults so the subcommands are documented alongside
// the flags, and so the deprecated -self-update / -check-update spellings are
// left out: they still work, but a help text listing both forms would suggest
// there is a difference between them.
func usage(w io.Writer) {
	fmt.Fprintf(w, "Usage:\n  %s [flags]\n  %s <command>\n\nCommands:\n", os.Args[0], os.Args[0])
	fmt.Fprintln(w, "  self-update\tDownload and install the latest version of ccu")
	fmt.Fprintln(w, "  check-update\tCheck whether a newer version of ccu is available, without installing it")
	fmt.Fprintln(w, "\nFlags:")
	flag.VisitAll(func(f *flag.Flag) {
		// An empty usage string marks a flag kept only for backwards
		// compatibility; see the registrations in Parse.
		if f.Usage == "" {
			return
		}
		fmt.Fprintf(w, "  -%s\t%s", f.Name, f.Usage)
		if f.DefValue != "" && f.DefValue != "false" {
			fmt.Fprintf(w, " (default %s)", f.DefValue)
		}
		fmt.Fprintln(w)
	})
}
