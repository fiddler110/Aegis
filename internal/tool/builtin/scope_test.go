package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/permission"
)

func scopeInput(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestScopeToolSetShowClear(t *testing.T) {
	sc := permission.NewTaskScope()
	ctx := permission.WithTaskScope(context.Background(), sc)
	tl := &scopeTool{}

	// set
	res, err := tl.Execute(ctx, scopeInput(t, map[string]any{"action": "set", "paths": []string{"src/**", "cmd/main.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("set failed: %s", res.Content)
	}
	if !sc.Active() {
		t.Fatal("scope should be active after set")
	}
	if !sc.Allowed("src/a.go") || sc.Allowed("internal/x.go") {
		t.Errorf("scope patterns not applied: %v", sc.Patterns())
	}

	// show
	res, _ = tl.Execute(ctx, scopeInput(t, map[string]any{"action": "show"}))
	if !strings.Contains(res.Content, "src/**") {
		t.Errorf("show output = %q", res.Content)
	}

	// clear
	res, _ = tl.Execute(ctx, scopeInput(t, map[string]any{"action": "clear"}))
	if res.IsError || sc.Active() {
		t.Errorf("clear should deactivate scope, isErr=%v active=%v", res.IsError, sc.Active())
	}
}

func TestScopeToolSetRequiresPaths(t *testing.T) {
	ctx := permission.WithTaskScope(context.Background(), permission.NewTaskScope())
	tl := &scopeTool{}
	res, _ := tl.Execute(ctx, scopeInput(t, map[string]any{"action": "set"}))
	if !res.IsError {
		t.Error("set with no paths should error")
	}
}

func TestScopeToolUnavailableWithoutContext(t *testing.T) {
	tl := &scopeTool{}
	res, _ := tl.Execute(context.Background(), scopeInput(t, map[string]any{"action": "show"}))
	if !res.IsError || !strings.Contains(res.Content, "not available") {
		t.Errorf("expected unavailable error without scope context, got isErr=%v %q", res.IsError, res.Content)
	}
}
