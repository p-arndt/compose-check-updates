package tui

import (
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// cycleSideValue changes the focused field by delta. It is the only way the
// sidebar writes anything. The command it returns is the work a changed value
// costs — re-checking one image — and is nil for the fields that cost nothing.
func (m *Model) cycleSideValue(delta int) tea.Cmd {
	r := m.currentRow()
	if r == nil {
		return nil
	}
	// The sidebar cursor stays where it was as the list moves under it, so it can
	// be sitting on a field this row does not have. A field nobody can see is not
	// one a keypress may write.
	if !m.fieldVisible(m.sideField) {
		return nil
	}

	switch m.sideField {
	case fieldTarget:
		m.cycleRowTarget(delta)
	case fieldVersioning:
		return m.cycleVersioning(r, delta)
	case fieldCap:
		m.cycleCap(r, delta)
	case fieldScope:
		m.cycleScope(r, delta)
	}
	return nil
}

// cycleVersioning steps the scheme this image's tags are read under, writes it
// immediately the way the cap is written, and asks for the one image to be
// checked again. Re-checking is the whole point: a scheme that changes nothing
// on screen looks exactly like a setting that does not work.
func (m *Model) cycleVersioning(r *Row, delta int) tea.Cmd {
	image := r.Update.ImageName

	cur := max(slices.Index(versioningChoices, m.versioningFor(image)), 0)
	next := versioningChoices[stepIndex(len(versioningChoices), cur, delta)]

	// A scheme already recorded lives in one scope; a new one goes to the project
	// file, which is the narrower of the two and the easier to undo.
	scope := pinProject
	if m.versioningFor(image) != "" {
		scope = m.versioningScopeOf(image)
	}

	// Cleared from both files before being written, so a scheme can never end up
	// in two of them and the field cannot say which one it shows.
	for _, s := range scopeChoices {
		if err := m.setVersioning(s, image, ""); err != nil {
			m.setStatus(StatusError, fmt.Sprintf("could not update %s: %v", image, err))
			return nil
		}
	}
	if next != "" {
		if err := m.setVersioning(scope, image, next); err != nil {
			m.setStatus(StatusError, fmt.Sprintf("could not save %s: %v", image, err))
			return nil
		}
	}

	m.recordVersioning(scope, image, next)

	label := string(next)
	if next == "" {
		label = "the default (" + string(m.defaultVersioning()) + ")"
	}
	m.setStatus(StatusInfo, fmt.Sprintf("reading %s as %s…", image, label))

	return recheckImage(m.opts, *r)
}

// versioningScopeOf reports which scope holds the scheme for an image, so the
// next write goes back to the file the value came from. Project wins, the same
// way it wins when the two layers are merged.
func (m Model) versioningScopeOf(image string) pinScope {
	if m.versioningInScope(pinProject, image) != "" {
		return pinProject
	}
	return pinGlobal
}

// cycleCap steps the ceiling and writes it immediately, so what the field shows
// is what is on disk, with no unsaved state to lose.
func (m *Model) cycleCap(r *Row, delta int) {
	image := r.Update.ImageName

	choices := m.capChoicesFor(image)
	cur := max(slices.Index(choices, r.Pin), 0)
	next := choices[stepIndex(len(choices), cur, delta)]

	// A cap already recorded lives in one scope; a new one goes to the project
	// file, which is the narrower of the two and the easier to undo.
	scope := pinProject
	if r.Pin != "" {
		scope = m.pinScopeOf(image)
	}

	if !m.writeCapValue(scope, image, next) {
		return
	}

	if next == "" {
		m.setStatus(StatusSuccess, fmt.Sprintf("%s no longer capped", image))
		return
	}
	m.setStatus(StatusSuccess, fmt.Sprintf("%s capped at %s (%s)", image, next, scopeLabel(scope)))
}

// cycleScope moves an existing cap between the two files. There is nothing to
// move when no cap is set, and the field is not shown then either.
func (m *Model) cycleScope(r *Row, delta int) {
	if r.Pin == "" {
		return
	}

	image := r.Update.ImageName
	from := m.pinScopeOf(image)

	cur := max(slices.Index(scopeChoices, from), 0)
	to := scopeChoices[stepIndex(len(scopeChoices), cur, delta)]
	if to == from {
		return
	}

	// Cleared from the old file before being written to the new one, or the image
	// ends up capped in both and the field cannot say which it shows.
	if err := m.setCap(from, image, ""); err != nil {
		m.setStatus(StatusError, fmt.Sprintf("could not update %s: %v", image, err))
		return
	}
	if !m.writeCapValue(to, image, r.Pin) {
		return
	}
	m.setStatus(StatusSuccess, fmt.Sprintf("%s capped at %s (%s)", image, r.Pin, scopeLabel(to)))
}

// writeCapValue records level in scope — clearing every scope first, so a cap
// can never end up in two files — and reports whether it got that far. A failed
// write leaves the rows alone: the sidebar must not show a cap that is not on
// disk.
func (m *Model) writeCapValue(scope pinScope, image string, level policy.Level) bool {
	if level == "" {
		for _, s := range scopeChoices {
			if err := m.setCap(s, image, ""); err != nil {
				m.setStatus(StatusError, fmt.Sprintf("could not update %s: %v", image, err))
				return false
			}
		}
		m.applyPin(image, "", scope)
		return true
	}

	if err := m.setCap(scope, image, level); err != nil {
		m.setStatus(StatusError, fmt.Sprintf("could not save %s: %v", image, err))
		return false
	}
	m.applyPin(image, level, scope)
	return true
}

// applyPin records the new cap in the in-memory layers and restamps the rows, so
// marker and field agree with the file without re-reading it.
func (m *Model) applyPin(image string, level policy.Level, scope pinScope) {
	// Edited field by field rather than replaced, so a versioning recorded next
	// to the cap survives: the file keeps it, and the sidebar must not disagree.
	for s := range m.pins {
		m.updateImage(s, image, func(p *policy.Image) { p.Max = "" })
	}
	if level != "" {
		m.updateImage(scope, image, func(p *policy.Image) { p.Max = level })
	}

	m.refreshPins()
}
