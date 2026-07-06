package acp

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

// noopHandler discards every request/notification — the fuzz target below
// only cares whether Conn.dispatch itself panics on malformed wire frames,
// not what a real handler does with a well-formed one.
type noopHandler struct{}

func (noopHandler) HandleRequest(context.Context, string, json.RawMessage) (any, *RPCError) {
	return nil, nil
}
func (noopHandler) HandleNotification(context.Context, string, json.RawMessage) {}

// FuzzConnDispatch targets the newline-delimited JSON-RPC 2.0 frame parser
// ACP uses over stdio (Zed, Neovim, and any other ACP-speaking editor talk
// to `aegis acp` this way). One line of attacker- or bug-supplied input must
// never panic the daemon, regardless of how malformed the JSON is, whether
// required fields are missing, or whether id/method/params have the wrong
// JSON type entirely.
func FuzzConnDispatch(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"session/cancel","params":{}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad"}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"id":"not-a-number","method":"x"}`))
	f.Add([]byte(``))
	f.Add([]byte(`{"method":123}`))
	f.Fuzz(func(t *testing.T, line []byte) {
		c := NewConn(nil, io.Discard, noopHandler{})
		c.dispatch(context.Background(), line)
	})
}
