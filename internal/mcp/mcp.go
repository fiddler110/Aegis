// Package mcp is a minimal client for the Model Context Protocol over stdio.
// It speaks newline-delimited JSON-RPC 2.0 so the harness can consume external
// MCP servers as tools without a third-party dependency.
//
// Supported MCP features:
//   - tools/list, tools/call
//   - resources/list, resources/read
//   - prompts/list, prompts/get
//   - notifications/tools/list_changed (dynamic tool refresh)
//   - sampling/createMessage (server-initiated LLM calls)
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

const protocolVersion = "2024-11-05"

// ToolInfo describes a tool exposed by an MCP server.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ResourceInfo describes a resource exposed by an MCP server.
type ResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

// PromptArgument describes one argument of a prompt template.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptInfo describes a prompt template exposed by an MCP server.
type PromptInfo struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptMessage is one message returned by GetPrompt.
type PromptMessage struct {
	Role    string `json:"role"`
	Content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// SamplingContent is the content block of a sampling message.
type SamplingContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// SamplingMessage is one message in a server-initiated sampling request.
type SamplingMessage struct {
	Role    string          `json:"role"`
	Content SamplingContent `json:"content"`
}

// SamplingRequest is sent by an MCP server to ask the client to generate text.
type SamplingRequest struct {
	Messages      []SamplingMessage `json:"messages"`
	MaxTokens     int               `json:"maxTokens"`
	SystemPrompt  string            `json:"systemPrompt,omitempty"`
	Temperature   *float64          `json:"temperature,omitempty"`
	StopSequences []string          `json:"stopSequences,omitempty"`
}

// SamplingResponse is the client's reply to a sampling request.
type SamplingResponse struct {
	Role       string          `json:"role"`
	Content    SamplingContent `json:"content"`
	Model      string          `json:"model,omitempty"`
	StopReason string          `json:"stopReason,omitempty"`
}

// SamplingHandler is invoked when an MCP server issues a sampling/createMessage
// request. If nil, the client rejects sampling with "not supported".
type SamplingHandler func(ctx context.Context, req SamplingRequest) (SamplingResponse, error)

// Client is a connected MCP server session.
type Client struct {
	server string
	w      io.Writer
	closer io.Closer

	mu      sync.Mutex
	enc     *json.Encoder
	nextID  int
	pending map[int]chan rpcResponse

	closeOnce sync.Once

	// Sampling is called when the server requests text generation. May be nil.
	Sampling SamplingHandler

	// onToolsChanged is called when the server sends a tools/list_changed
	// notification. RegisterServers sets this up to refresh the tool registry.
	onToolsChanged func()
}

// rpcMessage is the unified shape for all incoming JSON-RPC 2.0 messages.
// Responses have ID+Result/Error; notifications have Method only; server
// requests have ID+Method+Params.
type rpcMessage struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// rpcResponse carries the result or error of a response we were waiting for.
type rpcResponse struct {
	ID     *int
	Result json.RawMessage
	Error  *rpcError
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message) }

// newClient wires a client to an arbitrary transport (used by tests).
func newClient(server string, r io.Reader, w io.Writer, closer io.Closer) *Client {
	c := &Client{
		server:  server,
		w:       w,
		closer:  closer,
		enc:     json.NewEncoder(w),
		pending: map[int]chan rpcResponse{},
	}
	go c.readLoop(r)
	return c
}

// NewStdio launches command as a subprocess and connects over its stdio.
func NewStdio(ctx context.Context, server, command string, args []string, env []string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	closer := closerFunc(func() error {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		return cmd.Wait()
	})
	c := newClient(server, stdout, stdin, closer)
	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Server returns the configured server name.
func (c *Client) Server() string { return c.server }

func (c *Client) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var msg rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}

		if msg.ID == nil {
			// Server notification — no response expected.
			if msg.Method == "notifications/tools/list_changed" && c.onToolsChanged != nil {
				go c.onToolsChanged()
			}
			continue
		}

		c.mu.Lock()
		ch := c.pending[*msg.ID]
		delete(c.pending, *msg.ID)
		c.mu.Unlock()

		if ch != nil {
			// Response to one of our pending requests.
			ch <- rpcResponse{ID: msg.ID, Result: msg.Result, Error: msg.Error}
			continue
		}

		// Server-initiated request (ID not in pending). Only sampling is handled.
		if msg.Method == "sampling/createMessage" {
			go c.handleSampling(msg)
		}
	}
}

func (c *Client) handleSampling(msg rpcMessage) {
	if c.Sampling == nil {
		c.sendRPCError(msg.ID, -32601, "sampling not supported by this client")
		return
	}
	var req SamplingRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		c.sendRPCError(msg.ID, -32600, "invalid params: "+err.Error())
		return
	}
	resp, err := c.Sampling(context.Background(), req)
	if err != nil {
		c.sendRPCError(msg.ID, -32000, err.Error())
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      msg.ID,
		"result":  resp,
	})
}

func (c *Client) sendRPCError(id *int, code int, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	err := c.enc.Encode(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (c *Client) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enc.Encode(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) initialize(ctx context.Context) error {
	_, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"sampling": map[string]any{},
		},
		"clientInfo": map[string]any{"name": "aegis", "version": "0.1"},
	})
	if err != nil {
		return err
	}
	return c.notify("notifications/initialized", nil)
}

// ListTools returns the tools advertised by the server.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	res, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool invokes a tool and returns its concatenated text content.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = json.RawMessage(args)
	}
	res, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return "", false, err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", false, err
	}
	var text strings.Builder
	for _, block := range out.Content {
		text.WriteString(block.Text)
	}
	return text.String(), out.IsError, nil
}

// ListResources returns the resources advertised by the server. Servers that
// do not support resources return an MCP error, which is propagated as-is.
func (c *Client) ListResources(ctx context.Context) ([]ResourceInfo, error) {
	res, err := c.call(ctx, "resources/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Resources []ResourceInfo `json:"resources"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	return out.Resources, nil
}

// ReadResource fetches a resource by URI and returns its text content and MIME
// type. Binary resources (blob only) return empty text.
func (c *Client) ReadResource(ctx context.Context, uri string) (string, string, error) {
	res, err := c.call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return "", "", err
	}
	var out struct {
		Contents []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", "", err
	}
	if len(out.Contents) == 0 {
		return "", "", nil
	}
	item := out.Contents[0]
	return item.Text, item.MIMEType, nil
}

// ListPrompts returns the prompt templates advertised by the server.
func (c *Client) ListPrompts(ctx context.Context) ([]PromptInfo, error) {
	res, err := c.call(ctx, "prompts/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Prompts []PromptInfo `json:"prompts"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	return out.Prompts, nil
}

// GetPrompt retrieves a named prompt template, expanding args into its messages.
// Returns the description and the list of rendered messages.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (string, []PromptMessage, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	res, err := c.call(ctx, "prompts/get", params)
	if err != nil {
		return "", nil, err
	}
	var out struct {
		Description string          `json:"description"`
		Messages    []PromptMessage `json:"messages"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", nil, err
	}
	return out.Description, out.Messages, nil
}

// Close terminates the session.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		if c.closer != nil {
			err = c.closer.Close()
		}
	})
	return err
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }
