package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type srvReq struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// fakeServer answers a minimal MCP handshake and tool protocol.
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
			continue // notification
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

func TestRegisterServersSkipsBadConfig(t *testing.T) {
	// A config with no command must be skipped without connecting.
	clients := RegisterServers(context.Background(), nil, []ServerConfig{{Name: "x"}}, discardLogger())
	if len(clients) != 0 {
		t.Errorf("expected no clients for command-less config")
	}
}
