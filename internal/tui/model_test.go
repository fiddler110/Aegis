package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdModelBareArgsShowsCurrentModel checks the no-args fast path returns
// the current model without touching the daemon client — the client is nil
// here specifically to prove this branch never calls it.
func TestCmdModelBareArgsShowsCurrentModel(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "model"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "test-model") {
		t.Errorf("expected current model in output, got: %s", res.Output)
	}
}

// TestSetSessionResyncsModelToTheLoadedSessions checks the bug where a
// session switch (Ctrl+Y, /fork, a rewind's reload) left the dispatcher
// showing the previous session's model: SetSession must adopt the new
// session's own override, and fall back to the TUI's boot-time default when
// the newly loaded session has none — never leave d.model pointing at
// whatever the old session happened to have picked.
func TestSetSessionResyncsModelToTheLoadedSession(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess-a", "build", "boot-default", "")
	d.model = "picked-in-session-a" // simulates a prior /model in this session

	// Switching to a session that pins its own model adopts that model.
	d.SetSession("sess-b", "build", "pinned-in-session-b")
	if got := d.EffectiveModel(); got != "pinned-in-session-b" {
		t.Errorf("EffectiveModel() after switching to a pinned session = %q, want %q", got, "pinned-in-session-b")
	}

	// Switching to a session with no override falls back to the boot-time
	// default, not the departed session's model.
	d.SetSession("sess-c", "build", "")
	if got := d.EffectiveModel(); got != "boot-default" {
		t.Errorf("EffectiveModel() after switching to an unpinned session = %q, want %q (boot default), not the previous session's model", got, "boot-default")
	}
}

// TestCmdModelDefaultFallsBackToBootDefault checks the bug where "/model
// default" cleared d.model to "" instead of the boot-time default: a
// following bare "/model" printed a blank current model, and the TUI's
// status bar / /models picker (driven by SlashResult.Model) never learned
// what model reverting the override actually left in effect.
func TestCmdModelDefaultFallsBackToBootDefault(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions/sess", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method %s", r.Method)
		}
		var req api.UpdateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model == nil || *req.Model != "" {
			t.Fatalf("expected empty model in clear request, got %v", req.Model)
		}
		_ = json.NewEncoder(w).Encode(api.SessionMeta{ID: "sess", Model: ""})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d := NewSlashDispatcher(client.New(srv.URL), "sess", "build", "boot-default", "")
	d.model = "picked-model" // simulates a prior /model switch

	res := d.Dispatch(&commands.ParsedCommand{Name: "model", Args: []string{"default"}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if res.Model == nil || *res.Model != "boot-default" {
		t.Fatalf("expected SlashResult.Model to report the boot default, got %v", res.Model)
	}
	if d.model != "boot-default" {
		t.Errorf("d.model after clearing = %q, want boot default %q, not blank", d.model, "boot-default")
	}

	bare := d.Dispatch(&commands.ParsedCommand{Name: "model"})
	if !strings.Contains(bare.Output, "boot-default") {
		t.Errorf("bare /model after clearing = %q, want it to name the boot default", bare.Output)
	}
}
