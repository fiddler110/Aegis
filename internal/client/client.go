// Package client is a thin HTTP/SSE client for the harness daemon.
package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/session"
)

// Client talks to a running harness daemon.
type Client struct {
	base      string
	http      *http.Client // no timeout — used for SSE streaming
	httpShort *http.Client // 30s timeout — used for non-streaming RPCs

	// authToken holds the daemon's bearer credential as a mutable byte slice
	// rather than an immutable Go string, so Zero (below) can overwrite the
	// backing array in place once the Client is done being used
	// (FIND-33/P24.21, 2026-07-10 STRIDE-A, Low severity / CVSS 2.8). This is
	// best-effort defense in depth, not a hard guarantee — the finding
	// itself notes that host/OS access sufficient to scrape this process's
	// memory is already a significant compromise. Several things stay
	// outside this type's control even when Zero is called promptly:
	//   - the garbage collector may copy/move slice backing arrays over the
	//     token's lifetime, leaving stale copies Zero never touches;
	//   - if the token ever passes through a Go string (WithToken's
	//     parameter is one, and the runtime may intern short strings), that
	//     copy is immutable and unreachable from here;
	//   - setAuth formats "Bearer <token>" as a new string, and
	//     http.Request.Header.Set copies that string into the request's own
	//     header map — both copies live outside this struct and are never
	//     scrubbed.
	// Zero narrows the window during which this struct's own backing array
	// holds the token; it does not eliminate the token's presence in
	// process memory.
	authToken []byte
}

// New returns a client for the daemon at addr (host:port).
func New(addr string) *Client {
	base := addr
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{
		base:      strings.TrimRight(base, "/"),
		http:      &http.Client{Timeout: 0},
		httpShort: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithToken returns a copy of c that authenticates with the given token.
// The returned copy shares the underlying HTTP transports. token arrives as
// a string (the public API predates the []byte-backed storage added for
// FIND-33/P24.21) so a copy is unavoidably made here; WithTokenFile avoids
// that extra copy by writing the file's bytes into authToken directly.
func (c *Client) WithToken(token string) *Client {
	c2 := *c
	c2.authToken = []byte(token)
	return &c2
}

// WithTokenFile reads the auth token from path and returns an authenticated
// copy of c. If the file cannot be read, c is returned unchanged. Unlike
// WithToken, the file's bytes are trimmed and stored directly without ever
// materializing an intermediate Go string, keeping one fewer immutable copy
// of the token in memory (FIND-33/P24.21).
func (c *Client) WithTokenFile(path string) *Client {
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	c2 := *c
	c2.authToken = bytes.TrimSpace(data)
	return &c2
}

// WithTLS returns a copy of c that trusts the daemon's certificate at
// certPath instead of the system root CA pool, and switches the base URL to
// https:// (FIND-32/P24.18). This is certificate pinning, not general TLS
// verification: certPath is expected to be the daemon's own self-signed
// daemon.crt (see config.Config.TLSCertPath), read directly off local disk,
// the same trust model as reading daemon.token for the bearer credential.
// InsecureSkipVerify is deliberately never used — an unrecognized
// certificate (wrong file, stale after regeneration, a MITM) fails closed
// with a TLS handshake error rather than silently connecting.
//
// If certPath cannot be read or does not contain a valid PEM certificate, c
// is returned unchanged (mirroring WithTokenFile's fail-open-to-previous-
// state behavior). Callers that want "enable TLS only if configured" should
// use NewFromConfig rather than calling this directly.
func (c *Client) WithTLS(certPath string) *Client {
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return c
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return c
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	c2 := *c
	c2.http = &http.Client{Timeout: 0, Transport: transport}
	c2.httpShort = &http.Client{Timeout: 30 * time.Second, Transport: transport}
	if after, ok := strings.CutPrefix(c2.base, "http://"); ok {
		c2.base = "https://" + after
	}
	return &c2
}

// NewFromConfig builds a Client wired up the way every CLI command needs it:
// base address, bearer token from cfg.AuthTokenPath, and — only when the
// daemon config enables it — TLS pinned to the daemon's certificate
// (FIND-32/P24.18). Centralizing this here means the scheme/token/TLS wiring
// changes in one place as transport options evolve, instead of at every
// `client.New(cfg.Server.Addr)...` call site in internal/cli.
func NewFromConfig(cfg *config.Config) *Client {
	c := New(cfg.Server.Addr)
	if cfg.Server.TLS.Enabled {
		c = c.WithTLS(cfg.TLSCertPath())
	}
	return c.WithTokenFile(cfg.AuthTokenPath())
}

func (c *Client) setAuth(req *http.Request) {
	if len(c.authToken) != 0 {
		// The string conversion and concatenation here are exactly the kind
		// of copy authToken's doc comment warns Zero cannot reach — see
		// that comment for the full explanation.
		req.Header.Set("Authorization", "Bearer "+string(c.authToken))
	}
}

// Zero overwrites authToken's backing bytes with zeroes in place and clears
// the field, so a Client that is done being used no longer holds the bearer
// token in its own memory (FIND-33/P24.21). Call it once a Client's useful
// life is over: at the end of a one-shot CLI command (typically via defer),
// or when replacing a cached client with a freshly-authenticated one. Safe
// to call multiple times, and a no-op on a Client with no token. See
// authToken's doc comment for what Zero can and cannot guarantee.
func (c *Client) Zero() {
	for i := range c.authToken {
		c.authToken[i] = 0
	}
	c.authToken = nil
}

// Health checks daemon reachability.
func (c *Client) Health(ctx context.Context) error {
	_, err := c.status(ctx)
	return err
}

// Status returns the daemon's health status, including whether it fell back
// to an unsandboxed execution backend (P7.4).
func (c *Client) Status(ctx context.Context) (*api.HealthStatus, error) {
	return c.status(ctx)
}

func (c *Client) status(ctx context.Context) (*api.HealthStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	resp, err := c.httpShort.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon unhealthy: %d", resp.StatusCode)
	}
	var out api.HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode health response: %w", err)
	}
	return &out, nil
}

// CreateSession creates a new session.
func (c *Client) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.SessionMeta, error) {
	var out api.SessionMeta
	if err := c.do(ctx, http.MethodPost, "/sessions", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSessions returns session metadata.
func (c *Client) ListSessions(ctx context.Context) ([]api.SessionMeta, error) {
	var out []api.SessionMeta
	if err := c.do(ctx, http.MethodGet, "/sessions", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListRuns returns message runs currently in flight across all sessions.
func (c *Client) ListRuns(ctx context.Context) ([]api.RunInfo, error) {
	var out []api.RunInfo
	if err := c.do(ctx, http.MethodGet, "/runs", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSession returns a full session including messages.
func (c *Client) GetSession(ctx context.Context, id string) (*session.Session, error) {
	var out session.Session
	if err := c.do(ctx, http.MethodGet, "/sessions/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSession removes a session.
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/"+id, nil, nil)
}

// ArchiveSession soft-deletes a session; it is hidden from normal listings.
func (c *Client) ArchiveSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+id+"/archive", nil, nil)
}

// UnarchiveSession restores an archived session to active status.
func (c *Client) UnarchiveSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+id+"/unarchive", nil, nil)
}

// ListArchivedSessions returns all sessions including archived ones.
func (c *Client) ListArchivedSessions(ctx context.Context) ([]api.SessionMeta, error) {
	var out []api.SessionMeta
	if err := c.do(ctx, http.MethodGet, "/sessions?archived=true", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PruneSessions deletes non-archived sessions older than days. Pass 0 to use
// the server's configured TTL.
func (c *Client) PruneSessions(ctx context.Context, days int) (*api.PruneResponse, error) {
	path := "/sessions/prune"
	if days > 0 {
		path = fmt.Sprintf("/sessions/prune?days=%d", days)
	}
	var out api.PruneResponse
	if err := c.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Teammates returns the sub-agents tracked by the daemon's swarm registry.
func (c *Client) Teammates(ctx context.Context) ([]api.Teammate, error) {
	var out []api.Teammate
	if err := c.do(ctx, http.MethodGet, "/teammates", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateSession patches a session's system prompt and/or mode.
func (c *Client) UpdateSession(ctx context.Context, id string, req api.UpdateSessionRequest) (*api.SessionMeta, error) {
	var out api.SessionMeta
	if err := c.do(ctx, http.MethodPatch, "/sessions/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Scan runs the security scanners directly against the daemon's workspace and
// returns the formatted report — no session or model turn involved.
func (c *Client) Scan(ctx context.Context, req api.ScanRequest) (*api.ScanResponse, error) {
	var out api.ScanResponse
	if err := c.do(ctx, http.MethodPost, "/security/scan", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetConfigSandbox returns the daemon's currently effective sandbox: config
// (P15.2). scope selects "project" or "global"; empty lets the daemon pick a
// default (see server.resolveScope).
func (c *Client) GetConfigSandbox(ctx context.Context, scope string) (*api.ConfigSandboxResponse, error) {
	var out api.ConfigSandboxResponse
	if err := c.do(ctx, http.MethodGet, "/config/sandbox"+scopeQuery(scope), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PatchConfigSandbox partially updates the sandbox: config block.
func (c *Client) PatchConfigSandbox(ctx context.Context, req api.ConfigSandboxPatchRequest) (*api.ConfigSandboxResponse, error) {
	var out api.ConfigSandboxResponse
	if err := c.do(ctx, http.MethodPatch, "/config/sandbox", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetConfigSecurity returns the daemon's currently effective security:
// config (P15.2). scope selects "project" or "global"; empty lets the
// daemon pick a default.
func (c *Client) GetConfigSecurity(ctx context.Context, scope string) (*api.ConfigSecurityResponse, error) {
	var out api.ConfigSecurityResponse
	if err := c.do(ctx, http.MethodGet, "/config/security"+scopeQuery(scope), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PatchConfigSecurity partially updates the security: config block.
func (c *Client) PatchConfigSecurity(ctx context.Context, req api.ConfigSecurityPatchRequest) (*api.ConfigSecurityResponse, error) {
	var out api.ConfigSecurityResponse
	if err := c.do(ctx, http.MethodPatch, "/config/security", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetConfigSkills returns which embedded built-in skills are currently
// active (P15.2). scope selects "project" or "global"; empty lets the
// daemon pick a default.
func (c *Client) GetConfigSkills(ctx context.Context, scope string) (*api.ConfigSkillsResponse, error) {
	var out api.ConfigSkillsResponse
	if err := c.do(ctx, http.MethodGet, "/config/skills"+scopeQuery(scope), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PatchConfigSkills replaces the skills.builtin_enabled list. names is the
// full desired set, not a delta.
func (c *Client) PatchConfigSkills(ctx context.Context, scope string, names []string) (*api.ConfigSkillsResponse, error) {
	var out api.ConfigSkillsResponse
	req := api.ConfigSkillsPatchRequest{Scope: scope, BuiltinEnabled: names}
	if err := c.do(ctx, http.MethodPatch, "/config/skills", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConfigHarden applies (or, with Confirm false, previews) the hardened
// profile config.ComputeHardenPlan computes — the HTTP equivalent of
// `aegis harden` (P15.2).
func (c *Client) ConfigHarden(ctx context.Context, req api.ConfigHardenRequest) (*api.ConfigHardenResponse, error) {
	var out api.ConfigHardenResponse
	if err := c.do(ctx, http.MethodPost, "/config/harden", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SecurityStatus reports every built-in scanner's configured/resolved
// status (P15.2) — the same live-availability probe the `/security-config`
// TUI dialog shows.
func (c *Client) SecurityStatus(ctx context.Context) (*api.SecurityStatusResponse, error) {
	var out api.SecurityStatusResponse
	if err := c.do(ctx, http.MethodGet, "/security/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SecurityBaseline returns the project's accepted-risk suppression
// allowlist (.aegis/security-baseline.yaml, P11.8).
func (c *Client) SecurityBaseline(ctx context.Context) (*api.SecurityBaselineResponse, error) {
	var out api.SecurityBaselineResponse
	if err := c.do(ctx, http.MethodGet, "/security/baseline", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SecurityInstall runs (or, with req.Confirm false, previews) a security
// scanner's guided host install (P15.2) — the same underlying command
// `aegis security install` and the `/security-config` TUI wizard run.
func (c *Client) SecurityInstall(ctx context.Context, req api.SecurityInstallRequest) (*api.SecurityInstallResponse, error) {
	var out api.SecurityInstallResponse
	if err := c.do(ctx, http.MethodPost, "/security/install", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func scopeQuery(scope string) string {
	if scope == "" {
		return ""
	}
	return "?scope=" + url.QueryEscape(scope)
}

// Debate runs a multi-agent debate (P12) over a claim directly against the
// daemon's configured model and returns the formatted transcript plus the
// arbiter's parsed verdict — no session involved, though (unlike Scan) it does
// spend model turns per role per round.
func (c *Client) Debate(ctx context.Context, req api.DebateRequest) (*api.DebateResponse, error) {
	var out api.DebateResponse
	if err := c.do(ctx, http.MethodPost, "/debate", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Knowledge indexes or queries the project knowledge base directly against
// the daemon's own store — no session or model turn involved.
func (c *Client) Knowledge(ctx context.Context, req api.KnowledgeRequest) (*api.KnowledgeResponse, error) {
	var out api.KnowledgeResponse
	if err := c.do(ctx, http.MethodPost, "/knowledge", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RepoMapIndex rebuilds the repository map directly against the daemon's own
// workspace, refreshing both the on-disk cache and the daemon's cached
// system-prompt block — no session or model turn involved.
func (c *Client) RepoMapIndex(ctx context.Context) (*api.RepoMapIndexResponse, error) {
	var out api.RepoMapIndexResponse
	if err := c.do(ctx, http.MethodPost, "/repomap/index", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StatusInfo fetches the P14.5 /status surface: daemon/provider identity,
// sandbox fallback state, and cross-session daily spend against the P9.5/
// P10.5 caps. Distinct from Status(), which hits the minimal, frequently
// polled /healthz used for readiness checks.
func (c *Client) StatusInfo(ctx context.Context) (*api.StatusInfo, error) {
	var out api.StatusInfo
	if err := c.do(ctx, http.MethodGet, "/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCommands returns custom slash commands from the daemon.
func (c *Client) ListCommands(ctx context.Context) ([]api.CommandInfo, error) {
	var out []api.CommandInfo
	if err := c.do(ctx, http.MethodGet, "/commands", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMemory returns the current memory and skills state.
func (c *Client) GetMemory(ctx context.Context) (*api.MemoryResponse, error) {
	var out api.MemoryResponse
	if err := c.do(ctx, http.MethodGet, "/memory", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AppendMemory adds a memory entry via the daemon.
func (c *Client) AppendMemory(ctx context.Context, req api.AppendMemoryRequest) error {
	return c.do(ctx, http.MethodPost, "/memory", req, nil)
}

// ActivateSkill enables a dormant embedded built-in skill for this session
// only, effective immediately (no daemon restart, no config write) — used by
// slash commands like /threat-model that invoke a specific skill on demand.
func (c *Client) ActivateSkill(ctx context.Context, sessionID, name string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/skills/activate", api.ActivateSkillRequest{Name: name}, nil)
}

// ListPersonas returns available persona names and descriptions.
func (c *Client) ListPersonas(ctx context.Context) ([]api.PersonaInfo, error) {
	var out []api.PersonaInfo
	if err := c.do(ctx, http.MethodGet, "/personas", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListCheckpoints returns the rewind points captured for a session.
func (c *Client) ListCheckpoints(ctx context.Context, sessionID string) ([]api.CheckpointInfo, error) {
	var out []api.CheckpointInfo
	if err := c.do(ctx, http.MethodGet, "/sessions/"+sessionID+"/checkpoints", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Rewind restores a session to a checkpoint. scope is "both", "code", or
// "conversation" (empty means "both").
func (c *Client) Rewind(ctx context.Context, sessionID, checkpointID, scope string) (*api.RewindResponse, error) {
	var out api.RewindResponse
	req := api.RewindRequest{CheckpointID: checkpointID, Scope: scope}
	if err := c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/rewind", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Fork creates a new session as a copy of sessionID's conversation, optionally
// truncated to a checkpoint (P22.3) — an empty checkpointID forks at the
// current end of the conversation. Unlike Rewind, the source session is left
// untouched; the returned response describes the new session.
func (c *Client) Fork(ctx context.Context, sessionID, checkpointID string) (*api.ForkResponse, error) {
	var out api.ForkResponse
	req := api.ForkRequest{CheckpointID: checkpointID}
	if err := c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/fork", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Rollback is like Rewind but also runs `git reset --hard <sha>` to restore
// git HEAD to the pre-turn state (P3.4). Scope defaults to "both".
func (c *Client) Rollback(ctx context.Context, sessionID, checkpointID, scope string) (*api.RewindResponse, error) {
	var out api.RewindResponse
	req := api.RewindRequest{CheckpointID: checkpointID, Scope: scope, GitRollback: true}
	if err := c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/rewind", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Compact forces context compaction on a session's message history now,
// rather than waiting for the automatic budget-driven trigger (P19.2).
func (c *Client) Compact(ctx context.Context, sessionID string) (*api.CompactResponse, error) {
	var out api.CompactResponse
	if err := c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/compact", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetBackground marks or unmarks a session as a background (detached) session.
// When background is true, subsequent message runs use a server-level context
// so the turn continues even if the TUI disconnects (P3.2).
func (c *Client) SetBackground(ctx context.Context, sessionID string, background bool) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/background",
		api.SetBackgroundRequest{Background: background}, nil)
}

// GetBGEvents returns buffered engine events for a background session.
// since is the last event id received; pass 0 to start from the beginning.
func (c *Client) GetBGEvents(ctx context.Context, sessionID string, since int64) ([]api.BGEventItem, error) {
	path := fmt.Sprintf("/sessions/%s/events?since=%d", sessionID, since)
	var out []api.BGEventItem
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SendApproval answers a pending interactive approval request for the given
// session. approved=true lets the tool run; false denies it. approvalID must
// match the approval_id from the KindApprovalRequest event. allowAlways=true
// persists an auto-approve entry for this (session, tool) pair.
func (c *Client) SendApproval(ctx context.Context, sessionID, approvalID string, approved, allowAlways bool) error {
	return c.SendApprovalReq(ctx, sessionID,
		api.ApproveRequest{Approved: approved, AllowAlways: allowAlways, ID: approvalID})
}

// SendApprovalReq is SendApproval with the full request body, allowing a
// pattern-scoped allow-always rule to be attached (TQ6).
func (c *Client) SendApprovalReq(ctx context.Context, sessionID string, req api.ApproveRequest) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/approve", req, nil)
}

// Steer injects a mid-run instruction into an active session run. The text is
// delivered to the engine between tool rounds; returns an error if no run is
// currently active for the session.
func (c *Client) Steer(ctx context.Context, sessionID, text string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/steer", api.SteerRequest{Text: text}, nil)
}

// PostMessage streams engine events for a user turn. Events are delivered on
// the returned channel, which is closed when the run finishes or ctx is done.
func (c *Client) PostMessage(ctx context.Context, id, text string) (<-chan api.Event, error) {
	return c.PostMessageReq(ctx, id, api.PostMessageRequest{Text: text})
}

// PostMessageReq is like PostMessage but takes the full request, allowing image
// attachments and other fields to be set.
func (c *Client) PostMessageReq(ctx context.Context, id string, reqBody api.PostMessageRequest) (<-chan api.Event, error) {
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/sessions/"+id+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}

	// Buffered so the SSE-parsing goroutine can run ahead of the consumer's
	// render loop (P21.1): a fast stream fills this buffer, and the TUI drains
	// many queued events in one Update to do a single markdown re-render,
	// instead of one full re-render per token. FIFO order across the single
	// producer/consumer is preserved. Sized to absorb a burst without the
	// producer blocking on the common case; a sustained overrun just makes the
	// producer block as before (backpressure), never drops events.
	out := make(chan api.Event, 256)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			rest, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			var ev api.Event
			if err := json.Unmarshal([]byte(strings.TrimSpace(rest)), &ev); err != nil {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var bodyReader io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)
	resp, err := c.httpShort.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func decodeError(resp *http.Response) error {
	var e api.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&e); err == nil && e.Error != "" {
		return fmt.Errorf("daemon error (%d): %s", resp.StatusCode, e.Error)
	}
	return fmt.Errorf("daemon error: %d", resp.StatusCode)
}
