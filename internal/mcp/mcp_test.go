package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"
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

func newPipeClient(t *testing.T) *Client {
	t.Helper()
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	go fakeServer(serverReader, serverWriter)
	c := newClient("test", clientReader, clientWriter, nil)
	if err := c.initialize(context.Background()); err != nil {
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

		// Drain client→server traffic so writes don't block.
		go io.Copy(io.Discard, serverReader)

		var req srvReq
		_ = dec.Decode(&req) // initialize
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": map[string]any{"protocolVersion": protocolVersion}})

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
	if err := c.initialize(context.Background()); err != nil {
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

		// Drain client→server traffic so writes don't block.
		go io.Copy(io.Discard, serverReader)

		var req srvReq
		_ = dec.Decode(&req) // initialize
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": map[string]any{"protocolVersion": protocolVersion}})

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
	if err := c.initialize(context.Background()); err != nil {
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
	if err := c.initialize(context.Background()); err != nil {
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

func TestRegisterServersSkipsBadConfig(t *testing.T) {
	// A config with no command must be skipped without connecting.
	clients := RegisterServers(context.Background(), nil, []ServerConfig{{Name: "x"}}, discardLogger())
	if len(clients) != 0 {
		t.Errorf("expected no clients for command-less config")
	}
}
