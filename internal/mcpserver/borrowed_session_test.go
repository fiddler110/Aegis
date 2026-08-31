package mcpserver

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// P80.1 / FIND-21: aegis_list_sessions shows every session on the daemon,
// including the auto-mode one a human is driving in the TUI, and aegis_prompt
// used to take that id verbatim — inheriting its mode, persona and workdir.
// The F1 clamp binds only sessions this server creates, so reuse bypassed it.
// The ceiling now applies at prompt time to a session this server did not
// create.
func TestPromptIntoBorrowedSessionAboveDefaultModeIsRefused(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events:    []api.Event{{Kind: api.KindText, Text: "ok"}},
		sessions:  []api.SessionMeta{{ID: "human-tui", Mode: "auto"}},
	}
	peer, cleanup := newTestPeer(t, backend, Options{DefaultMode: "plan"})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi", SessionID: "human-tui"})})
	out := peer.readResponse()
	if out.Error == nil {
		t.Fatalf("expected a refusal posting into an auto-mode session under default_mode=plan, got %+v", out)
	}
	if !strings.Contains(out.Error.Message, "auto") || !strings.Contains(out.Error.Message, "default_mode") {
		t.Errorf("error message does not explain the ceiling: %q", out.Error.Message)
	}
}

// A borrowed session at or below the ceiling is untouched — this is the case
// the roadmap entry protects: an editor plugin resuming its own session after
// an mcp-serve restart, when the in-memory created-set no longer has the id.
func TestPromptIntoBorrowedSessionAtOrBelowDefaultModeIsAllowed(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events:    []api.Event{{Kind: api.KindText, Text: "ok"}},
		sessions:  []api.SessionMeta{{ID: "resumed", Mode: "plan"}},
	}
	peer, cleanup := newTestPeer(t, backend, Options{DefaultMode: "build"})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi", SessionID: "resumed"})})
	if out := peer.readResponse(); out.Error != nil {
		t.Fatalf("resuming a session at or below the ceiling must still work, got %+v", out.Error)
	}
}

// An operator who opted into caller escalation has said the ceiling is not
// wanted; the borrowed-session check honours the same switch F1 does.
func TestBorrowedSessionCeilingRespectsAllowCallerModeEscalation(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events:    []api.Event{{Kind: api.KindText, Text: "ok"}},
		sessions:  []api.SessionMeta{{ID: "human-tui", Mode: "auto"}},
	}
	peer, cleanup := newTestPeer(t, backend, Options{DefaultMode: "plan", AllowCallerModeEscalation: true})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi", SessionID: "human-tui"})})
	if out := peer.readResponse(); out.Error != nil {
		t.Fatalf("escalation opt-in should leave the borrowed session reachable, got %+v", out.Error)
	}
}

// A session this server created in the same process is its own, and is not
// re-checked against the ceiling — it was created at or below it by
// resolveMode. This is the fast path, and it must not depend on the daemon
// listing the session at all.
func TestOwnSessionIsNotSubjectToTheBorrowedCheck(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "mine",
		events:    []api.Event{{Kind: api.KindText, Text: "ok"}},
		sessions:  []api.SessionMeta{{ID: "mine", Mode: "auto"}}, // as if the daemon reports it high
	}
	peer, cleanup := newTestPeer(t, backend, Options{DefaultMode: "plan"})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_new_session", Arguments: mustJSON(newSessionArgs{})})
	if out := peer.readResponse(); out.Error != nil {
		t.Fatalf("new session: %+v", out.Error)
	}
	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi", SessionID: "mine"})})
	if out := peer.readResponse(); out.Error != nil {
		t.Fatalf("prompting into this server's own session was refused: %+v", out.Error)
	}
}

// P81.14: a session with a durable origin recorded by a different surface
// (a human's TUI session) is refused outright, not merely mode-ceilinged —
// this is the real fix P80.1 named as still open (the schema decision), now
// closed rather than left to enumeration control alone.
func TestPromptIntoBorrowedSessionFromADifferentOriginIsRefused(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events:    []api.Event{{Kind: api.KindText, Text: "ok"}},
		sessions:  []api.SessionMeta{{ID: "human-tui", Mode: "plan", Origin: "tui"}},
	}
	peer, cleanup := newTestPeer(t, backend, Options{DefaultMode: "plan"})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi", SessionID: "human-tui"})})
	out := peer.readResponse()
	if out.Error == nil {
		t.Fatalf("expected a refusal posting into a tui-origin session even at/below default_mode, got %+v", out)
	}
	if !strings.Contains(out.Error.Message, "not created by an MCP client") {
		t.Errorf("error message does not explain the origin refusal: %q", out.Error.Message)
	}
}

// A session predating the origin column (Origin == "") is treated the same
// as before — mode-ceilinged, not refused — so an upgrade doesn't strand an
// editor plugin's pre-existing session.
func TestPromptIntoBorrowedSessionWithEmptyOriginIsOnlyModeChecked(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events:    []api.Event{{Kind: api.KindText, Text: "ok"}},
		sessions:  []api.SessionMeta{{ID: "legacy", Mode: "plan"}}, // Origin left zero-value
	}
	peer, cleanup := newTestPeer(t, backend, Options{DefaultMode: "plan"})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi", SessionID: "legacy"})})
	if out := peer.readResponse(); out.Error != nil {
		t.Fatalf("a pre-migration session at or below the ceiling must still work, got %+v", out.Error)
	}
}

// An id the daemon does not list is unverifiable, not evidence of escalation:
// the enumeration this defends against only reaches listed sessions, and
// PostMessageReq rejects an id that does not exist.
func TestUnlistedSessionIsAllowedThrough(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events:    []api.Event{{Kind: api.KindText, Text: "ok"}},
	}
	peer, cleanup := newTestPeer(t, backend, Options{DefaultMode: "plan"})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi", SessionID: "unknown"})})
	if out := peer.readResponse(); out.Error != nil {
		t.Fatalf("an unlisted session id should not be refused: %+v", out.Error)
	}
}
