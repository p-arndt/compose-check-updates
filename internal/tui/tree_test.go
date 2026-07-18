package tui

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// treeOf builds a model holding exactly one row per given compose file, which is
// all the tree cares about: the shape comes from the paths, never from the rows.
func treeOf(t *testing.T, paths ...string) Model {
	t.Helper()
	m := newTestModel()
	for i, p := range paths {
		m = feed(t, m, updateEvent(p, fmt.Sprintf("img%02d", i), "1.0", "2.0", "major"))
	}
	return m
}

// nodeIdx is the index of the tree node with the given key. Tests address nodes
// by key because the index is an implementation detail that shifts whenever a
// path is added above it.
func nodeIdx(t *testing.T, m Model, key string) int {
	t.Helper()
	for i, n := range m.nodes {
		if n.key == key {
			return i
		}
	}
	t.Fatalf("no tree node %q; have %v", key, nodeKeys(m))
	return -1
}

func nodeKeys(m Model) []string {
	out := make([]string, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, n.key)
	}
	return out
}

// dir/file build the expected nodes compactly, so a table row reads as the tree
// it describes rather than as five struct fields.
func dir(key, label string, depth, parent int) node {
	return node{key: key, label: label, depth: depth, isFile: false, parent: parent}
}

func file(key, label string, depth, parent int) node {
	return node{key: key, label: label, depth: depth, isFile: true, parent: parent}
}

// The tree is the whole feature: a directory chain nobody branched at is one
// row, and every branch point is a row of its own.
func TestTreeConstruction(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []node
	}{
		{
			name:  "a single file with no directory at all",
			paths: []string{"compose.yml"},
			want:  []node{file("compose.yml", "compose.yml", 0, -1)},
		},
		{
			// The historical one-header-per-file layout: one directory holding one
			// file compresses back to exactly the line the flat list used to draw.
			name:  "one directory with one file compresses to one row",
			paths: []string{"a/compose.yml"},
			want:  []node{file("a/compose.yml", "a/compose.yml", 0, -1)},
		},
		{
			name:  "a chain of single-child directories collapses into one row",
			paths: []string{"a/b/c/compose.yml"},
			want:  []node{file("a/b/c/compose.yml", "a/b/c/compose.yml", 0, -1)},
		},
		{
			name:  "two files in the same directory make that directory a row",
			paths: []string{"a/one.yml", "a/two.yml"},
			want: []node{
				dir("a", "a", 0, -1),
				file("a/one.yml", "one.yml", 1, 0),
				file("a/two.yml", "two.yml", 1, 0),
			},
		},
		{
			name:  "two siblings under one root",
			paths: []string{"stacks/blue/compose.yml", "stacks/green/compose.yml"},
			want: []node{
				dir("stacks", "stacks", 0, -1),
				file("stacks/blue/compose.yml", "blue/compose.yml", 1, 0),
				file("stacks/green/compose.yml", "green/compose.yml", 1, 0),
			},
		},
		{
			// The motivating layout: .NOT_RUNNING/ branches, so it keeps its own
			// row, while each app directory holds one file and merges with it.
			name: "the branching root keeps its row and each app merges with its file",
			paths: []string{
				".NOT_RUNNING/appsmith/docker-compose.yml",
				".NOT_RUNNING/gitea/docker-compose.yml",
			},
			want: []node{
				dir(".NOT_RUNNING", ".NOT_RUNNING", 0, -1),
				file(".NOT_RUNNING/appsmith/docker-compose.yml", "appsmith/docker-compose.yml", 1, 0),
				file(".NOT_RUNNING/gitea/docker-compose.yml", "gitea/docker-compose.yml", 1, 0),
			},
		},
		{
			// Compression has to stop at the branch and start again after it: srv
			// and apps are separate rows, other/compose.yml is a single one.
			name: "compression stops at a branch and resumes below it",
			paths: []string{
				"srv/apps/db/compose.yml",
				"srv/apps/web/compose.yml",
				"srv/other/compose.yml",
			},
			want: []node{
				dir("srv", "srv", 0, -1),
				dir("srv/apps", "apps", 1, 0),
				file("srv/apps/db/compose.yml", "db/compose.yml", 2, 1),
				file("srv/apps/web/compose.yml", "web/compose.yml", 2, 1),
				file("srv/other/compose.yml", "other/compose.yml", 1, 0),
			},
		},
		{
			// A path scanned on Windows arrives with backslashes; the tree is the
			// one place that would otherwise show them, so it normalises.
			name:  "windows separators are normalised to forward slashes",
			paths: []string{`tests\folder1\compose.yaml`, `tests\folder2\compose.yaml`},
			want: []node{
				dir("tests", "tests", 0, -1),
				file("tests/folder1/compose.yaml", "folder1/compose.yaml", 1, 0),
				file("tests/folder2/compose.yaml", "folder2/compose.yaml", 1, 0),
			},
		},
		{
			name:  "a root-level file sits beside a directory as its own root",
			paths: []string{"compose.yml", "svc/compose.yml"},
			want: []node{
				file("compose.yml", "compose.yml", 0, -1),
				file("svc/compose.yml", "svc/compose.yml", 0, -1),
			},
		},
		{
			name:  "several unrelated roots each stand alone",
			paths: []string{"a/compose.yml", "b/compose.yml"},
			want: []node{
				file("a/compose.yml", "a/compose.yml", 0, -1),
				file("b/compose.yml", "b/compose.yml", 0, -1),
			},
		},
		{
			name: "a directory holding both a file and a subdirectory",
			paths: []string{
				"srv/compose.yml",
				"srv/db/compose.yml",
			},
			want: []node{
				dir("srv", "srv", 0, -1),
				file("srv/compose.yml", "compose.yml", 1, 0),
				file("srv/db/compose.yml", "db/compose.yml", 1, 0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := treeOf(t, tt.paths...)
			assert.Equal(t, tt.want, m.nodes)
		})
	}
}

// Nodes are listed in depth-first pre-order, which is what makes an entry list
// built by walking them come out in reading order.
func TestTreeNodesArePreOrderAndParentsPrecedeChildren(t *testing.T) {
	m := treeOf(t,
		"srv/apps/db/compose.yml",
		"srv/apps/web/compose.yml",
		"srv/other/compose.yml",
		"zz/compose.yml",
	)

	for i, n := range m.nodes {
		require.GreaterOrEqual(t, n.parent, -1)
		require.Less(t, n.parent, i, "a parent must be listed before its child")
		if n.parent < 0 {
			assert.Equal(t, 0, n.depth, "%s is a root", n.key)
			continue
		}
		assert.Equal(t, m.nodes[n.parent].depth+1, n.depth, "%s", n.key)
		assert.False(t, m.nodes[n.parent].isFile, "%s: a file node owns rows, never nodes", n.key)
	}
}

// Rows stream in from many goroutines in whatever order registries answer, so
// the tree may not depend on arrival order — addRow sorts the rows, and the tree
// has to be a pure function of the resulting set.
func TestTreeIsDeterministicRegardlessOfArrivalOrder(t *testing.T) {
	paths := []string{
		"srv/apps/web/compose.yml",
		"srv/other/compose.yml",
		"srv/apps/db/compose.yml",
		"a/compose.yml",
	}
	want := treeOf(t, paths...).nodes

	orders := [][]string{
		{paths[3], paths[2], paths[1], paths[0]},
		{paths[1], paths[3], paths[0], paths[2]},
		{paths[2], paths[0], paths[3], paths[1]},
	}
	for i, order := range orders {
		assert.Equal(t, want, treeOf(t, order...).nodes, "order %d", i)
	}
}

// The tree is the shape of what is on screen, so it is rebuilt against the
// filtered rows: a directory left holding one visible file is no longer a branch
// and compresses away, exactly as it would had the other files never scanned.
// Total still counts the hidden rows, so nothing is concealed — see groupInfo.
func TestTreeFollowsTheFilter(t *testing.T) {
	m := newTestModel()
	m = feed(t, m,
		updateEvent("stacks/blue/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("stacks/green/compose.yml", "redis", "7.0", "7.2", "minor"),
		updateEvent("stacks/red/compose.yml", "postgres", "15", "16", "major"),
	)
	require.Equal(t, []string{
		"stacks", "stacks/blue/compose.yml", "stacks/green/compose.yml", "stacks/red/compose.yml",
	}, nodeKeys(m))

	m = feed(t, m, keyMsg("f")) // filter=major: only stacks/red has a row left
	assert.Equal(t, []string{"stacks/red/compose.yml"}, nodeKeys(m),
		"a branch with one visible file is no branch at all")
	assert.Equal(t, []string{"stacks/red/compose.yml", "stacks/red/compose.yml/postgres"},
		entryPaths(m))

	// Widening the filter puts the branch back exactly as it was.
	m = feed(t, m, keyMsg("f"), keyMsg("f"), keyMsg("f"), keyMsg("f"))
	require.Equal(t, FilterAll, m.filter)
	assert.Equal(t, []string{
		"stacks", "stacks/blue/compose.yml", "stacks/green/compose.yml", "stacks/red/compose.yml",
	}, nodeKeys(m))
}

// nestedTree is the fixture the folding and navigation tests share: one
// stand-alone file and one branching directory holding two files.
//
//	solo/compose.yml        nginx
//	stacks/
//	  blue/compose.yml      caddy, postgres
//	  green/compose.yml     redis
func nestedTree(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	return feed(t, m,
		updateEvent("stacks/blue/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("stacks/blue/compose.yml", "postgres", "15", "16", "major"),
		updateEvent("stacks/green/compose.yml", "redis", "7.0", "7.2", "minor"),
		updateEvent("solo/compose.yml", "nginx", "1.24", "1.25", "major"),
	)
}

func nestedEntries() []string {
	return []string{
		"solo/compose.yml", "solo/compose.yml/nginx",
		"stacks",
		"stacks/blue/compose.yml", "stacks/blue/compose.yml/caddy", "stacks/blue/compose.yml/postgres",
		"stacks/green/compose.yml", "stacks/green/compose.yml/redis",
	}
}

func TestNestedEntriesAreHeadersInReadingOrder(t *testing.T) {
	m := nestedTree(t)
	require.Equal(t, nestedEntries(), entryPaths(m))

	// A header carries its node; a row carries -1 and identifies its file through
	// the path it shares with the header above it.
	for i, e := range m.entries {
		if e.kind == entryHeader {
			require.GreaterOrEqual(t, e.node, 0, "entry %d", i)
			assert.Equal(t, m.nodes[e.node].key, e.path, "a header's path is its node key")
			assert.Equal(t, -1, e.row)
			continue
		}
		assert.Equal(t, -1, e.node, "entry %d: a row hangs off no node of its own", i)
		assert.Equal(t, m.rows[e.row].FilePath(), e.path)
		assert.True(t, m.nodes[nodeIdx(t, m, e.path)].isFile, "a row's path names a file node")
	}
}

func TestCollapsingADirectoryHidesEveryDescendantHeaderAndRow(t *testing.T) {
	m := nestedTree(t)
	m.toggleGroup("stacks")

	assert.Equal(t, []string{
		"solo/compose.yml", "solo/compose.yml/nginx",
		"stacks",
	}, entryPaths(m), "a folded directory leaves only its own header")

	// Folding is display-only: nothing left the filtered set.
	assert.Len(t, m.visible, 4)
	assert.Len(t, m.rows, 4)

	m.toggleGroup("stacks")
	assert.Equal(t, nestedEntries(), entryPaths(m), "expanding restores exactly what was there")
}

func TestCollapsingAFileHidesOnlyItsOwnRows(t *testing.T) {
	m := nestedTree(t)
	m.toggleGroup("stacks/blue/compose.yml")

	assert.Equal(t, []string{
		"solo/compose.yml", "solo/compose.yml/nginx",
		"stacks",
		"stacks/blue/compose.yml",
		"stacks/green/compose.yml", "stacks/green/compose.yml/redis",
	}, entryPaths(m), "the sibling file and the parent directory are untouched")
}

// A collapsed ancestor wins outright: expanding a node inside it changes what
// would be shown if the ancestor opened, and nothing on screen right now.
func TestACollapsedAncestorHidesAnExpandedDescendant(t *testing.T) {
	m := nestedTree(t)
	m.toggleGroup("stacks/blue/compose.yml") // fold the file
	m.toggleGroup("stacks")                  // then its directory

	require.Equal(t, []string{"solo/compose.yml", "solo/compose.yml/nginx", "stacks"}, entryPaths(m))

	// Unfolding the directory reveals the file header still folded, exactly as
	// the user left it.
	m.toggleGroup("stacks")
	assert.Equal(t, []string{
		"solo/compose.yml", "solo/compose.yml/nginx",
		"stacks",
		"stacks/blue/compose.yml",
		"stacks/green/compose.yml", "stacks/green/compose.yml/redis",
	}, entryPaths(m))
}

func TestSetAllCollapsedFoldsAndUnfoldsEveryDepth(t *testing.T) {
	m := nestedTree(t)

	m.setAllCollapsed(true)
	assert.Equal(t, []string{"solo/compose.yml", "stacks"}, entryPaths(m),
		"only the roots survive a collapse-all")
	for _, n := range m.nodes {
		assert.True(t, m.collapsed[n.key], "%s must be folded at every depth", n.key)
	}

	m.setAllCollapsed(false)
	assert.Equal(t, nestedEntries(), entryPaths(m))
	for _, n := range m.nodes {
		assert.False(t, m.collapsed[n.key], "%s", n.key)
	}
}

func TestCollapseAllAndExpandAllKeysReachEveryDepth(t *testing.T) {
	m := nestedTree(t)
	m = feed(t, m, keyMsg("C"))
	assert.Equal(t, []string{"solo/compose.yml", "stacks"}, entryPaths(m))

	m = feed(t, m, keyMsg("E"))
	assert.Equal(t, nestedEntries(), entryPaths(m))
}

// The cursor is the user's place in the list. Folding a directory it was deep
// inside has exactly one sensible landing spot: the directory they just folded.
func TestCollapsingAnAncestorLandsTheCursorOnThatAncestor(t *testing.T) {
	m := nestedTree(t)
	m.cursor = 4 // stacks/blue/compose.yml → caddy
	require.Equal(t, "caddy", m.currentRow().Update.ImageName)

	m.toggleGroup("stacks")

	require.True(t, cursorOnHeader(m))
	assert.Equal(t, "stacks", m.cursorGroup())
	assert.Equal(t, 2, m.cursor)
	assert.Nil(t, m.currentRow(), "the cursor must never point into folded-away content")

	// And the row it came from is found again when the directory reopens.
	m.toggleGroup("stacks")
	assert.Equal(t, "stacks", m.cursorGroup(), "expanding does not jump the viewport")
	assert.Equal(t, 2, m.cursor)
}

func TestCollapsingAFileLandsTheCursorOnThatFile(t *testing.T) {
	m := nestedTree(t)
	m.cursor = 5 // postgres, the second row of stacks/blue
	require.Equal(t, "postgres", m.currentRow().Update.ImageName)

	m.toggleGroup("stacks/blue/compose.yml")
	assert.True(t, cursorOnHeader(m))
	assert.Equal(t, "stacks/blue/compose.yml", m.cursorGroup())
}

// A row streaming in mid-scan re-sorts the list and rebuilds the tree; the
// cursor has to come out on the same image it went in on.
func TestRebuildAfterAddRowKeepsTheCursorOnTheSameEntry(t *testing.T) {
	m := nestedTree(t)
	m.cursor = 7 // stacks/green → redis, the last entry
	require.Equal(t, "redis", m.currentRow().Update.ImageName)

	// Rows sorting above it, one of them creating a whole new branch.
	m = feed(t, m,
		updateEvent("stacks/blue/compose.yml", "alpine", "3.18", "3.19", "minor"),
		updateEvent("aaa/compose.yml", "busybox", "1.35", "1.36", "minor"),
	)

	require.NotNil(t, m.currentRow())
	assert.Equal(t, "redis", m.currentRow().Update.ImageName)
	assert.Equal(t, "stacks/green/compose.yml", m.cursorGroup())

	// The same holds when the cursor is on a directory header.
	d := nestedTree(t)
	d.cursor = 2
	require.Equal(t, "stacks", d.cursorGroup())
	d = feed(t, d, updateEvent("aaa/compose.yml", "busybox", "1.35", "1.36", "minor"))
	assert.True(t, cursorOnHeader(d))
	assert.Equal(t, "stacks", d.cursorGroup())
}

// A new file arriving under a folded directory must not spring it open, no
// matter how deep the branch it creates.
func TestCollapseStateSurvivesANewBranchBelowIt(t *testing.T) {
	m := nestedTree(t)
	m.toggleGroup("stacks")
	require.True(t, m.collapsed["stacks"])

	m = feed(t, m, updateEvent("stacks/red/deep/compose.yml", "mariadb", "10", "11", "major"))

	assert.True(t, m.collapsed["stacks"])
	assert.Equal(t, []string{"solo/compose.yml", "solo/compose.yml/nginx", "stacks"}, entryPaths(m))

	// The node is there all the same, waiting under the fold.
	require.Contains(t, nodeKeys(m), "stacks/red/deep/compose.yml")
}

func TestCursorNodeResolvesRowsToTheirFileNode(t *testing.T) {
	m := nestedTree(t)

	m.cursor = 4 // caddy
	require.Equal(t, "caddy", m.currentRow().Update.ImageName)
	assert.Equal(t, nodeIdx(t, m, "stacks/blue/compose.yml"), m.cursorNode())
	assert.Equal(t, "stacks/blue/compose.yml", m.cursorGroup())

	m.cursor = 2 // the stacks/ directory header
	assert.Equal(t, nodeIdx(t, m, "stacks"), m.cursorNode())
	assert.Equal(t, "stacks", m.cursorGroup())

	empty := newTestModel()
	assert.Equal(t, -1, empty.cursorNode())
	assert.Equal(t, "", empty.cursorGroup())
}

func TestSubtreeRowsAndFiles(t *testing.T) {
	m := nestedTree(t)

	imagesUnder := func(m Model, key string) []string {
		var out []string
		for _, ri := range m.subtreeRows(nodeIdx(t, m, key)) {
			out = append(out, m.rows[ri].Update.ImageName)
		}
		sort.Strings(out)
		return out
	}

	assert.Equal(t, []string{"caddy", "postgres", "redis"}, imagesUnder(m, "stacks"))
	assert.Equal(t, []string{"caddy", "postgres"}, imagesUnder(m, "stacks/blue/compose.yml"))
	assert.Equal(t, []string{"nginx"}, imagesUnder(m, "solo/compose.yml"))

	assert.Equal(t, []string{"stacks/blue/compose.yml", "stacks/green/compose.yml"},
		m.subtreeFiles(nodeIdx(t, m, "stacks")))
	assert.Equal(t, []string{"stacks/blue/compose.yml"},
		m.subtreeFiles(nodeIdx(t, m, "stacks/blue/compose.yml")),
		"a file node's subtree is itself")

	// Folding is display-only, so a fold anywhere below leaves the set intact —
	// this is what keeps `a` and the header counts collapse-blind.
	folded := nestedTree(t)
	folded.toggleGroup("stacks/blue/compose.yml")
	assert.Equal(t, []string{"caddy", "postgres", "redis"}, imagesUnder(folded, "stacks"))
	folded.toggleGroup("stacks")
	assert.Equal(t, []string{"caddy", "postgres", "redis"}, imagesUnder(folded, "stacks"))

	// The filter, unlike folding, does decide what a subtree holds — and here it
	// leaves stacks/ with a single visible file, which compresses the branch away.
	filtered := feed(t, nestedTree(t), keyMsg("f")) // major only
	assert.Equal(t, []string{"postgres"}, imagesUnder(filtered, "stacks/blue/compose.yml"))
	assert.NotContains(t, nodeKeys(filtered), "stacks")

	assert.Empty(t, m.subtreeRows(-1), "no node under the cursor is not a panic")
	assert.Empty(t, m.subtreeFiles(-1))
	assert.Empty(t, m.subtreeRows(len(m.nodes)))
}

func TestGroupInfoAggregatesOverTheWholeSubtree(t *testing.T) {
	m := nestedTree(t)
	// `a` is scoped to the cursor's subtree, so stacks/ is selected from its own
	// header — which is the case this test is about.
	m = feed(t, m, homeKey(), keyMsg("j"), keyMsg("j"), keyMsg("a"))
	require.Equal(t, "stacks", m.cursorGroup())
	require.Equal(t, 3, m.selectedCount())

	g := m.groupInfo(nodeIdx(t, m, "stacks"), false)
	assert.Equal(t, "stacks", g.Path)
	assert.Equal(t, "stacks", g.Label)
	assert.Equal(t, 0, g.Depth)
	assert.True(t, g.IsDir)
	assert.Equal(t, 3, g.Total, "the directory speaks for all three of its rows")
	assert.Equal(t, 3, g.Shown)
	assert.Equal(t, 3, g.Selected)

	blue := m.groupInfo(nodeIdx(t, m, "stacks/blue/compose.yml"), true)
	assert.Equal(t, "blue/compose.yml", blue.Label)
	assert.Equal(t, 1, blue.Depth)
	assert.False(t, blue.IsDir, "a compose file owns rows, so it is not a directory")
	assert.True(t, blue.Cursor)
	assert.Equal(t, 2, blue.Total)
	assert.Equal(t, 2, blue.Selected)

	assert.Equal(t, 1, m.groupInfo(nodeIdx(t, m, "solo/compose.yml"), false).Total)
}

// The existing invariant, now one level up: a selection the filter hides is
// still counted, and a directory has to add up the hidden ones under every file
// beneath it or folding could conceal an entire branch's staged writes.
func TestGroupInfoOnADirectoryCountsSelectionsTheFilterHides(t *testing.T) {
	// Both files keep a major row, so the branch survives the filter and the
	// directory header is still there to under-report if it gets this wrong.
	m := newTestModel()
	m = feed(t, m,
		updateEvent("stacks/blue/compose.yml", "caddy", "2.7", "2.8", "minor"),
		updateEvent("stacks/blue/compose.yml", "postgres", "15", "16", "major"),
		updateEvent("stacks/green/compose.yml", "redis", "7.0", "7.2", "minor"),
		updateEvent("stacks/green/compose.yml", "mariadb", "10", "11", "major"),
	)
	// Home puts the cursor on stacks/, whose subtree is all four rows.
	m = feed(t, m, homeKey(), keyMsg("a"))
	require.Equal(t, "stacks", m.cursorGroup())
	require.Equal(t, 4, m.selectedCount())

	m = feed(t, m, keyMsg("f")) // filter=major: one row per file survives

	g := m.groupInfo(nodeIdx(t, m, "stacks"), false)
	assert.Equal(t, 2, g.Shown)
	assert.Equal(t, 4, g.Total)
	assert.Equal(t, 4, g.Selected, "a selection the filter hides must still be reported")

	assert.Contains(t, plainText(m.theme.GroupHeader(g, 80)), "(2 of 4 updates, 4 selected)")
}

func TestGroupInfoOnAnInvalidNodeIsEmpty(t *testing.T) {
	m := nestedTree(t)
	for _, idx := range []int{-1, len(m.nodes), len(m.nodes) + 5} {
		g := m.groupInfo(idx, true)
		assert.Equal(t, GroupInfo{Cursor: true}, g, "index %d", idx)
	}
}

func TestCollapseOrParentFoldsThenClimbs(t *testing.T) {
	m := nestedTree(t)
	m.cursor = 3 // stacks/blue/compose.yml's header, one level down
	require.Equal(t, "stacks/blue/compose.yml", m.cursorGroup())

	// First press folds the node the cursor is on.
	m.collapseOrParent()
	assert.True(t, m.collapsed["stacks/blue/compose.yml"])
	assert.Equal(t, "stacks/blue/compose.yml", m.cursorGroup(), "the cursor stays put")

	// Second press has nothing left to fold here, so it climbs.
	m.collapseOrParent()
	assert.Equal(t, "stacks", m.cursorGroup())
	assert.True(t, cursorOnHeader(m))
	assert.False(t, m.collapsed["stacks"], "climbing must not fold the parent as well")

	// From the parent it folds, then has nowhere left to climb to.
	m.collapseOrParent()
	assert.True(t, m.collapsed["stacks"])
	m.collapseOrParent()
	assert.Equal(t, "stacks", m.cursorGroup(), "a root with no parent simply stays")
}

// From a row the key means "get me out of here": fold the file the row lives in.
func TestCollapseOrParentFromARowFoldsItsFile(t *testing.T) {
	m := nestedTree(t)
	m.cursor = 5 // postgres
	require.Equal(t, "postgres", m.currentRow().Update.ImageName)

	m.collapseOrParent()
	assert.True(t, m.collapsed["stacks/blue/compose.yml"])
	assert.True(t, cursorOnHeader(m))
	assert.Equal(t, "stacks/blue/compose.yml", m.cursorGroup())
}

func TestExpandOrChildUnfoldsThenDescends(t *testing.T) {
	m := nestedTree(t)
	m.setAllCollapsed(true)
	m.cursor = 1 // the stacks/ header, everything below it folded
	require.Equal(t, "stacks", m.cursorGroup())

	// First press opens it, leaving the cursor where it is.
	m.expandOrChild()
	assert.False(t, m.collapsed["stacks"])
	assert.Equal(t, "stacks", m.cursorGroup())

	// Second press steps into the first child.
	m.expandOrChild()
	assert.Equal(t, "stacks/blue/compose.yml", m.cursorGroup())
	assert.True(t, cursorOnHeader(m))

	// Which is itself still folded, so the pair repeats one level down.
	require.True(t, m.collapsed["stacks/blue/compose.yml"])
	m.expandOrChild()
	assert.False(t, m.collapsed["stacks/blue/compose.yml"])
	m.expandOrChild()
	require.NotNil(t, m.currentRow())
	assert.Equal(t, "caddy", m.currentRow().Update.ImageName, "the first row is the first child")
}

// A row has nothing to expand, so → walks on to the next row of the same file
// and stops at its end: descending never leaves the group the cursor is in.
func TestExpandOrChildFromARowStaysInsideItsFile(t *testing.T) {
	m := nestedTree(t)
	m.cursor = 4 // caddy, the first of stacks/blue's two rows
	require.Equal(t, "caddy", m.currentRow().Update.ImageName)

	m.expandOrChild()
	require.NotNil(t, m.currentRow())
	assert.Equal(t, "postgres", m.currentRow().Update.ImageName)

	// The next entry belongs to the sibling file, so this is where it stops.
	m.expandOrChild()
	assert.Equal(t, "postgres", m.currentRow().Update.ImageName,
		"→ must not spill into the next group")
}

func TestTreeNavigationOnDegenerateLists(t *testing.T) {
	// Empty list: every tree key is a safe no-op.
	m := newTestModel()
	require.NotPanics(t, func() {
		m.collapseOrParent()
		m.expandOrChild()
		m.setAllCollapsed(true)
		m.setAllCollapsed(false)
		m.toggleGroup("")
		m.toggleGroup("nothing/here.yml")
	})
	assert.Empty(t, m.nodes)
	assert.Equal(t, 0, m.cursor)
	assert.Equal(t, -1, m.cursorNode())

	// A key naming no node must not invent one either.
	one := treeOf(t, "a/compose.yml")
	one.toggleGroup("a")
	assert.Equal(t, []string{"a/compose.yml", "a/compose.yml/img00"}, entryPaths(one),
		"a key that is a prefix but not a node must not fold anything")
}

func TestDeepTreeNavigationKeepsTheCursorInRange(t *testing.T) {
	m := newTestModel()
	for _, p := range []string{
		"root/a/x/compose.yml", "root/a/y/compose.yml",
		"root/b/compose.yml", "root/b/other.yml",
		"solo.yml",
	} {
		m = feed(t, m, updateEvent(p, "caddy", "2.7", "2.8", "minor"),
			updateEvent(p, "redis", "7.0", "7.2", "minor"))
	}
	m.width, m.height = 80, 12
	m.syncScroll()

	inRange := func(m Model) {
		t.Helper()
		require.True(t, m.cursor >= 0 && m.cursor < len(m.entries))
		require.GreaterOrEqual(t, m.cursor, m.offset)
		require.Less(t, m.cursor, m.offset+m.listHeight())
	}

	for i := 0; i < len(m.entries)+3; i++ {
		m = feed(t, m, keyMsg("j"))
		inRange(m)
	}
	m = feed(t, m, keyMsg("C"))
	inRange(m)
	for i := 0; i < len(m.entries)+3; i++ {
		m = feed(t, m, keyMsg("k"))
		inRange(m)
	}
	m = feed(t, m, keyMsg("E"))
	inRange(m)

	// Folding and unfolding at arbitrary depths must never strand the cursor.
	for _, n := range m.nodes {
		m.toggleGroup(n.key)
		inRange(m)
	}
	for _, n := range m.nodes {
		m.toggleGroup(n.key)
		inRange(m)
	}
	assert.Equal(t, len(m.entries), m.displayCount())
}

// Selections live on rows, so nothing about the tree may change what is applied.
func TestSelectionsSurviveFoldingAtEveryDepth(t *testing.T) {
	m := nestedTree(t)
	// `a` selects the subtree under the cursor, so covering all four rows means
	// selecting each root: solo/compose.yml, then stacks/ with its two files.
	m = feed(t, m, homeKey(), keyMsg("a"), keyMsg("j"), keyMsg("j"), keyMsg("a"))
	require.Equal(t, 4, m.selectedCount())

	m.setAllCollapsed(true)
	assert.Equal(t, 4, m.selectedCount(), "folding must not shrink what is staged")
	assert.Len(t, m.selectedRows(), 4)

	next, cmd := m.Update(keyMsg("A"))
	m = next.(Model)
	require.NotNil(t, cmd, "a fully folded tree must not turn A into a no-op")
	assert.Equal(t, 4, m.applyActive)
}
