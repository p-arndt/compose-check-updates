package config

import (
	"fmt"
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

	fmt.Fprintf(w, "  versioning: %s\n", effective.DefaultVersioning())
	fmt.Fprintf(w, "  pin_floating: %t\n", effective.PinFloatingEnabled())
	fmt.Fprintf(w, "  dockerfiles: %t\n", effective.DockerfilesEnabled())

	showImages(w, effective)
}

// showImages lists the per-image settings in a stable order. A map iterates at
// random, and a report whose lines move between two identical runs is one nobody
// can diff.
func showImages(w io.Writer, effective Config) {
	caps := effective.Caps()
	schemes := effective.Versionings()
	if len(caps) == 0 && len(schemes) == 0 {
		fmt.Fprintln(w, "  images: (nothing set)")
		return
	}

	seen := make(map[string]struct{}, len(caps)+len(schemes))
	images := make([]string, 0, len(caps)+len(schemes))
	for _, m := range []map[string]string{caps, schemes} {
		for image := range m {
			if _, dup := seen[image]; dup {
				continue
			}
			seen[image] = struct{}{}
			images = append(images, image)
		}
	}
	sort.Strings(images)

	fmt.Fprintln(w, "  images:")
	for _, image := range images {
		var settings []string
		if cap, ok := caps[image]; ok {
			settings = append(settings, "max "+cap)
		}
		if scheme, ok := schemes[image]; ok {
			settings = append(settings, "versioning "+scheme)
		}
		fmt.Fprintf(w, "    %s: %s\n", image, strings.Join(settings, ", "))
	}
}
