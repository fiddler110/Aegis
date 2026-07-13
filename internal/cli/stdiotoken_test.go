package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fiddler110/aegis/internal/acp"
	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/mcpserver"
)

func discardLoggerCLI() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestResolveStdioAuthTokenAutoGeneratesWhenUnset covers the core P27.4/
// FIND-06 fix: when the env var isn't set, a token must be generated and
// persisted rather than the interface running open.
func TestResolveStdioAuthTokenAutoGeneratesWhenUnset(t *testing.T) {
	// A unique, never-set env var name stands in for AEGIS_MCP_TOKEN left
	// unconfigured — the default state for every existing integration.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.token")

	token, err := resolveStdioAuthToken("AEGIS_MCP_TOKEN_TEST_UNSET", path, "mcp-serve", discardLoggerCLI())
	if err != nil {
		t.Fatalf("resolveStdioAuthToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty auto-generated token")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected token file to be written: %v", err)
	}
	if string(data) != token {
		t.Errorf("token file contents = %q, want %q", data, token)
	}

	// A second resolution (e.g. a subsequent invocation of the command)
	// generates a fresh token, mirroring the daemon's own daemon.token
	// bootstrap (internal/server's generateAndWriteToken never reuses an
	// existing file's contents either).
	token2, err := resolveStdioAuthToken("AEGIS_MCP_TOKEN_TEST_UNSET", path, "mcp-serve", discardLoggerCLI())
	if err != nil {
		t.Fatalf("resolveStdioAuthToken (second call): %v", err)
	}
	if token2 == token {
		t.Error("expected a fresh token on each resolution, got the same value twice")
	}
}

// TestResolveStdioAuthTokenHonorsExplicitEnvVar confirms an operator's
// explicit AEGIS_MCP_TOKEN/AEGIS_ACP_TOKEN still wins and that no token file
// is generated in that case (nothing to auto-generate).
func TestResolveStdioAuthTokenHonorsExplicitEnvVar(t *testing.T) {
	t.Setenv("AEGIS_ACP_TOKEN_TEST_SET", "operator-chosen-secret")

	dir := t.TempDir()
	path := filepath.Join(dir, "acp.token")

	token, err := resolveStdioAuthToken("AEGIS_ACP_TOKEN_TEST_SET", path, "acp", discardLoggerCLI())
	if err != nil {
		t.Fatalf("resolveStdioAuthToken: %v", err)
	}
	if token != "operator-chosen-secret" {
		t.Errorf("token = %q, want the explicit env var value", token)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("expected no token file to be generated when the env var is already set")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat token file: %v", err)
	}
}

// TestResolveStdioAuthTokenFileOwnerRestricted checks the mode bits
// resolveStdioAuthToken's generated file ends up with. This exercises the
// full path through config.GenerateAndWriteToken (fsguard-hardened ACL on
// Windows, checked separately in internal/config/token_windows_test.go and
// internal/cli/stdiotoken_windows_test.go — 0600 mode bits are cosmetic
// there, see fsguard's package doc — and 0600 mode bits on POSIX).
func TestResolveStdioAuthTokenFileOwnerRestricted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are cosmetic on Windows; see stdiotoken_windows_test.go")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.token")

	if _, err := resolveStdioAuthToken("AEGIS_MCP_TOKEN_TEST_RESTRICTED", path, "mcp-serve", discardLoggerCLI()); err != nil {
		t.Fatalf("resolveStdioAuthToken: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("token file mode = %v, want no group/other bits set", got)
	}
}

// --- end-to-end: the generated token actually gates the interfaces ---

// fakeStdioBackend is a minimal Backend satisfying both mcpserver.Backend
// and acp.Backend for the auth wiring tests below — the tool/session
// mechanics are already covered by internal/mcpserver and internal/acp's own
// tests, so this only needs to not panic.
type fakeStdioBackend struct{}

func (fakeStdioBackend) CreateSession(context.Context, api.CreateSessionRequest) (*api.SessionMeta, error) {
	return &api.SessionMeta{ID: "sess-1"}, nil
}

func (fakeStdioBackend) PostMessageReq(ctx context.Context, _ string, _ api.PostMessageRequest) (<-chan api.Event, error) {
	ch := make(chan api.Event)
	close(ch)
	return ch, nil
}

func (fakeStdioBackend) SendApproval(context.Context, string, string, bool, bool) error { return nil }

func (fakeStdioBackend) ListSessions(context.Context) ([]api.SessionMeta, error) { return nil, nil }

type rpcFrame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func sendRPC(t *testing.T, enc *json.Encoder, id int, method string, params any) {
	t.Helper()
	raw, _ := json.Marshal(params)
	if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": json.RawMessage(raw)}); err != nil {
		t.Fatalf("send %s: %v", method, err)
	}
}

func readRPC(t *testing.T, dec *bufio.Reader) rpcFrame {
	t.Helper()
	line, err := dec.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var f rpcFrame
	if err := json.Unmarshal(line, &f); err != nil {
		t.Fatalf("unmarshal response: %v (line: %s)", err, line)
	}
	return f
}

// TestMCPServeGeneratedTokenIsRequired proves the full chain: a token
// resolved by resolveStdioAuthToken with the env var unset (i.e. exactly
// what `aegis mcp-serve` passes to mcpserver.Options.AuthToken by default
// now) actually gates tools/call — unauthenticated calls are rejected, and
// the same token via aegis/authenticate unblocks them (P27.4/FIND-06).
func TestMCPServeGeneratedTokenIsRequired(t *testing.T) {
	dir := t.TempDir()
	token, err := resolveStdioAuthToken("AEGIS_MCP_TOKEN_TEST_E2E", filepath.Join(dir, "mcp.token"), "mcp-serve", discardLoggerCLI())
	if err != nil {
		t.Fatalf("resolveStdioAuthToken: %v", err)
	}

	srv := mcpserver.NewServer(fakeStdioBackend{}, mcpserver.Options{AuthToken: token}, discardLoggerCLI())
	toSrvR, toSrvW := io.Pipe()
	fromSrvR, fromSrvW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, toSrvR, fromSrvW) }()
	defer func() { cancel(); toSrvW.Close(); fromSrvW.Close(); <-done }()

	enc := json.NewEncoder(toSrvW)
	dec := bufio.NewReader(fromSrvR)

	sendRPC(t, enc, 1, "tools/call", map[string]any{"name": "aegis_list_sessions"})
	if f := readRPC(t, dec); f.Error == nil || f.Error.Code != -32001 {
		t.Fatalf("expected unauthorized before authenticating, got %+v", f)
	}

	sendRPC(t, enc, 2, "aegis/authenticate", map[string]any{"token": token})
	if f := readRPC(t, dec); f.Error != nil {
		t.Fatalf("authenticate with generated token: unexpected error: %+v", f.Error)
	}

	sendRPC(t, enc, 3, "tools/call", map[string]any{"name": "aegis_list_sessions"})
	if f := readRPC(t, dec); f.Error != nil {
		t.Fatalf("tools/call after authenticating: unexpected error: %+v", f.Error)
	}
}

// TestACPGeneratedTokenIsRequired is TestMCPServeGeneratedTokenIsRequired's
// counterpart for `aegis acp` / AEGIS_ACP_TOKEN.
func TestACPGeneratedTokenIsRequired(t *testing.T) {
	dir := t.TempDir()
	token, err := resolveStdioAuthToken("AEGIS_ACP_TOKEN_TEST_E2E", filepath.Join(dir, "acp.token"), "acp", discardLoggerCLI())
	if err != nil {
		t.Fatalf("resolveStdioAuthToken: %v", err)
	}

	agent := acp.NewAgent(fakeStdioBackend{}, "build", discardLoggerCLI(), token)
	toAgentR, toAgentW := io.Pipe()
	fromAgentR, fromAgentW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = agent.Serve(ctx, toAgentR, fromAgentW) }()
	defer func() { cancel(); toAgentW.Close(); fromAgentW.Close(); <-done }()

	enc := json.NewEncoder(toAgentW)
	dec := bufio.NewReader(fromAgentR)

	sendRPC(t, enc, 1, "session/new", map[string]any{"cwd": dir})
	if f := readRPC(t, dec); f.Error == nil || f.Error.Code != -32001 {
		t.Fatalf("expected unauthorized before authenticating, got %+v", f)
	}

	sendRPC(t, enc, 2, "authenticate", map[string]any{"methodId": "shared_secret", "token": token})
	if f := readRPC(t, dec); f.Error != nil {
		t.Fatalf("authenticate with generated token: unexpected error: %+v", f.Error)
	}

	sendRPC(t, enc, 3, "session/new", map[string]any{"cwd": dir})
	if f := readRPC(t, dec); f.Error != nil {
		t.Fatalf("session/new after authenticating: unexpected error: %+v", f.Error)
	}
}
