package check

import (
	"fmt"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal/compose"
)

// TagVar records that an image's tag is not written in the image line but
// interpolated from a variable, and where the text behind it lives. Without it
// an update would rewrite the tag into the compose file and leave the variable —
// the thing the running stack actually reads — pointing at the old release.
type TagVar struct {
	// Name is the variable, Raw the construct spelling it, e.g.
	// "${IMMICH_VERSION:-release}".
	Name string
	Raw  string
	// Value is the text the variable contributed to the tag, and Prefix and
	// Suffix the literal tag text around it: `postgres:${PG}-alpine` on PG=17
	// gives value "17" and suffix "-alpine". A new tag is written back by
	// stripping those again, so the variable keeps holding a version and not a
	// whole tag.
	Prefix, Suffix string
	Value          string

	// EnvPath and Env name the .env assignment holding Value, empty when the
	// value came from anywhere else.
	EnvPath string
	Env     compose.EnvEntry
	// FromDefault marks a value that came from the reference's own default, which
	// is rewritten in the image line rather than in a file of its own.
	FromDefault bool

	// Unset marks a variable nothing defines. The reference then names no tag at
	// all, and what it does parse as is not worth reporting.
	Unset bool

	// Unwritable says why a new tag cannot be written back, empty when it can.
	// The check itself still runs: knowing an update exists is worth something
	// even where ccu must leave the editing to the user.
	Unwritable string
}

// tagVarFor works out whether the tag of this occurrence came from a variable.
// It answers by byte range rather than by re-parsing: a variable can just as
// well supply the registry host or the repository name, and only the one landing
// inside the tag has anything to do with an update.
func tagVarFor(occ compose.Occurrence) *TagVar {
	if len(occ.Expansions) == 0 {
		return nil
	}

	start, end, ok := tagRange(occ.Reference)
	if !ok {
		return nil
	}

	var inside []compose.Expansion
	for _, e := range occ.Expansions {
		if e.End < start || e.Start > end {
			continue
		}
		if e.Start < start || e.End > end {
			// The variable reaches beyond the tag, so changing the tag would mean
			// changing the repository too. Nothing sane to write back.
			return &TagVar{Name: e.Name, Raw: e.Raw, Value: e.Value,
				Unwritable: fmt.Sprintf("%s spells more than this image's tag, so ccu cannot rewrite the tag on its own", e.Raw)}
		}
		inside = append(inside, e)
	}

	if len(inside) == 0 {
		return nil
	}
	if len(inside) > 1 {
		return &TagVar{Name: inside[0].Name, Raw: inside[0].Raw, Value: inside[0].Value,
			Unwritable: "this tag is assembled from more than one variable, so ccu cannot tell which of them a new version belongs in"}
	}

	e := inside[0]
	v := &TagVar{
		Name:   e.Name,
		Raw:    e.Raw,
		Value:  e.Value,
		Prefix: occ.Reference[start:e.Start],
		Suffix: occ.Reference[e.End:end],
	}

	switch e.Source {
	case compose.VarEnvFile:
		v.EnvPath, v.Env = occ.EnvPath, e.Env
	case compose.VarDefault:
		v.FromDefault = true
	case compose.VarProcessEnv:
		v.Unwritable = fmt.Sprintf("%s is set in ccu's environment, not in a file it could rewrite", e.Name)
	case compose.VarAlternate:
		v.Unwritable = fmt.Sprintf("%s substitutes a fixed text for the variable, so there is no version in it to raise", e.Raw)
	case compose.VarUnset:
		v.Unset = true
		v.Unwritable = fmt.Sprintf("%s is not set anywhere ccu can see, and the reference gives no default", e.Name)
	}

	return v
}

// tagRange bounds the tag inside an interpolated reference. The same split
// registry.ParseRef makes, but reported as a range: what matters here is not the
// tag's text but which part of the line produced it.
func tagRange(reference string) (start, end int, ok bool) {
	remainder := reference
	if at := strings.LastIndex(reference, "@"); at != -1 {
		remainder = reference[:at]
	}

	colon := strings.LastIndex(remainder, ":")
	// A colon before the last slash is a registry port, not a tag separator.
	if colon < 0 || colon < strings.LastIndex(remainder, "/") {
		return 0, 0, false
	}
	return colon + 1, len(remainder), true
}

// valueFor is the value the variable has to hold for the tag to read latestTag,
// reporting false when the literal text around the variable does not survive the
// move — a tag family changing under it, which no rewrite of the variable alone
// could produce.
func (t *TagVar) valueFor(latestTag string) (string, bool) {
	if len(latestTag) < len(t.Prefix)+len(t.Suffix) {
		return "", false
	}
	if !strings.HasPrefix(latestTag, t.Prefix) || !strings.HasSuffix(latestTag, t.Suffix) {
		return "", false
	}
	return latestTag[len(t.Prefix) : len(latestTag)-len(t.Suffix)], true
}

// rawWithValue respells the construct with a new default, for the reference that
// carries its own: `${TAG:-1.2.3}` on 1.2.4 becomes `${TAG:-1.2.4}`.
func (t *TagVar) rawWithValue(value string) (string, bool) {
	tail := t.Value + "}"
	if !strings.HasSuffix(t.Raw, tail) {
		return "", false
	}
	return strings.TrimSuffix(t.Raw, tail) + value + "}", true
}

// envLineWithValue respells the .env assignment, leaving its quoting, spacing
// and trailing comment exactly as they were.
func (t *TagVar) envLineWithValue(value string) string {
	line := t.Env.Line
	return line[:t.Env.ValueStart] + value + line[t.Env.ValueEnd:]
}
