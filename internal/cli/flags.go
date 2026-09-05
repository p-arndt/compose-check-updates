// Package cli reads the command line.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// Flags is what the command line said, before any config file is merged into
// it. It stays a plain record of the invocation: the fields that end in Set
// exist so a later layer can tell "the user asked" from "this is the default".
type Flags struct {
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
	// Refresh ignores every cached registry answer for this run. The escape hatch
	// for "a release went out a minute ago": the cache is short-lived by design,
	// but this makes the run ask the registries about everything regardless.
	Refresh     bool
	Version     bool     // Version of ccu
	SelfUpdate  bool     // Download and install the latest version of ccu
	CheckUpdate bool     // Check whether a newer version of ccu is available, without installing it
	Exclude     []string // Directories to exclude from search
	Config      string   // Explicit config file to read instead of searching for one
	Format      string   // Output format of the report: auto, pretty or json
	ShowConfig  bool     // Print the resolved configuration and where it came from
	// Image narrows the `config` command to a single image: instead of the merged
	// result it then explains how that image's settings were resolved and which
	// layer produced each of them. Empty means the whole configuration.
	Image string
	// Images narrows a scan to the images these patterns match. Deliberately
	// flag-only, with no config key behind it: it selects what this one run looks
	// at, and a selection written down in a file would silently hide images from
	// every later run.
	Images []string
	// MinAge is how long a tag has to have been published before it is offered,
	// as a duration ("7d", "36h"). Empty leaves the config to decide; per-image
	// entries still win, for the same reason they do over -versioning.
	MinAge string
	// Versioning overrides the default scheme image tags are read under for this
	// run. Empty leaves the config, and failing that `semver`, to decide.
	// Per-image entries still win: naming an image is the more specific statement.
	Versioning policy.Versioning
}

// commaList collects a flag that may be repeated and may carry a
// comma-separated list, so `-image a,b` and `-image a -image b` are the same
// selection, and `-exclude a,b` needs no parsing of its own. flag's own
// StringVar would keep only the last one.
type commaList []string

func (l *commaList) String() string {
	return strings.Join(*l, ",")
}

func (l *commaList) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*l = append(*l, part)
		}
	}
	return nil
}

// plainOnlyFlags are the flags that only the non-interactive report reads: the
// TUI resolves every level itself and applies on a keypress. Naming one of them
// therefore states which mode was meant, even when `check` was left out.
var plainOnlyFlags = map[string]bool{
	"u": true, "r": true, "f": true, "major": true, "minor": true, "patch": true,
}

// subcommands are the leading words that pick a mode of their own, each with
// what it sets. One table rather than a list and a switch elsewhere: a command
// present in only one of the two would either parse and then do nothing, or be
// rejected as unknown while still being handled.
var subcommands = map[string]func(*Flags){
	"check":        func(args *Flags) { args.Check = true },
	"self-update":  func(args *Flags) { args.SelfUpdate = true },
	"check-update": func(args *Flags) { args.CheckUpdate = true },
	"config":       func(args *Flags) { args.ShowConfig = true },
	"help":         func(args *Flags) { args.Help = true },
	"version":      func(args *Flags) { args.Version = true },
}

// splitSubcommand pulls a leading subcommand off the argument list. A
// subcommand ignores the options the others read, which a flag cannot say as
// plainly. Anything else is handed back untouched.
func splitSubcommand(argv []string) (sub string, rest []string) {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		return "", argv
	}
	if _, ok := subcommands[argv[0]]; ok {
		return argv[0], argv[1:]
	}
	return "", argv
}

// Parse reads the invocation and returns it as Flags. It exits the process for
// the requests that are answered here and nowhere else — -v, -h and an unknown
// command — so callers only ever see an invocation that means to do work.
func Parse(version string) Flags {
	args := Flags{}
	versioningName := ""

	sub, rest := splitSubcommand(os.Args[1:])

	registerFlags(&args, &versioningName)

	flag.Usage = func() { usage(flag.CommandLine.Output()) }
	// flag.CommandLine is an ExitOnError set: a parse failure prints the usage
	// and exits, so the returned error is always nil by the time we get here.
	_ = flag.CommandLine.Parse(rest)

	// Not a subcommand, and flag parsing stopped at it — `ccu nonsense -d /srv`
	// would otherwise silently ignore the -d and scan the wrong directory.
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", flag.Arg(0))
		usage(os.Stderr)
		os.Exit(2)
	}

	applySubcommand(&args, sub)
	inferMode(&args)

	if args.Version {
		println("Version:", version)
		os.Exit(0)
	}

	if args.Help {
		flag.Usage()
		os.Exit(0)
	}

	expandFlags(&args, versioningName)

	return args
}

// registerFlags declares every option on the global flag set, which is also the
// one usage walks to print them — a flag registered anywhere else would parse
// but never be documented.
func registerFlags(args *Flags, versioningName *string) {
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
	flag.BoolVar(&args.Refresh, "refresh", false, "Ignore the cached registry answers and ask every registry again")
	flag.BoolVar(&args.Version, "v", false, "Show version information")
	// Kept registered so existing scripts keep working, hidden from the usage text
	// so only the subcommand form is taught. Unlike -v and -h, which Parse acts on,
	// neither is handled here: they talk to the network and Parse cannot report a
	// failure.
	flag.BoolVar(&args.SelfUpdate, "self-update", false, "")
	flag.BoolVar(&args.CheckUpdate, "check-update", false, "")
	flag.Var((*commaList)(&args.Exclude), "exclude", "Comma-separated list of directories to exclude from search")
	flag.StringVar(&args.Config, "config", "", "Read this config file instead of searching for one")
	flag.StringVar(&args.Format, "format", "auto", "Report output format: auto, pretty or json")
	flag.Var((*commaList)(&args.Images), "image", "Only check images matching this name or pattern (repeatable, comma-separated); with the config command: explain how one image's settings were resolved")
	flag.StringVar(&args.MinAge, "min-age", "", "Only offer tags published at least this long ago, e.g. 7d or 36h (per-image config still wins)")
	flag.StringVar(versioningName, "versioning", "", "Default scheme for reading image tags as versions: semver or loose (per-image config still wins)")
}

// applySubcommand turns the leading word into the mode it names.
func applySubcommand(args *Flags, sub string) {
	if apply, ok := subcommands[sub]; ok {
		apply(args)
	}
}

// inferMode reads which flags were actually passed, for the two questions a
// flag's value alone cannot answer: which mode was meant, and whether a
// setting came from the command line or from its default.
func inferMode(args *Flags) {
	// Whether `check` was the subcommand, read before the pass below can set
	// Check itself: only a mode that was inferred is the legacy spelling main
	// warns about.
	subCheck := args.Check

	flag.Visit(func(f *flag.Flag) {
		switch {
		// A report-only flag without `check` is what every pre-TUI-default script
		// looks like, and `ccu -u` in a cron entry would hang waiting for a
		// terminal. The implied mode is honoured, and main names the new spelling
		// once.
		case !subCheck && plainOnlyFlags[f.Name]:
			args.Check, args.LegacyPlain = true, true
		// -format only ever describes the report, so asking for one names the mode
		// as plainly as `check` does, and there is no old spelling to warn about.
		// Without this, `ccu -format=json` would open the TUI instead.
		case !subCheck && f.Name == "format" && args.Format != "auto":
			args.Check = true
		// Read whatever the mode is: `ccu check -pin-floating=false` has to be
		// seen as well.
		case f.Name == "pin-floating":
			args.PinFloatingSet = true
		case f.Name == "dockerfiles":
			args.DockerfilesSet = true
		}
	})
}

// expandFlags settles the values that are shorthand for others, so that the
// rest of the program reads one spelling of each setting.
func expandFlags(args *Flags, versioningName string) {
	if args.Full {
		args.Major = true
		args.Minor = true
		args.Patch = true
	}

	args.Versioning = policy.Versioning(versioningName)

	// `config -image` explains one image and has no use for a list, so it reads
	// the first name given rather than growing a second spelling of the flag.
	if len(args.Images) > 0 {
		args.Image = args.Images[0]
	}
}

// usage replaces flag.PrintDefaults so the subcommands are documented alongside
// the flags, and the deprecated -self-update / -check-update spellings are left
// out: listing both forms would suggest there is a difference between them.
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
	fmt.Fprintln(tw, "  config -image <name>\tExplain how one image's settings were resolved, and from which file")
	fmt.Fprintln(tw, "  help\tShow this help message")
	fmt.Fprintln(tw, "  version\tShow version information")
	fmt.Fprintf(tw, "\nFlags (-d, -exclude, -image, -config, -pin-floating, -dockerfiles, -versioning, -min-age and -refresh apply to both modes, the rest only to `%s check`):\n", name)
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
