package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/mcp"
	"github.com/fiddler110/aegis/internal/notify"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
)

// localRepoMapMaxBytes caps the repo map injected under the local prompt
// profile (P25.6): a large repo map is one of the heavier always-injected
// blocks, and small local models pay for it in prompt-processing latency on
// every turn regardless of whether the task touches most of the repo. The
// default profile never applies this cap.
const localRepoMapMaxBytes = 4000

// effectiveSystem combines the session's base system prompt with platform
// context, loaded project/user memory, skills, and context files (AGENTS.md,
// CLAUDE.md). sessionID selects which on-demand-activated built-in skills
// (see activateSessionSkill) are layered on top of the persistently
// configured set; pass "" where no session is in scope (e.g. tests).
func (s *Server) effectiveSystem(base, sessionID string) string {
	var parts []string
	if base != "" {
		parts = append(parts, base)
	}
	local := s.cfg.Provider.LocalPromptProfile()
	parts = append(parts, persona.ToolUseBlockFor(local))
	parts = append(parts, persona.CompletingTasksBlockFor(local))
	parts = append(parts, persona.PlatformBlockFor(local))
	if ctx := s.memory.LoadContext(); ctx != "" {
		parts = append(parts, ctx)
	}
	if mem := s.memory.Load(); mem != "" {
		parts = append(parts, mem)
	}
	workdir := s.workdirFor(sessionID)
	if sk := skills.BuildIndex(workdir, s.cfg.DataDir, s.sessionEnabledSkills(sessionID)); sk != "" {
		parts = append(parts, sk)
	}
	repoMap := s.repoMapFor(workdir)
	if repoMap != "" && !(local && len(repoMap) > localRepoMapMaxBytes) {
		parts = append(parts, repoMap)
	}
	if dt := deferredToolsBlock(s.toolRegistryFor(sessionID)); dt != "" {
		parts = append(parts, dt)
	}
	if db := debateIntegrationBlock(s.cfg.Security.Debate); db != "" {
		parts = append(parts, db)
	}
	return strings.Join(parts, "\n\n")
}

// debateIntegrationBlock returns the P12.5 opt-in instruction text wiring the
// `agent` tool's debate mode into the two existing security workflows that
// benefit from adversarial review, or "" if neither toggle is enabled (the
// default — debate multiplies model calls per item, so this is never
// injected silently). Both toggles can be on independently; the block only
// mentions the ones actually enabled.
func debateIntegrationBlock(cfg config.DebateIntegrationConfig) string {
	if !cfg.ThreatModel && !cfg.Triage {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Debate mode (P12)\n")
	if cfg.ThreatModel {
		b.WriteString("- Threat modeling: before writing an identified threat/mitigation pair into the threat model document, call the `agent` tool with mode:\"debate\" and claim set to that threat/mitigation pair. Adjust the entry's severity/mitigation per the arbiter's verdict before finalizing it.\n")
	}
	if cfg.Triage {
		b.WriteString("- Security-audit triage: before suppressing a borderline or disputed-severity scan finding via the baseline, call the `agent` tool with mode:\"debate\" and claim set to the finding (severity, location, rationale). Only suppress if the verdict upholds the low-risk assessment.\n")
	}
	return b.String()
}

// toExecSpecs converts config hook entries into hooks.ExecSpec values.
func toExecSpecs(cfgHooks []config.HookConfig) []hooks.ExecSpec {
	specs := make([]hooks.ExecSpec, 0, len(cfgHooks))
	for _, h := range cfgHooks {
		specs = append(specs, hooks.ExecSpec{
			Event:      h.Event,
			Command:    h.Command,
			Tools:      h.Tools,
			TimeoutSec: h.TimeoutSec,
		})
	}
	return specs
}

// deferredToolsBlock advertises tools that are registered but not exposed by
// default (P4.6). The model loads them on demand with the tool_search tool.
//
// One line per tool, and that line is the tool's Summary rather than its full
// Description (P62.6). Printing the manuals made this block 2,953 tokens — 38%
// of the local profile's entire base prompt, spent on 26 tools that are *not
// loaded*, against 3,614 for the 27 that are. Deferral had stopped being a
// saving.
//
// Nothing is lost to discovery by shortening it, which is what makes this
// cheap: tool_search matches its query against the full Name+Description via
// Registry.SearchDeferred, and that text lives in the registry, not in the
// prompt. A scanner name or a synonym that no longer appears here still finds
// its tool, and the full description comes back with the schema on load.
func deferredToolsBlock(reg *tool.Registry) string {
	if reg == nil {
		return ""
	}
	deferred := reg.Deferred()
	if len(deferred) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<deferred_tools>\n")
	sb.WriteString("These tools are not loaded yet. When a task needs one, call `tool_search` with keywords to load it before use.\n")
	for _, d := range deferred {
		fmt.Fprintf(&sb, "- %s: %s\n", d.Name, d.Summary)
	}
	sb.WriteString("</deferred_tools>")
	return sb.String()
}

// repoMapOptions renders the `repomap:` config section as build options
// (P62.1). Every daemon-side repo-map call has to pass the same pair, and the
// cached render is keyed on it — a load that reads with one budget and a
// rebuild that writes with another would flip the injected block's size
// depending on which path ran, so the translation lives here rather than being
// spelled out per call site.
func repoMapOptions(cfg *config.Config) repomap.Options {
	if cfg == nil {
		return repomap.Options{}
	}
	return repomap.Options{
		MaxBytes:          cfg.RepoMap.MaxBytes,
		MaxSymbolsPerFile: cfg.RepoMap.MaxSymbolsPerFile,
	}
}

// loadRepoMap loads the cached repository map for cwd, rebuilding it when the
// cache is stale (a source file changed since the last `aegis index`). The map
// is opt-in: when no cache exists, this returns an empty string and nothing is
// injected. Returns a ready-to-inject <repo_map> block, or "" on any failure.
func loadRepoMap(cwd string, opts repomap.Options, logger *slog.Logger) string {
	cache := filepath.Join(cwd, ".aegis", "repomap.json")
	rendered, fresh, err := repomap.Load(cwd, cache, opts)
	if err != nil || rendered == "" {
		return "" // not indexed, or unreadable cache
	}
	if !fresh {
		// The repo changed since indexing; rebuild so the prompt isn't stale.
		if m, buildErr := repomap.Build(cwd, opts); buildErr == nil {
			if saveErr := m.Save(cache); saveErr != nil {
				logger.Warn("repo map rebuilt but cache not saved", "err", saveErr)
			}
			rendered = m.Render()
		}
	}
	return repomap.Block(rendered)
}

func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNotFound) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.logger.Error("store error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}

// buildSamplingHandler returns a mcp.SamplingHandler that calls the provider
// adapter to fulfil server-initiated sampling/createMessage requests. The
// response is assembled by collecting all text deltas from the stream.
func buildSamplingHandler(adapter provider.Adapter, model string, maxTokens int, logger *slog.Logger) mcp.SamplingHandler {
	return func(ctx context.Context, req mcp.SamplingRequest) (mcp.SamplingResponse, error) {
		var msgs []provider.Message
		for _, m := range req.Messages {
			role := provider.RoleUser
			if m.Role == "assistant" {
				role = provider.RoleAssistant
			}
			msgs = append(msgs, provider.Message{
				Role:    role,
				Content: []provider.Block{provider.TextBlock{Text: m.Content.Text}},
			})
		}

		mt := maxTokens
		if req.MaxTokens > 0 && req.MaxTokens < mt {
			mt = req.MaxTokens
		}

		stream, err := adapter.Stream(ctx, provider.Request{
			Model:     model,
			System:    req.SystemPrompt,
			Messages:  msgs,
			MaxTokens: mt,
		})
		if err != nil {
			return mcp.SamplingResponse{}, fmt.Errorf("mcp sampling: %w", err)
		}

		var sb strings.Builder
		var stopReason string
		for ev := range stream {
			switch ev.Type {
			case provider.EventTextDelta:
				sb.WriteString(ev.Text)
			case provider.EventDone:
				stopReason = string(ev.Stop)
			case provider.EventError:
				logger.Warn("mcp sampling stream error", "err", ev.Err)
				return mcp.SamplingResponse{}, ev.Err
			}
		}

		return mcp.SamplingResponse{
			Role:       "assistant",
			Content:    mcp.SamplingContent{Type: "text", Text: sb.String()},
			Model:      model,
			StopReason: stopReason,
		}, nil
	}
}

// cronShellRunner returns a function that runs a cron job's command using the
// given sandbox backend, streaming output to the task buffer via emit.
// defaultCwd is the daemon's own working directory; the returned function
// accepts a per-call dir override (P25.8's Job.Workdir) that, when non-empty,
// takes precedence — without this every cron job fired in the daemon's cwd
// regardless of which session's workdir created it.
func cronShellRunner(sb sandbox.Backend, defaultCwd string) func(ctx context.Context, command, dir string, emit func(string)) error {
	const cronJobTimeout = 10 * time.Minute
	return func(ctx context.Context, command, dir string, emit func(string)) error {
		ctx, cancel := context.WithTimeout(ctx, cronJobTimeout)
		defer cancel()
		if dir == "" {
			dir = defaultCwd
		}
		return sb.ExecStreaming(ctx, command, sandbox.ExecOpts{Dir: dir}, emit)
	}
}

// newCronRunFunc builds the cron.RunFunc that fires a due job: it applies the
// fire-time permission check (permCheck), runs the job's command via
// runCronCmd, and — independent of both — persists a durable cron_runs audit
// record for every fire attempt: gate-blocked, executed-and-failed, or
// executed-and-succeeded (FIND-34/P24.9). Extracted from Server.New's
// construction so the fire-time logic (permission gate + run recording) can
// be unit tested without standing up a full daemon. permCheck is a thunk
// rather than a fixed decision so each fire re-reads the daemon's *current*
// permission mode/rules, not whatever they were when the scheduler was
// constructed.
// notifyRun, when non-nil, is additionally invoked once per fire attempt with
// the same (status, output) pair written to the audit record — but only for a
// job that opted in via Job.Notify (P58.1). Without this, a scheduled job's
// result reached the user only if they remembered to call cron_history, which
// makes a scheduled digest or watch job pointless: the whole value of firing
// it unattended is being told the outcome without asking.
func newCronRunFunc(
	cronStore *cron.Store,
	taskMgr *task.Manager,
	runCronCmd func(ctx context.Context, command, dir string, emit func(string)) error,
	permCheck func(ctx context.Context, j cron.Job) (bool, string),
	notifyRun func(j cron.Job, status, output string),
	logger *slog.Logger,
) cron.RunFunc {
	return func(j cron.Job) {
		// Fire time is captured here, before the task-manager goroutine
		// starts, so the audit record's fired-at reflects when the scheduler
		// actually attempted the fire, not when the command happened to
		// finish.
		firedAt := time.Now()
		title := j.Title
		if title == "" {
			title = "cron: " + j.Command
		}
		_, _ = taskMgr.Start(task.Spec{Kind: "cron", Title: title}, func(ctx context.Context, emit func(string)) (string, error) {
			// Mirror everything sent to the task-manager's live emit into a
			// local buffer too, so a durable, truncated copy of the run's
			// combined stdout/stderr can be persisted to the cron_runs audit
			// log below — independent of the task-manager view, which isn't
			// cron-scoped or guaranteed to stay queryable by job over time.
			var outBuf strings.Builder
			capture := func(s string) {
				outBuf.WriteString(s)
				emit(s)
			}
			// Ends one fire attempt: persist the durable audit record and, for
			// an opted-in job, deliver the outcome out-of-band. Both take the
			// same (status, output) pair and fire at the same three points, so
			// they share one call site — a notification that disagreed with
			// the audit log would be worse than no notification.
			finishRun := func(status string) {
				out := outBuf.String()
				if err := cronStore.RecordRun(context.Background(), j.ID, firedAt, status, out); err != nil {
					logger.Error("cron: record run", "job", j.ID, "err", err)
				}
				if j.Notify && notifyRun != nil {
					notifyRun(j, status, out)
				}
			}

			// Cron jobs fire unattended, with no session and no human present
			// to resolve an approval prompt — so route the fire-time decision
			// through the identical permission gate stack interactive shell
			// calls get (mode + text allow/deny rules + contextual
			// egress/network policy), not just the coarse mode check
			// (P27.15/FIND-08 — extends FIND-03/P24.3's mode-only gate). A
			// job's auto_approve opt-in resolves any Ask-tier decision within
			// that stack since nothing can answer an interactive prompt here;
			// an explicit deny rule or a Deny-mode decision still blocks
			// regardless of auto_approve.
			if ok, reason := permCheck(ctx, j); !ok {
				blocked := fmt.Sprintf("cron job %q blocked: %s", j.Title, reason)
				logger.Warn("cron: job blocked by permission check", "job", j.ID, "reason", reason)
				capture(blocked)
				finishRun("blocked")
				return "", errors.New(blocked)
			}
			runErr := runCronCmd(ctx, j.Command, j.Workdir, capture)
			status := "ok"
			if runErr != nil {
				status = "error"
			}
			finishRun(status)
			return "", runErr
		})
	}
}

// cronNotifyExcerptBytes caps how much of a job's output is inlined into the
// human-readable notification message. A desktop notification is truncated by
// the OS well before this, but the same message also reaches webhook
// consumers that render it as a single line; the untruncated output travels
// separately in Event.Output.
const cronNotifyExcerptBytes = 280

// cronNotify delivers one cron fire outcome over the configured notification
// channels (P58.1). It reads s.notifier at call time rather than capturing it,
// because the cron.RunFunc is constructed in Server.New before the notifier
// field is assigned — and returns silently when no channel is configured,
// which is the common case.
func (s *Server) cronNotify(j cron.Job, status, output string) {
	if s.notifier == nil {
		return
	}
	title := j.Title
	if title == "" {
		title = j.Command
	}
	ev := notify.Event{
		JobID:  j.ID,
		Title:  title,
		Output: output,
	}
	switch status {
	case "ok":
		ev.Status = notify.StatusCompleted
		ev.Message = fmt.Sprintf("Cron job %q completed", title)
	case "blocked":
		ev.Status = notify.StatusBlocked
		ev.Message = fmt.Sprintf("Cron job %q was blocked by the permission gate", title)
	default:
		ev.Status = notify.StatusError
		ev.Message = fmt.Sprintf("Cron job %q failed", title)
	}
	// The output is the payload for a digest-style job, so lead the message
	// with a slice of it rather than making the user go read cron_history.
	if excerpt := cronOutputExcerpt(output); excerpt != "" {
		ev.Message += ": " + excerpt
	}
	s.notifier.Notify(context.Background(), ev)
}

// cronOutputExcerpt flattens a run's captured output into a single-line
// excerpt suitable for a notification body, or "" when there was no output.
func cronOutputExcerpt(output string) string {
	flat := strings.Join(strings.Fields(output), " ")
	if flat == "" {
		return ""
	}
	if len(flat) > cronNotifyExcerptBytes {
		// Trim on a rune boundary — a byte slice can split a multi-byte
		// character and produce U+FFFD in the notification.
		cut := flat[:cronNotifyExcerptBytes]
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		flat = cut + "…"
	}
	return flat
}

// cronPermCheck is the fire-time permission check for cron jobs
// (P27.15/FIND-08): it runs the job's command through the exact same gate
// stack buildGate assembles for every interactive engine run — mode, text
// allow/deny rules, and the contextual egress/network policy — evaluated
// against the real "shell" tool and the job's command, instead of the
// coarse mode-only check FIND-03/P24.3 originally added. No persona is
// passed (empty persona.Persona{}, matching sub-agent runs) since a cron job
// has no persona of its own. The approver resolves any Ask-tier decision
// from AutoApprove{} when the job opted in via auto_approve, or AutoDeny{}
// otherwise — mirroring the pre-P27.15 behavior where auto_approve was the
// only way past the mode-level Ask, now extended uniformly to every layer of
// the stack (e.g. the egress-then-write contextual rule) rather than just
// the top one.
func (s *Server) cronPermCheck(ctx context.Context, j cron.Job) (bool, string) {
	approver := permission.Approver(permission.AutoDeny{})
	if j.AutoApprove {
		approver = permission.AutoApprove{}
	}
	gate, _ := s.buildGate(s.cfg.Permission.Mode, approver, persona.Persona{})
	shellTool, ok := s.tools.Get("shell")
	if !ok {
		return false, "shell tool is not registered"
	}
	input, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: j.Command})
	if err != nil {
		return false, "failed to encode command for permission check: " + err.Error()
	}
	return gate.Check(ctx, shellTool, input)
}
