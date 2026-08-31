package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/tool"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type srvReq struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// fakeServer answers a minimal MCP handshake plus tool, resource, and prompt
// protocols. Unknown methods return an empty result.
func fakeServer(r io.Reader, w io.Writer) {
	dec := json.NewDecoder(r)
	enc := json.NewEncoder(w)
	respond := func(id int, result any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	for {
		var req srvReq
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.ID == nil {
			continue // notification — no response
		}
		switch req.Method {
		case "initialize":
			respond(*req.ID, map[string]any{"protocolVersion": protocolVersion})
		case "tools/list":
			respond(*req.ID, map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "echoes input", "inputSchema": map[string]any{"type": "object"}},
			}})
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			respond(*req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "called " + p.Name + " with " + string(p.Arguments)}},
				"isError": false,
			})
		case "resources/list":
			respond(*req.ID, map[string]any{"resources": []map[string]any{
				{"uri": "file:///example.txt", "name": "example", "mimeType": "text/plain"},
			}})
		case "resources/read":
			var p struct {
				URI string `json:"uri"`
			}
			_ = json.Unmarshal(req.Params, &p)
			respond(*req.ID, map[string]any{
				"contents": []map[string]any{
					{"uri": p.URI, "mimeType": "text/plain", "text": "content of " + p.URI},
				},
			})
		case "prompts/list":
			respond(*req.ID, map[string]any{"prompts": []map[string]any{
				{"name": "greet", "description": "greet someone", "arguments": []map[string]any{
					{"name": "name", "required": true},
				}},
			}})
		case "prompts/get":
			var p struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			respond(*req.ID, map[string]any{
				"description": "greeting prompt",
				"messages": []map[string]any{
					{"role": "user", "content": map[string]any{"type": "text", "text": "Hello, " + p.Arguments["name"] + "!"}},
				},
			})
		default:
			respond(*req.ID, map[string]any{})
		}
	}
}

// initCtx bounds an initialize handshake against a pipe-based fake server.
//
// Client.call blocks on its response channel with no deadline of its own, so a
// fake server that never answers hangs the test until Go's 10-minute package
// timeout panics and takes the whole `go test ./...` run with it — a failure
// mode that reads as CI flakiness rather than as this package's bug (P66.24).
// Ten seconds is far longer than a pipe handshake can legitimately take and far
// shorter than the package timeout, so a regression here is a named test
// failure in seconds.
func initCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func newPipeClient(t *testing.T) *Client {
	t.Helper()
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	go fakeServer(serverReader, serverWriter)
	c := newClient("test", clientReader, clientWriter, nil)
	if err := c.initialize(initCtx(t)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

func TestListAndCallTools(t *testing.T) {
	c := newPipeClient(t)
	ctx := context.Background()

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	text, isErr, err := c.CallTool(ctx, "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if isErr {
		t.Errorf("unexpected isError")
	}
	if text != `called echo with {"x":1}` {
		t.Errorf("call result = %q", text)
	}
}

func TestListResources(t *testing.T) {
	c := newPipeClient(t)
	resources, err := c.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///example.txt" {
		t.Errorf("unexpected resources: %+v", resources)
	}
	if resources[0].MIMEType != "text/plain" {
		t.Errorf("unexpected mime type: %q", resources[0].MIMEType)
	}
}

func TestReadResource(t *testing.T) {
	c := newPipeClient(t)
	text, mime, err := c.ReadResource(context.Background(), "file:///example.txt")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if mime != "text/plain" {
		t.Errorf("unexpected mime type: %q", mime)
	}
	if text != "content of file:///example.txt" {
		t.Errorf("unexpected content: %q", text)
	}
}

func TestListPrompts(t *testing.T) {
	c := newPipeClient(t)
	prompts, err := c.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "greet" {
		t.Errorf("unexpected prompts: %+v", prompts)
	}
	if len(prompts[0].Arguments) != 1 || prompts[0].Arguments[0].Name != "name" {
		t.Errorf("unexpected arguments: %+v", prompts[0].Arguments)
	}
}

func TestGetPrompt(t *testing.T) {
	c := newPipeClient(t)
	desc, msgs, err := c.GetPrompt(context.Background(), "greet", map[string]string{"name": "World"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if desc != "greeting prompt" {
		t.Errorf("unexpected description: %q", desc)
	}
	if len(msgs) != 1 || msgs[0].Content.Text != "Hello, World!" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestToolsChangedNotification(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	go func() {
		dec := json.NewDecoder(serverReader)
		enc := json.NewEncoder(serverWriter)
		defer serverWriter.Close()

		var req srvReq
		_ = dec.Decode(&req) // initialize
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": map[string]any{"protocolVersion": protocolVersion}})

		// Only now drain the rest of the client→server traffic
		// (notifications/initialized and anything after) so the client's
		// writes never block. Starting this *before* the read above put two
		// readers on one pipe: whichever won got the initialize request, and
		// when io.Copy won, dec.Decode blocked forever and no response was
		// ever sent (P66.24).
		go io.Copy(io.Discard, serverReader)

		// Send tools/list_changed notification to client.
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/tools/list_changed"})
	}()

	changed := make(chan struct{}, 1)
	c := newClient("test", clientReader, clientWriter, nil)
	c.onToolsChanged = func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	if err := c.initialize(initCtx(t)); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	select {
	case <-changed:
		// OK — notification was received and handler invoked.
	case <-time.After(time.Second):
		t.Error("onToolsChanged not called after tools/list_changed notification")
	}
}

func TestSamplingHandler(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	go func() {
		dec := json.NewDecoder(serverReader)
		enc := json.NewEncoder(serverWriter)
		defer serverWriter.Close()

		var req srvReq
		_ = dec.Decode(&req) // initialize
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": map[string]any{"protocolVersion": protocolVersion}})

		// Drain only after the initialize read — see the note in
		// TestToolsChangedNotification (P66.24).
		go io.Copy(io.Discard, serverReader)

		// Send a sampling/createMessage request to the client.
		samplingID := 42
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      samplingID,
			"method":  "sampling/createMessage",
			"params": map[string]any{
				"messages": []map[string]any{
					{"role": "user", "content": map[string]any{"type": "text", "text": "say hello"}},
				},
				"maxTokens": 50,
			},
		})
	}()

	handlerCalled := make(chan SamplingRequest, 1)
	c := newClient("test", clientReader, clientWriter, nil)
	c.Sampling = func(_ context.Context, req SamplingRequest) (SamplingResponse, error) {
		handlerCalled <- req
		return SamplingResponse{
			Role:    "assistant",
			Content: SamplingContent{Type: "text", Text: "hello"},
			Model:   "test-model",
		}, nil
	}
	if err := c.initialize(initCtx(t)); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	select {
	case req := <-handlerCalled:
		if len(req.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(req.Messages))
		}
		if req.Messages[0].Content.Text != "say hello" {
			t.Errorf("unexpected message text: %q", req.Messages[0].Content.Text)
		}
		if req.MaxTokens != 50 {
			t.Errorf("unexpected maxTokens: %d", req.MaxTokens)
		}
	case <-time.After(time.Second):
		t.Error("sampling handler not called")
	}
}

func TestSamplingHandlerNil(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	responseSeen := make(chan map[string]any, 1)
	go func() {
		dec := json.NewDecoder(serverReader)
		enc := json.NewEncoder(serverWriter)
		defer serverWriter.Close()

		var req srvReq
		_ = dec.Decode(&req) // initialize
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": map[string]any{"protocolVersion": protocolVersion}})

		// Send sampling request; expect an error response back.
		samplingID := 1
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      samplingID,
			"method":  "sampling/createMessage",
			"params":  map[string]any{"messages": []any{}, "maxTokens": 10},
		})

		// Read the client's error response, skipping any notifications
		// (e.g. notifications/initialized) that arrive first.
		for {
			var resp map[string]any
			if err := dec.Decode(&resp); err != nil {
				return
			}
			if _, hasID := resp["id"]; hasID {
				responseSeen <- resp
				return
			}
		}
	}()

	c := newClient("test", clientReader, clientWriter, nil)
	// Sampling is nil — client should respond with an RPC error.
	if err := c.initialize(initCtx(t)); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	select {
	case resp := <-responseSeen:
		if resp["error"] == nil {
			t.Errorf("expected error response when Sampling is nil, got: %v", resp)
		}
	case <-time.After(time.Second):
		t.Error("no response to sampling request")
	}
}

// TestReadLoopHandlesLineOverBufioDefault is the P9 regression for the
// scanner-buffer half of the finding: a single JSON-RPC response line well
// over bufio's 64KB default (a legitimate large tool result, not just an
// attack) must not silently kill the read loop the way it would have before
// maxMCPScanTokenBytes raised the cap.
func TestReadLoopHandlesLineOverBufioDefault(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	go io.Copy(io.Discard, serverReader) // drain outgoing requests; no responses needed
	c := newClient("test", clientReader, clientWriter, nil)

	const bigLen = 200 * 1024 // well over bufio's 64KB default token size
	go func() {
		big := strings.Repeat("x", bigLen)
		resp, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": big}},
				"isError": false,
			},
		})
		serverWriter.Write(append(resp, '\n'))
	}()

	text, isErr, err := c.CallTool(context.Background(), "big", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if isErr {
		t.Error("unexpected isError")
	}
	if len(text) != bigLen {
		t.Errorf("got %d bytes of content, want %d (line may have been silently dropped)", len(text), bigLen)
	}
}

// TestReadLoopDeathFailsPendingAndFutureCalls is the P9 regression for the
// error-handling half of the finding: once the transport dies, a call already
// in flight must fail promptly instead of blocking on its response channel
// forever (unless the caller happened to supply its own context deadline),
// and any call issued afterward must fail immediately too rather than
// registering into a pending map nothing will ever drain.
func TestReadLoopDeathFailsPendingAndFutureCalls(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	go io.Copy(io.Discard, serverReader)
	c := newClient("test", clientReader, clientWriter, nil)

	callErrCh := make(chan error, 1)
	go func() {
		_, err := c.call(context.Background(), "tools/call", map[string]any{})
		callErrCh <- err
	}()

	// Wait until the call has registered itself as pending, so closing the
	// connection below races the call being in flight, not before it starts.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		n := len(c.pending)
		c.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("call never registered as pending")
		}
		time.Sleep(time.Millisecond)
	}

	// Simulate the connection dying (server process exited, transport closed).
	serverWriter.Close()

	select {
	case err := <-callErrCh:
		if err == nil {
			t.Error("pending call should fail once the connection dies, not hang")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending call never returned after the connection died — it would hang forever without a caller-supplied context deadline")
	}

	if _, err := c.call(context.Background(), "tools/call", map[string]any{}); err == nil {
		t.Error("a call issued after the connection is known dead should fail immediately")
	}
}

func TestRegisterServersSkipsBadConfig(t *testing.T) {
	// A config with no command must be skipped without connecting.
	clients := RegisterServers(context.Background(), nil, []ServerConfig{{Name: "x"}}, discardLogger(), nil)
	if len(clients) != 0 {
		t.Errorf("expected no clients for command-less config")
	}
}

// TestParseCapabilityDefaultsToExecute is the core P7.1 regression: any
// unlabeled or unrecognized capability string must resolve to the most
// restrictive class, not silently fall through to something permissive.
func TestParseCapabilityDefaultsToExecute(t *testing.T) {
	cases := []struct {
		in   string
		want tool.Capability
	}{
		{"read", tool.CapRead},
		{"write", tool.CapWrite},
		{"network", tool.CapNetwork},
		{"execute", tool.CapExecute},
		{"spawn", tool.CapSpawn},
		{"", tool.CapExecute},
		{"bogus", tool.CapExecute},
	}
	for _, tc := range cases {
		if got := parseCapability(tc.in); got != tc.want {
			t.Errorf("parseCapability(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveCapabilityPerToolOverride(t *testing.T) {
	sc := ServerConfig{
		Name:       "fs",
		Capability: "execute",
		ToolCapabilities: map[string]string{
			"read_file":  "read",
			"write_file": "write",
		},
	}
	if got := resolveCapability(sc, "read_file"); got != tool.CapRead {
		t.Errorf("read_file capability = %q, want read", got)
	}
	if got := resolveCapability(sc, "write_file"); got != tool.CapWrite {
		t.Errorf("write_file capability = %q, want write", got)
	}
	// No override present — falls back to the server default.
	if got := resolveCapability(sc, "delete_file"); got != tool.CapExecute {
		t.Errorf("delete_file capability = %q, want execute (server default)", got)
	}
}

func TestResolveCapabilityDefaultsExecuteWithNoConfig(t *testing.T) {
	// A server with no capability declared at all must not launder its tools
	// as tool.CapNetwork — the pre-P7.1 behavior that bypassed the permission
	// gate unconditionally, including in plan mode.
	sc := ServerConfig{Name: "untrusted"}
	if got := resolveCapability(sc, "whatever"); got != tool.CapExecute {
		t.Errorf("capability = %q, want execute", got)
	}
}
