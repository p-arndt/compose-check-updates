package tui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/p-arndt/compose-check-updates/internal"
)

// ansiEscape strips styling so assertions can talk about what the user sees.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiEscape.ReplaceAllString(s, "") }

// helpBindings is the real key map, so the footer assertions break if a binding
// is added without a help string.
var helpBindings = DefaultKeyMap().Bindings()

// withColor forces a colour profile for the duration of a test. `go test` is
// not attached to a TTY, so lipgloss otherwise strips every escape sequence and
// the colour-placement assertions would be vacuous.
func withColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestVersionDeltaColorsOnlyChangedSegments(t *testing.T) {
	withColor(t)
	th := DefaultTheme()

	out := th.VersionDelta("1.2.3", "1.2.9", "patch")
	assert.Equal(t, "1.2.3 → 1.2.9", plain(out))

	// The shared "1.2." prefix must sit outside the coloured span: styling has
	// to change between it and the differing "9".
	assert.Regexp(t, regexp.MustCompile(`1\.2\.(\x1b\[[0-9;]*m)+9`), out)
	// ...which also means the latest tag is never one contiguous run of text.
	assert.NotContains(t, out, "1.2.9")

	// A major bump shares nothing, so the whole tag is coloured.
	major := th.VersionDelta("1.2.3", "2.0.0", "major")
	assert.Equal(t, "1.2.3 → 2.0.0", plain(major))
}

func TestVersionDeltaPlainForms(t *testing.T) {
	th := DefaultTheme()
	assert.Equal(t, "v1.2.3 → v1.3.0", plain(th.VersionDelta("v1.2.3", "v1.3.0", "minor")))
	assert.Equal(t, "latest", plain(th.VersionDelta("", "latest", "digest")))
	assert.Equal(t, "1.0.0", plain(th.VersionDelta("1.0.0", "", "")))
}

func TestBadgeHasStableVisibleWidth(t *testing.T) {
	withColor(t)
	th := DefaultTheme()

	for _, level := range []string{"major", "minor", "patch", "digest", "", "nonsense-level"} {
		assert.Equal(t, badgeWidth, lipgloss.Width(th.Badge(level)), "level %q", level)
		assert.Equal(t, badgeWidth, len([]rune(plain(th.Badge(level)))), "level %q", level)
	}
	assert.Equal(t, " MAJOR  ", plain(th.Badge("major")))
}

func sampleRow() Row {
	return Row{
		Update: internal.UpdateInfo{
			FilePath:      "tests/docker-compose.yml",
			RawLine:       "    image: nginx:1.2.3",
			ImageName:     "nginx",
			FullImageName: "docker.io/library/nginx:1.2.3",
			CurrentTag:    "1.2.3",
			LatestTag:     "1.2.9",
		},
		Level: "patch",
	}
}

func TestRowLineRespectsWidth(t *testing.T) {
	withColor(t)
	th := DefaultTheme()

	long := sampleRow()
	long.Update.FullImageName = "registry.example.internal:5000/a-very/long/namespace/with/an/image:1.2.3"
	long.Update.LatestTag = "10.20.30-rc.1+build.7"

	rows := []Row{sampleRow(), long}
	rows[0].Selected = true

	applied := sampleRow()
	applied.State = RowApplied
	failed := sampleRow()
	failed.State = RowFailed
	failed.Err = errors.New("permission denied writing tests/docker-compose.yml")
	noTarget := targetRow()
	noTarget.Update.PatchTag, noTarget.Update.MinorTag = "", ""
	var m Model
	m.retarget(&noTarget, TargetPatch)
	rows = append(rows, applied, failed, targetRow(), noTarget)

	widths := []int{-5, 0, 1, 2, 5, 20, 21, 24, 30, 40, 60, 80, 120}
	for _, r := range rows {
		for _, w := range widths {
			for _, cursor := range []bool{false, true} {
				out := th.RowLine(r, cursor, w)
				limit := clampWidth(w)
				assert.LessOrEqual(t, lipgloss.Width(out), limit, "width %d cursor %v", w, cursor)
				assert.NotContains(t, out, "\n", "row must stay on one line")
			}
		}
	}
}

func TestRowLineContent(t *testing.T) {
	th := DefaultTheme()

	r := sampleRow()
	assert.Contains(t, plain(th.RowLine(r, false, 100)), "[ ]")
	r.Selected = true
	assert.Contains(t, plain(th.RowLine(r, false, 100)), "[x]")

	r.State = RowApplied
	assert.Contains(t, plain(th.RowLine(r, false, 100)), "✓")

	r.State = RowFailed
	r.Err = errors.New("boom")
	out := plain(th.RowLine(r, false, 100))
	assert.Contains(t, out, "✗")
	assert.Contains(t, out, "boom")

	// The cursor marker only appears on the highlighted row.
	assert.Contains(t, plain(th.RowLine(sampleRow(), true, 100)), "▸")
	assert.NotContains(t, plain(th.RowLine(sampleRow(), false, 100)), "▸")
}

// targetRow is an image with a release at every level, the case the target
// feature exists for: traefik v2.9.3 with 2.9.4, 2.11.0 and 3.7.8 available.
func targetRow() Row {
	return Row{
		Update: internal.UpdateInfo{
			FilePath:      "tests/docker-compose.yml",
			RawLine:       "    image: traefik:v2.9.3",
			ImageName:     "traefik",
			FullImageName: "traefik:v2.9.3",
			CurrentTag:    "v2.9.3",
			LatestTag:     "3.7.8",
			PatchTag:      "2.9.4",
			MinorTag:      "2.11.0",
			MajorTag:      "3.7.8",
		},
		Level:  "major",
		Target: TargetMajor,
	}
}

func TestRowLineShowsSelectedTargetAndHintsOthers(t *testing.T) {
	th := DefaultTheme()

	r := targetRow()
	out := plain(th.RowLine(r, false, 120))
	assert.Contains(t, out, "MAJOR")
	assert.Contains(t, out, "v2.9.3 → 3.7.8")
	// Two other levels exist for this image, which is the only hint the user has
	// that T would do anything here.
	assert.Contains(t, out, "(+2)")

	// Pointed at its patch release, the badge must follow the SELECTED tag.
	var m Model
	m.retarget(&r, TargetPatch)
	patch := plain(th.RowLine(r, false, 120))
	assert.Contains(t, patch, "PATCH")
	assert.NotContains(t, patch, "MAJOR")
	assert.Contains(t, patch, "v2.9.3 → 2.9.4")
	assert.Contains(t, patch, "(+2)")
}

func TestRowLineNoTargetIsInert(t *testing.T) {
	th := DefaultTheme()

	// Only a major release exists, so asking for patch leaves nothing to apply.
	r := targetRow()
	r.Update.PatchTag, r.Update.MinorTag = "", ""

	var m Model
	m.retarget(&r, TargetPatch)
	require.True(t, r.NoTarget)

	out := plain(th.RowLine(r, false, 120))
	assert.Contains(t, out, "[-]", "an unavailable row must not look tickable")
	assert.Contains(t, out, "no patch update")
	// The tag it used to point at must not be advertised any more.
	assert.NotContains(t, out, "3.7.8")
}

func TestDetailDigestLines(t *testing.T) {
	th := DefaultTheme()

	u := sampleRow().Update
	out := plain(th.Detail(u, "patch", 80))
	assert.NotContains(t, out, "digest")
	assert.Contains(t, out, "tests/docker-compose.yml")
	assert.Contains(t, out, "1.2.3 → 1.2.9")

	u.CurrentDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	u.LatestDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	withDigest := plain(th.Detail(u, "digest", 80))
	assert.Contains(t, withDigest, "digest")
	assert.Contains(t, withDigest, "new digest")
	assert.Contains(t, withDigest, "sha256:111111111111…")
	// Long digests must not blow the pane out.
	for _, line := range strings.Split(th.Detail(u, "digest", 80), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 80)
	}
}

func TestDetailRespectsWidth(t *testing.T) {
	withColor(t)
	th := DefaultTheme()

	u := sampleRow().Update
	u.CurrentDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	u.LatestDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	for _, w := range []int{-1, 0, 1, 3, 20, 25, 40, 100} {
		for _, line := range strings.Split(th.Detail(u, "digest", w), "\n") {
			assert.LessOrEqual(t, lipgloss.Width(line), clampWidth(w), "width %d", w)
		}
	}
}

func TestRenderersSurviveDegenerateWidths(t *testing.T) {
	withColor(t)
	th := DefaultTheme()
	r := sampleRow()

	for _, w := range []int{-100, -1, 0, 1, 2, 7, 19, 20} {
		require.NotPanics(t, func() {
			th.Title(w)
			th.FileHeader("some/deeply/nested/path/docker-compose.yml", 3, 5, w)
			th.RowLine(r, true, w)
			th.Detail(r.Update, "patch", w)
			th.Legend(FilterMinor, TargetMajor, w)
			th.Help(helpBindings, w)
			th.Empty("no updates found", w)
		}, "width %d", w)

		limit := clampWidth(w)
		for name, out := range map[string]string{
			"title":  th.Title(w),
			"header": th.FileHeader("some/deeply/nested/path/docker-compose.yml", 3, 5, w),
			"legend": th.Legend(FilterMinor, TargetMajor, w),
			"help":   th.Help(helpBindings, w),
			"empty":  th.Empty("no updates found", w),
		} {
			assert.LessOrEqual(t, lipgloss.Width(out), limit, "%s at width %d", name, w)
			assert.NotContains(t, out, "\n", "%s must stay on one line", name)
		}
	}
}

func TestStatusAndLegendContent(t *testing.T) {
	th := DefaultTheme()

	assert.Equal(t, "✓ applied 3 updates", plain(th.Status(StatusSuccess, "applied 3 updates")))
	assert.Equal(t, "✗ nope", plain(th.Status(StatusError, "nope")))
	assert.Equal(t, "! careful", plain(th.Status(StatusWarn, "careful")))
	assert.Equal(t, "• scanning", plain(th.Status(StatusInfo, "scanning")))

	legend := plain(th.Legend(FilterMajor, TargetMinor, 200))
	assert.Contains(t, legend, "[major]")
	assert.Contains(t, legend, "minor")
	// The filter and the target are two different settings; the legend has to
	// name both or "[major]" is ambiguous.
	assert.Contains(t, legend, "show ")
	assert.Contains(t, legend, "target [minor]")
}

func TestFileHeaderShowsCounts(t *testing.T) {
	th := DefaultTheme()
	assert.Equal(t,
		"tests/docker-compose.yml (3 of 5)",
		plain(th.FileHeader("tests/docker-compose.yml", 3, 5, 80)))
}

func TestNoTrailingWhitespaceOnSingleLineRenderers(t *testing.T) {
	th := DefaultTheme()
	r := sampleRow()

	for name, out := range map[string]string{
		"header": th.FileHeader("tests/docker-compose.yml", 1, 2, 80),
		"row":    th.RowLine(r, false, 80),
		"legend": th.Legend(FilterAll, TargetMajor, 200),
		"help":   th.Help(helpBindings, 80),
		"status": th.Status(StatusInfo, "hi"),
	} {
		assert.Equal(t, plain(out), strings.TrimRight(plain(out), " "), "%s has trailing whitespace", name)
	}
}
