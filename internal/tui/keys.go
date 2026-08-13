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
	// The Global pair widen SelectAll/SelectNone from the cursor's subtree back
	// to the whole list, which a is no longer able to reach on its own.
	SelectAllGlobal  key.Binding
	SelectNoneGlobal key.Binding
	// Fold keys. The list is a directory tree now, so ←/h and →/l were taken back
	// from the row-target cycle and given the meaning every tree in every file
	// browser already has: collapse-or-go-to-parent, expand-or-step-into-child.
	// z/C/E stay as the keyboard-only shortcuts, because t/T target, and space, a,
	// n, f, d are already selection, filter and detail.
	ToggleGroup key.Binding
	Collapse    key.Binding
	Expand      key.Binding
	CollapseAll key.Binding
	ExpandAll   key.Binding
	// Issues opens the pane listing every skipped image and unreadable file.
	// IssuesClose is read only while that pane is open, which is the one reason
	// it may share esc with Quit.
	Issues      key.Binding
	IssuesClose key.Binding
	Filter      key.Binding
	Target      key.Binding
	// The sidebar decides one image at a time: which release it moves to and
	// whether that is remembered. Focus moves there and back on tab, and the
	// two values are changed with ←/→ once it is there — which is why the row
	// target and the pin have no keys of their own any more.
	Focus     key.Binding
	FocusBack key.Binding
	ValueNext key.Binding
	ValuePrev key.Binding
	// Apply writes the selection; ApplyRow writes only the row under the cursor.
	// Both are deliberately off enter: enter is a reflex key, and a reflex key
	// must not rewrite compose files.
	Apply    key.Binding
	ApplyRow key.Binding
	Help     key.Binding
	Quit     key.Binding
	Yes      key.Binding
	No       key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Home:     key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first")),
		End:      key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last")),
		Toggle:   key.NewBinding(key.WithKeys(" ", "enter"), key.WithHelp("space/enter", "toggle")),
		// a and n act on the cursor's subtree, not the whole list, so their help
		// text says "here" rather than promising a sweep they no longer do.
		SelectAll:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select here")),
		SelectNone: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "deselect here")),

		// a and n act on the cursor's subtree, which on a tree several directories
		// wide means neither can reach the whole list any more. These two restore
		// that reach; ctrl pairs them with the letters they widen.
		SelectAllGlobal:  key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "select all")),
		SelectNoneGlobal: key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "deselect all")),

		ToggleGroup: key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "fold node")),
		Collapse:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse/parent")),
		Expand:      key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand/child")),
		CollapseAll: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "collapse all")),
		ExpandAll:   key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "expand all")),

		Issues:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "issues")),
		IssuesClose: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back to list")),

		Filter: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
		Target: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "target level")),
		// tab moves between the two halves of the frame, which is the one thing
		// every split layout already means by it. esc leaves the sidebar as well,
		// sharing the key with Quit the way IssuesClose does, and for the same
		// reason: while the sidebar is focused, esc means "back".
		Focus:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "details")),
		FocusBack: key.NewBinding(key.WithKeys("tab", "esc"), key.WithHelp("tab/esc", "back to list")),
		// ←/→ walk the tree in the list, so they are free to change a value once
		// the sidebar has the focus; h/l come along for the same reason.
		ValueNext: key.NewBinding(key.WithKeys("right", "l", "+"), key.WithHelp("→", "next value")),
		ValuePrev: key.NewBinding(key.WithKeys("left", "h", "-"), key.WithHelp("←", "previous value")),

		// Shift-a pairs with `a` (select all) and stays clear of every taken key;
		// `u` reads as "update this one" and is the only free letter near it.
		Apply:    key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "apply selected")),
		ApplyRow: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "apply row")),

		// p is the only free letter that reads as "pin"; g is free as well and is
		// the initial of the scope it answers for, so neither answer needs a key
		// the user has to be told twice.

		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit: key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
		Yes:  key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")),
		No:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),
	}
}

// Bindings lists every binding in display order for help rendering.
func (k KeyMap) Bindings() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End,
		k.Toggle, k.SelectAll, k.SelectNone, k.SelectAllGlobal, k.SelectNoneGlobal,
		k.ToggleGroup, k.Collapse, k.Expand, k.CollapseAll, k.ExpandAll,
		k.Filter, k.Target, k.Focus, k.ValueNext, k.ValuePrev, k.Issues, k.Apply, k.ApplyRow, k.Help, k.Quit,
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.Collapse, k.Expand, k.Focus, k.Filter, k.Target, k.Issues, k.Apply, k.ApplyRow, k.Help, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End},
		{k.Toggle, k.SelectAll, k.SelectNone, k.SelectAllGlobal, k.SelectNoneGlobal},
		{k.Filter, k.Issues},
		{k.ToggleGroup, k.Collapse, k.Expand, k.CollapseAll, k.ExpandAll},
		{k.Focus, k.ValuePrev, k.ValueNext, k.Target},
		{k.Apply, k.ApplyRow, k.Help, k.Quit},
	}
}

// The hint sets below back the always-on footer. They are per phase because a
// footer advertising `A apply selected` while the restart question is on screen is
// worse than no footer at all: every key it names would be ignored.

// ScanHints are the keys that already work while rows are still streaming in.
// Applying is deliberately absent — A and u work, but offering them mid-scan
// invites committing a half-finished list.
func (k KeyMap) ScanHints() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.Collapse, k.Expand, k.Filter, k.Issues, k.Help, k.Quit}
}

// IssueHints are the keys the issues pane reads. It leads with the way out,
// because a pane covering the list has to say how to leave it.
func (k KeyMap) IssueHints() []key.Binding {
	return []key.Binding{k.IssuesClose, k.Issues, k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End, k.Quit}
}

// BrowseHints are the full working set, once the scan has settled. The order is
// deliberately not ShortHelp's display order: the footer budgets the last hint
// first and then fills from the left, so the two keys that actually write —
// which no longer sit on the obvious `enter` — come early enough to survive a
// terminal too narrow for the whole set. The tree keys follow them rather than
// lead: ←/→ are the two hints a user is likeliest to try unprompted, so they are
// the cheapest ones to lose to truncation.
func (k KeyMap) BrowseHints() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.Toggle, k.Apply, k.ApplyRow, k.Focus,
		k.Collapse, k.Expand, k.Filter, k.Target, k.Issues, k.Help, k.Quit,
	}
}

// ApplyHints: the apply phase ignores every key but quit.
func (k KeyMap) ApplyHints() []key.Binding { return []key.Binding{k.Quit} }

// SideHints are the keys the sidebar reads while it has the focus. It leads
// with the way out, because a column that has taken the keyboard has to say how
// to give it back.
func (k KeyMap) SideHints() []key.Binding {
	return []key.Binding{k.FocusBack, k.Up, k.Down, k.ValuePrev, k.ValueNext, k.Quit}
}

// HelpHints are the only keys the help dialog reads. A footer advertising the
// list keys while a dialog is covering the list would name keys that phase
// throws away, which is the one thing the footer must never do.
func (k KeyMap) HelpHints() []key.Binding { return []key.Binding{k.Help, k.IssuesClose, k.Quit} }

// RestartHints are the only two answers the restart question accepts.
func (k KeyMap) RestartHints() []key.Binding { return []key.Binding{k.Yes, k.No, k.Quit} }
