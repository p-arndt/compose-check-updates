package internal

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

type CCUFlags struct {
	Help        bool   // Show help message
	Update      bool   // Update the Docker Compose files with the new image tags
	Restart     bool   // Restart the services after updating the Docker Compose files
	Interactive bool   // Interactively choose which docker images to update
	Check       bool   // Run the non-interactive report instead of the TUI
	LegacyPlain bool   // Check was inferred from a report-only flag rather than the `check` subcommand
	Directory   string // Root directory to search for Docker Compose files
	Full        bool   // Update to the latest semver version
	Major       bool   // Update to the latest major version
	Minor       bool   // Update to the latest minor version
	Patch       bool   // Update to the latest patch version
	PinFloating bool   // Pin floating tags to the digest they currently resolve to
	// PinFloatingSet records whether -pin-floating was actually passed, so that
	// -pin-floating=false can override a config that turned it on. A plain false
	// means "the command line said nothing", which the config still decides.
	PinFloatingSet bool
	Dockerfiles    bool // Check the base images of the Dockerfiles compose services build
	// DockerfilesSet records whether -dockerfiles was actually passed, for the
	// same reason PinFloatingSet does: this one defaults to on, so a plain true
	// says nothing about whether the user or the default asked for it.
	DockerfilesSet bool
	Version        bool     // Version of ccu
	SelfUpdate     bool     // Download and install the latest version of ccu
	CheckUpdate    bool     // Check whether a newer version of ccu is available, without installing it
	Exclude        []string // Directories to exclude from search
	ExcludeStr     string   // Comma-separated list of directories to exclude from search (flag only)
	Config         string   // Explicit config file to read instead of searching for one
	Format         string   // Output format of the report: auto, pretty or json
	ShowConfig     bool     // Print the resolved configuration and where it came from
}

// plainOnlyFlags are the flags that only the non-interactive report reads: the
// TUI resolves every level itself and applies on a keypress. Naming one of them
// therefore states which mode was meant, even when `check` was left out.
var plainOnlyFlags = map[string]bool{
	"u": true, "r": true, "f": true, "major": true, "minor": true, "patch": true,
}

// splitSubcommand pulls a leading subcommand off the argument list. Each picks a
// mode of its own and ignores the options the other modes read, so a subcommand
// states the shape of an invocation better than a flag would. Anything else is
// handed back untouched.
func splitSubcommand(argv []string) (sub string, rest []string) {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		return "", argv
	}
	switch argv[0] {
	case "check", "self-update", "check-update", "config", "help", "version":
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
	// The TUI is what a bare `ccu` runs now, so -i no longer selects anything;
	// it stays registered, and hidden from the usage text, so the invocation it
	// used to name keeps working.
	flag.BoolVar(&args.Interactive, "i", false, "")
	flag.StringVar(&args.Directory, "d", ".", "Root directory to search for Docker Compose files")
	flag.BoolVar(&args.Full, "f", false, "Consider every newer version, not just patches")
	flag.BoolVar(&args.Major, "major", false, "Update to the latest major version")
	flag.BoolVar(&args.Minor, "minor", false, "Update to the latest minor version")
	flag.BoolVar(&args.Patch, "patch", true, "Update to the latest patch version")
	flag.BoolVar(&args.PinFloating, "pin-floating", false, "Pin floating tags (latest, main, ...) to the digest they resolve to")
	flag.BoolVar(&args.Dockerfiles, "dockerfiles", true, "Also check the base images of Dockerfiles built by a compose service")
	flag.BoolVar(&args.Version, "v", false, "Show version information")
	// Kept registered so existing scripts keep working, hidden from the usage text
	// so only the subcommand form is taught. Unlike -v and -h below, neither is
	// handled here: they talk to the network and Parse cannot report a failure.
	flag.BoolVar(&args.SelfUpdate, "self-update", false, "")
	flag.BoolVar(&args.CheckUpdate, "check-update", false, "")
	flag.StringVar(&args.ExcludeStr, "exclude", "", "Comma-separated list of directories to exclude from search")
	flag.StringVar(&args.Config, "config", "", "Read this config file instead of searching for one")
	flag.StringVar(&args.Format, "format", "auto", "Report output format: auto, pretty or json")

	flag.Usage = func() { usage(flag.CommandLine.Output()) }
	flag.CommandLine.Parse(rest)

	// Not a subcommand, and flag parsing stopped at it — `ccu nonsense -d /srv`
	// would otherwise silently ignore the -d and scan the wrong directory.
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", flag.Arg(0))
		usage(os.Stderr)
		os.Exit(2)
	}

	switch sub {
	case "check":
		args.Check = true
	case "self-update":
		args.SelfUpdate = true
	case "check-update":
		args.CheckUpdate = true
	case "config":
		args.ShowConfig = true
	case "help":
		args.Help = true
	case "version":
		args.Version = true
	}

	// A report-only flag without `check` is what every pre-TUI-default script looks
	// like, and `ccu -u` in a cron entry would hang on a terminal that is not
	// there. So the implied mode is honoured and main names the new spelling once.
	// Not for -i, which means the TUI and is the default anyway.
	if !args.Check {
		flag.Visit(func(f *flag.Flag) {
			if plainOnlyFlags[f.Name] {
				args.Check, args.LegacyPlain = true, true
			}
			// -format only ever describes the report, so asking for one names the
			// mode as plainly as `check` does, and there is no old spelling to warn
			// about. Without this, `ccu -format=json` would open the TUI instead.
			if f.Name == "format" && args.Format != "auto" {
				args.Check = true
			}
		})
	}

	// Read outside the block above, which only runs when the mode is still open:
	// `ccu check -pin-floating=false` has to be seen as well.
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "pin-floating" {
			args.PinFloatingSet = true
		}
		if f.Name == "dockerfiles" {
			args.DockerfilesSet = true
		}
	})

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

	if args.ExcludeStr != "" {
		args.Exclude = strings.Split(args.ExcludeStr, ",")
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
	// The invocation name only, not the path it happened to be started from:
	// help text that reads `/tmp/build/ccu check` teaches the wrong command.
	name := filepath.Base(os.Args[0])

	// A tabwriter rather than plain tabs, so the descriptions line up whatever
	// the longest command or flag name turns out to be.
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Usage:\n  %s\tPick what to update in the full-screen TUI\n", name)
	fmt.Fprintf(tw, "  %s <command>\tRun one of the commands below\n\nCommands:\n", name)
	fmt.Fprintln(tw, "  check\tReport the available updates without the TUI, and optionally apply them")
	fmt.Fprintln(tw, "  self-update\tDownload and install the latest version of ccu")
	fmt.Fprintln(tw, "  check-update\tCheck whether a newer version of ccu is available, without installing it")
	fmt.Fprintln(tw, "  config\tShow the resolved configuration and the files it was read from")
	fmt.Fprintln(tw, "  help\tShow this help message")
	fmt.Fprintln(tw, "  version\tShow version information")
	fmt.Fprintf(tw, "\nFlags (-d, -exclude, -config and -pin-floating apply to both modes, the rest only to `%s check`):\n", name)
	flag.VisitAll(func(f *flag.Flag) {
		// An empty usage string marks a flag kept only for backwards
		// compatibility; see the registrations in Parse.
		if f.Usage == "" {
			return
		}
		fmt.Fprintf(tw, "  -%s\t%s", f.Name, f.Usage)
		if f.DefValue != "" && f.DefValue != "false" {
			fmt.Fprintf(tw, " (default %s)", f.DefValue)
		}
		fmt.Fprintln(tw)
	})
	tw.Flush()
}
