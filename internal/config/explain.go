package config

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// Explain writes how one image's settings were resolved: the value in effect and
// the layer that produced it — which is what Show's merged result cannot say.
//
// flagVersioning is -versioning as passed, and arrives separately because main
// has already folded it into effective, where it is indistinguishable from a
// config file.
func Explain(w io.Writer, loaded Loaded, effective Config, image string, flagVersioning policy.Versioning) {
	fmt.Fprintf(w, "Image: %s\n\n", image)

	if len(loaded.Sources) == 0 {
		fmt.Fprintln(w, "Config files: none found")
	} else {
		fmt.Fprintln(w, "Config files (later files layer over earlier ones):")
		for _, s := range loaded.Sources {
			fmt.Fprintf(w, "  %s\n", s)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Settings in effect for %s:\n", image)

	scheme, from := explainVersioning(loaded, effective, image, flagVersioning)
	fmt.Fprintf(w, "  versioning: %s (%s)\n", scheme, from)

	cap, from := explainMax(loaded, effective, image)
	if cap == "" {
		fmt.Fprintf(w, "  max: no cap (%s)\n", from)
	} else {
		fmt.Fprintf(w, "  max: %s (%s)\n", cap, from)
	}

	explainMatch(w, loaded, effective, image)
}

// explainVersioning resolves the scheme and names the layer it came from. The
// value comes from policy.Set.For, the one copy of the precedence rule, so this
// can never report a scheme the checker would not use.
func explainVersioning(loaded Loaded, effective Config, image string, flagVersioning policy.Versioning) (scheme policy.Versioning, from string) {
	scheme = effective.Policies().For(image).Versioning

	if effective.Images[image].Versioning != "" {
		return scheme, entryOrigin(loaded, image, ".versioning")
	}
	if flagVersioning != "" {
		return scheme, "-versioning on the command line"
	}
	// A key set in both files was resolved by the project one: that is the layer
	// merge applied last.
	if loaded.Project.Versioning != "" {
		return scheme, keyOrigin("versioning", loaded.ProjectPath)
	}
	if loaded.Global.Versioning != "" {
		return scheme, keyOrigin("versioning", loaded.GlobalPath)
	}
	return scheme, "built-in default"
}

// explainMax does the same for the cap. There are only two layers here — a cap
// is per-image or it is nothing — but which *file* the entry came from is the
// whole question when a global preference and a project one disagree.
func explainMax(loaded Loaded, effective Config, image string) (cap, from string) {
	if level := effective.MaxLevel(image); level != "" {
		return string(level), entryOrigin(loaded, image, ".max")
	}
	// An entry that exists but sets no cap is worth telling apart from no entry:
	// it is the shape a user lands in after writing only `versioning:` under an
	// image and expecting the cap to have survived from the other file.
	if _, ok := effective.Images[image]; ok {
		return "", "no max set in " + entryOrigin(loaded, image, "")
	}
	return "", "no entry for this image"
}

// entryOrigin names the images entry a value came from, file and all. The
// project layer is checked first: mergeImages lets an entry there replace the
// global one outright.
func entryOrigin(loaded Loaded, image, field string) string {
	key := "images." + image + field
	if _, ok := loaded.Project.Images[image]; ok {
		return keyOrigin(key, loaded.ProjectPath)
	}
	if _, ok := loaded.Global.Images[image]; ok {
		return keyOrigin(key, loaded.GlobalPath)
	}
	// No layer owns it, which happens only for a Config assembled in memory
	// rather than read from disk. Naming the key without a file still beats
	// claiming a path that does not exist.
	return key
}

// keyOrigin spells one "key in file" phrase, dropping the file when there is
// none to name.
func keyOrigin(key, path string) string {
	if path == "" {
		return key
	}
	return key + " in " + path
}

// explainMatch reports whether any entry named the image at all, and points at
// the entries that nearly did — the config looks right, the setting is ignored,
// and nothing else says why.
func explainMatch(w io.Writer, loaded Loaded, effective Config, image string) {
	if _, ok := effective.Images[image]; ok {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "No config entry names %q.\n", image)
	fmt.Fprintln(w, "  Lookup is exact and on the image name without tag or digest, as ccu")
	fmt.Fprintln(w, `  reports it — e.g. "library/traefik", not "traefik:1.2".`)

	for _, near := range nearMisses(effective.Images, image) {
		fmt.Fprintf(w, "  Did you mean %q? (%s)\n", near, entryOrigin(loaded, near, ""))
	}
}

// nearMisses lists the configured keys that look like what the user typed, in a
// stable order: a map iterates at random, and a hint that moves between two
// identical runs is one nobody can diff.
func nearMisses(images map[string]policy.Image, image string) []string {
	var out []string
	for key := range images {
		if key != image && isNearMiss(image, key) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// isNearMiss decides whether key is plausibly the entry the user meant. The two
// mistakes are writing the reference instead of the name ("traefik:1.2") or the
// wrong namespace ("traefik" for "library/traefik"), so the comparison drops the
// tag, the digest and everything before the last path segment.
func isNearMiss(image, key string) bool {
	image, key = strings.ToLower(bareName(image)), strings.ToLower(key)
	if image == key {
		return true
	}
	return lastSegment(image) == lastSegment(key)
}

// bareName strips the tag and digest off a reference, which is what the config
// keys are spelled as. Only a colon after the last slash is a tag separator: a
// registry may carry a port, as in "registry.local:5000/redis".
func bareName(image string) string {
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	name := image
	if slash := strings.LastIndex(image, "/"); slash >= 0 {
		name = image[slash+1:]
	}
	if colon := strings.LastIndex(name, ":"); colon >= 0 {
		image = image[:len(image)-len(name)+colon]
	}
	return image
}

func lastSegment(name string) string {
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		return name[slash+1:]
	}
	return name
}
