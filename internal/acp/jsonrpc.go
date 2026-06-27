// Package acp implements an Agent Client Protocol (ACP) server for Aegis,
// letting ACP-speaking editors (Zed, Neovim, …) drive the agent over stdio
// without a per-IDE plugin. It is a thin adapter: ACP methods are translated
// into calls against the daemon's session API, and engine stream events are
// translated back into ACP session/update notifications.
//
// Transport is JSON-RPC 2.0 with newline-delimited messages (one JSON object
// per line) over an io.Reader/io.Writer pair — stdin/stdout when run as the
// `aegis acp` subprocess.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// jsonRPCVersion is the only JSON-RPC version ACP uses.
const jsonRPCVersion = "2.0"

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message) }

// Standard JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

func errorf(code int, format string, args ...any) *RPCError {
	return &RPCError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Handler routes inbound requests and notifications from the client (editor).
type Handler interface {
	// HandleRequest processes a client request and returns a result value to be
	// JSON-encoded, or an error. It may run for the duration of a turn.
	HandleRequest(ctx context.Context, method string, params json.RawMessage) (any, *RPCError)
	// HandleNotification processes a client notification (no response expected).
	HandleNotification(ctx context.Context, method string, params json.RawMessage)
}

// wireMessage is the union of every JSON-RPC frame shape we read or write.
type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Conn is a JSON-RPC 2.0 connection over a line-delimited stream. It is safe for
// concurrent writers; the read loop dispatches each inbound request in its own
// goroutine so long-running handlers don't block cancellation or our own
// outbound client requests.
type Conn struct {
	r       *bufio.Reader
	w       io.Writer
	writeMu sync.Mutex
	handler Handler

	nextID  atomic.Int64
	pending sync.Map // int64 -> chan wireMessage
}

// NewConn builds a connection over r/w using handler for inbound traffic.
func NewConn(r io.Reader, w io.Writer, handler Handler) *Conn {
	return &Conn{
		r:       bufio.NewReaderSize(r, 1<<20),
		w:       w,
		handler: handler,
	}
}

// write serializes one frame followed by a newline.
func (c *Conn) write(msg wireMessage) error {
	msg.JSONRPC = jsonRPCVersion
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.w.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// Notify sends a client-bound notification (no response expected).
func (c *Conn) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(wireMessage{Method: method, Params: raw})
}

// Call issues a request to the client and blocks until the response arrives or
// ctx is cancelled. The decoded result is returned as raw JSON.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	id := c.nextID.Add(1)
	ch := make(chan wireMessage, 1)
	c.pending.Store(id, ch)
	defer c.pending.Delete(id)

	idJSON, _ := json.Marshal(id)
	if err := c.write(wireMessage{ID: idJSON, Method: method, Params: raw}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// Run reads and dispatches frames until the stream closes or ctx is cancelled.
func (c *Conn) Run(ctx context.Context) error {
	for {
		line, err := c.r.ReadBytes('\n')
		if len(line) > 0 {
			c.dispatch(ctx, line)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (c *Conn) dispatch(ctx context.Context, line []byte) {
	var msg wireMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		_ = c.write(wireMessage{Error: errorf(codeParseError, "parse error: %v", err)})
		return
	}

	switch {
	case msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null":
		// Inbound request: handle in a goroutine so a long turn doesn't stall
		// the read loop (cancellations and permission replies must keep flowing).
		go c.serveRequest(ctx, msg)
	case msg.Method != "":
		// Inbound notification.
		c.handler.HandleNotification(ctx, msg.Method, msg.Params)
	default:
		// Response to one of our outbound Calls.
		c.deliverResponse(msg)
	}
}

func (c *Conn) serveRequest(ctx context.Context, msg wireMessage) {
	result, rpcErr := c.handler.HandleRequest(ctx, msg.Method, msg.Params)
	if rpcErr != nil {
		_ = c.write(wireMessage{ID: msg.ID, Error: rpcErr})
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		_ = c.write(wireMessage{ID: msg.ID, Error: errorf(codeInternalError, "marshal result: %v", err)})
		return
	}
	_ = c.write(wireMessage{ID: msg.ID, Result: raw})
}

func (c *Conn) deliverResponse(msg wireMessage) {
	var id int64
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return
	}
	if ch, ok := c.pending.LoadAndDelete(id); ok {
		ch.(chan wireMessage) <- msg
	}
}
