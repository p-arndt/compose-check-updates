package compose

import (
	"path/filepath"
	"regexp"
	"strings"
)

// envFileName is the file docker compose reads interpolation variables from: the
// one next to the compose file. `env_file:` is deliberately not consulted — that
// one populates a container's environment and takes no part in interpolating the
// compose file itself.
const envFileName = ".env"

// envAssignment matches one `KEY=value` line. The `export ` prefix is tolerated
// because a .env doubling as something to `source` in a shell is common.
var envAssignment = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)

// EnvEntry is one assignment of a .env file. It carries the line it was read
// from and where the value sits inside it, so writing a new value back changes
// nothing else about the line — quoting, spacing and any trailing comment
// survive.
type EnvEntry struct {
	Value string
	// Line is the assignment verbatim and LineNo its 1-based position. The
	// position is what a rewrite seeks by: a file may assign the same variable
	// twice, and matching on the text alone would change the wrong one.
	Line   string
	LineNo int
	// ValueStart and ValueEnd bound Value inside Line, quotes excluded.
	ValueStart, ValueEnd int
}

// EnvFile returns the path of the .env belonging to the compose file at path,
// whether or not it exists. Callers that only want to report where a value came
// from need the path, not the contents.
func EnvFile(composePath string) string {
	return filepath.Join(filepath.Dir(composePath), envFileName)
}

// readEnv reads the .env next to the compose file at path. A missing or
// unreadable file yields no variables rather than an error: most stacks have
// none, and one that cannot be read is not a reason to stop checking images
// whose tags are written out in full.
func readEnv(composePath string) (map[string]EnvEntry, string) {
	path := EnvFile(composePath)
	env := make(map[string]EnvEntry)

	lineNo := 0
	if err := eachLine(path, func(line string) {
		lineNo++

		m := envAssignment.FindStringSubmatchIndex(line)
		if m == nil {
			return
		}
		name := line[m[2]:m[3]]
		start, end := valueRange(line, m[1])

		// Last assignment wins, as it does for docker compose.
		env[name] = EnvEntry{Value: line[start:end], Line: line, LineNo: lineNo, ValueStart: start, ValueEnd: end}
	}); err != nil {
		return nil, ""
	}

	return env, path
}

// valueRange bounds the value a .env line assigns, starting the search at from —
// the offset just past the `=`. Surrounding quotes are excluded so the value
// reads the same either way, and an unquoted value ends at a trailing comment,
// which needs a space before the `#` to count as one.
func valueRange(line string, from int) (start, end int) {
	i := from
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}

	if i < len(line) && (line[i] == '"' || line[i] == '\'') {
		if j := strings.IndexByte(line[i+1:], line[i]); j >= 0 {
			return i + 1, i + 1 + j
		}
	}

	rest, _, _ := strings.Cut(line[i:], " #")
	return i, i + len(strings.TrimRight(rest, " \t\r"))
}
