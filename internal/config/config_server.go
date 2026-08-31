package config

// ServerConfig configures the local daemon.
type ServerConfig struct {
	Addr string `koanf:"addr"` // host:port the daemon listens on

	// AllowRemote must be explicitly set to bind Addr to a non-loopback
	// address (FIND-08). The daemon's API is protected only by a bearer
	// token with no rate limiting; binding it to a network-reachable address
	// (e.g. "0.0.0.0:4127") silently exposes it to anyone who can reach that
	// address unless the operator has deliberately acknowledged the
	// tradeoff. Loopback addresses (127.0.0.1, ::1, localhost) never require
	// this flag. Off by default.
	AllowRemote bool `koanf:"allow_remote"`

	// SessionWorkdirAllowlist bounds which directories a client may request
	// as a session's working directory (P25.1) once AllowRemote is set: the
	// resolved path must be the daemon's own default workspace (or nested
	// under it) or nested under one of these entries. Ignored — every
	// existing-directory request is accepted — on the default loopback-only
	// bind, where a client is already as trusted as a local shell user.
	// Without this, a remote-accessible daemon combined with per-session
	// workdirs would let any bearer-token holder point a session at an
	// arbitrary directory the daemon process can read, turning it into a
	// filesystem oracle far beyond its own project.
	SessionWorkdirAllowlist []string `koanf:"session_workdir_allowlist"`

	// TrustProxyHeaders must be explicitly set to make the daemon honor
	// X-Forwarded-Proto (and, for now, only that header — X-Forwarded-For and
	// other forwarded headers are out of scope) from any client able to reach
	// it (P32.10). This exists for operators who run the daemon behind a
	// reverse proxy that terminates TLS itself and forwards plaintext to the
	// loopback daemon: on that backend hop r.TLS is nil even though the
	// browser used HTTPS, so without this flag the web UI's CSRF cookie would
	// be minted without Secure. Only enable this when the daemon sits behind
	// a reverse proxy the operator controls that strips or overwrites any
	// client-supplied X-Forwarded-Proto before forwarding — otherwise a
	// caller with direct network access to the daemon could spoof the header
	// and the protection this flag is meant to restore becomes a lie. Off by
	// default so the existing loopback-plaintext and built-in-TLS (see
	// ServerTLSConfig below) deployment shapes are unaffected.
	TrustProxyHeaders bool `koanf:"trust_proxy_headers"`

	// TLS encrypts client<->daemon traffic (FIND-32/P24.18). On by default
	// since P27.5/FIND-13: the loopback-only bind (AllowRemote above) already
	// limits exposure to off-host attackers, but plaintext HTTP still leaves
	// the bearer token and full conversation content readable to another
	// local account on a shared host with packet-capture privilege — a real,
	// if narrow, gap defense-in-depth closes at effectively no cost (the
	// pinned self-signed cert is auto-generated and every in-repo client
	// already pins it transparently via client.NewFromConfig). See
	// ServerTLSConfig's doc comment for exactly what enabling it does.
	TLS ServerTLSConfig `koanf:"tls"`

	// MaxConcurrentRuns caps how many message-turn runs (POST
	// /sessions/{id}/messages) may be actively executing across all sessions
	// at once (P21.5). A request that would exceed the cap is rejected
	// immediately with 429 rather than queued indefinitely — the per-session
	// sessionSems gate only prevents two runs racing on the *same* session,
	// so with no global cap a caller that fans out many sessions (e.g. a
	// hostile or misbehaving `aegis mcp-serve` client) could still exhaust
	// host resources. Defaults to 10 (P27.12/FIND-14): only top-level
	// HTTP-driven runs consume a slot here — sub-agents spawned in-process by
	// the `agent`/swarm tool run directly through the engine, not through
	// this HTTP path, so a normal single-user TUI/web-UI session (which
	// drives at most one or two runs at a time) never gets close to this
	// ceiling; it exists to bound a lower-trust caller like `aegis
	// mcp-serve`. 0 = unlimited; set explicitly to opt back out.
	MaxConcurrentRuns int `koanf:"max_concurrent_runs"`

	// MaxRunDurationSec aborts a single run once it has been active this many
	// seconds, the same way an interrupted/cancelled request is handled.
	// cost.max_tokens_per_run/budget_usd are the primary spend guardrails;
	// this is a coarser wall-clock backstop for a run that never trips those
	// (e.g. a local model stuck making tool calls with near-zero token cost,
	// or a hostile caller trying to hold a run, and the session/global
	// concurrency slot it occupies, open forever). Defaults to 1800 (30
	// minutes, P27.12/FIND-14) — generous enough for a long agentic run with
	// many tool calls, short enough to reclaim a wedged slot well within a
	// working session. 0 = unlimited; set explicitly to opt back out.
	MaxRunDurationSec int `koanf:"max_run_duration_sec"`

	// SSEBufferSize bounds how many not-yet-flushed SSE events are queued for
	// a single run's HTTP connection before the daemon drops the oldest
	// queued event to make room for the newest (P21.5). Protects daemon
	// memory from a slow or stalled SSE consumer (TUI, web UI, or an
	// mcp-serve client) that reads events slower than the engine produces
	// them; the run itself keeps executing and persisting to the session
	// store regardless of how far behind the client falls. 0 falls back to
	// the built-in default (256).
	SSEBufferSize int `koanf:"sse_buffer_size"`
}

// DefaultSSEBufferSize is the fallback per-connection SSE event queue depth
// used when ServerConfig.SSEBufferSize is left at 0 (P21.5).
const DefaultSSEBufferSize = 256

// ServerTLSConfig configures transport encryption for the daemon's HTTP API
// (FIND-32/P24.18). Loopback binding already keeps client<->daemon traffic
// off the network, but plain HTTP still leaves the bearer token and full
// conversation content observable to another local account on a shared host
// with packet-capture privilege. Enabled defaults to true since P27.5/
// FIND-13 to close that gap; it does not protect against Host/OS-level
// compromise of the same account, which can already read daemon.token (and,
// with TLS enabled, daemon.key) directly off disk — see docs/configuration.md.
//
// When Enabled is true and CertFile/KeyFile are left empty, the daemon
// generates a self-signed ECDSA P-256 certificate on first start and persists
// it under DataDir as daemon.crt/daemon.key (mirroring the daemon.token
// convention — generated once, reused across restarts unless missing). The
// client must be told to trust that specific certificate (see
// client.WithTLS); this is certificate pinning to a file that never leaves
// the local machine, not verification against a public CA or hostname, so
// there is no browser/OS trust store involved and no renewal workflow. Every
// in-repo client goes through client.NewFromConfig, which wires this up
// automatically — a browser opening `aegis ui` is the one consumer that
// isn't pinned and will show a self-signed-certificate warning, which the
// CLI calls out explicitly when TLS is on (see internal/cli/ui.go).
type ServerTLSConfig struct {
	// Enabled turns on TLS for the daemon's HTTP listener and switches
	// client.NewFromConfig to https:// with the pinned cert. On by default
	// (P27.5/FIND-13); set to false to keep the pre-P27.5 plain-HTTP
	// behavior (no cert/key files written, no scheme change).
	Enabled bool `koanf:"enabled"`

	// CertFile/KeyFile let an operator supply their own certificate instead
	// of the auto-generated self-signed one (e.g. one issued by an internal
	// CA). Both empty (the default) means auto-generate/reuse
	// <DataDir>/daemon.crt and <DataDir>/daemon.key.
	CertFile string `koanf:"cert_file"`
	KeyFile  string `koanf:"key_file"`
}

// PermissionConfig sets the default agent permission posture.
type PermissionConfig struct {
	Mode            string   `koanf:"mode"`              // "plan" or "build"
	AutoApproveExec bool     `koanf:"auto_approve_exec"` // auto-approve shell/execute tool calls
	Rules           []string `koanf:"rules"`             // text-based allow/deny rules, e.g. "allow bash(npm test*)", "deny write(/etc/*)"

	// AllowUnsandboxedAutoExec opts into the combination the daemon otherwise
	// refuses to start with (P25.2): auto_approve_exec: true while the
	// effective sandbox backend is the unsandboxed local one. That pairing
	// means every model-issued shell command runs on the host with no
	// approval and no isolation — previously only a startup WARN line, easy
	// to miss, for what amounts to unattended remote code execution. Set
	// this only when that's a deliberate, understood choice (e.g. an
	// already-isolated CI container running Aegis itself).
	AllowUnsandboxedAutoExec bool `koanf:"allow_unsandboxed_auto_exec"`

	// PlanModeShellReads controls whether the shell tool's read-only
	// classification (P25.4c) applies in plan mode. Default true, which is the
	// shipped behavior: `shell("git log")` in plan mode is gated as CapRead and
	// allowed without a prompt, rather than being denied as an execute call.
	//
	// It exists because plan mode's documented guarantee — "the workspace may
	// not be mutated or commands run at all" — is mediated by
	// classifyShellCommand, ~1,080 lines of hand-written argument parsing
	// across 40+ commands and three shell dialects (DR-2). That design is right
	// on its own terms; before it, a `git status` in plan mode was *silently
	// denied*, which is worse. But it means every defect in that parser is a
	// plan-mode defect, and plan mode is the posture an operator picks
	// precisely when they want a hard boundary rather than a convenient one —
	// reviewing an untrusted repository being the canonical case.
	//
	// Set it to false to make plan mode's guarantee unconditional: the shell
	// tool is then CapExecute in plan mode whatever the classifier says, and
	// the parser's correctness stops being load-bearing for that posture.
	// Plan mode *denies* execute rather than asking, so this makes `shell`
	// unusable in plan mode outright — which is the guarantee ("no commands
	// run"), not a side effect of it. Build and auto mode are unaffected
	// either way, so the ergonomics the downgrade buys are kept where they
	// are wanted.
	PlanModeShellReads *bool `koanf:"plan_mode_shell_reads"`
}

// PlanModeShellReadsEnabled resolves PlanModeShellReads, defaulting to true
// (the shipped behavior) when the operator has expressed no preference.
func (p PermissionConfig) PlanModeShellReadsEnabled() bool {
	return p.PlanModeShellReads == nil || *p.PlanModeShellReads
}

// GitConfig configures the git-facing built-in tools (currently just the
// pre-commit test gate, P46.2).
type GitConfig struct {
	// PreCommitTestCommand, when set, is a shell command the git_commit tool
	// runs in the workspace before every commit; a non-zero exit aborts the
	// commit and the command's output is returned to the model instead. This
	// makes "tests pass before every commit" a mechanical gate rather than
	// unenforced persona/skill prose a model can drop under context pressure
	// (P46.2). Empty (the default) is a no-op — git_commit stays a straight
	// passthrough, so existing sessions with no test command declared are
	// unaffected. It executes an arbitrary host command, so it is treated as a
	// security-relevant setting frozen under the workspace-trust gate (P27.1):
	// an untrusted project's .aegis/config.yaml cannot introduce or change it.
	PreCommitTestCommand string `koanf:"pre_commit_test_command"`
	// PreCommitTestTimeoutSec bounds how long PreCommitTestCommand may run
	// before it is killed and the commit aborted. 0 falls back to
	// DefaultPreCommitTestTimeoutSec.
	PreCommitTestTimeoutSec int `koanf:"pre_commit_test_timeout_sec"`
}

// DefaultPreCommitTestTimeoutSec is the pre-commit test command's timeout when
// GitConfig.PreCommitTestTimeoutSec is unset.
const DefaultPreCommitTestTimeoutSec = 600
