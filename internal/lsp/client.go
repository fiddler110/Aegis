// Package lsp provides a minimal LSP client that manages language server
// processes, pulls diagnostics, and resolves references. It speaks JSON-RPC 2.0
// over stdio, the same transport LSP servers expect.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Client is a connected LSP server session.
type Client struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	mu      sync.Mutex
	nextID  int
	pending map[int]chan json.RawMessage
	initd   bool
}

// NewClient launches command as an LSP server and connects over stdio.
func NewClient(ctx context.Context, name, command string, args []string, rootURI string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: start %s: %w", command, err)
	}
	c := &Client{
		name:    name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		pending: make(map[int]chan json.RawMessage),
	}
	go c.readLoop()

	if err := c.initialize(ctx, rootURI); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Name returns the server name.
func (c *Client) Name() string { return c.name }

// Close shuts down the server and kills the process.
func (c *Client) Close() error {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

// --- JSON-RPC transport (LSP uses Content-Length framed messages) ---

func (c *Client) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan json.RawMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		return result, nil
	}
}

func (c *Client) notify(method string, params any) error {
	return c.send(rpcNotification{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) readLoop() {
	reader := bufio.NewReader(c.stdout)
	tp := textproto.NewReader(reader)
	for {
		header, err := tp.ReadMIMEHeader()
		if err != nil {
			return
		}
		lengthStr := header.Get("Content-Length")
		if lengthStr == "" {
			continue
		}
		length, err := strconv.Atoi(strings.TrimSpace(lengthStr))
		if err != nil || length <= 0 {
			continue
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return
		}

		var resp rpcResponse
		if json.Unmarshal(body, &resp) != nil || resp.ID == nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[*resp.ID]
		delete(c.pending, *resp.ID)
		c.mu.Unlock()
		if ch != nil {
			if resp.Error != nil {
				ch <- nil
			} else {
				ch <- resp.Result
			}
		}
	}
}

// --- LSP protocol methods ---

func (c *Client) initialize(ctx context.Context, rootURI string) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
			},
		},
	}
	_, err := c.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("lsp: initialize: %w", err)
	}
	c.initd = true
	return c.notify("initialized", map[string]any{})
}

// Diagnostic is a single LSP diagnostic.
type Diagnostic struct {
	URI      string `json:"uri"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// Location is a source code location.
type Location struct {
	URI       string `json:"uri"`
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_col"`
}

// Diagnostics requests diagnostics for a file by opening it and pulling
// textDocument/diagnostic. For servers that publish diagnostics asynchronously
// (most), this opens the file and returns what is available.
func (c *Client) Diagnostics(ctx context.Context, fileURI, content string, version int) ([]Diagnostic, error) {
	if err := c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        fileURI,
			"languageId": langID(fileURI),
			"version":    version,
			"text":       content,
		},
	}); err != nil {
		return nil, err
	}

	// Pull diagnostics if the server supports the pull model.
	res, err := c.call(ctx, "textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": fileURI},
	})
	if err != nil || res == nil {
		return nil, err
	}

	var report struct {
		Items []struct {
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
			Severity int    `json:"severity"`
			Message  string `json:"message"`
			Source   string `json:"source"`
		} `json:"items"`
	}
	if json.Unmarshal(res, &report) != nil {
		return nil, nil
	}

	var out []Diagnostic
	for _, item := range report.Items {
		out = append(out, Diagnostic{
			URI:      fileURI,
			Line:     item.Range.Start.Line + 1,
			Col:      item.Range.Start.Character + 1,
			Severity: severityName(item.Severity),
			Message:  item.Message,
			Source:   item.Source,
		})
	}
	return out, nil
}

// References finds all references to the symbol at the given position.
func (c *Client) References(ctx context.Context, fileURI string, line, col int) ([]Location, error) {
	res, err := c.call(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": fileURI},
		"position":     map[string]any{"line": line - 1, "character": col - 1},
		"context":      map[string]any{"includeDeclaration": true},
	})
	if err != nil || res == nil {
		return nil, err
	}

	var locs []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
	}
	if json.Unmarshal(res, &locs) != nil {
		return nil, nil
	}

	var out []Location
	for _, l := range locs {
		out = append(out, Location{
			URI:       l.URI,
			StartLine: l.Range.Start.Line + 1,
			StartCol:  l.Range.Start.Character + 1,
			EndLine:   l.Range.End.Line + 1,
			EndCol:    l.Range.End.Character + 1,
		})
	}
	return out, nil
}

// DidChange notifies the server of a file change.
func (c *Client) DidChange(ctx context.Context, fileURI, content string, version int) error {
	return c.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": fileURI, "version": version},
		"contentChanges": []map[string]any{
			{"text": content},
		},
	})
}

func severityName(s int) string {
	switch s {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "unknown"
	}
}

func langID(uri string) string {
	switch {
	case strings.HasSuffix(uri, ".go"):
		return "go"
	case strings.HasSuffix(uri, ".py"):
		return "python"
	case strings.HasSuffix(uri, ".ts"), strings.HasSuffix(uri, ".tsx"):
		return "typescript"
	case strings.HasSuffix(uri, ".js"), strings.HasSuffix(uri, ".jsx"):
		return "javascript"
	case strings.HasSuffix(uri, ".rs"):
		return "rust"
	case strings.HasSuffix(uri, ".java"):
		return "java"
	case strings.HasSuffix(uri, ".c"), strings.HasSuffix(uri, ".h"):
		return "c"
	case strings.HasSuffix(uri, ".cpp"), strings.HasSuffix(uri, ".hpp"):
		return "cpp"
	default:
		return "plaintext"
	}
}
