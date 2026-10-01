package tui

import (
	"fmt"
	"sort"

	"charm.land/bubbles/v2/list"

	"github.com/fiddler110/aegis/internal/api"
)

// treeFilterMode is the P85.4 /tree dialog's cyclable filter (tab key).
type treeFilterMode int

const (
	// treeFilterAll shows every session on the daemon, forked ones nested
	// under their parent.
	treeFilterAll treeFilterMode = iota
	// treeFilterLineage narrows to just the current session's own fork
	// lineage — its ultimate ancestor and every descendant of that ancestor —
	// hiding every unrelated top-level session and its forks.
	treeFilterLineage
)

func (f treeFilterMode) String() string {
	if f == treeFilterLineage {
		return "this lineage"
	}
	return "all sessions"
}

// sessionTreeState is the /tree dialog's own state, held in
// model.overlays.sessionTree alongside the generic listDialog (which only
// ever sees the current flattened row set — see the field's doc comment in
// tui.go). Rebuilding the flattened rows after a fold toggle or filter cycle
// needs the full fetched list back, which is why this keeps it rather than
// discarding it once the initial rows are computed.
type sessionTreeState struct {
	all     []api.SessionMeta
	current string // session ID to highlight and to anchor treeFilterLineage
	filter  treeFilterMode
	// collapsed holds node IDs the user has folded; absent means expanded.
	// Folding is opt-out rather than opt-in because forks are typically
	// shallow and few — defaulting everything open shows the whole shape at a
	// glance, which is the point of a tree view over a flat list.
	collapsed map[string]bool
}

// sessionTreeNode is one node in the forest built from a flat session list's
// ParentID edges (P85.4).
type sessionTreeNode struct {
	meta     api.SessionMeta
	children []*sessionTreeNode
}

// buildSessionForest nests sessions under the parent named by ParentID,
// sorting roots most-recently-updated first (matching the plain session
// picker's own ordering) and each node's children oldest-fork-first (matching
// the order they actually branched off in). A session whose ParentID names a
// session not present in all (already deleted, or excluded by a filter
// upstream) is treated as a root rather than dropped — an orphaned fork is
// still worth seeing, just not nested under a parent that isn't there.
func buildSessionForest(all []api.SessionMeta) []*sessionTreeNode {
	nodes := make(map[string]*sessionTreeNode, len(all))
	for _, s := range all {
		nodes[s.ID] = &sessionTreeNode{meta: s}
	}
	var roots []*sessionTreeNode
	for _, s := range all {
		n := nodes[s.ID]
		if s.ParentID != "" {
			if parent, ok := nodes[s.ParentID]; ok {
				parent.children = append(parent.children, n)
				continue
			}
		}
		roots = append(roots, n)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].meta.UpdatedAt.After(roots[j].meta.UpdatedAt) })
	var sortChildren func(*sessionTreeNode)
	sortChildren = func(n *sessionTreeNode) {
		sort.Slice(n.children, func(i, j int) bool { return n.children[i].meta.CreatedAt.Before(n.children[j].meta.CreatedAt) })
		for _, c := range n.children {
			sortChildren(c)
		}
	}
	for _, r := range roots {
		sortChildren(r)
	}
	return roots
}

// lineageIDs returns the ID set of current's own fork lineage: walk up
// ParentID to the ultimate ancestor, then every ID reachable back down from
// there. Empty (nil) if current isn't found in all at all — the filter then
// has nothing to narrow to and callers should fall back to showing
// everything rather than an empty dialog.
func lineageIDs(all []api.SessionMeta, current string) map[string]bool {
	byID := make(map[string]api.SessionMeta, len(all))
	for _, s := range all {
		byID[s.ID] = s
	}
	root, ok := byID[current]
	if !ok {
		return nil
	}
	for root.ParentID != "" {
		p, ok := byID[root.ParentID]
		if !ok {
			break
		}
		root = p
	}
	childrenOf := make(map[string][]string, len(all))
	for _, s := range all {
		if s.ParentID != "" {
			childrenOf[s.ParentID] = append(childrenOf[s.ParentID], s.ID)
		}
	}
	ids := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if ids[id] {
			return
		}
		ids[id] = true
		for _, c := range childrenOf[id] {
			walk(c)
		}
	}
	walk(root.ID)
	return ids
}

// filterForest prunes roots (and, recursively, children) to only IDs present
// in keep. A nil keep is treated as "no filter" — the whole forest passes
// through unchanged, matching lineageIDs' documented fallback.
func filterForest(roots []*sessionTreeNode, keep map[string]bool) []*sessionTreeNode {
	if keep == nil {
		return roots
	}
	var out []*sessionTreeNode
	for _, r := range roots {
		if !keep[r.meta.ID] {
			continue
		}
		filtered := filterForest(r.children, keep)
		out = append(out, &sessionTreeNode{meta: r.meta, children: filtered})
	}
	return out
}

// sessionTreeItem is one flattened, display-ready row of the /tree dialog.
type sessionTreeItem struct {
	meta        api.SessionMeta
	prefix      string // tree connector glyphs + fold marker, precomputed by flattenSessionTree
	isCurrent   bool
	hasChildren bool
}

func (it sessionTreeItem) FilterValue() string { return it.meta.Title + " " + it.meta.ID }
func (it sessionTreeItem) Title() string {
	title := it.meta.Title
	if title == "" {
		title = "(untitled)"
	}
	title = it.prefix + title
	if it.isCurrent {
		title += " (current)"
	}
	return title
}
func (it sessionTreeItem) Description() string {
	// Indent to align under the title text rather than under its connector
	// glyphs, so the description column stays readable at every depth.
	indent := make([]rune, len([]rune(it.prefix)))
	for i := range indent {
		indent[i] = ' '
	}
	return fmt.Sprintf("%s%s · %s · %s", string(indent), short(it.meta.ID), it.meta.Mode, it.meta.UpdatedAt.Format("2006-01-02 15:04"))
}

// flattenSessionTree walks roots depth-first in display order, building each
// visible row's tree-connector prefix and skipping the children of any node
// in collapsed. current highlights the active session; nothing here mutates
// collapsed, so re-flattening after a toggle is just calling this again.
func flattenSessionTree(roots []*sessionTreeNode, collapsed map[string]bool, current string) []sessionTreeItem {
	var out []sessionTreeItem
	var walk func(n *sessionTreeNode, ancestorBars []bool, isLast bool, isRoot bool)
	walk = func(n *sessionTreeNode, ancestorBars []bool, isLast bool, isRoot bool) {
		var prefix string
		for _, bar := range ancestorBars {
			if bar {
				prefix += "│  "
			} else {
				prefix += "   "
			}
		}
		if !isRoot {
			if isLast {
				prefix += "└─ "
			} else {
				prefix += "├─ "
			}
		}
		hasChildren := len(n.children) > 0
		if hasChildren {
			if collapsed[n.meta.ID] {
				prefix += "▸ "
			} else {
				prefix += "▾ "
			}
		}
		out = append(out, sessionTreeItem{meta: n.meta, prefix: prefix, isCurrent: n.meta.ID == current, hasChildren: hasChildren})
		if hasChildren && !collapsed[n.meta.ID] {
			for i, c := range n.children {
				walk(c, append(append([]bool(nil), ancestorBars...), !isLast || isRoot), i == len(n.children)-1, false)
			}
		}
	}
	for i, r := range roots {
		walk(r, nil, i == len(roots)-1, true)
	}
	return out
}

// sessionTreeRows rebuilds the flattened row set from st's current filter and
// fold state — the single path both the initial open and every subsequent
// fold/filter key press go through, so the two can never drift.
func (st *sessionTreeState) sessionTreeRows() []list.Item {
	forest := buildSessionForest(st.all)
	if st.filter == treeFilterLineage {
		forest = filterForest(forest, lineageIDs(st.all, st.current))
	}
	rows := flattenSessionTree(forest, st.collapsed, st.current)
	items := make([]list.Item, len(rows))
	for i, r := range rows {
		items[i] = r
	}
	return items
}

// sessionTreePickerH sizes the picker the same way the other list dialogs do.
func sessionTreePickerH(termH, n int) int { return dialogListH(termH, n, 10) }

// newSessionTreePicker opens the /tree dialog already populated — cmdTree
// fetches synchronously (see its own doc comment), so unlike the loading
// pickers there is no spinner phase here.
func newSessionTreePicker(termW, termH int, st *sessionTreeState) listDialog {
	items := st.sessionTreeRows()
	title := fmt.Sprintf("Session Tree (%s)", st.filter)
	return newListDialog(dialogSessionTree, min(termW-6, 76), sessionTreePickerH(termH, len(items)), title, true, items)
}
