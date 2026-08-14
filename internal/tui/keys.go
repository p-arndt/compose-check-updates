package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

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
	// Fold keys. ←/h and →/l carry their usual tree meaning; on a row, which has
	// nothing to expand, →/l opens the detail column instead.
	ToggleGroup key.Binding
	Collapse    key.Binding
	Expand      key.Binding
	CollapseAll key.Binding
	ExpandAll   key.Binding
	// Issues opens the pane listing every skipped image and unreadable file.
	// IssuesClose is esc, which everywhere means "back", never "quit".
	Issues      key.Binding
	IssuesClose key.Binding
	Filter      key.Binding
	Target      key.Binding
	// Three places can hold the keyboard: the list, the bar and the detail
	// column. Focus/FocusPrev/Bar reach them; FocusBack is the way out.
	Focus     key.Binding
	FocusPrev key.Binding
	FocusBack key.Binding
	Bar       key.Binding
	// ValueNext/ValuePrev act on whatever has the focus; BarNext/BarPrev move
	// along the bar; SideNext/SidePrev step the detail column's options.
	ValueNext key.Binding
	ValuePrev key.Binding
	BarNext   key.Binding
	BarPrev   key.Binding
	SideNext  key.Binding
	SidePrev  key.Binding
	// Apply writes the selection; ApplyRow writes only the row under the cursor.
	// Neither is on enter: a reflex key must not rewrite compose files. The bar's
	// apply button is the exception, and only once walked onto deliberately.
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
		// a and n act on the cursor's subtree, hence "here" in the help text.
		SelectAll:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select here")),
		SelectNone: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "deselect here")),

		// a and n cannot reach past the cursor's subtree; these restore that
		// reach, with ctrl pairing them to the letters they widen.
		SelectAllGlobal:  key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "select all")),
		SelectNoneGlobal: key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "deselect all")),

		ToggleGroup: key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "fold node")),
		Collapse:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse / back")),
		Expand:      key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand / details")),
		CollapseAll: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "collapse all")),
		ExpandAll:   key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "expand all")),

		Issues:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "issues")),
		IssuesClose: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back to list")),

		Filter: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
		Target: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "target level")),
		// tab asks what the cursor is on: an image opens the detail column, a
		// header goes to the bar. tab and esc both leave the column again.
		Focus:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "details / bar")),
		FocusBack: key.NewBinding(key.WithKeys("tab", "esc"), key.WithHelp("tab/esc", "back to list")),

		// One rule everywhere: arrows move, space and enter act on whatever has
		// the focus — a row selects, a bar or column setting steps.
		ValueNext: key.NewBinding(key.WithKeys(" ", "enter", "+"), key.WithHelp("space/enter", "change")),
		ValuePrev: key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "step back")),

		// ←/→ only move between stops; they never change a value.
		BarNext: key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→", "next stop")),
		BarPrev: key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←", "previous stop")),

		// In the detail column the same arrows step the options of the field under
		// the cursor; nothing there is a tree. Leaving is tab/esc.
		SideNext: key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next option")),
		SidePrev: key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "previous option")),

		// A pairs with `a` (select all); `u` reads as "update this one".
		Apply:    key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "apply selected")),
		ApplyRow: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "apply row")),

		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		// esc is deliberately absent: it means "back" everywhere, never "quit".
		Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Yes:  key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")),
		No:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),

		// Backwards along the bar, so a stop overshot costs one keypress rather
		// than a lap.
		FocusPrev: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),

		// tab reaches the bar only from a header, so `m` is what reaches it from a
		// row, and pressing it again walks along it.
		Bar: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "bar / next stop")),
	}
}

// Bindings lists every binding in display order for help rendering.
func (k KeyMap) Bindings() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End,
		k.Toggle, k.SelectAll, k.SelectNone, k.SelectAllGlobal, k.SelectNoneGlobal,
		k.ToggleGroup, k.Collapse, k.Expand, k.CollapseAll, k.ExpandAll,
		k.Filter, k.Target, k.Focus, k.Bar, k.BarNext, k.BarPrev, k.ValueNext, k.ValuePrev, k.Issues, k.Apply, k.ApplyRow, k.Help, k.Quit,
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
		{k.Focus, k.Bar, k.ValuePrev, k.ValueNext, k.Target},
		{k.Apply, k.ApplyRow, k.Help, k.Quit},
	}
}

// The hint sets below back the always-on footer. They are per phase so it never
// advertises a key the current phase throws away.

// ScanHints are the keys that already work while rows are still streaming in.
// Applying is deliberately absent — A and u work, but offering them mid-scan
// invites committing a half-finished list.
func (k KeyMap) ScanHints() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.Collapse, k.Expand, k.Filter, k.Issues, k.Help, k.Quit}
}

// IssueHints are the keys the issues pane reads, leading with the way out.
func (k KeyMap) IssueHints() []key.Binding {
	return []key.Binding{k.IssuesClose, k.Issues, k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End, k.Quit}
}

// BrowseHints are the full working set, once the scan has settled. The footer
// budgets the last hint first and fills from the left, so the order puts the two
// apply keys early enough to survive a narrow terminal and the tree keys — the
// ones a user tries unprompted anyway — last.
func (k KeyMap) BrowseHints() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.Toggle, k.Apply, k.ApplyRow, k.Focus, k.Bar,
		k.Collapse, k.Expand, k.Filter, k.Target, k.Issues, k.Help, k.Quit,
	}
}

// ApplyHints: the apply phase ignores every key but quit.
func (k KeyMap) ApplyHints() []key.Binding { return []key.Binding{k.Quit} }

// SideHints are the keys the sidebar reads while it has the focus, leading with
// the way out.
func (k KeyMap) SideHints() []key.Binding {
	return []key.Binding{k.FocusBack, k.Up, k.Down, k.SidePrev, k.SideNext, k.Quit}
}

// HelpHints are the only keys the help dialog reads.
func (k KeyMap) HelpHints() []key.Binding { return []key.Binding{k.Help, k.IssuesClose, k.Quit} }

// BarHints are the keys the bar reads while it has the focus, leading with the
// way out. It stays one line: the footer's height feeds listHeight.
func (k KeyMap) BarHints() []key.Binding {
	return []key.Binding{k.IssuesClose, k.BarPrev, k.BarNext, k.ValueNext, k.Quit}
}

// RestartHints are the only two answers the restart question accepts.
func (k KeyMap) RestartHints() []key.Binding { return []key.Binding{k.Yes, k.No, k.Quit} }

// HelpEntry is one line of the help dialog: the keys, and what they do. It is
// built from the bindings rather than written out, so a rebinding still cannot
// leave the dialog naming a key that does nothing.
type HelpEntry struct {
	Keys string
	Desc string
}

// HelpSection groups the entries the dialog draws under one heading.
type HelpSection struct {
	Title   string
	Entries []HelpEntry
}

// helpEntry takes a binding's own help text verbatim.
func helpEntry(b key.Binding) HelpEntry {
	h := b.Help()
	return HelpEntry{Keys: h.Key, Desc: h.Desc}
}

// helpPair merges two bindings that are one idea — ↑ and ↓, say — onto one line,
// with a description neither half could carry alone.
func helpPair(desc string, bs ...key.Binding) HelpEntry {
	keys := make([]string, 0, len(bs))
	for _, b := range bs {
		if k := b.Help().Key; k != "" {
			keys = append(keys, k)
		}
	}
	return HelpEntry{Keys: strings.Join(keys, " "), Desc: desc}
}

// HelpSections is what `?` shows: every binding, grouped by the thing it acts
// on rather than by KeyMap declaration order.
func (k KeyMap) HelpSections() []HelpSection {
	return []HelpSection{
		{Title: "LIST", Entries: []HelpEntry{
			helpPair("move", k.Up, k.Down),
			helpPair("page", k.PageUp, k.PageDown),
			helpPair("first / last", k.Home, k.End),
			helpEntry(k.Filter),
			helpEntry(k.Issues),
			helpEntry(k.Help),
			helpEntry(k.Quit),
		}},
		{Title: "TREE", Entries: []HelpEntry{
			helpEntry(k.Collapse),
			helpEntry(k.Expand),
			helpEntry(k.ToggleGroup),
			helpEntry(k.CollapseAll),
			helpEntry(k.ExpandAll),
		}},
		{Title: "SELECT", Entries: []HelpEntry{
			helpEntry(k.Toggle),
			helpEntry(k.SelectAll),
			helpEntry(k.SelectNone),
			helpEntry(k.SelectAllGlobal),
			helpEntry(k.SelectNoneGlobal),
		}},
		{Title: "BAR & DETAILS", Entries: []HelpEntry{
			helpEntry(k.Focus),
			helpEntry(k.Bar),
			helpPair("move between the bar's stops", k.BarPrev, k.BarNext),
			helpEntry(k.FocusPrev),
			helpPair("move between the column's fields", k.Up, k.Down),
			helpPair("step the options of the column's field", k.SidePrev, k.SideNext),
			helpPair("change the setting under the cursor", k.ValueNext, k.ValuePrev),
			// The bar still leaves in the direction it sits from the list; the
			// column does not, because its arrows are spent on its options.
			{Keys: k.Down.Help().Key, Desc: "off the bar (it is above)"},
			{Keys: k.Up.Help().Key, Desc: "onto the bar, from the top of the list"},
			helpEntry(k.FocusBack),
		}},
		{Title: "APPLY", Entries: []HelpEntry{
			helpEntry(k.Apply),
			helpEntry(k.ApplyRow),
			{Keys: "y / n", Desc: "answer the restart question"},
		}},
		{Title: "ISSUES", Entries: []HelpEntry{
			helpEntry(k.Issues),
			helpEntry(k.IssuesClose),
		}},
	}
}
