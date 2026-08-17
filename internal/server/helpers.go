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
	"sync"
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

// localContextFilesMaxBytes caps the project context files (AGENTS.md,
// CLAUDE.md, .aegis/context.md) injected under the local prompt profile
// (P66.7, LLM-01). Before this they were the one always-injected block with no
// bound at all, which is how the most carefully budgeted prompt in the project
// came to be blown by the file documenting the budget.
//
// The number is derived, not measured off one repository — LLM-01's 11,611
// tokens were this repo's CLAUDE.md at the time, and it reads 2,560 tokens
// today, so sizing against it would be sizing against a number that moves.
// The derivation:
//
//   - The served window under the documented local configuration is 32,768
//     (docs/providers.md pins num_ctx in a Modelfile). The always-injected
//     prefix should fit in a quarter of it — 8,192 tokens — so the three
//     quarters left for the transcript are what the LLM-16 50%-of-window
//     notice is spent reporting on, rather than the prefix tripping it alone.
//   - Of that quarter, localBasePromptCeilingTokens already claims 4,550
//     (persona blocks + <deferred_tools> + tool schemas) and localRepoMapMaxBytes
//     another ~1,000. That leaves ~2,600 tokens ≈ 10,400 bytes.
//   - 8,000 bytes (~2,000 tokens at tokenest's ASCII rate) takes that with
//     room to spare, and lands on exactly twice localRepoMapMaxBytes — which
//     is the ordering worth stating: hand-written project instructions are
//     worth more per byte than a generated repo map, so they get double its
//     room, while still staying under half the base-prompt ceiling.
//
// Nothing here rescues Ollama's *default* 4,096-token window: the base prompt
// alone exceeds it, which is a fact about the default, not about this cap. The
// cap's job is to stop context files from being the multiplier on top.
//
// The default profile never applies this cap, exactly as with the repo map.
const localContextFilesMaxBytes = 8000

// promptSection is one named block of the assembled system prompt (P67.2).
//
// The prompt already has a size invariant — localBasePromptCeilingTokens fails
// the suite when the local base prompt grows — but it had no *stability*
// invariant, which is the axis that costs more per turn. Anything that varies
// turn to turn (a timestamp, a running cost figure, a count that moves with
// session state) assembled into the system prefix breaks the provider's prompt
// cache on every single turn, and shows up only as unexplained prefill cost.
// Naming the blocks and forcing each one to declare which kind it is does most
// of the enforcement; TestPromptSections_StabilityInvariant catches the rest.
type promptSection struct {
	name  string
	build func() string
	// volatile marks a section that is deliberately recomputed every turn.
	// rationale is the written justification volatileSection requires — the
	// point of the constructor is that adding a per-turn block costs a
	// sentence of explanation, in the file, at the call site.
	volatile  bool
	rationale string
}

// stableSection declares a block whose text is fixed for the life of a
// conversation. It is computed once per session and memoized (see
// Server.sectionText), so a stable section that is secretly not stable would
// serve stale text rather than merely costing cache misses — which is why the
// stability test treats an undeclared difference as a failure and not a
// warning.
func stableSection(name string, build func() string) promptSection {
	return promptSection{name: name, build: build}
}

// volatileSection declares a block that must be recomputed on every turn, and
// takes the reason as a required argument (P67.2). An empty justification is a
// programming error, not a runtime condition: the list is built on every
// effectiveSystem call and by the tests, so the panic fires the first time the
// section is constructed rather than in production only.
func volatileSection(name, justification string, build func() string) promptSection {
	if strings.TrimSpace(justification) == "" {
		panic("server: volatile prompt section " + name + " needs a written justification (P67.2)")
	}
	return promptSection{name: name, build: build, volatile: true, rationale: justification}
}

// promptSectionKey keys the per-conversation memo for stable sections.
//
// Getting this key right matters more than the cache does: a wrong key serves
// one session the other's prompt, which is worse than recomputing. sessionID is
// in it because almost every section reads session-scoped state — the session's
// workdir, its skill activations, its tool-registry clone — so a process-global
// memo would be plainly wrong. local is in it because the memoized sections are
// the three persona blocks and the debate block, and the persona blocks are
// selected by the local prompt profile, which is derived from
// provider.base_url; the daemon does not rewrite config at runtime, but keying
// on the one input those sections actually branch on costs nothing and removes
// the question.
type promptSectionKey struct {
	section string
	local   bool
}

// sectionText renders one section, memoizing it per conversation when the
// section is stable. The memo is dropped with the rest of a session's in-memory
// state in handleDeleteSession, which is also what bounds it: entries are a few
// KB per live session and never outlive the session.
func (s *Server) sectionText(sessionID string, sec promptSection, local bool) string {
	if sec.volatile {
		return sec.build()
	}
	v, _ := s.promptSectionCache.LoadOrStore(sessionID, &sync.Map{})
	perSession := v.(*sync.Map)
	key := promptSectionKey{section: sec.name, local: local}
	if cached, ok := perSession.Load(key); ok {
		return cached.(string)
	}
	text := sec.build()
	perSession.Store(key, text)
	return text
}

// promptSections lists the system prompt's blocks in assembly order, each
// declared stable (memoized for the conversation) or volatile with a written
// reason (P67.2). effectiveSystem renders this list; the stability test walks
// the same list, so neither can drift from the other.
//
// The split came out asymmetric, and that is the honest answer rather than a
// missed optimization: only the config-derived prose blocks are safe to memoize.
// Every other block reads state Aegis itself mutates mid-conversation, and
// correctness beats the cache — a section that cannot be memoized safely is
// declared volatile with the reason, not served stale.
func (s *Server) promptSections(base, sessionID string) []promptSection {
	local := s.cfg.Provider.LocalPromptProfile()
	workdir := s.workdirFor(sessionID)
	return []promptSection{
		volatileSection("persona system prompt",
			"not a computed block at all — it is the caller's argument, and the session's system prompt is swapped by /persona mid-conversation while persona files hot-reload under a fixed name.",
			func() string { return base }),
		stableSection("tool-use block", func() string { return persona.ToolUseBlockFor(local) }),
		stableSection("completing-tasks block", func() string { return persona.CompletingTasksBlockFor(local) }),
		stableSection("platform block", func() string { return persona.PlatformBlockFor(local) }),
		// Note the posture difference from the repo map below: an over-cap repo map
		// is dropped whole, because it is generated, ranked and degrades gracefully
		// to nothing. Context files are the project's instructions — dropping them
		// whole would change how the session behaves, so they are truncated
		// head-first with a notice instead. See localContextFilesMaxBytes and
		// memory.LoadContextCapped.
		volatileSection("memory: context files",
			"AGENTS.md, CLAUDE.md and .aegis/context.md are re-read from disk each turn; the user edits them and the agent writes them, and a session running against instructions the file no longer carries is a worse failure than a cache miss.",
			func() string {
				contextBudget := 0
				if local {
					contextBudget = localContextFilesMaxBytes
				}
				return s.memory.LoadContextCapped(contextBudget)
			}),
		volatileSection("memory: project/user",
			"the `memory` tool appends facts during the run; memoizing would hide from the next turn the very fact the model just chose to remember.",
			func() string { return s.memory.Load() }),
		volatileSection("<skills_available>",
			"activateSessionSkill layers a built-in skill onto this session mid-conversation (e.g. /threat-model), and project/user skill files are picked up without a restart.",
			func() string {
				return skills.BuildIndex(workdir, s.cfg.DataDir, s.sessionEnabledSkills(sessionID))
			}),
		volatileSection("<repo_map>",
			"loadRepoMap rebuilds the map when the workspace has changed since indexing, which is exactly what the agent spends the conversation doing; a memoized map describes the repository as it was at the first turn.",
			func() string {
				repoMap := s.repoMapFor(workdir)
				if repoMap == "" || (local && len(repoMap) > localRepoMapMaxBytes) {
					return ""
				}
				return repoMap
			}),
		volatileSection("<deferred_tools>",
			"the inventory is the complement of what is exposed, and that moves within a conversation: tool_search loads a deferred tool, persona preloading exposes more, and MCP's tools/list_changed rewrites the parent registry that every clone shares.",
			func() string { return deferredToolsBlock(s.toolRegistryFor(sessionID)) }),
		stableSection("debate block", func() string { return debateIntegrationBlock(s.cfg.Security.Debate) }),
	}
}

// effectiveSystem combines the session's base system prompt with platform
// context, loaded project/user memory, skills, and context files (AGENTS.md,
// CLAUDE.md). sessionID selects which on-demand-activated built-in skills
// (see activateSessionSkill) are layered on top of the persistently
// configured set; pass "" where no session is in scope (e.g. tests).
//
// The blocks and their order live in promptSections (P67.2); this function is
// only the join, so that what goes into the prompt and how it is assembled stay
// one list rather than two.
func (s *Server) effectiveSystem(base, sessionID string) string {
	local := s.cfg.Provider.LocalPromptProfile()
	var parts []string
	for _, sec := range s.promptSections(base, sessionID) {
		if text := s.sectionText(sessionID, sec, local); text != "" {
			parts = append(parts, text)
		}
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
			// P67.3: an MCP server's sampling request is somebody else's call
			// made on our credentials and our backend. It gets the fail-fast
			// policy — the alternative is an external server's retry loop
			// multiplied by ours during a capacity window.
			Purpose: provider.PurposeSampling,
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
