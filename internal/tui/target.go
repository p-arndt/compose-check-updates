package tui

import "fmt"

// retarget points one row at its tag for the given level. A row with no release
// at that level is marked NoTarget and deselected rather than quietly keeping
// the higher tag it was showing. Rows with no levels at all — a digest move, or
// a tag semver cannot read — are left untouched.
func (m *Model) retarget(r *Row, target Target) {
	if r.State != RowPending || len(r.Update.AvailableTargets()) == 0 {
		return
	}

	r.Target = target

	// SelectTarget reports whether the tag *changed*, which is false both when
	// there is nothing at this level and when the row already sits on it — so
	// availability is decided by TagForTarget, not by that bool.
	if r.Update.TagForTarget(target) == "" {
		r.NoTarget = true
		r.Selected = false
		r.Level = ""
		return
	}

	r.Update.SelectTarget(target)
	r.NoTarget = false
	r.Level = r.Update.Level()
}

// setTarget re-points every row and rebuilds the view, because a row that lost
// its update also lost the level the filter matches on.
func (m *Model) setTarget(target Target) {
	m.target = target
	for i := range m.rows {
		m.retarget(&m.rows[i], target)
	}
	m.rebuild(m.cursorKey())
	m.syncScroll()
}

// cycleRowTarget moves the highlighted row to its next available level, staying
// inside AvailableTargets so the cycle only ever offers versions that exist.
// delta is +1 or -1.
func (m *Model) cycleRowTarget(delta int) {
	r := m.currentRow()
	if r == nil || r.State != RowPending {
		return
	}

	avail := r.Update.AvailableTargets()
	if len(avail) == 0 {
		m.setStatus(StatusWarn, "no alternative versions for this image")
		return
	}

	// Match on the level of the tag actually selected rather than the requested
	// one: TagForTarget degrades downwards, so a row asked for "major" can be
	// sitting on its patch release, and the cycle must continue from there.
	i := -1
	for j, t := range avail {
		if t == r.Level || Target(t) == r.Target {
			i = j
			break
		}
	}
	switch {
	case i < 0 && delta < 0:
		i = len(avail) - 1
	case i < 0:
		i = 0
	default:
		i = ((i+delta)%len(avail) + len(avail)) % len(avail)
	}

	key := rowKey(*r)
	m.retarget(r, Target(avail[i]))
	m.setStatus(StatusInfo, fmt.Sprintf("%s → %s (%s)", r.Update.ImageName, r.Update.LatestTag, targetLabel(r.Target)))
	m.rebuild(key)
	m.syncScroll()
}
