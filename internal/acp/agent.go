package acp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/fiddler110/aegis/internal/api"
)

// Backend is the subset of the daemon client the ACP agent needs. *client.Client
// satisfies it, so the agent is a thin translator over the existing session API.
type Backend interface {
	CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.SessionMeta, error)
	PostMessageReq(ctx context.Context, id string, req api.PostMessageRequest) (<-chan api.Event, error)
	SendApproval(ctx context.Context, sessionID, approvalID string, approved, allowAlways bool) error
	// ListSessions backs the P80.1 mode ceiling on a borrowed session: it is
	// the only way this package can learn the mode of a session it did not
	// create. Nothing in the ACP protocol surface exposes the list itself.
	ListSessions(ctx context.Context) ([]api.SessionMeta, error)
}

// Agent implements the ACP Handler, translating ACP methods into daemon calls
// and engine stream events into ACP session/update notifications.
type Agent struct {
	backend   Backend
	mode      string // permission mode for new sessions
	logger    *slog.Logger
	authToken string // non-empty requires authenticate before session/new|prompt (FIND-02/P24.2)

	conn *Conn

	mu            sync.Mutex
	cancels       map[string]context.CancelFunc // sessionId -> cancel for in-flight prompt
	authenticated bool
	// created records the sessions this Agent created. Like mcpserver's set,
	// it is a fast path rather than the control — it does not survive a
	// restart, and an unrecognised id is checked against the mode ceiling
	// instead of being refused (P80.1).
	created map[string]struct{}
}

// NewAgent builds an ACP agent over backend. mode is the permission mode applied
// to sessions it creates ("plan", "build", or "auto"). authToken, when
// non-empty, requires the client to call "authenticate" with a matching
// shared secret before session/new or session/prompt is allowed to proceed
// (FIND-02/P24.2); empty leaves session/new and session/prompt reachable
// without authenticating first — this package's own default, kept for
// callers embedding Agent directly, but never what `aegis acp` passes: that
// command always resolves a non-empty token (AEGIS_ACP_TOKEN if set,
// otherwise one it generates and writes to config.Config.ACPTokenPath)
// before calling NewAgent, so the CLI interface itself is never
// unauthenticated by default (FIND-06/P27.4).
func NewAgent(backend Backend, mode string, logger *slog.Logger, authToken string) *Agent {
	if logger == nil {
		logger = slog.Default()
	}
	return &Agent{
		backend:   backend,
		mode:      mode,
		logger:    logger,
		authToken: authToken,
		cancels:   map[string]context.CancelFunc{},
		created:   map[string]struct{}{},
	}
}

// Serve runs the ACP protocol over r/w until the stream closes or ctx ends.
func (a *Agent) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	a.conn = NewConn(r, w, a)
	return a.conn.Run(ctx)
}

// HandleRequest implements Handler.
func (a *Agent) HandleRequest(ctx context.Context, method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case methodInitialize:
		return a.handleInitialize(params)
	case methodAuthenticate:
		return a.handleAuthenticate(params)
	case methodNewSession:
		if !a.isAuthenticated() {
			return nil, errorf(codeUnauthorized, "authentication required: call authenticate with the shared token first")
		}
		return a.handleNewSession(ctx, params)
	case methodPrompt:
		if !a.isAuthenticated() {
			return nil, errorf(codeUnauthorized, "authentication required: call authenticate with the shared token first")
		}
		return a.handlePrompt(ctx, params)
	case methodLoadSession:
		return nil, errorf(codeMethodNotFound, "session/load is not supported")
	default:
		return nil, errorf(codeMethodNotFound, "unknown method %q", method)
	}
}

// isAuthenticated reports whether the client may proceed to session/new or
// session/prompt: always true when no authToken is configured (back-compat
// default), otherwise true only after a successful authenticate call.
func (a *Agent) isAuthenticated() bool {
	if a.authToken == "" {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.authenticated
}

func (a *Agent) handleAuthenticate(params json.RawMessage) (any, *RPCError) {
	if a.authToken == "" {
		return map[string]any{}, nil
	}
	var p authenticateParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, errorf(codeInvalidParams, "invalid authenticate params: %v", err)
		}
	}
	if subtle.ConstantTimeCompare([]byte(p.Token), []byte(a.authToken)) != 1 {
		return nil, errorf(codeUnauthorized, "invalid token")
	}
	a.mu.Lock()
	a.authenticated = true
	a.mu.Unlock()
	return map[string]any{}, nil
}

// HandleNotification implements Handler.
func (a *Agent) HandleNotification(_ context.Context, method string, params json.RawMessage) {
	switch method {
	case methodCancel:
		var p struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return
		}
		a.cancelSession(p.SessionID)
	}
}

func (a *Agent) handleInitialize(params json.RawMessage) (any, *RPCError) {
	var p initializeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, errorf(codeInvalidParams, "invalid initialize params: %v", err)
		}
	}
	var authMethods []authMethod
	if a.authToken != "" {
		authMethods = []authMethod{{
			ID:          authMethodSharedSecret,
			Name:        "Shared secret",
			Description: "Token set via AEGIS_ACP_TOKEN, or auto-generated and written to the agent's acp.token file",
		}}
	}
	return initializeResult{
		ProtocolVersion: protocolVersion,
		AgentCapabilities: agentCapabilities{
			LoadSession: false,
			PromptCapabilities: promptCapabilities{
				Image:           true,
				Audio:           false,
				EmbeddedContext: true,
			},
		},
		AuthMethods: authMethods,
	}, nil
}

func (a *Agent) handleNewSession(ctx context.Context, params json.RawMessage) (any, *RPCError) {
	var p newSessionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, errorf(codeInvalidParams, "invalid session/new params: %v", err)
	}
	meta, err := a.backend.CreateSession(ctx, api.CreateSessionRequest{Mode: a.mode, Workdir: p.Cwd})
	if err != nil {
		return nil, errorf(codeInternalError, "create session: %v", err)
	}
	a.mu.Lock()
	a.created[meta.ID] = struct{}{}
	a.mu.Unlock()
	return newSessionResult{SessionID: meta.ID}, nil
}

// checkBorrowedSessionMode enforces this agent's configured mode as a ceiling
// on a session it did not create — the ACP half of P80.1 / FIND-21.
//
// session/prompt takes the client's sessionId verbatim, so an authenticated
// editor client can post a turn into any session on the daemon, including an
// interactive auto-mode one a human started in the TUI, inheriting its mode,
// persona and workdir. `aegis acp --mode` then describes only the sessions
// this agent creates, which is not what an operator setting it reads it as.
// Unlike the MCP surface, ACP exposes no session listing, so this is reachable
// only by a client that already knows an id — which is why it is the same
// finding at a lower reach, not a separate one.
//
// It refuses rather than downgrading: the session belongs to someone else. A
// session at or below the ceiling is untouched, so resuming a session across
// an agent restart keeps working, and an id the daemon does not list is
// allowed through as unverifiable (PostMessageReq rejects a nonexistent one).
func (a *Agent) checkBorrowedSessionMode(ctx context.Context, id string) *RPCError {
	a.mu.Lock()
	_, own := a.created[id]
	a.mu.Unlock()
	if own {
		return nil
	}
	metas, err := a.backend.ListSessions(ctx)
	if err != nil {
		a.logger.Warn("acp: could not check a borrowed session's mode against the configured mode", "session", id, "err", err)
		return nil
	}
	for _, m := range metas {
		if m.ID != id {
			continue
		}
		if permModeRank(m.Mode) <= permModeRank(a.mode) {
			return nil
		}
		a.logger.Warn("acp: refusing a prompt into a session this agent did not create whose mode exceeds the configured mode",
			"session", id, "session_mode", m.Mode, "mode", a.mode)
		return errorf(codeInvalidParams,
			"session %s is in %s mode, which exceeds this agent's configured mode (%s), and was not created by this agent; call session/new instead",
			id, m.Mode, a.mode)
	}
	return nil
}

// modeRankUnknown sorts above every known mode, so an unrecognised mode string
// counts as an escalation rather than passing a comparison against zero.
const modeRankUnknown = 99

// permModeRank orders the permission modes from least to most permissive. A
// small deliberate duplicate of internal/server's ranking (and of
// internal/mcpserver's, for the same reason): this package talks to the daemon
// over api.CreateSessionRequest and does not import internal/server.
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

func (a *Agent) handlePrompt(ctx context.Context, params json.RawMessage) (any, *RPCError) {
	var p promptParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, errorf(codeInvalidParams, "invalid session/prompt params: %v", err)
	}
	if p.SessionID == "" {
		return nil, errorf(codeInvalidParams, "sessionId is required")
	}

	req := buildMessageRequest(p.Prompt)
	if strings.TrimSpace(req.Text) == "" && len(req.Images) == 0 {
		return nil, errorf(codeInvalidParams, "prompt is empty")
	}

	if rerr := a.checkBorrowedSessionMode(ctx, p.SessionID); rerr != nil {
		return nil, rerr
	}

	// A cancellable context tied to the session so session/cancel can stop it.
	runCtx, cancel := context.WithCancel(ctx)
	a.setCancel(p.SessionID, cancel)
	defer func() {
		cancel()
		a.clearCancel(p.SessionID)
	}()

	events, err := a.backend.PostMessageReq(runCtx, p.SessionID, req)
	if err != nil {
		return nil, errorf(codeInternalError, "post message: %v", err)
	}

	stop := a.streamEvents(runCtx, p.SessionID, events)
	return promptResult{StopReason: stop}, nil
}

// streamEvents consumes engine events and emits ACP session/update
// notifications, returning the ACP stop reason for the turn.
func (a *Agent) streamEvents(ctx context.Context, sessionID string, events <-chan api.Event) string {
	tracker := newToolTracker()
	stop := stopEndTurn

	for ev := range events {
		switch ev.Kind {
		case api.KindText:
			a.notifyUpdate(sessionID, messageChunk{SessionUpdate: updAgentMessageChunk, Content: textBlock(ev.Text)})
		case api.KindThinking:
			a.notifyUpdate(sessionID, messageChunk{SessionUpdate: updAgentThoughtChunk, Content: textBlock(ev.Text)})
		case api.KindToolCallStart:
			// P33.21: mirrors the TUI's provisional card (P33.3) — announce
			// the call the moment the model names it, before its arguments
			// have finished streaming, so an editor client can show
			// "preparing <tool>…" instead of dead air. The matching
			// KindToolCall below reuses this id via tracker.current and
			// upgrades in place; a daemon that never emits this event
			// leaves tracker.current empty and KindToolCall falls back to
			// its old open-a-new-call behavior.
			id := tracker.start(ev.Tool)
			a.notifyUpdate(sessionID, toolCall{
				SessionUpdate: updToolCall,
				ToolCallID:    id,
				Title:         ev.Tool,
				Kind:          toolKind(ev.Tool),
				Status:        statusPending,
			})
		case api.KindToolCall:
			id := tracker.current(ev.Tool)
			if id == "" {
				id = tracker.start(ev.Tool)
				a.notifyUpdate(sessionID, toolCall{
					SessionUpdate: updToolCall,
					ToolCallID:    id,
					Title:         ev.Tool,
					Kind:          toolKind(ev.Tool),
					Status:        statusInProgress,
					RawInput:      ev.ToolInput,
				})
				break
			}
			a.notifyUpdate(sessionID, toolCall{
				SessionUpdate: updToolCallUpdate,
				ToolCallID:    id,
				Status:        statusInProgress,
				RawInput:      ev.ToolInput,
			})
		case api.KindToolResult:
			id := tracker.finish(ev.Tool)
			status := statusCompleted
			if ev.ToolIsError {
				status = statusFailed
			}
			upd := toolCall{SessionUpdate: updToolCallUpdate, ToolCallID: id, Status: status}
			if ev.ToolResult != "" {
				upd.Content = []toolCallContent{{Type: "content", Content: textBlock(ev.ToolResult)}}
			}
			a.notifyUpdate(sessionID, upd)
		case api.KindApprovalRequest:
			approved := a.requestPermission(ctx, sessionID, ev, tracker.current(ev.Tool))
			if err := a.backend.SendApproval(ctx, sessionID, ev.ApprovalID, approved, false); err != nil {
				a.logger.Warn("acp: send approval failed", "err", err)
			}
		case api.KindGuard:
			// EXEC-2: the answer just streamed is being withdrawn and replaced
			// by a corrective retry (P25.3). ACP chunks are already in the
			// editor and cannot be recalled, so say so in line rather than
			// letting the retry read as a second answer appended to the first.
			if ev.GuardRetrying {
				note := "\n\n[output guard: the answer above was withdrawn"
				if ev.Text != "" {
					note += " (" + ev.Text + ")"
				}
				note += " and is being retried]\n\n"
				a.notifyUpdate(sessionID, messageChunk{SessionUpdate: updAgentMessageChunk, Content: textBlock(note)})
			}
		case api.KindError:
			// Surface the error text to the user; the turn still ends normally.
			if ev.Error != "" {
				a.notifyUpdate(sessionID, messageChunk{SessionUpdate: updAgentMessageChunk, Content: textBlock("\n[error] " + ev.Error)})
			}
		}
	}

	if ctx.Err() != nil {
		stop = stopCancelled
	}
	return stop
}

// requestPermission asks the editor to approve a tool call and returns whether
// it was allowed. A cancelled or errored exchange denies the call.
func (a *Agent) requestPermission(ctx context.Context, sessionID string, ev api.Event, toolCallID string) bool {
	params := requestPermissionParams{
		SessionID: sessionID,
		ToolCall: toolCall{
			SessionUpdate: updToolCallUpdate,
			ToolCallID:    toolCallID,
			Title:         approvalTitle(ev),
			Kind:          toolKind(ev.Tool),
			Status:        statusInProgress,
		},
		Options: []permissionOption{
			{OptionID: "allow", Name: "Allow", Kind: "allow_once"},
			{OptionID: "reject", Name: "Reject", Kind: "reject_once"},
		},
	}
	raw, err := a.conn.Call(ctx, methodRequestPermission, params)
	if err != nil {
		a.logger.Warn("acp: request_permission failed", "err", err)
		return false
	}
	var res requestPermissionResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return false
	}
	return res.Outcome.Outcome == "selected" && res.Outcome.OptionID == "allow"
}

func approvalTitle(ev api.Event) string {
	if ev.ApprovalReason != "" {
		return ev.ApprovalReason
	}
	return "Run " + ev.Tool
}

func (a *Agent) notifyUpdate(sessionID string, update any) {
	if err := a.conn.Notify(methodSessionUpdate, sessionUpdateParams{SessionID: sessionID, Update: update}); err != nil {
		a.logger.Warn("acp: notify failed", "err", err)
	}
}

// --- session cancel bookkeeping ---

func (a *Agent) setCancel(sessionID string, cancel context.CancelFunc) {
	a.mu.Lock()
	a.cancels[sessionID] = cancel
	a.mu.Unlock()
}

func (a *Agent) clearCancel(sessionID string) {
	a.mu.Lock()
	delete(a.cancels, sessionID)
	a.mu.Unlock()
}

func (a *Agent) cancelSession(sessionID string) {
	a.mu.Lock()
	cancel := a.cancels[sessionID]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// buildMessageRequest converts ACP prompt content blocks into a daemon message
// request: text and embedded-resource text are concatenated; images become
// attachments; resource links are referenced inline.
func buildMessageRequest(blocks []contentBlock) api.PostMessageRequest {
	var text strings.Builder
	var images []api.ImageInput
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "image":
			if b.Data != "" {
				images = append(images, api.ImageInput{MediaType: b.MimeType, Data: b.Data})
			}
		case "resource_link":
			ref := b.URI
			if b.Name != "" {
				ref = b.Name + " (" + b.URI + ")"
			}
			fmt.Fprintf(&text, "\n[attached resource: %s]\n", ref)
		case "resource":
			if b.Resource != nil && b.Resource.Text != "" {
				if b.Resource.URI != "" {
					fmt.Fprintf(&text, "\n[%s]\n", b.Resource.URI)
				}
				text.WriteString(b.Resource.Text)
			}
		}
	}
	return api.PostMessageRequest{Text: text.String(), Images: images}
}
