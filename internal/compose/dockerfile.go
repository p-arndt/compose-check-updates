package compose

import (
	"regexp"
	"strings"
)

// fromPattern captures the image of a FROM line plus the stage name it may be
// given. Flags such as --platform sit between the two; the reference is the
// first argument that is not one. A trailing comment must not hide it: a FROM
// this fails to match is a stage left behind on the old base image.
var fromPattern = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--\S+\s+)*(\S+)(?:\s+AS\s+(\S+))?\s*(?:#.*)?$`)

// DockerfileImages returns the base images a Dockerfile builds on, in file
// order. Stages built on earlier stages of the same file are left out, as are
// `FROM scratch` and ARG-interpolated references: no registry can answer for
// any of them.
func DockerfileImages(path string) ([]Occurrence, error) {
	var found []Occurrence
	stages := make(map[string]struct{})

	err := eachLine(path, func(line string) {
		image, stage, ok := parseFrom(line)
		if !ok {
			return
		}

		_, builtHere := stages[strings.ToLower(image)]
		// Recorded after the image was judged, because `FROM x AS x` still names
		// the image x.
		if stage != "" {
			stages[strings.ToLower(stage)] = struct{}{}
		}
		if builtHere || strings.EqualFold(image, "scratch") || strings.Contains(image, "$") {
			return
		}

		found = append(found, Occurrence{Line: line, Raw: image, Reference: image})
	})

	return found, err
}

// parseFrom reads one line as a FROM instruction, reporting the image it names
// and the stage it declares.
func parseFrom(line string) (image, stage string, ok bool) {
	m := fromPattern.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
