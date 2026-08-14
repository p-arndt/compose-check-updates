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
	// Fold keys. The list is a directory tree, so ←/h and →/l carry the meaning
	// every tree in every file browser gives them: collapse-or-go-to-parent,
	// expand-or-step-into-child. On a row, which has nothing to expand, →/l opens
	// the detail column instead and ←/h closes it — the same gesture one level
	// out. z/C/E stay as the keyboard-only shortcuts.
	ToggleGroup key.Binding
	Collapse    key.Binding
	Expand      key.Binding
	CollapseAll key.Binding
	ExpandAll   key.Binding
	// Issues opens the pane listing every skipped image and unreadable file.
	// IssuesClose is esc, which now means one thing everywhere it is read: back
	// out of whatever has the keyboard, never out of the program.
	Issues      key.Binding
	IssuesClose key.Binding
	Filter      key.Binding
	Target      key.Binding
	// Three places can hold the keyboard: the list, the bar above it and the
	// detail column beside it. Focus/FocusPrev/Bar reach them, FocusBack leaves
	// the column, and each place is left in the direction it sits from the list.
	Focus     key.Binding
	FocusPrev key.Binding
	FocusBack key.Binding
	Bar       key.Binding
	// ValueNext/ValuePrev act on whatever has the focus; BarNext/BarPrev move
	// along the bar. Movement and change are separate keys everywhere, which is
	// the rule the whole arrangement rests on.
	ValueNext key.Binding
	ValuePrev key.Binding
	BarNext   key.Binding
	BarPrev   key.Binding
	// Apply writes the selection; ApplyRow writes only the row under the cursor.
	// Both are deliberately off enter: enter is a reflex key, and a reflex key
	// must not rewrite compose files. The one exception is the bar's apply
	// button, which enter does press — but only once the keyboard has been
	// walked onto it on purpose.
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
		Collapse:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse / back")),
		Expand:      key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand / details")),
		CollapseAll: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "collapse all")),
		ExpandAll:   key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "expand all")),

		Issues:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "issues")),
		IssuesClose: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back to list")),

		Filter: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
		Target: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "target level")),
		// tab asks what the cursor is on: an image has a detail column to open, a
		// header has not, so there it goes to the bar. tab and esc both leave the
		// column again.
		Focus:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "details / bar")),
		FocusBack: key.NewBinding(key.WithKeys("tab", "esc"), key.WithHelp("tab/esc", "back to list")),

		// One rule, everywhere: the arrows move, space and enter act on whatever
		// has the focus. In the list that is the row, so they select; on the bar
		// and in the detail column it is the setting under the cursor, so they
		// step it. Nothing has to be remembered per pane — which is what every
		// earlier arrangement here failed at.
		ValueNext: key.NewBinding(key.WithKeys(" ", "enter", "+"), key.WithHelp("space/enter", "change")),
		ValuePrev: key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "step back")),

		// ←/→ move between the bar's stops, the way ↑/↓ move between the column's
		// fields. They no longer change anything: an arrow that navigates in one
		// pane and edits in another is the confusion this rule exists to end.
		BarNext: key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→", "next stop")),
		BarPrev: key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←", "previous stop")),

		// Shift-a pairs with `a` (select all) and stays clear of every taken key;
		// `u` reads as "update this one" and is the only free letter near it.
		Apply:    key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "apply selected")),
		ApplyRow: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "apply row")),

		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		// esc is not on here. It means "back" in every pane that can take the
		// keyboard, and a key that goes back four times and quits the fifth is a
		// key you learn to distrust. Quitting is `q`, deliberately and only.
		Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Yes:  key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")),
		No:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),

		// Backwards along the bar, so a stop overshot costs one keypress rather
		// than a lap.
		FocusPrev: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),

		// tab lands on the bar only when the cursor is on no image at all, so `m`
		// is what reaches it from a row — and pressing it again walks it, which
		// tab cannot do there without contradicting what it says about the row.
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
		k.Up, k.Down, k.Toggle, k.Apply, k.ApplyRow, k.Focus, k.Bar,
		k.Collapse, k.Expand, k.Filter, k.Target, k.Issues, k.Help, k.Quit,
	}
}

// ApplyHints: the apply phase ignores every key but quit.
func (k KeyMap) ApplyHints() []key.Binding { return []key.Binding{k.Quit} }

// SideHints are the keys the sidebar reads while it has the focus. It leads
// with the way out, because a column that has taken the keyboard has to say how
// to give it back.
func (k KeyMap) SideHints() []key.Binding {
	return []key.Binding{k.Collapse, k.Up, k.Down, k.ValueNext, k.ValuePrev, k.Quit}
}

// HelpHints are the only keys the help dialog reads. A footer advertising the
// list keys while a dialog is covering the list would name keys that phase
// throws away, which is the one thing the footer must never do.
func (k KeyMap) HelpHints() []key.Binding { return []key.Binding{k.Help, k.IssuesClose, k.Quit} }

// BarHints are the keys the bar reads while it has the focus. It leads with the
// way out, the way the issues pane and the detail column do, and for the same
// reason: anything that has taken the keyboard has to say how to give it back.
// It stays one line — the footer feeds listHeight, and a taller hint set would
// resize the list.
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

// helpPair merges two bindings that are one idea onto one line — ↑ and ↓ are
// never looked up separately — keeping the keys from the bindings and giving
// the pair a description neither half could carry alone.
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
// on rather than by the order the KeyMap happens to declare them. The grouping
// is the point — a flat list of twenty keys is a list nobody reads twice.
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
			helpPair("change the setting under the cursor", k.ValueNext, k.ValuePrev),
			// Each pane leaves in the direction it sits from the list. That is the
			// whole rule, and it is worth spelling out rather than leaving to be
			// discovered once per pane.
			{Keys: k.Collapse.Help().Key, Desc: "out of the column (it is to the right)"},
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
