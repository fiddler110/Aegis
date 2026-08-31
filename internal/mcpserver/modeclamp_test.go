package mcpserver

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// TestCallerRequestedModeIsClampedToDefault is the C1/F1 regression test.
//
// The mode reaching the daemon came straight from the caller's tool arguments,
// with mcp_server.default_mode used only when the caller omitted it. The
// daemon's own resolveSessionMode then treats an explicit request mode as
// final by design — it clamps a persona's mode and nothing else — so an MCP
// client could ask for "auto" and get it whatever mcp_server.default_mode and
// permission.mode said. Under "auto" no approval is ever raised, which also
// made mcp_server.auto_approve (this package's headline safety control)
// vacuous for any caller that asked for it.
//
// The clamp is one-directional: a caller may still choose any mode at or below
// the configured default, because asking for *less* is never an escalation.
func TestCallerRequestedModeIsClampedToDefault(t *testing.T) {
	cases := []struct {
		name        string
		defaultMode string
		requested   string
		escalation  bool
		want        string
	}{
		{"auto is clamped to the plan default", "plan", "auto", false, "plan"},
		{"build is clamped to the plan default", "plan", "build", false, "plan"},
		{"auto is clamped to a build default", "build", "auto", false, "build"},
		{"build is allowed under a build default", "build", "build", false, "build"},
		{"asking for less is always allowed", "auto", "plan", false, "plan"},
		{"an unknown mode is treated as an escalation", "plan", "wideopen", false, "plan"},
		{"omitting the mode still gets the default", "build", "", false, "build"},
		{"the operator can opt back into escalation", "plan", "auto", true, "auto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, tool := range []string{"aegis_new_session", "aegis_prompt"} {
				t.Run(tool, func(t *testing.T) {
					backend := &fakeBackend{sessionID: "sess-1", events: []api.Event{{Kind: api.KindText, Text: "ok"}}}
					peer, cleanup := newTestPeer(t, backend, Options{
						DefaultMode:               tc.defaultMode,
						AllowCallerModeEscalation: tc.escalation,
					})
					defer cleanup()

					var args any
					if tool == "aegis_prompt" {
						args = promptArgs{Text: "hi", Mode: tc.requested}
					} else {
						args = newSessionArgs{Mode: tc.requested}
					}
					peer.request("tools/call", toolsCallParams{Name: tool, Arguments: mustJSON(args)})
					if out := peer.readResponse(); out.Error != nil {
						t.Fatalf("unexpected error: %+v", out.Error)
					}

					backend.mu.Lock()
					defer backend.mu.Unlock()
					if len(backend.createReqs) != 1 {
						t.Fatalf("got %d CreateSession calls, want 1", len(backend.createReqs))
					}
					if got := backend.createReqs[0].Mode; got != tc.want {
						t.Errorf("session mode = %q, want %q (caller asked for %q, default %q)",
							got, tc.want, tc.requested, tc.defaultMode)
					}
				})
			}
		})
	}
}

// TestAutoApproveIsScopedToConfiguredTools is the C1/F2 regression test: with
// no human in the loop, one blanket yes-to-everything is the only grant the
// server could make. AutoApproveTools narrows it, and the blanket behavior
// stays the default so the option is additive.
func TestAutoApproveIsScopedToConfiguredTools(t *testing.T) {
	cases := []struct {
		name    string
		opts    Options
		want    []bool
		wantErr string
	}{
		{
			name: "blanket auto-approve grants both",
			opts: Options{AutoApprove: true},
			want: []bool{true, true},
		},
		{
			name: "a tool list grants only what it names",
			opts: Options{AutoApprove: true, AutoApproveTools: []string{"read_file"}},
			want: []bool{true, false},
		},
		{
			name: "a tool list is inert while auto-approve is off",
			opts: Options{AutoApproveTools: []string{"read_file", "shell"}},
			want: []bool{false, false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeBackend{
				sessionID: "sess-1",
				events: []api.Event{
					{Kind: api.KindApprovalRequest, ApprovalID: "a1", Tool: "read_file"},
					{Kind: api.KindApprovalRequest, ApprovalID: "a2", Tool: "shell"},
					{Kind: api.KindText, Text: "done"},
				},
			}
			peer, cleanup := newTestPeer(t, backend, tc.opts)
			defer cleanup()

			peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
			_ = peer.readResponse()

			backend.mu.Lock()
			defer backend.mu.Unlock()
			if len(backend.approvals) != len(tc.want) {
				t.Fatalf("approvals = %v, want %v", backend.approvals, tc.want)
			}
			for i, want := range tc.want {
				if backend.approvals[i] != want {
					t.Errorf("approval %d (%s) = %v, want %v", i,
						[]string{"read_file", "shell"}[i], backend.approvals[i], want)
				}
			}
		})
	}
}

// TestOptionsDocMatchesTheClamp guards the half of C1/F1 that was a
// documentation defect as much as a code one: the package doc promised a
// conservative posture the code did not enforce. If the clamp is ever removed,
// this fails alongside the behavior tests rather than leaving the doc as the
// only description of a rule nothing applies.
func TestOptionsDocMatchesTheClamp(t *testing.T) {
	s := &Server{defaultMode: "plan", logger: discardLogger()}
	if got := s.resolveMode("auto"); got != "plan" {
		t.Fatalf("resolveMode(auto) = %q under a plan default, want plan — the Options doc says a caller cannot exceed DefaultMode", got)
	}
	if !strings.EqualFold(s.resolveMode(""), "plan") {
		t.Error("an omitted mode must fall back to DefaultMode")
	}
}
