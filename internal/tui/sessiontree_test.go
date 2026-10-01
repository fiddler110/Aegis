package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

func metaAt(id, parent, title string, t time.Time) api.SessionMeta {
	return api.SessionMeta{ID: id, ParentID: parent, Title: title, Mode: "build", CreatedAt: t, UpdatedAt: t}
}

// TestBuildSessionForestNestsForksUnderParent checks the basic shape: a
// top-level session with two forks, one of which itself has a fork, renders
// as a three-level tree rather than four unrelated flat rows.
func TestBuildSessionForestNestsForksUnderParent(t *testing.T) {
	base := time.Now()
	all := []api.SessionMeta{
		metaAt("root", "", "Root", base),
		metaAt("fork-a", "root", "Fork A", base.Add(time.Minute)),
		metaAt("fork-b", "root", "Fork B", base.Add(2*time.Minute)),
		metaAt("fork-a1", "fork-a", "Fork A sub", base.Add(3*time.Minute)),
	}
	forest := buildSessionForest(all)
	if len(forest) != 1 {
		t.Fatalf("expected 1 root, got %d", len(forest))
	}
	root := forest[0]
	if root.meta.ID != "root" || len(root.children) != 2 {
		t.Fatalf("expected root with 2 children, got %+v", root)
	}
	if root.children[0].meta.ID != "fork-a" || root.children[1].meta.ID != "fork-b" {
		t.Fatalf("expected children ordered oldest-fork-first, got %s, %s", root.children[0].meta.ID, root.children[1].meta.ID)
	}
	if len(root.children[0].children) != 1 || root.children[0].children[0].meta.ID != "fork-a1" {
		t.Fatalf("expected fork-a1 nested under fork-a, got %+v", root.children[0])
	}
}

// TestBuildSessionForestOrphanedParentBecomesRoot checks a fork whose parent
// isn't in the list (already deleted, or excluded upstream) still shows up
// rather than vanishing.
func TestBuildSessionForestOrphanedParentBecomesRoot(t *testing.T) {
	all := []api.SessionMeta{
		metaAt("orphan", "gone", "Orphan", time.Now()),
	}
	forest := buildSessionForest(all)
	if len(forest) != 1 || forest[0].meta.ID != "orphan" {
		t.Fatalf("expected the orphan to surface as a root, got %+v", forest)
	}
}

// TestBuildSessionForestRootsMostRecentlyUpdatedFirst mirrors the plain
// session picker's own ordering for unrelated top-level sessions.
func TestBuildSessionForestRootsMostRecentlyUpdatedFirst(t *testing.T) {
	base := time.Now()
	all := []api.SessionMeta{
		{ID: "old", UpdatedAt: base},
		{ID: "new", UpdatedAt: base.Add(time.Hour)},
	}
	forest := buildSessionForest(all)
	if len(forest) != 2 || forest[0].meta.ID != "new" || forest[1].meta.ID != "old" {
		t.Fatalf("expected new-first ordering, got %+v", forest)
	}
}

// TestLineageIDsIncludesAncestorAndDescendants checks the treeFilterLineage
// narrowing: a fork's lineage is its ultimate ancestor plus every descendant
// reachable from there, not just its own direct chain.
func TestLineageIDsIncludesAncestorAndDescendants(t *testing.T) {
	all := []api.SessionMeta{
		metaAt("root", "", "Root", time.Now()),
		metaAt("fork-a", "root", "Fork A", time.Now()),
		metaAt("fork-b", "root", "Fork B", time.Now()),
		metaAt("fork-a1", "fork-a", "Fork A sub", time.Now()),
		{ID: "unrelated", UpdatedAt: time.Now()},
	}
	ids := lineageIDs(all, "fork-a1")
	want := map[string]bool{"root": true, "fork-a": true, "fork-b": true, "fork-a1": true}
	if len(ids) != len(want) {
		t.Fatalf("lineageIDs = %v, want %v", ids, want)
	}
	for id := range want {
		if !ids[id] {
			t.Errorf("expected %q in lineage, got %v", id, ids)
		}
	}
	if ids["unrelated"] {
		t.Error("unrelated session leaked into the lineage")
	}
}

// TestLineageIDsUnknownCurrentReturnsNil checks the documented fallback: a
// current session id absent from all (a stale reference) disables the
// filter rather than narrowing to nothing.
func TestLineageIDsUnknownCurrentReturnsNil(t *testing.T) {
	all := []api.SessionMeta{{ID: "a"}}
	if got := lineageIDs(all, "missing"); got != nil {
		t.Fatalf("lineageIDs = %v, want nil", got)
	}
}

// TestFlattenSessionTreeConnectorsAndFold checks the flattened row order, the
// tree-connector prefixes, and that a collapsed node hides its children.
func TestFlattenSessionTreeConnectorsAndFold(t *testing.T) {
	base := time.Now()
	all := []api.SessionMeta{
		metaAt("root", "", "Root", base),
		metaAt("fork-a", "root", "Fork A", base.Add(time.Minute)),
		metaAt("fork-b", "root", "Fork B", base.Add(2*time.Minute)),
	}
	forest := buildSessionForest(all)

	rows := flattenSessionTree(forest, map[string]bool{}, "fork-a")
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows expanded, got %d", len(rows))
	}
	if rows[0].meta.ID != "root" || rows[1].meta.ID != "fork-a" || rows[2].meta.ID != "fork-b" {
		t.Fatalf("unexpected row order: %+v", rows)
	}
	if !rows[1].isCurrent {
		t.Error("expected fork-a marked current")
	}
	if rows[0].prefix == "" {
		t.Error("expected the root's fold marker prefix (it has children)")
	}

	collapsed := map[string]bool{"root": true}
	rows = flattenSessionTree(forest, collapsed, "fork-a")
	if len(rows) != 1 || rows[0].meta.ID != "root" {
		t.Fatalf("expected collapsing root to hide its children, got %+v", rows)
	}
}

// TestSessionTreeSlashOpensDialog drives the /tree slash result end-to-end
// through the model, checking the dialog opens with every session rendered
// and the current one marked.
func TestSessionTreeSlashOpensDialog(t *testing.T) {
	m := idleModel(t)
	base := time.Now()
	m = driveUpdate(t, m, slashResultMsg{SessionTree: []api.SessionMeta{
		metaAt("s", "", "Current", base),
		metaAt("fork-a", "s", "Fork A", base.Add(time.Minute)),
	}})
	if m.overlays.dialog == nil || m.overlays.dialog.kind != dialogSessionTree {
		t.Fatalf("expected the tree dialog open, got %+v", m.overlays.dialog)
	}
	if m.overlays.sessionTree == nil {
		t.Fatal("expected sessionTree state set")
	}
	view := plainView(m)
	if !strings.Contains(view, "Current (current)") {
		t.Errorf("expected the current session marked, got:\n%s", view)
	}
	if !strings.Contains(view, "Fork A") {
		t.Errorf("expected the fork listed, got:\n%s", view)
	}
}

// TestSessionTreeFoldToggleHidesChildren checks the space key toggles fold
// state on the selected node and re-flattens the visible rows.
func TestSessionTreeFoldToggleHidesChildren(t *testing.T) {
	m := idleModel(t)
	base := time.Now()
	m = driveUpdate(t, m, slashResultMsg{SessionTree: []api.SessionMeta{
		metaAt("s", "", "Root", base),
		metaAt("fork-a", "s", "Fork A", base.Add(time.Minute)),
	}})
	// The root is the first (and initially selected) row.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	if !m.overlays.sessionTree.collapsed["s"] {
		t.Fatal("expected space to collapse the selected node")
	}
	view := plainView(m)
	if strings.Contains(view, "Fork A") {
		t.Errorf("expected the collapsed node's child hidden, got:\n%s", view)
	}

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.overlays.sessionTree.collapsed["s"] {
		t.Fatal("expected a second space to re-expand")
	}
}

// TestSessionTreeFilterCycleNarrowsToLineage checks tab cycles between "all
// sessions" and "this lineage only".
func TestSessionTreeFilterCycleNarrowsToLineage(t *testing.T) {
	m := idleModel(t) // SessionID = "s"
	base := time.Now()
	m = driveUpdate(t, m, slashResultMsg{SessionTree: []api.SessionMeta{
		metaAt("s", "", "Current", base),
		{ID: "unrelated", Title: "Unrelated", UpdatedAt: base},
	}})
	if strings.Count(plainView(m), "Unrelated") == 0 {
		t.Fatal("expected the unrelated session listed under the default 'all' filter")
	}

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.overlays.sessionTree.filter != treeFilterLineage {
		t.Fatalf("filter = %v, want treeFilterLineage after tab", m.overlays.sessionTree.filter)
	}
	if strings.Contains(plainView(m), "Unrelated") {
		t.Errorf("expected the unrelated session hidden under 'this lineage', got:\n%s", plainView(m))
	}

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.overlays.sessionTree.filter != treeFilterAll {
		t.Fatalf("filter = %v, want treeFilterAll after a second tab", m.overlays.sessionTree.filter)
	}
}

// TestSessionTreeSelectSwitchesSessionAndClearsState checks picking a row
// switches sessions and that overlays.sessionTree is cleared so a stale
// pointer doesn't outlive the dialog it belonged to.
func TestSessionTreeSelectSwitchesSessionAndClearsState(t *testing.T) {
	m := idleModel(t)
	base := time.Now()
	m = driveUpdate(t, m, slashResultMsg{SessionTree: []api.SessionMeta{
		metaAt("s", "", "Current", base),
		metaAt("fork-a", "s", "Fork A", base.Add(time.Minute)),
	}})
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	mm, cmd := m.updateDialogSelectedForTest()
	if cmd == nil {
		t.Fatal("expected a switch-session command for a different session")
	}
	if mm.overlays.dialog != nil || mm.overlays.sessionTree != nil {
		t.Fatalf("expected both dialog and sessionTree state cleared, got dialog=%v tree=%v", mm.overlays.dialog, mm.overlays.sessionTree)
	}
}

// updateDialogSelectedForTest presses enter on the open dialog and drives the
// resulting message through the model, returning the final state. Split out
// only so the switch-session assertion above can inspect the returned cmd
// directly rather than the message driveUpdate would otherwise swallow.
func (m model) updateDialogSelectedForTest() (model, tea.Cmd) {
	mm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next := mm.(model)
	if cmd == nil {
		return next, nil
	}
	msg := cmd()
	mm2, cmd2 := next.Update(msg)
	return mm2.(model), cmd2
}
