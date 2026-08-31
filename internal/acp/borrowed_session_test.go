package acp

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// P80.1 / FIND-21, ACP half: session/prompt takes the client's sessionId
// verbatim, so an authenticated editor client could post a turn into a
// session it never created — including the auto-mode one a human is driving
// in the TUI — inheriting its mode, persona and workdir. `aegis acp --mode`
// then described only the sessions this agent creates, which is not how an
// operator setting it reads it. The configured mode is now a ceiling on a
// borrowed session too.
func TestPromptIntoBorrowedSessionAboveConfiguredModeIsRefused(t *testing.T) {
	peer, _, backend, cleanup := newTestPeer(t) // agent mode is "build"
	defer cleanup()
	backend.sessions = []api.SessionMeta{{ID: "human-tui", Mode: "auto"}}

	id := peer.request(methodPrompt, promptParams{
		SessionID: "human-tui",
		Prompt:    []contentBlock{textBlock("do something")},
	})
	msg := peer.read()
	if string(msg.ID) != string(jsonInt(id)) {
		t.Fatalf("expected the response to the prompt, got %+v", msg)
	}
	if msg.Error == nil {
		t.Fatal("expected a refusal prompting into an auto-mode session from a build-mode agent")
	}
	if !strings.Contains(msg.Error.Message, "auto") {
		t.Errorf("error message does not explain the ceiling: %q", msg.Error.Message)
	}
}

// The other direction: a session at or below the configured mode is
// untouched, which is what keeps an editor resuming its own session across an
// agent restart — when the in-memory created-set no longer holds the id —
// working.
func TestPromptIntoBorrowedSessionAtOrBelowConfiguredModeIsAllowed(t *testing.T) {
	peer, _, backend, cleanup := newTestPeer(t) // agent mode is "build"
	defer cleanup()
	backend.sessions = []api.SessionMeta{{ID: "resumed", Mode: "plan"}}
	backend.events = []api.Event{{Kind: api.KindText, Text: "ok"}}

	id := peer.request(methodPrompt, promptParams{
		SessionID: "resumed",
		Prompt:    []contentBlock{textBlock("continue")},
	})
	for {
		msg := peer.read()
		if string(msg.ID) != string(jsonInt(id)) {
			continue // session/update notifications
		}
		if msg.Error != nil {
			t.Fatalf("resuming a session at or below the ceiling must still work: %+v", msg.Error)
		}
		return
	}
}

// P81.14: a session with a durable origin recorded by a different surface is
// refused outright, not merely mode-ceilinged.
func TestPromptIntoBorrowedSessionFromADifferentOriginIsRefused(t *testing.T) {
	peer, _, backend, cleanup := newTestPeer(t) // agent mode is "build"
	defer cleanup()
	backend.sessions = []api.SessionMeta{{ID: "human-tui", Mode: "plan", Origin: "tui"}}

	id := peer.request(methodPrompt, promptParams{
		SessionID: "human-tui",
		Prompt:    []contentBlock{textBlock("do something")},
	})
	msg := peer.read()
	if string(msg.ID) != string(jsonInt(id)) {
		t.Fatalf("expected the response to the prompt, got %+v", msg)
	}
	if msg.Error == nil {
		t.Fatal("expected a refusal prompting into a tui-origin session even at/below the configured mode")
	}
	if !strings.Contains(msg.Error.Message, "not created by an ACP client") {
		t.Errorf("error message does not explain the origin refusal: %q", msg.Error.Message)
	}
}

// A session predating the origin column (Origin == "") is only mode-checked,
// as before, so an upgrade doesn't strand an editor's pre-existing session.
func TestPromptIntoBorrowedSessionWithEmptyOriginIsOnlyModeChecked(t *testing.T) {
	peer, _, backend, cleanup := newTestPeer(t) // agent mode is "build"
	defer cleanup()
	backend.sessions = []api.SessionMeta{{ID: "legacy", Mode: "plan"}} // Origin left zero-value
	backend.events = []api.Event{{Kind: api.KindText, Text: "ok"}}

	id := peer.request(methodPrompt, promptParams{
		SessionID: "legacy",
		Prompt:    []contentBlock{textBlock("continue")},
	})
	for {
		msg := peer.read()
		if string(msg.ID) != string(jsonInt(id)) {
			continue // session/update notifications
		}
		if msg.Error != nil {
			t.Fatalf("a pre-migration session at or below the ceiling must still work: %+v", msg.Error)
		}
		return
	}
}
