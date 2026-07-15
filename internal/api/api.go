// Package api defines the wire types shared by the harness daemon and its
// clients (the TUI and CLI).
package api

import (
	"encoding/json"
	"time"
)

// CreateSessionRequest creates a new session.
type CreateSessionRequest struct {
	Title   string `json:"title"`
	System  string `json:"system"`
	Mode    string `json:"mode"`
	Persona string `json:"persona"` // named persona; sets the system prompt when System is empty
	// Workdir is the client's own working directory (P25.1): an absolute or
	// client-relative path the daemon resolves, validates (must exist, be a
	// directory, and — for a remote-accessible daemon — fall within an
	// allowed scope), and confines this session's tools/shell to. Empty
	// keeps the daemon's own default workspace, matching pre-P25.1 behavior.
	Workdir string `json:"workdir,omitempty"`
}

// SessionMeta describes a session without its messages.
type SessionMeta struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Mode         string     `json:"mode"`
	Workdir      string     `json:"workdir,omitempty"`    // P25.1: session's working directory; "" = daemon's default workspace
	Model        string     `json:"model,omitempty"`      // P14.7: per-session override; "" = persona/global default
	Background   bool       `json:"background,omitempty"` // P3.2
	Archived     bool       `json:"archived,omitempty"`
	InputTokens  int        `json:"input_tokens,omitempty"`
	OutputTokens int        `json:"output_tokens,omitempty"`
	CostUSD      float64    `json:"cost_usd,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
}

// HealthStatus is the /healthz response. SandboxFallback signals that the
// configured sandbox backend failed to initialize and the daemon fell back to
// unsandboxed local execution (P7.4); clients should warn the user rather
// than silently trusting a sandbox that isn't actually there.
type HealthStatus struct {
	Status                string `json:"status"`
	Model                 string `json:"model"`
	SandboxFallback       bool   `json:"sandbox_fallback,omitempty"`
	SandboxFallbackReason string `json:"sandbox_fallback_reason,omitempty"`
}

// StatusInfo is the /status response (P14.5) — richer daemon health info than
// /healthz, which stays deliberately minimal since waitForDaemon polls it
// repeatedly during startup. DailyCostUSD/DailyTokens are the cross-session
// totals the P9.5/P10.5 daily caps already track in the session store; the
// Cap fields are 0 when the corresponding cap is unconfigured (unlimited).
type StatusInfo struct {
	Provider              string  `json:"provider"`
	Model                 string  `json:"model"`
	SandboxFallback       bool    `json:"sandbox_fallback,omitempty"`
	SandboxFallbackReason string  `json:"sandbox_fallback_reason,omitempty"`
	DailyCostUSD          float64 `json:"daily_cost_usd"`
	DailyCapUSD           float64 `json:"daily_cap_usd,omitempty"`
	DailyTokens           int     `json:"daily_tokens"`
	DailyTokenCap         int     `json:"daily_token_cap,omitempty"`

	// AgentConcurrency is the adaptive limiter's current cap (P17) on how
	// many sub-agents a 'parallel' workflow batch runs simultaneously.
	// AgentConcurrencyMax is the fixed ceiling it adapts toward.
	AgentConcurrency    int `json:"agent_concurrency"`
	AgentConcurrencyMax int `json:"agent_concurrency_max"`

	// ContextWindow is the effective model context window (tokens) the daemon
	// uses for compaction thresholds — from config, or auto-detected from the
	// local Ollama server when unset. 0 when unknown. ContextWindowSource says
	// where the value came from: "config", "ollama:loaded", "ollama:modelfile",
	// or "ollama:default".
	ContextWindow       int    `json:"context_window,omitempty"`
	ContextWindowSource string `json:"context_window_source,omitempty"`

	// Workspace is the daemon's own default working directory root — what
	// any session created without an explicit Workdir (CreateSessionRequest,
	// P25.1) actually operates on. `aegis doctor` (P26.1) compares this
	// against the CLI's own cwd to catch the P25.1 failure mode: a client
	// running from a different directory than the daemon silently getting
	// the daemon's workspace instead of its own.
	Workspace string `json:"workspace,omitempty"`

	// WorkdirAllowlist mirrors server.session_workdir_allowlist (P25.1) so a
	// client choosing a session's working directory — the web UI's new-chat
	// picker (P15.13) — can suggest directories known to be accepted instead
	// of guessing. Empty on the default loopback-only bind, where
	// workdirAllowed accepts any existing directory and this list is
	// informational only, not enforced.
	WorkdirAllowlist []string `json:"workdir_allowlist,omitempty"`

	// ProviderReachable/ProviderLatencyMS (P28.7) surface a lightweight
	// connection-health probe so the TUI status area and web UI header can
	// show "is the model actually reachable" at a glance instead of a user
	// spending a conversational turn on it (the recurring "test that the
	// model is connected" session pattern that motivated this field). For an
	// Ollama-style provider this is a live, short-timeout probe timed for
	// latency; for a cloud provider a live call on every /status poll would
	// be wasteful/costly, so reachability there mirrors `aegis doctor`'s
	// check — an API key present in the resolved config — and
	// ProviderLatencyMS stays 0 (unmeasured). See
	// Server.probeProviderReachability for the exact rule.
	ProviderReachable bool  `json:"provider_reachable"`
	ProviderLatencyMS int64 `json:"provider_latency_ms,omitempty"`
}

// PruneResponse reports how many sessions were deleted by a prune operation.
type PruneResponse struct {
	Deleted int `json:"deleted"`
}

// PostMessageRequest sends a user turn into a session.
type PostMessageRequest struct {
	Text string `json:"text"`
	// Images attaches images to the turn (vision-capable models only).
	Images []ImageInput `json:"images,omitempty"`
	// GuardEnabled overrides the configured output_guard.enabled default for this
	// turn when non-nil (per-session /guard toggle).
	GuardEnabled *bool `json:"guard_enabled,omitempty"`
	// Resumable opts this run into surviving an SSE connection drop (P28.5):
	// the run keeps executing server-side on a daemon-rooted context instead
	// of being cancelled when the HTTP request context is, and every event is
	// additionally buffered (like a Background session's) so a client can
	// reattach via GET /sessions/{id}/events?since=N. Since a dropped
	// connection can no longer be used to stop the run, a client that sets
	// this must explicitly stop it via POST /sessions/{id}/stop instead of
	// just aborting the request. The TUI/CLI leave this false and keep
	// today's disconnect-cancels-the-run behavior; the web UI sets it.
	Resumable bool `json:"resumable,omitempty"`
}

// ImageInput attaches an image to a user turn. Provide either a Path (the daemon
// reads and base64-encodes the file, detecting its media type) or inline base64
// Data with an explicit MediaType. Path is convenient for the local TUI/CLI;
// Data is for remote clients.
type ImageInput struct {
	Path      string `json:"path,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// EventKind mirrors engine.EventKind on the wire.
type EventKind string

const (
	KindText     EventKind = "text"
	KindThinking EventKind = "thinking"
	// KindToolCallStart announces a tool call the model has begun streaming —
	// emitted as soon as the provider names the tool, while its arguments are
	// still being generated, which on a local model is frequently the longest
	// phase of a turn (P33.3). Tool is set; ToolInput is not, and ToolID only
	// when the provider named the call and assigned its ID together. The
	// KindToolCall for the same call still follows unchanged, so a client that
	// doesn't know this kind behaves as it always has.
	KindToolCallStart   EventKind = "tool_call_start"
	KindToolCall        EventKind = "tool_call"
	KindToolResult      EventKind = "tool_result"
	KindTurnDone        EventKind = "turn_done"
	KindDone            EventKind = "done"
	KindError           EventKind = "error"
	KindApprovalRequest EventKind = "approval_request" // engine awaiting user approval
	KindSteer           EventKind = "steer"            // mid-run steering instruction injected
	// KindSteerUnconsumed carries back a steer the run ended without ever
	// injecting — the engine only drains the steer channel between tool
	// rounds, so one sent while the model is writing its final answer (or
	// during a text-only run) has nowhere to land (P33.2). Text is the
	// original steer; a client that doesn't know this kind behaves as it
	// always has, and a run with nothing left over never emits it.
	KindSteerUnconsumed EventKind = "steer_unconsumed"
	KindGuard           EventKind = "guard"      // output validation warning
	KindCostAlert       EventKind = "cost_alert" // spend crossed the configured alert threshold (P9.5)
	KindNotice          EventKind = "notice"     // engine advisory (context fill, compaction, step limit)
)

// Event is one server-sent event during a message run.
type Event struct {
	Kind      EventKind       `json:"kind"`
	Text      string          `json:"text,omitempty"`
	Tool      string          `json:"tool,omitempty"` // KindToolCallStart / KindToolCall / KindToolResult
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	// ToolID is the provider tool_use ID, carried on both KindToolCall and
	// its matching KindToolResult so a client can correlate the two exactly
	// — e.g. for concurrent tool calls, which don't necessarily produce
	// results in call order (P21.2; see engine.Event.ToolID).
	ToolID       string  `json:"tool_id,omitempty"`
	ToolResult   string  `json:"tool_result,omitempty"`
	ToolIsError  bool    `json:"tool_is_error,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Error        string  `json:"error,omitempty"`
	// Cache token usage (Anthropic prompt caching), surfaced for observability.
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
	// TokensEstimated is true when token counts were inferred from character
	// length because the provider did not report usage (e.g. local/Ollama models).
	TokensEstimated bool `json:"tokens_estimated,omitempty"`
	// KindApprovalRequest fields
	ApprovalReason string `json:"approval_reason,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"` // run id to echo back when answering
	// GuardRetrying marks a KindGuard failure whose answer is about to be
	// replaced by a corrective retry (P25.3): clients should withdraw the
	// answer text they just rendered — the retry replaces it, not follows it.
	GuardRetrying bool `json:"guard_retrying,omitempty"`
}

// ApproveRequest is posted to /sessions/{id}/approve to answer a pending
// approval request. Approved true lets the tool run; false denies it. ID must
// match the approval_id from the KindApprovalRequest event.
//
// AllowAlways with a Pattern creates a persistent text permission rule
// "allow <tool>(<pattern>)" scoped to that command/path pattern, saved to the
// project config so it survives daemon restarts (TQ6). AllowAlways without a
// Pattern falls back to the coarser session-lifetime per-tool cache.
type ApproveRequest struct {
	Approved    bool   `json:"approved"`
	AllowAlways bool   `json:"allow_always,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	ID          string `json:"id,omitempty"`
}

// Teammate describes a sub-agent tracked by the swarm registry.
type Teammate struct {
	AgentID   string    `json:"agent_id"`
	Name      string    `json:"name"`
	Team      string    `json:"team"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
}

// UpdateSessionRequest patches a session's system prompt, mode, persona, or
// model. Setting Persona switches the session's full behavioral profile:
// system prompt, persisted persona name (which carries model, permission
// rules, and output-guard overrides on subsequent turns), and — unless Mode
// is also set — the persona's permission mode. Model (P14.7) sets a
// per-session override that takes precedence over the persona's own Model on
// every subsequent turn; an empty string clears it back to the persona/global
// default. Neither Model nor the persona-level override is validated against
// the configured provider's actual model list — an unrecognized id surfaces
// as a provider error on the next turn, not at switch time.
type UpdateSessionRequest struct {
	System  *string `json:"system,omitempty"`
	Mode    *string `json:"mode,omitempty"`
	Persona *string `json:"persona,omitempty"`
	Model   *string `json:"model,omitempty"`
}

// MemoryResponse describes the current memory and skills state.
type MemoryResponse struct {
	ProjectMemory string   `json:"project_memory"`
	UserMemory    string   `json:"user_memory"`
	Skills        []string `json:"skills"`
}

// AppendMemoryRequest adds a memory entry.
type AppendMemoryRequest struct {
	Entry string `json:"entry"`
	Scope string `json:"scope"` // "project" (default) or "user"
}

// ActivateSkillRequest turns on a dormant embedded built-in skill for the
// remainder of one session — e.g. a slash command like /threat-model that
// invokes a specific skill on demand — without persisting it to config or
// affecting any other session.
type ActivateSkillRequest struct {
	Name string `json:"name"`
}

// PersonaInfo describes an available persona.
type PersonaInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CommandInfo describes a custom slash command.
type CommandInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Args        []string `json:"args"`
}

// CheckpointInfo describes a rewind point captured at the start of a turn.
type CheckpointInfo struct {
	ID        string    `json:"id"`
	Seq       int       `json:"seq"`               // conversation message count at capture
	Label     string    `json:"label"`             // the user prompt that began the turn
	GitSHA    string    `json:"git_sha,omitempty"` // HEAD commit at checkpoint time (P3.4)
	FileCount int       `json:"file_count"`        // number of files snapshotted in the turn
	CreatedAt time.Time `json:"created_at"`
}

// RewindRequest restores a session to a checkpoint. Scope selects what to
// restore: "code" (files only), "conversation" (messages only), or "both"
// (default). When GitRollback is true and the checkpoint has a GitSHA, the
// server runs `git reset --hard <sha>` before restoring files (P3.4).
type RewindRequest struct {
	CheckpointID string `json:"checkpoint_id"`
	Scope        string `json:"scope,omitempty"`
	GitRollback  bool   `json:"git_rollback,omitempty"` // P3.4: also reset git HEAD
}

// RewindResponse reports the result of a rewind.
type RewindResponse struct {
	Scope         string `json:"scope"`
	FilesRestored int    `json:"files_restored"`
	MessagesKept  int    `json:"messages_kept"`
}

// ForkRequest creates a new session that starts as a copy of an existing
// session's conversation, optionally truncated to a checkpoint (P22.3). An
// empty CheckpointID forks at the current end of the conversation — a clean
// branch point to try something risky without touching the original session.
// A non-empty CheckpointID truncates the new session's messages to that
// checkpoint's Seq, the same cut point /rewind's "conversation" scope uses —
// but unlike rewind, the source session's own messages are never touched.
type ForkRequest struct {
	CheckpointID string `json:"checkpoint_id,omitempty"`
}

// ForkResponse reports the newly created forked session.
type ForkResponse struct {
	SessionID    string `json:"session_id"`
	Title        string `json:"title"`
	MessagesKept int    `json:"messages_kept"`
}

// CompactResponse reports the result of a manually triggered compaction.
type CompactResponse struct {
	Compacted      bool `json:"compacted"`
	MessagesBefore int  `json:"messages_before"`
	MessagesAfter  int  `json:"messages_after"`
}

// RunInfo describes an in-flight message run, surfaced so concurrent parallel
// sessions are observable.
type RunInfo struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	StartedAt time.Time `json:"started_at"`
	Tools     int       `json:"tools"`     // tool calls so far this run
	LastKind  string    `json:"last_kind"` // most recent event kind
}

// CronJobInfo describes a persisted cron job, surfaced over the API so an
// operator can review what fires unattended without going through the
// model-facing cron_list tool — in particular which jobs carry auto_approve,
// since those bypass interactive approval entirely at fire time
// (P27.15/FIND-08 review-view requirement).
type CronJobInfo struct {
	ID          string    `json:"id"`
	Schedule    string    `json:"schedule"`
	Command     string    `json:"command"`
	Title       string    `json:"title"`
	Enabled     bool      `json:"enabled"`
	AutoApprove bool      `json:"auto_approve"`
	LastRun     time.Time `json:"last_run"`
	Created     time.Time `json:"created"`
	Workdir     string    `json:"workdir,omitempty"`
}

// SteerRequest injects a mid-run instruction into an active session run.
type SteerRequest struct {
	Text string `json:"text"`
}

// SetBackgroundRequest marks a session as a background (detached) session (P3.2).
type SetBackgroundRequest struct {
	Background bool `json:"background"`
}

// BGEventItem is one buffered engine event from a background session.
type BGEventItem struct {
	ID   int64  `json:"id"`
	Data string `json:"data"`
}

// ErrorResponse is the body returned for non-2xx responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ScanRequest runs the security scanners directly (POST /security/scan),
// independent of any session/model turn — the same underlying scan the
// security_scan tool runs, exposed for `/scan` in the TUI and other direct
// callers that want a deterministic report without spending a model turn.
type ScanRequest struct {
	// Path is a workspace-relative subdirectory to scan (optional, defaults
	// to the whole workspace). Mutually exclusive with Image.
	Path string `json:"path,omitempty"`
	// Image is a container image reference to scan instead of the workspace
	// (e.g. "alpine:3.20"). Mutually exclusive with Path/SBOM.
	Image string `json:"image,omitempty"`
	// SBOM generates a CycloneDX SBOM via syft instead of scanning for
	// findings, persisting it under Path. Mutually exclusive with Image.
	SBOM bool `json:"sbom,omitempty"`
	// Targets runs network/host recon (nmap + nuclei, the recon_scan tool's
	// underlying scan) against a bare host/IP/CIDR list instead of scanning
	// the workspace. Mutually exclusive with Path/Image/SBOM. Every target is
	// checked against the shared security.dast.allowed_targets gate before
	// either scanner runs (P13.5).
	Targets []string `json:"targets,omitempty"`
	// Scanners restricts a Path scan to specific scanner names and/or
	// category aliases (e.g. "trufflehog", "secrets") instead of every
	// enabled scanner — resolved via security.ResolveSelector and
	// force-enabled for this run regardless of config. Only meaningful
	// alongside Path (or neither Path/Image/SBOM/Targets, i.e. the default
	// whole-workspace scan); ignored for Image/SBOM/Targets requests, which
	// already run their own fixed scanner set. Empty means "every enabled
	// scanner," and also triggers language auto-detection (P13.2 follow-up)
	// to auto-enable a matching opt-in language-specific SAST engine.
	Scanners []string `json:"scanners,omitempty"`
}

// ScanFinding mirrors security.Finding on the wire — one normalized issue
// from a scan. Like SecurityBaselineEntry, the api package mirrors the
// internal/security type rather than importing it, so the wire contract
// can't drift silently under an internal refactor. Severity is one of
// "CRITICAL"/"HIGH"/"MEDIUM"/"LOW"/"INFO" (security.Severity's values);
// Reachability, Verification, CWE, ASVS, and SeenBy are only set when the
// underlying scanner actually established them (see security.Finding's
// field comments — never guessed).
type ScanFinding struct {
	Tool         string   `json:"tool"`
	RuleID       string   `json:"rule_id"`
	Severity     string   `json:"severity"`
	Title        string   `json:"title"`
	Location     string   `json:"location"` // file:line or package/target
	Description  string   `json:"description,omitempty"`
	Remediation  string   `json:"remediation,omitempty"`
	Reachability string   `json:"reachability,omitempty"` // "reachable" / "unreachable" / "" (not analyzed)
	Verification string   `json:"verification,omitempty"` // "verified" / "unverified" / "" (not checked)
	CWE          string   `json:"cwe,omitempty"`
	ASVS         string   `json:"asvs,omitempty"`
	SeenBy       []string `json:"seen_by,omitempty"`
}

// ScanResponse carries the formatted scan report (or SBOM-generation
// summary) plus — for the request shapes that produce a security.Report
// (workspace/path, image, and recon scans; the SBOM branch stays
// report-text-only) — the same outcome structured, mirroring
// security.Report field for field so a client (the web UI's Security panel,
// P15.6) can render a findings table without parsing the formatted text.
// Report is always set, for callers that only want the text.
type ScanResponse struct {
	Report string `json:"report"`

	Findings []ScanFinding `json:"findings,omitempty"`
	// Suppressed holds findings hidden by an active accepted-risk baseline
	// entry (.aegis/security-baseline.yaml) — returned rather than dropped,
	// same never-a-silent-omission posture as security.Report.Suppressed.
	Suppressed []ScanFinding     `json:"suppressed,omitempty"`
	Ran        []string          `json:"ran,omitempty"`     // scanners that executed
	RanVia     map[string]string `json:"ran_via,omitempty"` // scanner -> "host" or "container"
	Skipped    map[string]string `json:"skipped,omitempty"` // scanner -> reason (disabled / unavailable / error)
	// Baseline diagnostics (security.Report's fields of the same names):
	// entries that suppressed nothing this run because they expired or never
	// parsed, and the parse error when the whole baseline file was unusable.
	ExpiredSuppressions []string `json:"expired_suppressions,omitempty"`
	InvalidSuppressions []string `json:"invalid_suppressions,omitempty"`
	BaselineError       string   `json:"baseline_error,omitempty"`
}

// DebateRequest runs a multi-agent debate (P12) directly (POST /debate),
// independent of any session — the same underlying mechanism the `agent`
// tool's debate mode runs, exposed for `/debate` in the TUI and `aegis debate`
// so a claim can be adversarially reviewed without spending a conversational
// turn first to produce it.
type DebateRequest struct {
	// Claim is the finding/threat-mitigation/design assertion — or any other
	// claim, e.g. about a document or plan — to debate.
	Claim string `json:"claim"`
	// Domain selects the default persona trio: "security" (default) or
	// "generic" for non-security claims (documents, plans, decisions).
	// Ignored for any role whose persona is explicitly overridden below.
	Domain string `json:"domain,omitempty"`
	// Files are paths the debate roles should read for grounding before
	// proposing/critiquing/rebutting, e.g. the documents a claim is about.
	Files []string `json:"files,omitempty"`
	// ProposerPersona/CriticPersona/ArbiterPersona override the default debate
	// role personas (security-researcher/security-critic/security-arbiter, or
	// general/critic/arbiter when Domain is "generic").
	ProposerPersona string `json:"proposer_persona,omitempty"`
	CriticPersona   string `json:"critic_persona,omitempty"`
	ArbiterPersona  string `json:"arbiter_persona,omitempty"`
	// MaxRounds overrides the default critique/rebuttal round bound (2).
	MaxRounds int `json:"max_rounds,omitempty"`
	// Workdir grounds Files and every debate role's tool calls in a specific
	// directory (P25.8) — debate is session-less, so without this it always
	// resolved against the daemon's own workspace regardless of which
	// session's directory a caller (e.g. the web UI's "stress-test a claim"
	// panel) actually meant. Empty keeps the pre-P25.8 behavior: the
	// daemon's default workspace.
	Workdir string `json:"workdir,omitempty"`
}

// DebateResponse carries the formatted transcript and the arbiter's parsed
// verdict fields (empty if the arbiter's response didn't parse).
type DebateResponse struct {
	Report     string `json:"report"`
	Verdict    string `json:"verdict"`
	Confidence string `json:"confidence"`
}

// KnowledgeRequest indexes or queries the project knowledge base directly
// (POST /knowledge), independent of any session/model turn — the same
// project_knowledge tool and `aegis knowledge index` machinery, exposed so
// `/knowledge` in the TUI can rebuild or search the index without spending a
// conversational turn first.
type KnowledgeRequest struct {
	// Action is "index" (rebuild) or "query" (search). Required.
	Action string `json:"action"`
	// Query is the search text; required when Action is "query".
	Query string `json:"query,omitempty"`
	// Limit caps query results (default 5, max 20); ignored for "index".
	Limit int `json:"limit,omitempty"`
}

// KnowledgeResult is one document matched by a knowledge query.
type KnowledgeResult struct {
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

// KnowledgeResponse carries the outcome of a KnowledgeRequest: DocCount/DBPath/
// EmbeddingsEnabled after an "index" rebuild, or Results/Count after a "query".
type KnowledgeResponse struct {
	DocCount          int               `json:"doc_count,omitempty"`
	DBPath            string            `json:"db_path,omitempty"`
	EmbeddingsEnabled bool              `json:"embeddings_enabled,omitempty"`
	Results           []KnowledgeResult `json:"results,omitempty"`
	Count             int               `json:"count,omitempty"`
}

// RepoMapIndexResponse carries the outcome of rebuilding the repository map
// (POST /repomap/index) — the same underlying build as `aegis index`, exposed
// so `/index` in the TUI can refresh it (and the daemon's cached system-prompt
// block) without a restart.
type RepoMapIndexResponse struct {
	FileCount int    `json:"file_count"`
	Path      string `json:"path"`
}

// ConfigScope selects which config file a config-mutation endpoint reads
// from/writes to: "project" (.aegis/config.yaml) or "global"
// (~/.config/aegis/config.yaml, see config.GlobalConfigPath). Every
// GET/PATCH /config/* and POST /config/harden request accepts it (a "scope"
// query param on GETs, a "scope" body field on PATCH/POST); an empty value
// lets the daemon pick a default (see server.resolveScope): "project" when
// its workspace has a .aegis/ directory, else "global".
type ConfigScope = string

// ConfigSandboxResponse is the GET /config/sandbox response (P15.2): the
// daemon's currently effective sandbox.* settings (config.Load()'s merged
// view across global/project/env layers), plus the scope a PATCH without an
// explicit "scope" would default to.
//
// ActiveBackend/Fallback/FallbackReason (P25.2) report what the running
// daemon actually selected via SelectSandbox at startup, which can differ
// from Backend/Runtime above: an unrecognized backend value, a container
// runtime that failed to initialize, etc. all silently degrade to the
// unsandboxed local backend, and a client trusting the config echo alone
// has no way to detect that. ActiveBackend is the real sandbox.Backend.Name()
// currently in use ("local", "container:podman", "os:...", ...); Fallback is
// true when it differs from what Backend/Runtime were configured to select.
type ConfigSandboxResponse struct {
	Scope          string   `json:"scope"`
	Backend        string   `json:"backend"`
	Runtime        string   `json:"runtime,omitempty"`
	Priority       []string `json:"priority,omitempty"`
	Image          string   `json:"image,omitempty"`
	Network        bool     `json:"network"`
	ActiveBackend  string   `json:"active_backend,omitempty"`
	Fallback       bool     `json:"fallback,omitempty"`
	FallbackReason string   `json:"fallback_reason,omitempty"`
}

// ConfigSandboxPatchRequest partially updates the sandbox: config block
// (PATCH /config/sandbox). Only fields present in the JSON body are changed;
// the rest keep their current value from config.Load() before the patch is
// written — this endpoint's semantics genuinely are PATCH (partial), unlike
// the underlying config.SandboxPatch/PatchGlobalSandbox/PatchProjectSandbox,
// which always replace the whole sandbox: block wholesale.
type ConfigSandboxPatchRequest struct {
	Scope    string    `json:"scope,omitempty"`
	Backend  *string   `json:"backend,omitempty"`
	Runtime  *string   `json:"runtime,omitempty"`
	Priority *[]string `json:"priority,omitempty"`
	Image    *string   `json:"image,omitempty"`
	Network  *bool     `json:"network,omitempty"`
}

// SecurityToolConfigWire mirrors config.SecurityToolConfig on the wire,
// using a plain bool (not *bool) for Enabled so JSON consumers don't need to
// know about Go's nil-vs-false distinction — omitted/absent from a PATCH
// request's Tools map leaves that tool's config untouched (map keys not
// present in the request are not modified), while an explicit entry always
// carries an explicit Enabled value.
type SecurityToolConfigWire struct {
	Enabled          bool   `json:"enabled"`
	Method           string `json:"method,omitempty"`
	Install          string `json:"install,omitempty"`
	Image            string `json:"image,omitempty"`
	TemplatesVersion string `json:"templates_version,omitempty"`
	Verify           bool   `json:"verify,omitempty"`
}

// DASTConfigWire mirrors config.DASTConfig on the wire.
type DASTConfigWire struct {
	AllowedTargets []string `json:"allowed_targets,omitempty"`
	AllowActive    bool     `json:"allow_active,omitempty"`
}

// ConfigSecurityResponse is the GET /config/security response (P15.2): the
// daemon's currently effective security: settings.
type ConfigSecurityResponse struct {
	Scope            string                            `json:"scope"`
	EgressThenWrite  bool                              `json:"egress_then_write"`
	NetworkAllowList []string                          `json:"network_allowlist,omitempty"`
	DefaultMethod    string                            `json:"default_method"`
	Tools            map[string]SecurityToolConfigWire `json:"tools,omitempty"`
	DAST             DASTConfigWire                    `json:"dast"`
}

// ConfigSecurityPatchRequest partially updates the security: config block
// (PATCH /config/security). Only fields present in the JSON body are
// changed. Tools, when present, replaces the entire tools map wholesale
// (matching how the existing /security-config TUI dialog and
// config.SecurityPatch.Tools already behave — a per-tool merge would need a
// separate "delete this override" signal that config.SecurityToolConfig has
// no room for).
type ConfigSecurityPatchRequest struct {
	Scope            string                            `json:"scope,omitempty"`
	EgressThenWrite  *bool                             `json:"egress_then_write,omitempty"`
	NetworkAllowList *[]string                         `json:"network_allowlist,omitempty"`
	DefaultMethod    *string                           `json:"default_method,omitempty"`
	Tools            map[string]SecurityToolConfigWire `json:"tools,omitempty"`
	DAST             *DASTConfigWire                   `json:"dast,omitempty"`
}

// BuiltinSkillInfo describes one embedded built-in skill for catalog
// listings (name + frontmatter description), independent of whether it is
// currently enabled.
type BuiltinSkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ConfigSkillsResponse is the GET /config/skills response (P15.2): which
// embedded built-in skills (internal/skills/builtin) are currently active,
// plus the full catalog of built-ins that could be enabled (P15.7) so a
// toggle UI doesn't have to hard-code the shipped skill names.
type ConfigSkillsResponse struct {
	Scope          string             `json:"scope"`
	BuiltinEnabled []string           `json:"builtin_enabled"`
	Available      []BuiltinSkillInfo `json:"available"`
}

// ConfigSkillsPatchRequest replaces the skills.builtin_enabled list (PATCH
// /config/skills). Unlike the sandbox/security patch requests, this is
// always a full replace — config.PatchGlobalSkillsEnabled/
// PatchProjectSkillsEnabled already require the full desired set, not a
// delta (see their doc comments), and a partial-update shape would just
// invite a caller to accidentally disable every skill it didn't mention.
type ConfigSkillsPatchRequest struct {
	Scope          string   `json:"scope,omitempty"`
	BuiltinEnabled []string `json:"builtin_enabled"`
}

// ConfigHardenRequest applies the hardened profile computed by
// config.ComputeHardenPlan (POST /config/harden, P15.2) — the HTTP
// equivalent of `aegis harden`. Confirm must be true for anything to be
// written; this is an HTTP analog of the CLI's "Apply? [y/N]" prompt (skipped
// entirely here since there's no interactive terminal to prompt), not
// something a caller can accidentally trigger by probing the endpoint.
type ConfigHardenRequest struct {
	Scope   string `json:"scope,omitempty"`
	Confirm bool   `json:"confirm"`
}

// ConfigHardenResponse reports what POST /config/harden changed (or would
// change, when Confirm was false) — the same "changed" vs "already X —
// unchanged" distinction `aegis harden` prints to the terminal.
type ConfigHardenResponse struct {
	Scope   string `json:"scope"`
	Applied bool   `json:"applied"` // false when Confirm was false: nothing was written

	SandboxChanged bool   `json:"sandbox_changed"`
	SandboxBackend string `json:"sandbox_backend"` // resulting (or would-be) sandbox.backend

	SecurityChanged bool `json:"security_changed"`

	CostChanges []string `json:"cost_changes,omitempty"`
}

// SecurityInstallRequest runs a security scanner's guided host install
// (POST /security/install) — the same underlying security.RunGuidedInstall
// the `aegis security install` CLI and the `/security-config` TUI wizard
// use. Confirm must be true for the command to actually run; installing
// software is a privileged, host-modifying action that must never happen
// silently (see security.RunGuidedInstall's doc comment) — with Confirm
// false, the response only reports what command *would* run.
type SecurityInstallRequest struct {
	Tool    string `json:"tool"`
	Confirm bool   `json:"confirm"`
}

// SecurityInstallResponse reports the guided-install command for Tool and,
// once Confirm was true, its outcome.
type SecurityInstallResponse struct {
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"` // the exact host command; empty when no guided install exists
	Ran     bool   `json:"ran"`               // true once the command was actually executed
	OK      bool   `json:"ok"`                // true when Ran and it exited zero
	Output  string `json:"output,omitempty"`  // combined stdout+stderr, only when Ran
	Error   string `json:"error,omitempty"`   // why Command is empty, or why it failed, or the "pass confirm" hint
}

// SecurityToolStatus is one scanner's entry in the GET /security/status
// response — the same live-availability probe (host binary / container
// runtime / WSL) and status wording the `/security-config` TUI dialog shows
// per tool (see internal/tui/securityconfig.go's resolveCmd/toolBadge).
type SecurityToolStatus struct {
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
	// Enabled mirrors config.SecurityToolConfig.ToolEnabled()'s configured
	// on/off badge state, the same one the TUI list shows next to each tool
	// name — it does not account for scanner-specific default-enabled
	// exceptions (see Status/Resolved below for the actual runnable verdict).
	Enabled bool `json:"enabled"`
	// Method is the configured resolver method for this tool: "auto"
	// (default), "host", or "container".
	Method string `json:"method"`
	// Resolved is the actual outcome of resolving this tool right now:
	// "host", "container", "wsl", or "unavailable".
	Resolved string `json:"resolved"`
	Runtime  string `json:"runtime,omitempty"` // container runtime name, only when Resolved == "container"
	// Status is a human-readable summary matching the exact wording
	// internal/tui/securityconfig.go's resolveCmd shows ("on PATH",
	// "container (docker)", "via WSL", "unavailable: <reason>[; note]").
	Status string `json:"status"`
}

// SecurityStatusResponse is the GET /security/status response: every
// built-in scanner's configured/resolved status, mirroring what
// `/security-config` already shows in the TUI (P15.2).
type SecurityStatusResponse struct {
	Tools []SecurityToolStatus `json:"tools"`
}

// SecurityBaselineEntry mirrors security.SuppressionEntry on the wire, with
// an added Status ("active"/"expired"/"invalid" — see
// security.SuppressionStatusLabel) so a caller doesn't need to reimplement
// the expiry/validity check client-side.
type SecurityBaselineEntry struct {
	RuleID   string `json:"rule_id"`
	Location string `json:"location,omitempty"`
	Reason   string `json:"reason"`
	Expires  string `json:"expires"`
	AddedBy  string `json:"added_by,omitempty"`
	Status   string `json:"status"`
}

// SecurityBaselineResponse is the GET /security/baseline response: the
// project's accepted-risk suppression allowlist (.aegis/security-
// baseline.yaml, P11.8), or an empty Suppressions list when no baseline file
// exists (the common case).
type SecurityBaselineResponse struct {
	Path         string                  `json:"path"`
	Suppressions []SecurityBaselineEntry `json:"suppressions,omitempty"`
}
