package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap is the single source of truth for the bindings. Help text is rendered
// from the same values, so a rebinding cannot silently leave the footer lying.
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Home       key.Binding
	End        key.Binding
	Toggle     key.Binding
	SelectAll  key.Binding
	SelectNone key.Binding
	Filter     key.Binding
	Target     key.Binding
	RowNext    key.Binding
	RowPrev    key.Binding
	Detail     key.Binding
	Apply      key.Binding
	Help       key.Binding
	Quit       key.Binding
	Yes        key.Binding
	No         key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:     key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Home:       key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first")),
		End:        key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last")),
		Toggle:     key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		SelectAll:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
		SelectNone: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "select none")),
		Filter:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
		Target:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "target level")),
		RowNext:    key.NewBinding(key.WithKeys("T", "right", "l"), key.WithHelp("T/→", "row target")),
		RowPrev:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←", "row target back")),
		Detail:     key.NewBinding(key.WithKeys("d", "tab"), key.WithHelp("d/tab", "detail")),
		Apply:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:       key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
		Yes:        key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")),
		No:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),
	}
}

// Bindings lists every binding in display order for help rendering.
func (k KeyMap) Bindings() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End,
		k.Toggle, k.SelectAll, k.SelectNone,
		k.Filter, k.Target, k.RowNext, k.Detail, k.Apply, k.Help, k.Quit,
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.Filter, k.Target, k.RowNext, k.Apply, k.Help, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End},
		{k.Toggle, k.SelectAll, k.SelectNone, k.Filter, k.Detail},
		{k.Target, k.RowNext, k.RowPrev},
		{k.Apply, k.Help, k.Quit},
	}
}
