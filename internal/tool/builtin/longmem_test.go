package builtin

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/longmem"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestLongMemToolsTagAndScopeByContextWorkdir is the P25.9 regression:
// entity_remember must tag facts with the calling session's own project
// (derived from the context workdir), and entity_recall must not leak
// another project's facts into the results.
func TestLongMemToolsTagAndScopeByContextWorkdir(t *testing.T) {
	dir := t.TempDir()
	store, err := longmem.Open("daemon-fallback", filepath.Join(dir, "longmem.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tools := LongMemTools(store, "/daemon/fallback/root")
	remember := tools[0]
	recall := tools[1]

	projectARoot := filepath.Join(string(filepath.Separator), "work", "projectA")
	projectBRoot := filepath.Join(string(filepath.Separator), "work", "projectB")

	ctxA := tool.WithWorkdir(context.Background(), projectARoot)
	if _, err := remember.Execute(ctxA, mustJSON(t, map[string]any{
		"entity_type": "system", "entity_name": "billing-api", "facts": "handles Stripe webhooks",
	})); err != nil {
		t.Fatalf("remember (A): %v", err)
	}

	ctxB := tool.WithWorkdir(context.Background(), projectBRoot)
	if _, err := remember.Execute(ctxB, mustJSON(t, map[string]any{
		"entity_type": "system", "entity_name": "billing-api", "facts": "handles PayPal webhooks",
	})); err != nil {
		t.Fatalf("remember (B): %v", err)
	}

	resA, err := recall.Execute(ctxA, mustJSON(t, map[string]any{"query": "billing-api"}))
	if err != nil {
		t.Fatalf("recall (A): %v", err)
	}
	if !strings.Contains(resA.Content, "Stripe") {
		t.Errorf("projectA recall missing its own fact: %s", resA.Content)
	}
	if strings.Contains(resA.Content, "PayPal") {
		t.Errorf("projectA recall leaked projectB's fact: %s", resA.Content)
	}

	resB, err := recall.Execute(ctxB, mustJSON(t, map[string]any{"query": "billing-api"}))
	if err != nil {
		t.Fatalf("recall (B): %v", err)
	}
	if !strings.Contains(resB.Content, "PayPal") {
		t.Errorf("projectB recall missing its own fact: %s", resB.Content)
	}
	if strings.Contains(resB.Content, "Stripe") {
		t.Errorf("projectB recall leaked projectA's fact: %s", resB.Content)
	}

	// Verify the tag itself (not just recall scoping): GetEntity under each
	// project name should see only that project's facts.
	facts, err := store.GetEntity(context.Background(), filepath.Base(projectARoot), "system", "billing-api")
	if err != nil {
		t.Fatalf("GetEntity(projectA): %v", err)
	}
	if !strings.Contains(facts, "Stripe") {
		t.Errorf("expected projectA's own entity row to hold the Stripe fact, got %q", facts)
	}
}
