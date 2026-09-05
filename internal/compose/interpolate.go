package compose

import (
	"os"
	"regexp"
	"strings"
)

// varPattern matches the interpolation forms compose understands: the `$$`
// escape, `${NAME}` with an optional operator and argument, and bare `$NAME`.
// A default is read up to the closing brace, so a nested `${A:-${B}}` is not
// supported — compose itself allows it, but no image tag is written that way.
var varPattern = regexp.MustCompile(`\$\$|\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-+?])([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// VarSource says what supplied a variable's value, which decides whether a new
// value can be written back and to which file.
type VarSource int

const (
	// VarUnset: nothing defined the variable and the reference gave no default.
	VarUnset VarSource = iota
	// VarEnvFile: the .env next to the compose file assigns it. This is the only
	// source ccu can rewrite in a file of its own.
	VarEnvFile
	// VarProcessEnv: ccu's own environment defines it, as it would define it for
	// `docker compose up`. There is no file to write it back to.
	VarProcessEnv
	// VarDefault: the value is the default written into the reference itself,
	// `${TAG:-1.2.3}`, so it lives in the image line.
	VarDefault
	// VarAlternate: the value comes from a `${TAG:+...}` alternate, where the
	// text is a substitute for the variable rather than the version itself.
	VarAlternate
)

// Expansion is one variable a line spells, and where its value ended up in the
// expanded text. The byte range is what lets a caller tell a variable that
// supplied the tag from one that supplied part of the repository name.
type Expansion struct {
	Raw        string // the construct verbatim, e.g. "${IMMICH_VERSION:-release}"
	Name       string
	Value      string
	Source     VarSource
	Start, End int // Value's byte range in the expanded string

	// Env is the .env assignment this value was read from, set only for
	// VarEnvFile. It travels along so a rewrite does not have to read the file
	// again and risk acting on a different line than the check saw.
	Env EnvEntry
}

// Expand resolves the variables in text the way docker compose would, and
// reports each one it substituted. Values come from ccu's own environment first
// and the .env file second, which is compose's own precedence: matching it is
// what makes ccu check the tag the running stack actually uses.
func Expand(text string, env map[string]EnvEntry) (string, []Expansion) {
	matches := varPattern.FindAllStringSubmatchIndex(text, -1)
	if matches == nil {
		return text, nil
	}

	var out strings.Builder
	var expansions []Expansion

	last := 0
	for _, m := range matches {
		out.WriteString(text[last:m[0]])
		last = m[1]

		raw := text[m[0]:m[1]]
		if raw == "$$" {
			// Compose's escape for a literal dollar sign.
			out.WriteByte('$')
			continue
		}

		name := group(text, m, 1)
		if name == "" {
			name = group(text, m, 4)
		}
		value, source, entry := substitute(name, group(text, m, 2), group(text, m, 3), env)

		expansions = append(expansions, Expansion{
			Raw:    raw,
			Name:   name,
			Value:  value,
			Source: source,
			Start:  out.Len(),
			End:    out.Len() + len(value),
			Env:    entry,
		})
		out.WriteString(value)
	}
	out.WriteString(text[last:])

	return out.String(), expansions
}

// substitute applies one `${NAME<op><arg>}` form. The `:?` and `?` forms abort a
// compose run when the variable is unset; ccu only reads files, so it reports
// the same nothing an unset variable gives and lets the caller say so.
func substitute(name, op, arg string, env map[string]EnvEntry) (string, VarSource, EnvEntry) {
	value, source, entry := lookup(name, env)
	set := source != VarUnset

	switch op {
	case ":-":
		if !set || value == "" {
			return arg, VarDefault, EnvEntry{}
		}
	case "-":
		if !set {
			return arg, VarDefault, EnvEntry{}
		}
	case ":+":
		if set && value != "" {
			return arg, VarAlternate, EnvEntry{}
		}
		return "", VarUnset, EnvEntry{}
	case "+":
		if set {
			return arg, VarAlternate, EnvEntry{}
		}
		return "", VarUnset, EnvEntry{}
	}

	return value, source, entry
}

func lookup(name string, env map[string]EnvEntry) (string, VarSource, EnvEntry) {
	if value, ok := os.LookupEnv(name); ok {
		return value, VarProcessEnv, EnvEntry{}
	}
	if entry, ok := env[name]; ok {
		return entry.Value, VarEnvFile, entry
	}
	return "", VarUnset, EnvEntry{}
}

// group returns submatch n of a FindAllStringSubmatchIndex result, empty when
// that group did not take part in the match.
func group(text string, m []int, n int) string {
	if 2*n+1 >= len(m) || m[2*n] < 0 {
		return ""
	}
	return text[m[2*n]:m[2*n+1]]
}
