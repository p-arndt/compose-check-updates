package config

import (
	"fmt"
	"github.com/p-arndt/compose-check-updates/internal/policy"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// Show writes the configuration a scan would run with: which files were read,
// in merge order, and what the merged result is once the command line has been
// layered on top. It is the answer to "ccu is not excluding my folder" — which
// is otherwise invisible, because a config file that was never found and one
// that was found and says nothing look exactly alike.
func Show(w io.Writer, loaded Loaded, effective Config) {
	if len(loaded.Sources) == 0 {
		fmt.Fprintln(w, "Config files: none found")
		fmt.Fprintf(w, "  looked for %s in the scan root and its parents\n", filepath.Join("<dir>", projectNames[0]))
		for _, dir := range globalDirs() {
			fmt.Fprintf(w, "  looked for %s\n", filepath.Join(dir, globalNames[0]))
		}
	} else {
		fmt.Fprintln(w, "Config files (later files layer over earlier ones):")
		for _, s := range loaded.Sources {
			fmt.Fprintf(w, "  %s\n", s)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Effective settings (config plus command line):")
	if len(effective.Exclude) == 0 {
		fmt.Fprintln(w, "  exclude: (none)")
	} else {
		fmt.Fprintln(w, "  exclude:")
		for _, e := range effective.Exclude {
			fmt.Fprintf(w, "    - %s\n", e)
		}
	}

	if len(effective.FloatingTags) == 0 {
		fmt.Fprintln(w, "  floating_tags: (built-in names only)")
	} else {
		fmt.Fprintf(w, "  floating_tags: %s (on top of the built-in names)\n", strings.Join(effective.FloatingTags, " "))
	}
	fmt.Fprintf(w, "  versioning: %s\n", effective.Policies().Versioning)
	fmt.Fprintf(w, "  pin_floating: %t\n", effective.PinFloatingEnabled())
	fmt.Fprintf(w, "  dockerfiles: %t\n", effective.DockerfilesEnabled())

	showImages(w, effective)
}

// showImages lists the per-image settings in a stable order. A map iterates at
// random, and a report whose lines move between two identical runs is one nobody
// can diff.
func showImages(w io.Writer, effective Config) {
	// The resolved policies rather than the raw entries, so what is printed is
	// what the scan uses: trimmed and deduplicated floating tags included.
	policies := effective.Policies().Images

	images := make([]string, 0, len(policies))
	for image, p := range policies {
		if !p.IsZero() {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		fmt.Fprintln(w, "  images: (nothing set)")
		return
	}
	sort.Strings(images)

	fmt.Fprintln(w, "  images:")
	for _, image := range images {
		fmt.Fprintf(w, "    %s: %s\n", image, strings.Join(imageSettings(policies[image]), ", "))
	}
}

// imageSettings is one image's policy as the labelled values `ccu config`
// prints. The pattern is shown rather than left implied: "why does this tag not
// count as a version" is answered by it more often than by anything else.
func imageSettings(p policy.Image) []string {
	var settings []string
	for _, s := range []struct{ label, value string }{
		{"max", p.Max.String()},
		{"versioning", p.Versioning.String()},
		{"pattern", p.VersioningPattern},
		{"reference_tag", p.ReferenceTag},
		{"floating_tags", strings.Join(p.FloatingTags, " ")},
	} {
		if s.value != "" {
			settings = append(settings, s.label+" "+s.value)
		}
	}
	return settings
}
