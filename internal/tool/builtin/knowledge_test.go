package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestProjectKnowledgeToolUsesProviderForContextWorkdir is the P25.9
// regression: with a KnowledgeProvider wired, the tool must query the store
// for the call's context workdir, not the fixed fallback store — and fall
// back to the fixed store when no provider is set (today's pre-P25.9
// behavior, still exercised by other callers like aegis chat).
func TestProjectKnowledgeToolUsesProviderForContextWorkdir(t *testing.T) {
	fallbackRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(fallbackRoot, "README.md"), []byte("# Fallback\n\nfallback-only-term"), 0o644); err != nil {
		t.Fatal(err)
	}
	fallbackStore, err := knowledge.Open(fallbackRoot, filepath.Join(fallbackRoot, ".aegis", "knowledge.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fallbackStore.Close()
	if _, err := fallbackStore.Index(context.Background()); err != nil {
		t.Fatal(err)
	}

	sessionRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionRoot, "README.md"), []byte("# Session\n\nsession-only-term"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionStore, err := knowledge.Open(sessionRoot, filepath.Join(sessionRoot, ".aegis", "knowledge.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sessionStore.Close()
	if _, err := sessionStore.Index(context.Background()); err != nil {
		t.Fatal(err)
	}

	provider := KnowledgeProviderFunc(func(root string) (*knowledge.Store, error) {
		if root == sessionRoot {
			return sessionStore, nil
		}
		return nil, os.ErrNotExist
	})

	tools := KnowledgeTools(fallbackStore, provider, fallbackRoot)
	kt := tools[0].(*projectKnowledgeTool)

	// A context workdir the provider recognizes routes to the session store.
	ctx := tool.WithWorkdir(context.Background(), sessionRoot)
	res, err := kt.Execute(ctx, mustJSON(t, map[string]any{"query": "session-only-term"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Content, "Session") {
		t.Errorf("expected the session store's result, got: %s", res.Content)
	}

	// No context workdir at all falls back to the fixed store.
	res, err = kt.Execute(context.Background(), mustJSON(t, map[string]any{"query": "fallback-only-term"}))
	if err != nil {
		t.Fatalf("Execute (no context workdir): %v", err)
	}
	if !strings.Contains(res.Content, "Fallback") {
		t.Errorf("expected the fallback store's result, got: %s", res.Content)
	}

	// A context workdir the provider can't resolve also falls back cleanly.
	ctx = tool.WithWorkdir(context.Background(), t.TempDir())
	res, err = kt.Execute(ctx, mustJSON(t, map[string]any{"query": "fallback-only-term"}))
	if err != nil {
		t.Fatalf("Execute (provider error): %v", err)
	}
	if !strings.Contains(res.Content, "Fallback") {
		t.Errorf("expected fallback on provider error, got: %s", res.Content)
	}
}

// TestProjectKnowledgeToolNilProviderUsesFixedStore checks the nil-provider
// path (aegis chat and any other caller that doesn't wire one) is unchanged.
func TestProjectKnowledgeToolNilProviderUsesFixedStore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Only\n\nonly-term"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := knowledge.Open(root, filepath.Join(root, ".aegis", "knowledge.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Index(context.Background()); err != nil {
		t.Fatal(err)
	}

	tools := KnowledgeTools(store, nil, root)
	kt := tools[0].(*projectKnowledgeTool)

	res, err := kt.Execute(context.Background(), mustJSON(t, map[string]any{"query": "only-term"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Content, "Only") {
		t.Errorf("expected the fixed store's result, got: %s", res.Content)
	}
}
