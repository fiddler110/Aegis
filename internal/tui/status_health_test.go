package tui

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// TestStatusInfoMsgUpdatesConnectionState covers the P28.7 connection/
// model-health indicator's state transitions in Update(): a successful
// /status round trip records reachability + latency, and a failed one
// (daemon itself unreachable) marks the connection down without touching
// the srvCtxWin fallback logic that statusInfoMsg also drives.
func TestStatusInfoMsgUpdatesConnectionState(t *testing.T) {
	var m model
	if m.conn.connKnown {
		t.Fatal("connKnown should start false")
	}

	next, cmd := m.Update(statusInfoMsg{info: api.StatusInfo{ProviderReachable: true, ProviderLatencyMS: 42}})
	m = next.(model)
	if cmd != nil {
		t.Errorf("expected nil cmd from statusInfoMsg, got %v", cmd)
	}
	if !m.conn.connKnown {
		t.Fatal("connKnown should be true after a /status response")
	}
	if !m.conn.connReachable {
		t.Error("connReachable should be true")
	}
	if m.conn.connLatencyMS != 42 {
		t.Errorf("connLatencyMS = %d, want 42", m.conn.connLatencyMS)
	}

	// A daemon-unreachable error (the client.StatusInfo call itself failing)
	// must flip to unreachable even though info.ProviderReachable is unset.
	next, _ = m.Update(statusInfoMsg{err: errTestDaemonDown})
	m = next.(model)
	if !m.conn.connKnown {
		t.Fatal("connKnown should remain true")
	}
	if m.conn.connReachable {
		t.Error("connReachable should be false after a /status request error")
	}
	if m.conn.connLatencyMS != 0 {
		t.Errorf("connLatencyMS = %d, want 0 after an error", m.conn.connLatencyMS)
	}
}

// TestStatusInfoMsgUpdatesSandboxBackend covers the P81.22/FIND-22 sidebar
// signal: a /status response's SandboxBackend populates m.conn.sandboxBackend, and
// a later transient error (SandboxBackend empty, err set) must NOT blank it
// out — the whole point is a signal that doesn't flicker on a daemon hiccup.
func TestStatusInfoMsgUpdatesSandboxBackend(t *testing.T) {
	var m model
	if m.conn.sandboxBackend != "" {
		t.Fatal("sandboxBackend should start empty")
	}

	next, _ := m.Update(statusInfoMsg{info: api.StatusInfo{SandboxBackend: "local"}})
	m = next.(model)
	if m.conn.sandboxBackend != "local" {
		t.Errorf("sandboxBackend = %q, want %q", m.conn.sandboxBackend, "local")
	}

	next, _ = m.Update(statusInfoMsg{err: errTestDaemonDown})
	m = next.(model)
	if m.conn.sandboxBackend != "local" {
		t.Errorf("sandboxBackend should survive a transient /status error, got %q", m.conn.sandboxBackend)
	}
}

// TestRenderSandboxBadge covers the P81.22/FIND-22 sidebar indicator: the
// unsandboxed local backend renders a distinguishable warning, any other
// backend renders its plain name.
func TestRenderSandboxBadge(t *testing.T) {
	cases := []struct {
		backend string
		wantSub string
	}{
		{"local", "unconfined"},
		{"container:docker", "container:docker"},
		{"os:bwrap", "os:bwrap"},
	}
	for _, tc := range cases {
		t.Run(tc.backend, func(t *testing.T) {
			m := model{conn: conn{sandboxBackend: tc.backend}}
			got := m.renderSandboxBadge(40)
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("renderSandboxBadge() = %q, want substring %q", got, tc.wantSub)
			}
		})
	}
}

// TestStatusTickMsgReschedules covers the P28.7 periodic refresh: handling
// statusTickMsg must return a non-nil Cmd (it batches a re-fetch plus the
// next tick) so the indicator keeps updating without user action.
func TestStatusTickMsgReschedules(t *testing.T) {
	var m model
	_, cmd := m.Update(statusTickMsg{})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd from statusTickMsg to reschedule the next poll")
	}
}

// TestRenderConnBadgeAndDetail sanity-checks the P28.7 indicator's three
// states render distinguishable, non-empty text (exact ANSI styling is not
// asserted — just that each state is represented and latency shows up when
// known).
func TestRenderConnBadgeAndDetail(t *testing.T) {
	cases := []struct {
		name    string
		m       model
		wantSub string // substring expected in renderConnDetail's output
	}{
		{"unknown", model{}, "checking"},
		{"reachable with latency", model{conn: conn{connKnown: true, connReachable: true, connLatencyMS: 7}}, "7ms"},
		{"reachable no latency", model{conn: conn{connKnown: true, connReachable: true}}, "reachable"},
		{"unreachable", model{conn: conn{connKnown: true, connReachable: false}}, "unreachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail := tc.m.renderConnDetail()
			if !strings.Contains(detail, tc.wantSub) {
				t.Errorf("renderConnDetail() = %q, want substring %q", detail, tc.wantSub)
			}
			// renderConnBadge must never panic/empty out regardless of state.
			if badge := tc.m.renderConnBadge(colSurface); badge == "" {
				t.Error("renderConnBadge() returned empty string")
			}
		})
	}
}

// errTestDaemonDown is a stand-in for a client.StatusInfo transport error.
var errTestDaemonDown = &testError{"daemon unreachable"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
