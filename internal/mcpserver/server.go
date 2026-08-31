package mcpserver

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/api"
)

// Backend is the subset of the daemon client the MCP server needs. *client.Client
// satisfies it, so the server is a thin translator over the existing session
// API — the same pattern internal/acp's Agent uses for ACP.
type Backend interface {
	CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.SessionMeta, error)
	PostMessageReq(ctx context.Context, id string, req api.PostMessageRequest) (<-chan api.Event, error)
	SendApproval(ctx context.Context, sessionID, approvalID string, approved, allowAlways bool) error
	ListSessions(ctx context.Context) ([]api.SessionMeta, error)
}

// Options configures the server's safety posture. An MCP tools/call is a
// synchronous request/response with no human in the loop to ask, so the
// defaults are deliberately conservative: new sessions start in plan mode
// (read + network only, nothing needs approval), a caller cannot ask for a
// mode more permissive than DefaultMode, and any approval request that does
// arise (e.g. the operator configured a more permissive DefaultMode) is denied
// unless the operator opts in.
//
// The middle clause is the one this package used to promise and not keep: the
// mode came straight from the caller's arguments, so "sessions start in plan
// mode" held only for a caller that declined to say otherwise. See
// AllowCallerModeEscalation.
type Options struct {
	// DefaultMode is applied to a new session when the caller doesn't specify
	// one. Defaults to "plan" if empty.
	DefaultMode string
	// AutoApprove lets a tool call needing approval proceed automatically
	// instead of being denied. Off by default.
	AutoApprove bool
	// AutoApproveTools narrows AutoApprove to a set of tool names. Empty (the
	// default) leaves AutoApprove blanket — every approval the run raises is
	// granted — which is what it has always meant; a non-empty list grants only
	// those tools and denies the rest (C1/F2).
	//
	// The distinction matters because both ends of the old switch are
	// unsatisfying: off, and a build-mode session's one CapExecute request is
	// denied with no way to allow just it; on, and an MCP client has, in
	// effect, permission.auto_approve_exec for arbitrary shell for the whole
	// run. There is no human watching either way, which is exactly why the
	// grant should be able to name what it covers.
	AutoApproveTools []string
	// AllowCallerModeEscalation lets a caller's `mode` tool argument exceed
	// DefaultMode. Off by default (C1/F1).
	//
	// The daemon's HTTP API deliberately treats an explicit request mode as the
	// user's own decision — resolveSessionMode clamps a *persona's* mode and
	// nothing else — because its authenticated caller *is* the user. An MCP
	// client is not: it is a program (an editor plugin, another agent) holding
	// a token it read from a file, relaying instructions from wherever it got
	// them. Without this clamp such a caller could ask for `auto` and get it
	// whatever mcp_server.default_mode and permission.mode said — and under
	// `auto`, permission.Policy.Decide allows every capability outright, so no
	// approval is ever raised and AutoApprove above becomes vacuous.
	//
	// A caller may still ask for anything *at or below* DefaultMode; the clamp
	// only stops escalation, and the downgrade is logged rather than refused so
	// a client that asks for more still gets a working session.
	AllowCallerModeEscalation bool
	// Version is reported as the server's version in the initialize response.
	// Defaults to "0.1" if empty.
	Version string
	// AuthToken, when non-empty, requires the client to authenticate (via the
	// custom "aegis/authenticate" request) with this shared secret before
	// tools/call is allowed to proceed. initialize/ping/tools/list stay
	// reachable unauthenticated since they expose no session-driving
	// capability. This closes FIND-02/P24.2: without it, any local process
	// able to write to this subprocess's stdin can drive full agent turns.
	// Empty leaves tools/call unauthenticated — this package's own default,
	// kept for callers embedding Server directly, but never what `aegis
	// mcp-serve` passes: that command always resolves a non-empty token
	// (AEGIS_MCP_TOKEN if set, otherwise one it generates and writes to
	// config.Config.MCPTokenPath) before constructing Options, so the CLI
	// interface itself is never unauthenticated by default (FIND-06/P27.4).
	AuthToken string
}

// Server implements the MCP server side of the protocol: initialize,
// tools/list, and tools/call, translating tool calls into calls against the
// daemon's session API (P6.3).
type Server struct {
	backend     Backend
	logger      *slog.Logger
	defaultMode string
	autoApprove bool
	// autoApproveTools is nil for a blanket grant and otherwise the set of tool
	// names AutoApprove covers.
	autoApproveTools map[string]struct{}
	allowEscalation  bool
	version          string
	authToken        string

	authMu        sync.Mutex
	authenticated bool
}

// NewServer builds an MCP server over backend.
func NewServer(backend Backend, opts Options, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	mode := opts.DefaultMode
	if mode == "" {
		mode = "plan"
	}
	version := opts.Version
	if version == "" {
		version = "0.1"
	}
	var tools map[string]struct{}
	if len(opts.AutoApproveTools) > 0 {
		tools = make(map[string]struct{}, len(opts.AutoApproveTools))
		for _, name := range opts.AutoApproveTools {
			tools[name] = struct{}{}
		}
	}
	return &Server{
		backend:          backend,
		logger:           logger,
		defaultMode:      mode,
		autoApprove:      opts.AutoApprove,
		autoApproveTools: tools,
		allowEscalation:  opts.AllowCallerModeEscalation,
		version:          version,
		authToken:        opts.AuthToken,
	}
}

// modeRankUnknown sorts above every known mode, so an unrecognized
// caller-supplied string counts as an escalation and is clamped rather than
// sailing through a lookup that happened to return zero.
const modeRankUnknown = 99

// permModeRank orders the permission modes from least to most permissive. It
// is a small deliberate duplicate of internal/server's own ranking: this
// package talks to the daemon over api.CreateSessionRequest and does not
// import internal/server.
func permModeRank(mode string) int {
	switch mode {
	case "", "plan":
		return 0
	case "build":
		return 1
	case "auto":
		return 2
	default:
		return modeRankUnknown
	}
}

// resolveMode picks the mode for a session this server creates: the caller's
// choice when it made one, the configured default otherwise, clamped so a
// caller can never ask for more than the operator configured (C1/F1). The
// clamp is skipped only when the operator opted into caller escalation.
func (s *Server) resolveMode(reqMode string) string {
	if reqMode == "" {
		return s.defaultMode
	}
	if s.allowEscalation || permModeRank(reqMode) <= permModeRank(s.defaultMode) {
		return reqMode
	}
	s.logger.Warn("mcp-serve: caller requested a more permissive mode than the configured default; clamping",
		"requested", reqMode, "default_mode", s.defaultMode)
	return s.defaultMode
}

// approvalFor answers one approval request. A blanket AutoApprove keeps its
// old meaning; a configured tool list narrows it to those tools. Every granted
// approval is logged at Info because there is no human in the loop to have
// seen the prompt — the log is the only record that a tool ran on nobody's
// explicit say-so.
func (s *Server) approvalFor(ev api.Event) bool {
	if !s.autoApprove {
		return false
	}
	if s.autoApproveTools != nil {
		if _, ok := s.autoApproveTools[ev.Tool]; !ok {
			s.logger.Info("mcp-serve: denying approval for a tool outside auto_approve_tools",
				"tool", ev.Tool, "reason", ev.ApprovalReason)
			return false
		}
	}
	s.logger.Info("mcp-serve: auto-approving a tool call with no human in the loop",
		"tool", ev.Tool, "input", string(ev.ToolInput), "reason", ev.ApprovalReason)
	return true
}

// authenticateParams is the payload for the custom "aegis/authenticate"
// request (not part of the base MCP spec — a client unaware of it simply
// never calls it, which is fine when AuthToken is unset).
type authenticateParams struct {
	Token string `json:"token"`
}

func (s *Server) handleAuthenticate(params json.RawMessage) (any, *rpcErr) {
	if s.authToken == "" {
		return map[string]any{"authenticated": true}, nil
	}
	var p authenticateParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcErr{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	if subtle.ConstantTimeCompare([]byte(p.Token), []byte(s.authToken)) != 1 {
		return nil, &rpcErr{Code: codeUnauthorized, Message: "invalid token"}
	}
	s.authMu.Lock()
	s.authenticated = true
	s.authMu.Unlock()
	return map[string]any{"authenticated": true}, nil
}

func (s *Server) isAuthenticated() bool {
	if s.authToken == "" {
		return true
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	return s.authenticated
}

// wireIn is a request or notification received from the client. Requests
// carry a non-null id; notifications omit it.
type wireIn struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type wireOut struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC 2.0 error codes, plus a server-defined code for the
// custom auth extension (outside the -32768..-32000 reserved range).
const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeUnauthorized   = -32001
)

// maxLineBytes caps a single JSON-RPC line, matching internal/mcp's client-side
// scanner limit — a verbose tool call/result should never need more than this.
const maxLineBytes = 8 << 20

// Serve reads newline-delimited JSON-RPC 2.0 frames from r and writes
// responses to w until the stream closes or ctx is cancelled. Each request is
// handled in its own goroutine so a long-running aegis_prompt call doesn't
// block other concurrent requests (or the read loop) from being served.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var writeMu sync.Mutex
	write := func(v wireOut) {
		v.JSONRPC = "2.0"
		data, err := json.Marshal(v)
		if err != nil {
			s.logger.Warn("mcp-serve: marshal response", "err", err)
			return
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := w.Write(append(data, '\n')); err != nil {
			s.logger.Warn("mcp-serve: write response", "err", err)
		}
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var in wireIn
		if err := json.Unmarshal(line, &in); err != nil {
			write(wireOut{Error: &rpcErr{Code: codeParseError, Message: "parse error: " + err.Error()}})
			continue
		}
		if len(in.ID) == 0 || string(in.ID) == "null" {
			// Notification: no response expected.
			s.handleNotification(in.Method)
			continue
		}
		id := append(json.RawMessage(nil), in.ID...)
		method := in.Method
		params := in.Params
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, rerr := s.handleRequest(ctx, method, params)
			if rerr != nil {
				write(wireOut{ID: id, Error: rerr})
				return
			}
			write(wireOut{ID: id, Result: result})
		}()
	}
	return scanner.Err()
}

func (s *Server) handleNotification(method string) {
	// notifications/initialized acknowledges the handshake; nothing to set up.
	// notifications/cancelled is not propagated to an in-flight aegis_prompt
	// call in v1 — a documented scope limit, not an oversight.
	if method != "notifications/initialized" {
		s.logger.Debug("mcp-serve: unhandled notification", "method", method)
	}
}

func (s *Server) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, *rpcErr) {
	switch method {
	case "initialize":
		return s.handleInitialize(), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return toolsListResult{Tools: listedTools()}, nil
	case "aegis/authenticate":
		return s.handleAuthenticate(params)
	case "tools/call":
		if !s.isAuthenticated() {
			return nil, &rpcErr{Code: codeUnauthorized, Message: "authentication required: call aegis/authenticate with the shared token first"}
		}
		return s.handleToolsCall(ctx, params)
	default:
		return nil, &rpcErr{Code: codeMethodNotFound, Message: "method not found: " + method}
	}
}

func (s *Server) handleInitialize() any {
	return initializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities:    map[string]any{"tools": map[string]any{}},
		ServerInfo:      serverInfo{Name: "aegis", Version: s.version},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (any, *rpcErr) {
	var p toolsCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcErr{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	switch p.Name {
	case "aegis_prompt":
		return s.callPrompt(ctx, p.Arguments)
	case "aegis_new_session":
		return s.callNewSession(ctx, p.Arguments)
	case "aegis_list_sessions":
		return s.callListSessions(ctx)
	default:
		return nil, &rpcErr{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
	}
}

func (s *Server) callNewSession(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	var a newSessionArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, &rpcErr{Code: codeInvalidParams, Message: "invalid arguments: " + err.Error()}
		}
	}
	meta, err := s.backend.CreateSession(ctx, api.CreateSessionRequest{Mode: s.resolveMode(a.Mode), Persona: a.Persona})
	if err != nil {
		return errResult(fmt.Errorf("create session: %w", err)), nil
	}
	return textResult(fmt.Sprintf("Created session %s (mode: %s)", meta.ID, meta.Mode)), nil
}

func (s *Server) callListSessions(ctx context.Context) (any, *rpcErr) {
	metas, err := s.backend.ListSessions(ctx)
	if err != nil {
		return errResult(fmt.Errorf("list sessions: %w", err)), nil
	}
	if len(metas) == 0 {
		return textResult("No sessions."), nil
	}
	var b strings.Builder
	for _, m := range metas {
		title := m.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "%s — %s [%s] updated %s\n", m.ID, title, m.Mode, m.UpdatedAt.Format(time.RFC3339))
	}
	return textResult(strings.TrimRight(b.String(), "\n")), nil
}

func (s *Server) callPrompt(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	var a promptArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpcErr{Code: codeInvalidParams, Message: "invalid arguments: " + err.Error()}
	}
	if strings.TrimSpace(a.Text) == "" {
		return nil, &rpcErr{Code: codeInvalidParams, Message: "text is required"}
	}

	sessionID := a.SessionID
	if sessionID == "" {
		meta, err := s.backend.CreateSession(ctx, api.CreateSessionRequest{Mode: s.resolveMode(a.Mode), Persona: a.Persona})
		if err != nil {
			return errResult(fmt.Errorf("create session: %w", err)), nil
		}
		sessionID = meta.ID
	}

	events, err := s.backend.PostMessageReq(ctx, sessionID, api.PostMessageRequest{Text: a.Text})
	if err != nil {
		return errResult(fmt.Errorf("post message: %w", err)), nil
	}

	var text strings.Builder
	var toolCalls int
	var runErr string
	for ev := range events {
		switch ev.Kind {
		case api.KindText:
			text.WriteString(ev.Text)
		case api.KindToolCall:
			toolCalls++
		case api.KindApprovalRequest:
			if aerr := s.backend.SendApproval(ctx, sessionID, ev.ApprovalID, s.approvalFor(ev), false); aerr != nil {
				s.logger.Warn("mcp-serve: send approval failed", "err", aerr)
			}
		case api.KindError:
			runErr = ev.Error
		}
	}

	out := strings.TrimSpace(text.String())
	switch {
	case out != "":
		// already have text
	case runErr != "":
		out = "(no text output — run ended with an error: " + runErr + ")"
	case toolCalls > 0:
		out = fmt.Sprintf("(no text output — %d tool call(s) executed)", toolCalls)
	default:
		out = "(no output)"
	}
	out += fmt.Sprintf("\n\n[session: %s]", sessionID)

	result := textResult(out)
	result.IsError = runErr != ""
	return result, nil
}
