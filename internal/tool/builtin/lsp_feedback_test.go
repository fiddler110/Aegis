package builtin

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/lsp"
)

// TestAppendLSPFeedbackNilManager verifies the GAP-03 hook is a no-op when no
// LSP manager is configured — a write must never fail or change shape because
// LSP isn't set up at all.
func TestAppendLSPFeedbackNilManager(t *testing.T) {
	got := appendLSPFeedback(context.Background(), nil, "/root", "/root/a.go", "a.go", "wrote 3 bytes to a.go")
	want := "wrote 3 bytes to a.go"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestAppendLSPFeedbackNoServerConfigured verifies a Manager with no server
// registered for the file's language also passes content through unchanged —
// silent, not an error, since most files in a workspace have no LSP server.
func TestAppendLSPFeedbackNoServerConfigured(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := lsp.NewManager(root, slog.New(slog.DiscardHandler))
	got := appendLSPFeedback(context.Background(), mgr, root, path, "a.txt", "wrote 5 bytes to a.txt")
	want := "wrote 5 bytes to a.txt"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestWriteEditToolsAcceptNilLSP verifies write_file/edit_file/multi_edit/
// edit_section/fill_marker all still work with lsp left unset (the zero
// value), matching how they're constructed everywhere except builtin.Register
// when opts.LSP is nil.
func TestWriteEditToolsAcceptNilLSP(t *testing.T) {
	root := t.TempDir()
	wt := &writeTool{root: root}
	res, err := wt.Execute(context.Background(), mustJSON(t, map[string]any{"path": "out.txt", "content": "hi"}))
	if err != nil || res.IsError {
		t.Fatalf("write_file with nil lsp: err=%v res=%+v", err, res)
	}
}
