package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/tool"
)

// extraRootsCtx builds the call context the engine hands a tool for a session
// rooted at workdir with the given additional roots (P52.13).
func extraRootsCtx(workdir string, extra ...sandbox.Root) context.Context {
	ctx := tool.WithWorkdir(context.Background(), workdir)
	return tool.WithExtraRoots(ctx, extra)
}

// TestEffectiveRootsWithoutExtraRootsIsSingleWritableRoot pins the no-config
// case: every session that has never heard of workspace.additional_roots must
// see exactly the one writable root it always did.
func TestEffectiveRootsWithoutExtraRootsIsSingleWritableRoot(t *testing.T) {
	ctx := tool.WithWorkdir(context.Background(), "/session/root")
	roots := effectiveRoots(ctx, "/daemon/default")
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1: %+v", len(roots), roots)
	}
	if roots[0].Path != "/session/root" || !roots[0].Writable {
		t.Errorf("roots[0] = %+v, want {/session/root true}", roots[0])
	}
}

// TestEffectiveRootsCannotDemotePrimary guards a stale-config foot-gun: an
// additional root naming the session workdir must not be able to make the
// workdir read-only.
func TestEffectiveRootsCannotDemotePrimary(t *testing.T) {
	ctx := extraRootsCtx("/session/root", sandbox.Root{Path: "/session/root"})
	roots := effectiveRoots(ctx, "/daemon/default")
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want the duplicate dropped: %+v", len(roots), roots)
	}
	if !roots[0].Writable {
		t.Error("primary root was demoted to read-only by a duplicate additional root")
	}
}

// TestReadToolReachesAdditionalRoot is the cross-repo workflow end to end at
// the tool level: research artifacts in repo A are readable from a session
// rooted in repo B, without starting Aegis from their common parent.
func TestReadToolReachesAdditionalRoot(t *testing.T) {
	docs := t.TempDir()
	research := t.TempDir()
	note := filepath.Join(research, "notes.md")
	if err := os.WriteFile(note, []byte("prior findings"), 0o600); err != nil {
		t.Fatal(err)
	}

	rt := &readTool{root: docs}
	ctx := extraRootsCtx(docs, sandbox.Root{Path: research})

	in, _ := json.Marshal(map[string]any{"path": note})
	res, err := rt.Execute(ctx, in)
	if err != nil {
		t.Fatalf("read from additional root: %v", err)
	}
	if res.IsError || !strings.Contains(res.Content, "prior findings") {
		t.Fatalf("read result = %+v, want the file's content", res)
	}

	// Without the additional root on the context, the same call is refused —
	// so the read is genuinely granted by config, not by a hole in confinement.
	if _, err := rt.Execute(tool.WithWorkdir(context.Background(), docs), in); err == nil {
		t.Error("read outside the workspace succeeded with no additional root configured")
	}
}

// TestWriteToolRefusedInReadOnlyAdditionalRoot is the other half of the
// default: reachable for reads does not mean writable.
func TestWriteToolRefusedInReadOnlyAdditionalRoot(t *testing.T) {
	docs := t.TempDir()
	research := t.TempDir()
	target := filepath.Join(research, "out.md")

	wt := &writeTool{root: docs}
	in, _ := json.Marshal(map[string]any{"path": target, "content": "written"})

	_, err := wt.Execute(extraRootsCtx(docs, sandbox.Root{Path: research}), in)
	if err == nil {
		t.Fatal("write into a read-only additional root was allowed")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("error %q does not explain the read-only refusal", err)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatal("file was created despite the refusal")
	}

	// Marked writable, the same call goes through.
	if _, err := wt.Execute(extraRootsCtx(docs, sandbox.Root{Path: research, Writable: true}), in); err != nil {
		t.Fatalf("write into a writable additional root: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}

// TestAdditionalRootsDoNotAdmitTheirParent is the confinement property that
// matters most once more than one root exists: admitting two sibling
// directories must not admit the directory that contains them.
func TestAdditionalRootsDoNotAdmitTheirParent(t *testing.T) {
	base := t.TempDir()
	docs := filepath.Join(base, "docs")
	research := filepath.Join(base, "research")
	for _, d := range []string{docs, research} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(base, "secret.txt")
	if err := os.WriteFile(secret, []byte("shared-parent secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	rt := &readTool{root: docs}
	in, _ := json.Marshal(map[string]any{"path": secret})
	if _, err := rt.Execute(extraRootsCtx(docs, sandbox.Root{Path: research}), in); err == nil {
		t.Error("a file in the roots' shared parent was readable")
	}
}
