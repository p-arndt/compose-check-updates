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

	if len(effective.FloatingTags) == 0 {
		fmt.Fprintln(w, "  floating_tags: (built-in names only)")
	} else {
		fmt.Fprintf(w, "  floating_tags: %s (on top of the built-in names)\n", strings.Join(effective.FloatingTags, " "))
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
	references := effective.ReferenceTags()
	floating := effective.ImageFloatingTags()
	// Shown rather than left implied: a `regex` image is only as good as its
	// pattern, and "why does this tag not count as a version" is answered by the
	// pattern itself far more often than by anything else on the line.
	patterns := effective.VersioningPatterns()
	if len(caps) == 0 && len(schemes) == 0 && len(references) == 0 && len(floating) == 0 && len(patterns) == 0 {
		fmt.Fprintln(w, "  images: (nothing set)")
		return
	}

	seen := make(map[string]struct{})
	var images []string
	add := func(image string) {
		if _, dup := seen[image]; dup {
			return
		}
		seen[image] = struct{}{}
		images = append(images, image)
	}
	for _, m := range []map[string]string{caps, schemes, references, patterns} {
		for image := range m {
			add(image)
		}
	}
	for image := range floating {
		add(image)
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
		if pattern, ok := patterns[image]; ok {
			settings = append(settings, "pattern "+pattern)
		}
		if reference, ok := references[image]; ok {
			settings = append(settings, "reference_tag "+reference)
		}
		if tags, ok := floating[image]; ok {
			settings = append(settings, "floating_tags "+strings.Join(tags, " "))
		}
		fmt.Fprintf(w, "    %s: %s\n", image, strings.Join(settings, ", "))
	}
}
