// Package report renders what a non-interactive run found. The same events are
// written either for a person reading a terminal or for whatever is on the other
// end of a pipe, so the choice of shape stays here and the scanning code above
// it stays unaware of which one is in effect.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/p-arndt/compose-check-updates/internal"
)

// Format is how a run writes its findings.
type Format string

const (
	// FormatAuto resolves to Pretty on a terminal and JSONL anywhere else.
	FormatAuto Format = "auto"
	// FormatPretty is the aligned, colourised report meant to be read.
	FormatPretty Format = "pretty"
	// FormatJSONL is one JSON object per line, written as the scan resolves.
	FormatJSONL Format = "jsonl"
)

// ParseFormat reads the -format value. "json" is accepted as a spelling of
// "jsonl" because the output is JSON and the line framing is a detail of it, and
// a user reaching for --format=json should not have to find that out.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", string(FormatAuto):
		return FormatAuto, nil
	case string(FormatPretty), "text":
		return FormatPretty, nil
	case string(FormatJSONL), "json":
		return FormatJSONL, nil
	}
	return "", fmt.Errorf("unknown format %q: use auto, pretty or json", s)
}

// Resolve turns FormatAuto into the format the run will actually use.
func (f Format) Resolve(stdoutIsTerminal bool) Format {
	if f != FormatAuto {
		return f
	}
	if stdoutIsTerminal {
		return FormatPretty
	}
	return FormatJSONL
}

// Result records what a run was asked to do with an update and what came of it.
// Asked-for and achieved are kept apart: an apply that failed is not the same as
// one that was never requested, and only the report can tell them apart.
type Result struct {
	ApplyRequested   bool
	Applied          bool
	RestartRequested bool
	Restarted        bool
}

// Writer receives everything a non-interactive run has to say about the compose
// files it looked at. Diagnostics about ccu itself do not go through it: those
// belong on stderr in every format.
type Writer interface {
	// Update reports one image that can move, along with what was done about it.
	Update(u internal.UpdateInfo, level string, res Result)
	// Error reports a failure that did not stop the scan.
	Error(file string, err error)
	// Close flushes anything held back. Nil for the streaming writers here, but
	// it keeps a buffered format from having to change this interface.
	Close() error
}

// New returns the writer for a resolved format. FormatAuto must have been
// resolved before this is called.
func New(format Format, out io.Writer) Writer {
	if format == FormatJSONL {
		return &jsonlWriter{enc: json.NewEncoder(out)}
	}
	return prettyWriter{}
}

// record is one line of the JSONL stream. Every line carries "kind" so a
// consumer can dispatch on it without guessing from which fields are present.
type record struct {
	Kind string `json:"kind"`

	Image     string   `json:"image,omitempty"`
	Reference string   `json:"reference,omitempty"`
	Services  []string `json:"services,omitempty"`
	File      string   `json:"file,omitempty"`

	Current string `json:"current,omitempty"`
	Latest  string `json:"latest,omitempty"`
	Level   string `json:"level,omitempty"`

	CurrentDigest string `json:"current_digest,omitempty"`
	LatestDigest  string `json:"latest_digest,omitempty"`

	// Targets lists the tag available at each level, so a consumer can offer the
	// same choice the TUI does instead of only the tag this run picked.
	Targets map[string]string `json:"targets,omitempty"`
	Cap     string            `json:"cap,omitempty"`

	// Applied and Restarted are only meaningful with -u / -r, and are left out
	// entirely otherwise rather than reporting a false for something the run was
	// never asked to do.
	Applied   *bool `json:"applied,omitempty"`
	Restarted *bool `json:"restarted,omitempty"`

	Error string `json:"error,omitempty"`
}

type jsonlWriter struct{ enc *json.Encoder }

func (w *jsonlWriter) Update(u internal.UpdateInfo, level string, res Result) {
	rec := record{
		Kind:          "update",
		Image:         u.ImageName,
		Reference:     u.FullImageName,
		Services:      u.Services,
		File:          u.FilePath,
		Current:       u.CurrentTag,
		Latest:        u.LatestTag,
		Level:         level,
		CurrentDigest: u.CurrentDigest,
		Cap:           u.Cap,
	}
	// Only a digest that actually differs describes the update; repeating the
	// current one under a "latest" key would claim a change that is not there.
	if u.IsDigestUpdate() {
		rec.LatestDigest = u.LatestDigest
	}

	targets := map[string]string{}
	for _, name := range u.AvailableTargets() {
		targets[name] = u.TagForTarget(name)
	}
	if len(targets) > 0 {
		rec.Targets = targets
	}

	// Reported whenever the run was asked for it, false included: a consumer
	// gating on "did everything get written" needs to see the ones that did not.
	if res.ApplyRequested {
		rec.Applied = &res.Applied
	}
	if res.RestartRequested {
		rec.Restarted = &res.Restarted
	}

	w.enc.Encode(rec)
}

func (w *jsonlWriter) Error(file string, err error) {
	w.enc.Encode(record{Kind: "error", File: file, Error: err.Error()})
}

func (w *jsonlWriter) Close() error { return nil }

// prettyWriter keeps the report a person reads exactly as it was: the lines go
// through slog, whose handler does the alignment and the colouring.
type prettyWriter struct{}

func (prettyWriter) Update(u internal.UpdateInfo, level string, res Result) {
	if !res.ApplyRequested && !res.RestartRequested {
		attrs := []any{"image", u.ImageName, "current", u.CurrentTag, "latest", u.LatestTag, "file", u.FilePath, "update_level", level}
		if u.IsDigestUpdate() {
			// A pin has no current digest to report — that is the whole point of it —
			// and an empty field would read as one that failed to resolve.
			if u.CurrentDigest != "" {
				attrs = append(attrs, "current_digest", u.CurrentDigest)
			}
			attrs = append(attrs, "latest_digest", u.LatestDigest)
		}
		slog.Info("New version", attrs...)
	}
	if res.Applied {
		slog.Info("Updated file", "file", u.FilePath, "image", u.ImageName, "latest", u.LatestTag)
	}
	if res.Restarted {
		slog.Info("Compose file restarted", "file", u.FilePath)
	}
}

func (prettyWriter) Error(file string, err error) {
	slog.Error("Error checking for updates", "error", err, "file", file)
}

func (prettyWriter) Close() error { return nil }
