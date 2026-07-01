package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeBackend scripts a sequence of engine events for a prompt and records
// approval decisions.
type fakeBackend struct {
	sessionID string
	events    []api.Event
	block     bool // hold the stream open until the run context is cancelled

	mu        sync.Mutex
	approvals []bool
}

func (f *fakeBackend) CreateSession(context.Context, api.CreateSessionRequest) (*api.SessionMeta, error) {
	return &api.SessionMeta{ID: f.sessionID}, nil
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
		if f.block {
			<-ctx.Done()
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

// testPeer drives the agent over a pair of pipes from the client side.
type testPeer struct {
	t   *testing.T
	enc *json.Encoder
	dec *bufio.Reader
	id  int
}

func newTestPeer(t *testing.T) (*testPeer, *Agent, *fakeBackend, func()) {
	t.Helper()
	backend := &fakeBackend{sessionID: "sess-1"}
	toAgentR, toAgentW := io.Pipe()
	fromAgentR, fromAgentW := io.Pipe()
	agent := NewAgent(backend, "build", discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = agent.Serve(ctx, toAgentR, fromAgentW) }()

	peer := &testPeer{t: t, enc: json.NewEncoder(toAgentW), dec: bufio.NewReader(fromAgentR)}
	cleanup := func() {
		cancel()
		toAgentW.Close()
		fromAgentW.Close()
	}
	return peer, agent, backend, cleanup
}

func (p *testPeer) request(method string, params any) int {
	p.id++
	id := p.id
	raw, _ := json.Marshal(params)
	if err := p.enc.Encode(wireMessage{JSONRPC: jsonRPCVersion, ID: jsonInt(id), Method: method, Params: raw}); err != nil {
		p.t.Fatalf("send request: %v", err)
	}
	return id
}

func (p *testPeer) notify(method string, params any) {
	raw, _ := json.Marshal(params)
	if err := p.enc.Encode(wireMessage{JSONRPC: jsonRPCVersion, Method: method, Params: raw}); err != nil {
		p.t.Fatalf("send notification: %v", err)
	}
}

func (p *testPeer) respond(id json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	if err := p.enc.Encode(wireMessage{JSONRPC: jsonRPCVersion, ID: id, Result: raw}); err != nil {
		p.t.Fatalf("send response: %v", err)
	}
}

// read returns the next frame from the agent, failing on timeout.
func (p *testPeer) read() wireMessage {
	p.t.Helper()
	type res struct {
		msg wireMessage
		err error
	}
	done := make(chan res, 1)
	go func() {
		line, err := p.dec.ReadBytes('\n')
		var m wireMessage
		if err == nil {
			err = json.Unmarshal(line, &m)
		}
		done <- res{m, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			p.t.Fatalf("read frame: %v", r.err)
		}
		return r.msg
	case <-time.After(3 * time.Second):
		p.t.Fatal("timed out reading frame")
		return wireMessage{}
	}
}

func jsonInt(i int) json.RawMessage {
	b, _ := json.Marshal(i)
	return b
}

func TestInitializeAndNewSession(t *testing.T) {
	peer, _, _, cleanup := newTestPeer(t)
	defer cleanup()

	id := peer.request(methodInitialize, initializeParams{ProtocolVersion: protocolVersion})
	resp := peer.read()
	if string(resp.ID) != string(jsonInt(id)) || resp.Error != nil {
		t.Fatalf("initialize resp = %+v", resp)
	}
	var init initializeResult
	if err := json.Unmarshal(resp.Result, &init); err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != protocolVersion || !init.AgentCapabilities.PromptCapabilities.Image {
		t.Errorf("unexpected initialize result: %+v", init)
	}

	peer.request(methodNewSession, newSessionParams{Cwd: "/tmp"})
	resp = peer.read()
	var ns newSessionResult
	if err := json.Unmarshal(resp.Result, &ns); err != nil {
		t.Fatal(err)
	}
	if ns.SessionID != "sess-1" {
		t.Errorf("sessionId = %q, want sess-1", ns.SessionID)
	}
}

func TestPromptStreamsUpdates(t *testing.T) {
	peer, _, backend, cleanup := newTestPeer(t)
	defer cleanup()
	backend.events = []api.Event{
		{Kind: api.KindText, Text: "Hello "},
		{Kind: api.KindText, Text: "world"},
		{Kind: api.KindToolCall, Tool: "read_file", ToolInput: json.RawMessage(`{"path":"x"}`)},
		{Kind: api.KindToolResult, Tool: "read_file", ToolResult: "file contents"},
	}

	promptID := peer.request(methodPrompt, promptParams{
		SessionID: "sess-1",
		Prompt:    []contentBlock{textBlock("read the file")},
	})

	var texts []string
	var sawToolCall, sawToolDone bool
	var stop string
	for {
		msg := peer.read()
		if len(msg.ID) > 0 && string(msg.ID) == string(jsonInt(promptID)) {
			var pr promptResult
			if err := json.Unmarshal(msg.Result, &pr); err != nil {
				t.Fatal(err)
			}
			stop = pr.StopReason
			break
		}
		if msg.Method != methodSessionUpdate {
			t.Fatalf("unexpected frame: %+v", msg)
		}
		var up sessionUpdateParams
		// Decode the update generically to inspect its tag.
		var rawUpdate struct {
			Update map[string]json.RawMessage `json:"update"`
		}
		if err := json.Unmarshal(msg.Params, &rawUpdate); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(msg.Params, &up)
		var tag string
		json.Unmarshal(rawUpdate.Update["sessionUpdate"], &tag)
		switch tag {
		case updAgentMessageChunk:
			var mc messageChunk
			json.Unmarshal(msg.Params, &struct {
				Update *messageChunk `json:"update"`
			}{&mc})
			texts = append(texts, mc.Content.Text)
		case updToolCall:
			sawToolCall = true
		case updToolCallUpdate:
			sawToolDone = true
		}
	}

	if stop != stopEndTurn {
		t.Errorf("stop reason = %q, want %q", stop, stopEndTurn)
	}
	joined := texts[0] + texts[1]
	if joined != "Hello world" {
		t.Errorf("message text = %q", joined)
	}
	if !sawToolCall || !sawToolDone {
		t.Errorf("missing tool updates: call=%v done=%v", sawToolCall, sawToolDone)
	}
}

func TestPromptPermissionApproved(t *testing.T) {
	peer, _, backend, cleanup := newTestPeer(t)
	defer cleanup()
	backend.events = []api.Event{
		{Kind: api.KindToolCall, Tool: "shell", ToolInput: json.RawMessage(`{"command":"ls"}`)},
		{Kind: api.KindApprovalRequest, Tool: "shell", ApprovalID: "run-9", ApprovalReason: "run shell: ls"},
		{Kind: api.KindToolResult, Tool: "shell", ToolResult: "a\nb"},
	}

	promptID := peer.request(methodPrompt, promptParams{
		SessionID: "sess-1",
		Prompt:    []contentBlock{textBlock("list files")},
	})

	var stop string
	for {
		msg := peer.read()
		// The agent's permission request (a request, not a notification).
		if msg.Method == methodRequestPermission {
			peer.respond(msg.ID, requestPermissionResult{
				Outcome: permissionOutcome{Outcome: "selected", OptionID: "allow"},
			})
			continue
		}
		if len(msg.ID) > 0 && string(msg.ID) == string(jsonInt(promptID)) {
			var pr promptResult
			json.Unmarshal(msg.Result, &pr)
			stop = pr.StopReason
			break
		}
	}

	if stop != stopEndTurn {
		t.Errorf("stop reason = %q", stop)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.approvals) != 1 || !backend.approvals[0] {
		t.Errorf("approvals = %v, want [true]", backend.approvals)
	}
}

func TestPromptCancel(t *testing.T) {
	peer, _, backend, cleanup := newTestPeer(t)
	defer cleanup()
	backend.block = true
	backend.events = []api.Event{{Kind: api.KindText, Text: "working"}}

	promptID := peer.request(methodPrompt, promptParams{
		SessionID: "sess-1",
		Prompt:    []contentBlock{textBlock("long task")},
	})

	// Drain the first update, then cancel.
	msg := peer.read()
	if msg.Method != methodSessionUpdate {
		t.Fatalf("expected an update before cancel, got %+v", msg)
	}
	peer.notify(methodCancel, map[string]string{"sessionId": "sess-1"})

	resp := peer.read()
	if string(resp.ID) != string(jsonInt(promptID)) {
		t.Fatalf("expected prompt response, got %+v", resp)
	}
	var pr promptResult
	json.Unmarshal(resp.Result, &pr)
	if pr.StopReason != stopCancelled {
		t.Errorf("stop reason = %q, want %q", pr.StopReason, stopCancelled)
	}
}

func TestBuildMessageRequest(t *testing.T) {
	req := buildMessageRequest([]contentBlock{
		textBlock("describe "),
		{Type: "image", MimeType: "image/png", Data: "abc"},
		textBlock("this"),
	})
	if req.Text != "describe this" {
		t.Errorf("text = %q", req.Text)
	}
	if len(req.Images) != 1 || req.Images[0].MediaType != "image/png" || req.Images[0].Data != "abc" {
		t.Errorf("images = %+v", req.Images)
	}
}
