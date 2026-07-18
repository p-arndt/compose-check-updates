package tui

import "strings"

// node is one foldable level of the path tree that backs the list headers. The
// tree exists because compose files are usually nested several directories deep
// and folding only at file level still leaves the list unreadable: a stack of
// twenty `.NOT_RUNNING/<app>/docker-compose.yml` headers says nothing the user
// can navigate by.
type node struct {
	// key is the full path prefix down to this node and is what collapse state
	// is keyed by. It stays stable across re-sorts and across compression, which
	// is why a merged chain keeps the deeper node's key rather than the label.
	key string
	// label is the segment (or the joined chain of segments) this row displays.
	label string
	// depth is 0 for roots; the renderer turns it into indentation.
	depth int
	// isFile marks the leaf that owns rows — the compose file itself.
	isFile bool
	// parent indexes into Model.nodes, -1 for a root. Parents always precede
	// their children in the slice, which lets the subtree walks below be plain
	// forward loops instead of recursion.
	parent int
}

// pathSegments splits a compose file path into its segments. The scanner walks
// with filepath.Walk, so on Windows the same run can produce both separators;
// normalising here is what keeps node keys — and therefore collapse state —
// identical no matter which one a path arrived with. A leading separator is
// folded into the first segment so an absolute path does not lose its root.
func pathSegments(path string) []string {
	p := strings.ReplaceAll(path, "\\", "/")
	abs := strings.HasPrefix(p, "/")

	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if abs && len(segs) > 0 {
		segs[0] = "/" + segs[0]
	}
	return segs
}

// treeNode is the mutable trie the tree is assembled in before it is flattened
// into the []node the model serves. Children are kept in a slice as well as a
// map so insertion order — which is the caller's sorted path order — survives,
// giving a deterministic tree with directories and files interleaved exactly as
// the sorted paths dictate.
type treeNode struct {
	seg      string
	key      string
	isFile   bool
	children []*treeNode
	index    map[string]*treeNode
}

// buildTree turns compose file paths into the flattened node tree, plus lookups
// from a node key and from a raw (un-normalised) file path to the node index.
// Paths are expected in their final sorted order.
func buildTree(paths []string) (nodes []node, byKey map[string]int, byPath map[string]int) {
	root := &treeNode{index: make(map[string]*treeNode)}
	leaf := make(map[string]*treeNode, len(paths))

	for _, p := range paths {
		segs := pathSegments(p)
		if len(segs) == 0 {
			continue
		}

		cur, key := root, ""
		for i, s := range segs {
			if i == 0 {
				key = s
			} else {
				key += "/" + s
			}
			child, ok := cur.index[s]
			if !ok {
				child = &treeNode{seg: s, key: key, index: make(map[string]*treeNode)}
				cur.index[s] = child
				cur.children = append(cur.children, child)
			}
			cur = child
		}
		cur.isFile = true
		leaf[p] = cur
	}

	// root is a sentinel holding the roots, so it is never compressed itself —
	// merging into it would prefix every label with an empty segment.
	for i, c := range root.children {
		root.children[i] = compress(c)
	}

	byKey = make(map[string]int, len(paths))
	var walk func(n *treeNode, depth, parent int)
	walk = func(n *treeNode, depth, parent int) {
		idx := len(nodes)
		nodes = append(nodes, node{key: n.key, label: n.seg, depth: depth, isFile: n.isFile, parent: parent})
		byKey[n.key] = idx
		for _, c := range n.children {
			walk(c, depth+1, idx)
		}
	}
	for _, c := range root.children {
		walk(c, 0, -1)
	}

	byPath = make(map[string]int, len(leaf))
	for p, n := range leaf {
		byPath[p] = byKey[n.key]
	}
	return nodes, byKey, byPath
}

// compress collapses single-child chains into one row: a directory that holds
// exactly one child and no rows of its own is not a fold the user can make a
// decision about, it is just a line to scroll past. The merged row keeps the
// deeper node's key so collapse state survives a directory gaining a second
// child mid-scan, and it keeps the shallower node's depth so the tree does not
// indent for levels that are no longer drawn.
func compress(n *treeNode) *treeNode {
	for i, c := range n.children {
		n.children[i] = compress(c)
	}
	if !n.isFile && len(n.children) == 1 {
		child := n.children[0]
		child.seg = n.seg + "/" + child.seg
		return child
	}
	return n
}

// subtreeMask marks the given node and every node below it. Children always
// follow their parent in the slice, so one forward pass is enough.
func (m Model) subtreeMask(nodeIdx int) []bool {
	in := make([]bool, len(m.nodes))
	if nodeIdx < 0 || nodeIdx >= len(m.nodes) {
		return in
	}
	in[nodeIdx] = true
	for i := nodeIdx + 1; i < len(m.nodes); i++ {
		if p := m.nodes[i].parent; p >= 0 && in[p] {
			in[i] = true
		}
	}
	return in
}

// fileNode is the node index a row entry belongs to, or -1 when the row's file
// is not in the current tree (which only happens between a filter change and
// the rebuild that follows it).
func (m Model) fileNode(path string) int {
	if i, ok := m.nodeByFile[path]; ok {
		return i
	}
	return -1
}

// cursorNode is the node the cursor is in: the header's own node, or the file
// node owning the row it sits on. -1 when the list is empty.
func (m Model) cursorNode() int {
	e, ok := m.currentEntry()
	if !ok {
		return -1
	}
	if e.kind == entryHeader {
		return e.node
	}
	return m.fileNode(e.path)
}

// subtreeRows is every row under a node, filter included — it is drawn from
// m.visible, so a key acting on a directory acts on exactly what that directory
// currently shows, folded or not.
func (m Model) subtreeRows(nodeIdx int) []int {
	out := []int{}
	if nodeIdx < 0 || nodeIdx >= len(m.nodes) {
		return out
	}
	in := m.subtreeMask(nodeIdx)
	for _, ri := range m.visible {
		if n := m.fileNode(m.rows[ri].FilePath()); n >= 0 && in[n] {
			out = append(out, ri)
		}
	}
	return out
}

// subtreeFiles is the compose files under a node, in list order and without
// duplicates. A file only has a node when it has at least one visible row, so
// this follows the filter for free.
func (m Model) subtreeFiles(nodeIdx int) []string {
	out := []string{}
	seen := make(map[string]bool)
	for _, ri := range m.subtreeRows(nodeIdx) {
		p := m.rows[ri].FilePath()
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// headerIndex is the entry index of a node's header line, or -1 when an
// ancestor is collapsed and the header is therefore not drawn.
func (m Model) headerIndex(nodeIdx int) int {
	for i, e := range m.entries {
		if e.kind == entryHeader && e.node == nodeIdx {
			return i
		}
	}
	return -1
}

// entryInSubtree reports whether a list line belongs under the given node.
func (m Model) entryInSubtree(e entry, nodeIdx int) bool {
	i := e.node
	if e.kind == entryRow {
		i = m.fileNode(e.path)
	}
	for ; i >= 0; i = m.nodes[i].parent {
		if i == nodeIdx {
			return true
		}
	}
	return false
}

// collapseOrParent is the left-hand half of tree navigation: fold what the
// cursor is in, or — when it is already folded — step out to the level above.
// At a collapsed root there is nothing left to leave, so it does nothing.
func (m *Model) collapseOrParent() {
	n := m.cursorNode()
	if n < 0 {
		return
	}
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}

	if key := m.nodes[n].key; !m.collapsed[key] {
		m.collapsed[key] = true
		// Rebuilding on the current key is what walks the cursor up onto the
		// header when the row it was sitting on has just been folded away.
		m.rebuild(m.cursorKey())
	} else if p := m.nodes[n].parent; p >= 0 {
		if i := m.headerIndex(p); i >= 0 {
			m.cursor = i
		}
	}

	m.clampCursor()
	m.syncScroll()
}

// expandOrChild is the mirror image: unfold what the cursor is on, or step into
// it. The step is one line down and only inside the node's own subtree, so it
// never leaks into the next sibling — on an expanded leaf's last row there is
// nothing further in, and the key does nothing.
func (m *Model) expandOrChild() {
	n := m.cursorNode()
	if n < 0 {
		return
	}
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool)
	}

	if key := m.nodes[n].key; m.collapsed[key] {
		m.collapsed[key] = false
		m.rebuild(m.cursorKey())
	} else if next := m.cursor + 1; next < len(m.entries) && m.entryInSubtree(m.entries[next], n) {
		m.cursor = next
	}

	m.clampCursor()
	m.syncScroll()
}
