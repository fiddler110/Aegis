package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/api"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeBackend scripts session creation and a scripted event sequence for
// PostMessageReq, recording every CreateSession request and approval decision
// so tests can assert on them.
type fakeBackend struct {
	sessionID string
	events    []api.Event
	sessions  []api.SessionMeta

	mu         sync.Mutex
	createReqs []api.CreateSessionRequest
	approvals  []bool
}

func (f *fakeBackend) CreateSession(_ context.Context, req api.CreateSessionRequest) (*api.SessionMeta, error) {
	f.mu.Lock()
	f.createReqs = append(f.createReqs, req)
	f.mu.Unlock()
	return &api.SessionMeta{ID: f.sessionID, Mode: req.Mode}, nil
}

func (f *fakeBackend) PostMessageReq(ctx context.Context, _ string, _ api.PostMessageRequest) (<-chan api.Event, error) {
	ch := make(chan api.Event)
	go func() {
		defer close(ch)
		for _, e := range f.events {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (f *fakeBackend) SendApproval(_ context.Context, _, _ string, approved, _ bool) error {
	f.mu.Lock()
	f.approvals = append(f.approvals, approved)
	f.mu.Unlock()
	return nil
}

func (f *fakeBackend) ListSessions(context.Context) ([]api.SessionMeta, error) {
	return f.sessions, nil
}

// testPeer drives a Server over a pair of pipes from the client side.
type testPeer struct {
	t   *testing.T
	enc *json.Encoder
	dec *bufio.Reader
	id  int
}

func newTestPeer(t *testing.T, backend *fakeBackend, opts Options) (*testPeer, func()) {
	t.Helper()
	srv := NewServer(backend, opts, discardLogger())

	toSrvR, toSrvW := io.Pipe()
	fromSrvR, fromSrvW := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, toSrvR, fromSrvW) }()

	peer := &testPeer{t: t, enc: json.NewEncoder(toSrvW), dec: bufio.NewReader(fromSrvR)}
	cleanup := func() {
		cancel()
		toSrvW.Close()
		fromSrvW.Close()
		<-done
	}
	return peer, cleanup
}

func (p *testPeer) request(method string, params any) int {
	p.id++
	id := p.id
	raw, _ := json.Marshal(params)
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": json.RawMessage(raw)}
	if err := p.enc.Encode(msg); err != nil {
		p.t.Fatalf("encode request: %v", err)
	}
	return id
}

func (p *testPeer) readResponse() wireOut {
	p.t.Helper()
	line, err := p.dec.ReadBytes('\n')
	if err != nil {
		p.t.Fatalf("read response: %v", err)
	}
	var out wireOut
	if err := json.Unmarshal(line, &out); err != nil {
		p.t.Fatalf("unmarshal response: %v (line: %s)", err, line)
	}
	return out
}

func decodeResult[T any](t *testing.T, out wireOut) T {
	t.Helper()
	var v T
	raw, err := json.Marshal(out.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return v
}

func TestInitialize(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1"}
	peer, cleanup := newTestPeer(t, backend, Options{Version: "9.9"})
	defer cleanup()

	peer.request("initialize", map[string]any{"protocolVersion": protocolVersion})
	out := peer.readResponse()
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	res := decodeResult[initializeResult](t, out)
	if res.ProtocolVersion != protocolVersion {
		t.Errorf("protocolVersion = %q, want %q", res.ProtocolVersion, protocolVersion)
	}
	if res.ServerInfo.Name != "aegis" || res.ServerInfo.Version != "9.9" {
		t.Errorf("serverInfo = %+v", res.ServerInfo)
	}
}

func TestToolsList(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1"}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/list", map[string]any{})
	out := peer.readResponse()
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	res := decodeResult[toolsListResult](t, out)
	if len(res.Tools) != 3 {
		t.Fatalf("got %d tools, want 3", len(res.Tools))
	}
	names := map[string]bool{}
	for _, tl := range res.Tools {
		names[tl.Name] = true
		if len(tl.InputSchema) == 0 {
			t.Errorf("tool %s has empty input schema", tl.Name)
		}
	}
	for _, want := range []string{"aegis_prompt", "aegis_new_session", "aegis_list_sessions"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

func TestToolsCallRequiresAuthWhenTokenConfigured(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1", events: []api.Event{{Kind: api.KindText, Text: "ok"}}}
	peer, cleanup := newTestPeer(t, backend, Options{AuthToken: "s3cret"})
	defer cleanup()

	// tools/list stays reachable unauthenticated.
	peer.request("tools/list", map[string]any{})
	if out := peer.readResponse(); out.Error != nil {
		t.Fatalf("tools/list before auth: unexpected error: %+v", out.Error)
	}

	// tools/call is denied before authenticating.
	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
	out := peer.readResponse()
	if out.Error == nil || out.Error.Code != codeUnauthorized {
		t.Fatalf("expected codeUnauthorized before authenticating, got %+v", out.Error)
	}

	// Wrong token is rejected and still leaves the call denied.
	peer.request("aegis/authenticate", authenticateParams{Token: "wrong"})
	if out := peer.readResponse(); out.Error == nil || out.Error.Code != codeUnauthorized {
		t.Fatalf("expected codeUnauthorized for wrong token, got %+v", out.Error)
	}
	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
	if out := peer.readResponse(); out.Error == nil || out.Error.Code != codeUnauthorized {
		t.Fatalf("expected still-unauthenticated after wrong token, got %+v", out.Error)
	}

	// Correct token authenticates; tools/call now succeeds.
	peer.request("aegis/authenticate", authenticateParams{Token: "s3cret"})
	if out := peer.readResponse(); out.Error != nil {
		t.Fatalf("authenticate with correct token: unexpected error: %+v", out.Error)
	}
	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
	out = peer.readResponse()
	if out.Error != nil {
		t.Fatalf("tools/call after auth: unexpected error: %+v", out.Error)
	}
}

func TestToolsCallUnaffectedWhenNoTokenConfigured(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1", events: []api.Event{{Kind: api.KindText, Text: "ok"}}}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
	out := peer.readResponse()
	if out.Error != nil {
		t.Fatalf("tools/call with no token configured: unexpected error: %+v", out.Error)
	}
}

func TestPromptCreatesSessionAndReturnsText(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events: []api.Event{
			{Kind: api.KindText, Text: "Hello, "},
			{Kind: api.KindText, Text: "world."},
		},
	}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
	out := peer.readResponse()
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	res := decodeResult[toolCallResult](t, out)
	if res.IsError {
		t.Fatalf("unexpected isError result: %+v", res)
	}
	got := res.Content[0].Text
	if !strings.Contains(got, "Hello, world.") {
		t.Errorf("result text = %q, want it to contain the assistant reply", got)
	}
	if !strings.Contains(got, "[session: sess-1]") {
		t.Errorf("result text = %q, want a session marker", got)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.createReqs) != 1 {
		t.Fatalf("got %d CreateSession calls, want 1", len(backend.createReqs))
	}
	if backend.createReqs[0].Mode != "plan" {
		t.Errorf("default new-session mode = %q, want %q", backend.createReqs[0].Mode, "plan")
	}
}

func TestPromptWithExistingSessionSkipsCreate(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1", events: []api.Event{{Kind: api.KindText, Text: "ok"}}}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi", SessionID: "existing"})})
	out := peer.readResponse()
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	res := decodeResult[toolCallResult](t, out)
	if !strings.Contains(res.Content[0].Text, "[session: existing]") {
		t.Errorf("result text = %q, want session marker for the existing session", res.Content[0].Text)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.createReqs) != 0 {
		t.Errorf("got %d CreateSession calls, want 0 (session_id was supplied)", len(backend.createReqs))
	}
}

func TestPromptApprovalDeniedByDefault(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events: []api.Event{
			{Kind: api.KindApprovalRequest, ApprovalID: "a1"},
			{Kind: api.KindText, Text: "done"},
		},
	}
	peer, cleanup := newTestPeer(t, backend, Options{DefaultMode: "build"})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
	_ = peer.readResponse()

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.approvals) != 1 || backend.approvals[0] != false {
		t.Errorf("approvals = %v, want [false] (auto-approve off by default)", backend.approvals)
	}
	if backend.createReqs[0].Mode != "build" {
		t.Errorf("mode = %q, want %q", backend.createReqs[0].Mode, "build")
	}
}

func TestPromptApprovalGrantedWhenAutoApprove(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events: []api.Event{
			{Kind: api.KindApprovalRequest, ApprovalID: "a1"},
			{Kind: api.KindText, Text: "done"},
		},
	}
	peer, cleanup := newTestPeer(t, backend, Options{AutoApprove: true})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
	_ = peer.readResponse()

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.approvals) != 1 || backend.approvals[0] != true {
		t.Errorf("approvals = %v, want [true] (auto-approve on)", backend.approvals)
	}
}

func TestPromptErrorEventMarksResultAsError(t *testing.T) {
	backend := &fakeBackend{
		sessionID: "sess-1",
		events:    []api.Event{{Kind: api.KindError, Error: "boom"}},
	}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{Text: "hi"})})
	out := peer.readResponse()
	res := decodeResult[toolCallResult](t, out)
	if !res.IsError {
		t.Errorf("expected isError, got %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "boom") {
		t.Errorf("result text = %q, want it to mention the error", res.Content[0].Text)
	}
}

func TestPromptMissingTextIsRPCError(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1"}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_prompt", Arguments: mustJSON(promptArgs{})})
	out := peer.readResponse()
	if out.Error == nil {
		t.Fatal("expected an RPC error for missing text")
	}
}

func TestNewSessionTool(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-7"}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_new_session", Arguments: mustJSON(newSessionArgs{})})
	out := peer.readResponse()
	res := decodeResult[toolCallResult](t, out)
	if !strings.Contains(res.Content[0].Text, "sess-7") {
		t.Errorf("result text = %q, want it to mention the new session id", res.Content[0].Text)
	}
}

func TestListSessionsTool(t *testing.T) {
	backend := &fakeBackend{sessions: nil}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_list_sessions", Arguments: json.RawMessage(`{}`)})
	out := peer.readResponse()
	res := decodeResult[toolCallResult](t, out)
	if res.Content[0].Text != "No sessions." {
		t.Errorf("empty list text = %q", res.Content[0].Text)
	}
}

func TestListSessionsToolWithEntries(t *testing.T) {
	backend := &fakeBackend{sessions: []api.SessionMeta{
		{ID: "s1", Title: "fix the bug", Mode: "build", Origin: "mcp", UpdatedAt: time.Now()},
		{ID: "s2", Mode: "plan", Origin: "mcp", UpdatedAt: time.Now()},
	}}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "aegis_list_sessions", Arguments: json.RawMessage(`{}`)})
	out := peer.readResponse()
	res := decodeResult[toolCallResult](t, out)
	text := res.Content[0].Text
	if !strings.Contains(text, "s1") || !strings.Contains(text, "fix the bug") {
		t.Errorf("missing s1 details: %q", text)
	}
	if !strings.Contains(text, "s2") || !strings.Contains(text, "(untitled)") {
		t.Errorf("missing s2 untitled fallback: %q", text)
	}
}

func TestUnknownToolIsRPCError(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1"}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("tools/call", toolsCallParams{Name: "not_a_tool", Arguments: json.RawMessage(`{}`)})
	out := peer.readResponse()
	if out.Error == nil {
		t.Fatal("expected an RPC error for an unknown tool")
	}
}

func TestUnknownMethodIsRPCError(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1"}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	peer.request("not/a/method", map[string]any{})
	out := peer.readResponse()
	if out.Error == nil {
		t.Fatal("expected an RPC error for an unknown method")
	}
}

func TestNotificationGetsNoResponse(t *testing.T) {
	backend := &fakeBackend{sessionID: "sess-1"}
	peer, cleanup := newTestPeer(t, backend, Options{})
	defer cleanup()

	msg := map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}
	if err := peer.enc.Encode(msg); err != nil {
		t.Fatalf("encode notification: %v", err)
	}
	// Follow with a real request; if the notification had wrongly produced a
	// response, this read would receive that stray frame instead.
	peer.request("tools/list", map[string]any{})
	out := peer.readResponse()
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	if _, ok := out.Result.(map[string]any); !ok {
		t.Fatalf("expected tools/list result, got %+v", out.Result)
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
