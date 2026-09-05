package tui

import (
	"maps"

	"github.com/p-arndt/compose-check-updates/internal/config"
	"github.com/p-arndt/compose-check-updates/internal/policy"
)

// writeCap is the real writer behind Model.setCap: it resolves the file the
// chosen scope writes to and then sets or clears the entry.
func writeCap(root string) func(pinScope, string, policy.Level) error {
	return func(scope pinScope, image string, max policy.Level) error {
		path, err := scopePath(scope, root)
		if err != nil {
			return err
		}
		if max == "" {
			return config.ClearImageMax(path, image)
		}
		return config.SetImageMax(path, image, max)
	}
}

// writeVersioning is the real writer behind Model.setVersioning, resolving the
// file the chosen scope writes to exactly as writeCap does.
func writeVersioning(root string) func(pinScope, string, policy.Versioning) error {
	return func(scope pinScope, image string, versioning policy.Versioning) error {
		path, err := scopePath(scope, root)
		if err != nil {
			return err
		}
		if versioning == "" {
			return config.ClearImageVersioning(path, image)
		}
		return config.SetImageVersioning(path, image, versioning)
	}
}

// scopePath is the file a scope writes to. The project path is derived from the
// scanned root, because that is the tree the user is looking at.
func scopePath(scope pinScope, root string) (string, error) {
	if scope == pinGlobal {
		return config.GlobalWritePath()
	}
	return config.ProjectWritePath(root)
}

// versioningInScope is the scheme one scope records for an image, or "" when it
// says nothing about it.
func (m Model) versioningInScope(scope pinScope, image string) policy.Versioning {
	return m.pins[scope].Images[image].Versioning
}

// versioningFor is the scheme recorded for an image, project first — the same
// precedence a cap has, and the same the two config layers have when merged.
// Empty means the image was never given one and takes the run's default.
func (m Model) versioningFor(image string) policy.Versioning {
	if v := m.versioningInScope(pinProject, image); v != "" {
		return v
	}
	return m.versioningInScope(pinGlobal, image)
}

// defaultVersioning is the scheme an image with none of its own is read under,
// named so the sidebar can say what "default" currently means.
func (m Model) defaultVersioning() policy.Versioning {
	if m.opts.Policies.Versioning == "" {
		return policy.VersioningSemver
	}
	return policy.Versioning(m.opts.Policies.Versioning)
}

// recordVersioning folds a written scheme back into the in-memory layers and
// into the scan options, so the re-check that follows reads the image the way
// the file now says to. Nothing else re-reads the config during a session.
func (m *Model) recordVersioning(scope pinScope, image string, versioning policy.Versioning) {
	for s := range m.pins {
		m.updateImage(s, image, func(p *policy.Image) { p.Versioning = "" })
	}
	if versioning != "" {
		m.updateImage(scope, image, func(p *policy.Image) { p.Versioning = versioning })
	}

	// Copied rather than written through: the map came from the loaded config and
	// is shared with whoever else was handed it.
	images := maps.Clone(m.opts.Policies.Images)
	if images == nil {
		// maps.Clone hands back nil for a nil map, and the entry below is written
		// straight into it — a config that recorded no image at all would panic.
		images = make(map[string]policy.Image, 1)
	}
	entry := images[image]
	entry.Versioning = versioning
	images[image] = entry
	m.opts.Policies.Images = images
}

// capInScope is the cap recorded for an image in one scope, or "" when that
// scope says nothing about it.
func (m Model) capInScope(scope pinScope, image string) policy.Level {
	return m.pins[scope].MaxLevel(image)
}

// capFor is the cap that applies to an image, project first: a project file
// exists to override the global one, so that is the level the row shows.
func (m Model) capFor(image string) policy.Level {
	if l := m.capInScope(pinProject, image); l != "" {
		return l
	}
	return m.capInScope(pinGlobal, image)
}

// updateImage edits one image's entry in one scope's in-memory config, creating
// the entry as needed and dropping it again once nothing is left to say.
func (m *Model) updateImage(scope pinScope, image string, edit func(*policy.Image)) {
	if m.pins == nil {
		m.pins = make(map[pinScope]config.Config)
	}
	cfg := m.pins[scope]

	entry := cfg.Images[image]
	edit(&entry)

	switch {
	case entry.IsZero() && cfg.Images != nil:
		delete(cfg.Images, image)
	case !entry.IsZero():
		if cfg.Images == nil {
			cfg.Images = make(map[string]policy.Image)
		}
		cfg.Images[image] = entry
	}

	m.pins[scope] = cfg
}

// refreshPins re-stamps every row with the cap for its image. Rows carry it so
// the renderer stays a function of the row alone.
func (m *Model) refreshPins() {
	for i := range m.rows {
		r := &m.rows[i]
		r.Pin = m.capFor(r.Update.ImageName)

		// The cap binds the selection too: a row left aimed above its own cap
		// would let `A` write the version the user just forbade.
		r.Update.Cap = r.Pin
		if r.Pin != "" && !r.Update.Cap.Allows(r.Target) {
			m.retarget(r, Target(r.Pin))
		}
	}
}
