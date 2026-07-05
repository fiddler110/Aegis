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
}

// SessionMeta describes a session without its messages.
type SessionMeta struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Mode         string     `json:"mode"`
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
	KindText            EventKind = "text"
	KindThinking        EventKind = "thinking"
	KindToolCall        EventKind = "tool_call"
	KindToolResult      EventKind = "tool_result"
	KindTurnDone        EventKind = "turn_done"
	KindDone            EventKind = "done"
	KindError           EventKind = "error"
	KindApprovalRequest EventKind = "approval_request" // engine awaiting user approval
	KindSteer           EventKind = "steer"            // mid-run steering instruction injected
	KindGuard           EventKind = "guard"            // output validation warning
	KindCostAlert       EventKind = "cost_alert"       // spend crossed the configured alert threshold (P9.5)
)

// Event is one server-sent event during a message run.
type Event struct {
	Kind         EventKind       `json:"kind"`
	Text         string          `json:"text,omitempty"`
	Tool         string          `json:"tool,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	ToolResult   string          `json:"tool_result,omitempty"`
	ToolIsError  bool            `json:"tool_is_error,omitempty"`
	InputTokens  int             `json:"input_tokens,omitempty"`
	OutputTokens int             `json:"output_tokens,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	Error        string          `json:"error,omitempty"`
	// Cache token usage (Anthropic prompt caching), surfaced for observability.
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
	// TokensEstimated is true when token counts were inferred from character
	// length because the provider did not report usage (e.g. local/Ollama models).
	TokensEstimated bool `json:"tokens_estimated,omitempty"`
	// KindApprovalRequest fields
	ApprovalReason string `json:"approval_reason,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"` // run id to echo back when answering
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

// UpdateSessionRequest patches a session's system prompt, mode, or persona.
// Setting Persona switches the session's full behavioral profile: system
// prompt, persisted persona name (which carries model, permission rules, and
// output-guard overrides on subsequent turns), and — unless Mode is also set —
// the persona's permission mode.
type UpdateSessionRequest struct {
	System  *string `json:"system,omitempty"`
	Mode    *string `json:"mode,omitempty"`
	Persona *string `json:"persona,omitempty"`
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
}

// ScanResponse carries the formatted scan report (or SBOM-generation summary).
type ScanResponse struct {
	Report string `json:"report"`
}

// DebateRequest runs a multi-agent debate (P12) directly (POST /debate),
// independent of any session — the same underlying mechanism the `agent`
// tool's debate mode runs, exposed for `/debate` in the TUI and `aegis debate`
// so a claim can be adversarially reviewed without spending a conversational
// turn first to produce it.
type DebateRequest struct {
	// Claim is the finding/threat-mitigation/design assertion to debate.
	Claim string `json:"claim"`
	// ProposerPersona/CriticPersona/ArbiterPersona override the default debate
	// role personas (security-researcher/security-critic/security-arbiter).
	ProposerPersona string `json:"proposer_persona,omitempty"`
	CriticPersona   string `json:"critic_persona,omitempty"`
	ArbiterPersona  string `json:"arbiter_persona,omitempty"`
	// MaxRounds overrides the default critique/rebuttal round bound (2).
	MaxRounds int `json:"max_rounds,omitempty"`
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
