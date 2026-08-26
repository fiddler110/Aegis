package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/drive"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/ollamainfo"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/tool"
)

// chatDrive is the state the two drive-to-completion loops share. It exists so
// each loop is a method with a name rather than a branch two hundred lines deep
// inside a command closure.
type chatDrive struct {
	eng     *engine.Engine
	conv    *engine.Conversation
	onEvent func(engine.Event)
	logger  *slog.Logger
	errOut  io.Writer

	cwd         string
	pendingRoot string
	skillName   string
	skillDir    string
	skillRunDir string
	taskPrompt  string
	maxTurns    int

	driveToCompletion bool

	toolCalls     int
	iterToolCalls int
	iterMutations int
}

// runPhased drives a skill with a declared phase plan phase-by-phase in a fresh
// context each phase (P38.8 in-harness), which is what keeps peak context
// bounded on a local model — the single-context generic drive is what stalled
// the P38.1 build.
func (d *chatDrive) runPhased(ctx context.Context, cfg *config.Config, adapter provider.Adapter, reg *tool.Registry, phases []drive.Phase, driveModelMax int) error {
	fmt.Fprintf(d.errOut, "\n[notice: driving %s in phased mode — one bounded fresh context per phase (%d content phases + verify), the in-harness form of P38.8]\n", d.skillName, len(phases))
	d.logger.Info("chat: using phased skill drive (P38.8 in-harness)", "skill", d.skillName, "phases", len(phases))

	// P47.5(b): let a phase escalate the serving window toward the
	// model max on a context overflow. Each call doubles num_ctx
	// (bounded by the model max) by mutating the live Ollama adapter,
	// which the next Stream picks up — no engine rebuild. Nil/no-op
	// when the provider can't escalate (non-Ollama, or already at the
	// ceiling); the drive then falls back to the P47.2/P47.7
	// fresh-context reset alone.
	//
	// P59.7 connects the escalation to the engine's compaction
	// *trigger* (via ContextWindowFloor), so a phase that just gained room
	// stops compacting as if it hadn't. The summarizer's own *budget* still
	// stays at the sized window, which is a deliberate choice rather than the
	// same oversight: a larger num_ctx only buys physical headroom against a
	// transient overshoot, and sizing the summary request to it would spend the
	// new room on the recovery rather than on the work.
	var escalateWindow func() (int, bool)
	if driveModelMax > 0 {
		curWin := cfg.Provider.ContextWindow
		escalateWindow = func() (int, bool) {
			next, grew := drive.NextWindow(curWin, driveModelMax)
			if !grew {
				return curWin, false
			}
			if provider.RaiseContextWindow(adapter, next) {
				curWin = next
				return next, true
			}
			return curWin, false
		}
	}
	// P50.1: a liveness probe so the drive can wait out a
	// crashed/restarting local model server and resume from disk
	// instead of aborting. Unwraps the retry/failover decorators to
	// the base adapter; returns supported==false for a backend with
	// no probe (a cloud adapter), and the drive then never waits on it.
	checkBackend := func(hctx context.Context) (bool, bool) {
		return provider.CheckBackendHealth(hctx, adapter)
	}
	return drive.Run(ctx, &drive.State{
		Engine: d.eng, System: d.conv.System, OnEvent: d.onEvent, Logger: d.logger,
		ErrOut: d.errOut, Cwd: d.cwd, SkillName: d.skillName, SkillDir: d.skillDir,
		TaskPrompt: d.taskPrompt, MaxTurns: d.maxTurns, EscalateWindow: escalateWindow,
		RunDir:       drive.RunDirResolver(d.skillName, d.skillRunDir),
		CheckBackend: checkBackend, Progress: &drive.Progress{},
		IterToolCalls: &d.iterToolCalls, IterMutations: &d.iterMutations,
		// Safe here specifically because the CLI drive owns this
		// registry outright — narrowing is registry-wide, so a host
		// sharing one registry across concurrent sessions must not
		// wire this.
		ScopeTools: reg.ScopeExposed,
	}, phases)
}

// runLinear is the generic single-context drive (P38.2). A multi-phase skill
// (threat model, deep research) is many turns in one context; a plain one-shot
// chat stops at the first yield, so a model that pauses to ask "shall I
// proceed?" mid-build (the motivating failure) leaves a partial suite behind its
// unresolved `<!-- PENDING -->` stubs. When --skill preloaded such a skill, keep
// running while any file under .aegis/ still carries a PENDING marker: append a
// continuation turn and run again — reusing the SAME conversation so context
// threads (and pruning/compaction apply) across the whole drive. Bounded by
// --max-turns and the P39.7 no-progress guard: a turn that mutates no suite file
// and leaves the PENDING set unchanged is an "announce then yield" stall, so
// re-prompt with an explicit "act now" nudge (bounded) rather than burning
// tokens on narration.
//
// The oracle is the stub-first pattern the skill uses (SKILL.md §4.1): a PENDING
// marker is unambiguous unfinished work. If a model instead writes full file
// content without ever stubbing (observed on qwen3:14b, which skips the setup
// step), no markers appear and the drive simply ends when the model yields —
// correct when it finished, and a known limitation when it didn't, but never a
// wrong forced continuation.
//
// Without --skill this is a plain one-shot turn: the loop breaks after the first
// iteration.
func (d *chatDrive) runLinear(ctx context.Context) error {
	var runErr error
	noProgress := 0
	verifyRounds := 0
	qualityReviewed := false // P38.1: run the final quality pass at most once
	preambleCompacted := false
	var prevPending []string
	for iter := 0; ; iter++ {
		d.iterToolCalls = 0
		d.iterMutations = 0
		runErr = d.eng.Run(ctx, d.conv, d.onEvent)
		d.logger.Info("chat: run finished", "iter", iter, "err", errString(runErr), "tool_calls", d.toolCalls, "iter_tool_calls", d.iterToolCalls, "iter_mutations", d.iterMutations)
		if runErr != nil || !d.driveToCompletion || ctx.Err() != nil {
			break
		}
		// P39.5: after the opening turn the model has seen the full
		// SKILL.md. Re-sending its ~9K-token body in the first user
		// message every turn is the drive's context-bounding failure — on
		// a 32K local window (prompt_bytes≈31534 at turn 0) the recon
		// digest plus a few file reads then leave no room to edit_file (a
		// scaffolded resume made 86 tool calls across 3 iterations and
		// cleared 0 of 23 markers). Rewrite the first message once,
		// swapping the skill body for a compact pointer the model can
		// re-read on demand — the same disposable-skill-reference logic
		// P36.2 applies to skill-reference reads. Guarded so it only
		// touches the message while it still carries the preamble (engine
		// compaction may have already rewritten it).
		if !preambleCompacted {
			preambleCompacted = true
			if compactFirstSkillMessage(d.conv, d.skillName, d.taskPrompt, d.skillDir) {
				d.logger.Info("chat: compacted SKILL.md preamble out of first message (P39.5)", "skill", d.skillName)
			}
		}
		pending := scanPendingMarkers(d.pendingRoot)
		if len(pending) == 0 {
			// P39.6: "all markers filled" is not the real done-condition —
			// "verifies clean" is. Run the skill's bundled phase-6 checks
			// (verify.py, lint_dfd.py, inventory.py --check); on failure,
			// feed the failure text back for an in-place fix and re-run,
			// bounded. When the skill ships no verifier / there's nothing to
			// check, verifySkillOutputs reports ran=false and the drive ends
			// as before. This is the autonomous analogue of SKILL.md §5's
			// fix-and-re-run round.
			failures, ran := drive.VerifySkillOutputs(d.skillName, d.skillDir, d.cwd)
			if !ran {
				break // nothing to verify (skill has no verifier / no run dir) — done
			}
			if failures == "" {
				// Mechanical checks are clean. P38.1: run one substantive
				// quality-and-sanity pass the scripts can't do (groundedness,
				// filler, internal coherence), then re-verify. Bounded to a
				// single pass so it terminates; a regression it introduces is
				// caught by the mechanical fix loop above on the next iteration.
				if !qualityReviewed {
					qualityReviewed = true
					d.logger.Info("chat: mechanical checks clean, running final quality pass (P38.1)")
					d.conv.Append(provider.Message{
						Role:    provider.RoleUser,
						Content: []provider.Block{provider.TextBlock{Text: drive.QualityReviewPrompt()}},
					})
					continue
				}
				break // verified clean and quality-reviewed — done
			}
			if verifyRounds++; verifyRounds > drive.MaxVerifyRounds {
				d.logger.Warn("chat: verification still failing after max rounds", "rounds", drive.MaxVerifyRounds)
				fmt.Fprintf(d.errOut, "\n[notice: phase-6 verification still failing after %d fix round(s); stopping with an unverified suite — inspect the run directory and the failures above]\n", drive.MaxVerifyRounds)
				fmt.Fprintf(d.errOut, "%s\n", failures)
				break
			}
			d.logger.Info("chat: verification failed, feeding back for fix", "round", verifyRounds)
			d.conv.Append(provider.Message{
				Role:    provider.RoleUser,
				Content: []provider.Block{provider.TextBlock{Text: drive.VerifyFixPrompt(failures)}},
			})
			continue
		}
		if iter+1 >= d.maxTurns {
			msg := fmt.Sprintf("drive-to-completion hit --max-turns=%d with %d file(s) still PENDING: %s", d.maxTurns, len(pending), strings.Join(pending, ", "))
			d.logger.Warn("chat: " + msg)
			fmt.Fprintf(d.errOut, "\n[notice: %s — re-run to resume]\n", msg)
			break
		}
		// P39.7 no-progress guard. A weak local model sometimes ends a
		// turn with a plan ("Now I'll write the file…") and no file
		// mutation at all — the "announce then yield" stall (reproduced on
		// gpt-oss:20b and qwen3.6:35b-a3b: markers present, 0 edit_file,
		// yields repeatedly). Treat a turn that neither mutated a suite
		// file nor changed the PENDING set (the model may narrate, read,
		// or re-run recon without editing) as no progress. Direct evidence
		// this is the right lever: adding an "act now" preamble to a
		// stalled gpt-oss:20b run landed the first real edit_file (P38.1).
		// Instead of silently yielding a partial suite, re-prompt with an
		// explicit "act now — call edit_file, no narration" nudge, bounded
		// to drive.MaxNoProgressTurns consecutive stalls before stopping.
		// Extends P39.2's tool-execution coaching from the malformed-call
		// case to the no-call case.
		madeProgress := d.iterMutations > 0 || !drive.SameStrings(pending, prevPending)
		prevPending = pending
		nudge := ""
		if !madeProgress {
			if noProgress++; noProgress >= drive.MaxNoProgressTurns {
				fmt.Fprintf(d.errOut, "\n[notice: model stalled %d turns without mutating a file while %d file(s) remain PENDING; stopping — re-run to resume]\n", noProgress, len(pending))
				break
			}
			nudge = drive.ActNowNudge()
		} else {
			noProgress = 0
		}
		d.conv.Append(provider.Message{
			Role:    provider.RoleUser,
			Content: []provider.Block{provider.TextBlock{Text: nudge + continuePrompt(pending)}},
		})
	}
	return runErr
}

// installSignalCancel derives a cancellable context from parent that cancels
// when an interrupt (Ctrl-C) or SIGTERM arrives, logging which signal fired
// first. It returns the context and a cleanup func that stops signal delivery
// and cancels the context (which also lets the watcher goroutine exit, so it
// does not leak). Ctrl-C behavior is preserved — an interrupt still cancels
// the run.
//
// P35.8: the bare signal.NotifyContext(os.Interrupt) this replaces recorded
// nothing about which signal fired, so a signal-driven exit was
// indistinguishable from a silent mid-run death in aegis.log. syscall.SIGTERM
// is portable in Go's os/signal across win32 and unix.
func installSignalCancel(parent context.Context, logger *slog.Logger) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go watchSignal(ctx, cancel, sigCh, logger)
	return ctx, func() {
		signal.Stop(sigCh)
		cancel()
	}
}

// driveCompaction builds the proactive-compaction wiring for the CLI drive
// engine: the effective context window and a Summarizer over it. Split out of
// the command closure (P47.1) so a regression test can assert the CLI path
// enables compaction (non-zero window + non-nil compactor) — guarding against
// the divergence where the CLI engine.New set neither ContextWindowTokens nor
// Compactor, so a multi-turn --skill drive grew context every turn with no
// defense until the model server hard-rejected the request. Mirrors the
// daemon's build (internal/server/server.go / engine_build.go): prefer a fast
// small model for the summary calls, and skip auto-compaction (rather than
// defaulting to the 120k cloud budget) when a local window is still unknown.
func driveCompaction(ctx context.Context, cfg *config.Config, adapter provider.Adapter, logger *slog.Logger) (engine.Compactor, int) {
	ctxWin := resolveDriveContextWindow(ctx, cfg, logger)
	compModel := cfg.Provider.Model
	if cfg.Provider.SmallModel != "" {
		compModel = cfg.Provider.SmallModel // prefer a fast small model for compaction
	}
	compOpts := compaction.Options{
		Adapter:       adapter,
		Model:         compModel,
		ContextWindow: ctxWin,
		// P66.14: the trigger reserves room for the completion, so it needs the
		// same max_tokens the engine's own gate uses. Without it the summarizer
		// gates at a flat 85% while the engine gates at half the window, and a
		// drive's compactions land too late to leave room for an answer.
		MaxTokens: cfg.Provider.MaxTokens,
		// Mirrors the daemon: on a prefix-caching local backend the prune
		// pre-pass is gated on headroom rather than run unconditionally, since
		// rewriting the middle of the conversation there costs a full prefill
		// recompute. A phased drive is exactly the workload that measured it.
		//
		// Resolved through PreservePrefixCacheOr rather than from
		// config.LocalBackend directly, which is what this line used to do — and
		// which meant compaction.preserve_prefix_cache was silently ignored on
		// the CLI path. The escape hatch the daemon honoured did not exist on the
		// path phased drives actually run on, i.e. the one the gate was measured
		// against in the first place.
		PreservePrefixCache: cfg.Compaction.PreservePrefixCacheOr(
			config.LocalBackend(cfg.Provider.Default, cfg.Provider.BaseURL)),
		// P67.6: mirrors the daemon. 0 takes the package default; the pass
		// floors it at 1.
		ColdCacheKeep: cfg.Compaction.ColdCacheKeep,
	}
	// A local provider whose window is still unknown: skip auto-compaction
	// rather than defaulting to the 120k cloud budget, which on a 4k-32k local
	// server would never fire before the server front-truncates the prompt.
	if ctxWin == 0 && cfg.Provider.Default == "ollama" {
		compOpts.MaxBudget = 0 // explicit skip
	}
	return compaction.New(compOpts), ctxWin
}

// recommendPhasedDriveWindow resolves, for a phased --skill drive on an
// Ollama-backed provider, the serving context window to size the run to (P47.5a)
// and the model's training-context max (the P47.5b escalation ceiling). It
// recommends ollamainfo.RecommendContextWindow(model max) — half the model max,
// memory-capped — so the phased build gets the room it needs without the manual
// AEGIS_PROVIDER_CONTEXT_WINDOW bump the 2026-07-24 run required. ok is false
// when the provider isn't plausibly Ollama-backed or the server can't be
// probed, in which case the caller leaves the configured window untouched.
// Mirrors resolveDriveContextWindow's Ollama-target gate.
func recommendPhasedDriveWindow(ctx context.Context, cfg *config.Config) (win, modelMax int, ok bool) {
	p := cfg.Provider
	if p.Default != "ollama" && (p.Default != "openai" || p.BaseURL == "") {
		return 0, 0, false
	}
	res, detected := ollamainfo.Detect(ctx, ollamainfo.NativeBase(p.BaseURL), p.Model)
	if !detected {
		return 0, 0, false
	}
	return ollamainfo.RecommendContextWindow(res.ModelMax), res.ModelMax, true
}

// resolveDriveContextWindow returns the context window the model server will
// actually honor, for the one-shot CLI path. It mirrors the daemon's
// initContextWindow (internal/server/contextwindow.go): the configured
// provider.context_window, reconciled downward when a *loaded* Ollama model is
// found to be serving less (Ollama silently front-truncates an oversized prompt
// otherwise). Returns 0 ("unknown") only when neither config nor detection
// yields a value — the compaction fallback handles that case. Unlike the
// daemon there is no long-lived retry state to maintain: a single `aegis chat`
// invocation resolves the window once, up front.
func resolveDriveContextWindow(ctx context.Context, cfg *config.Config, logger *slog.Logger) int {
	cfgWin := cfg.Provider.ContextWindow
	p := cfg.Provider
	// Only probe when the target could plausibly be Ollama: the explicit
	// "ollama" provider, or an "openai" provider re-pointed at a custom base
	// URL (the documented way to run against a local OpenAI-compat endpoint).
	if p.Default != "ollama" && (p.Default != "openai" || p.BaseURL == "") {
		return cfgWin
	}
	res, ok := ollamainfo.Detect(ctx, ollamainfo.NativeBase(p.BaseURL), p.Model)
	if !ok {
		return cfgWin
	}
	switch {
	case cfgWin > 0 && res.Authoritative() && res.ContextWindow < cfgWin:
		// Config promises more than Ollama is actually serving — trusting the
		// config here is exactly the silent-truncation failure. Serve reality.
		logger.Warn("configured context_window exceeds what Ollama is serving; using the served value",
			"configured", cfgWin, "served", res.ContextWindow,
			"hint", "raise OLLAMA_CONTEXT_LENGTH on the Ollama server or pin num_ctx in a modelfile")
		return res.ContextWindow
	case cfgWin > 0:
		return cfgWin
	default:
		logger.Info("auto-detected Ollama context window for drive engine", "window", res.Describe(), "model_max", res.ModelMax)
		return res.ContextWindow
	}
}

// watchSignal blocks until either a signal arrives (logs it, then cancels) or
// the context is done (run completed / cleanup called — nothing to log, just
// return so the goroutine does not leak). Split out from installSignalCancel so
// the log-and-cancel behavior is unit-testable without delivering a real OS
// signal.
func watchSignal(ctx context.Context, cancel context.CancelFunc, sigCh <-chan os.Signal, logger *slog.Logger) {
	select {
	case sig := <-sigCh:
		if logger != nil {
			// P35.8: log the signal cause BEFORE cancelling the run.
			logger.Warn("chat: received signal, cancelling run", "signal", sig.String())
		}
		cancel()
	case <-ctx.Done():
	}
}

// driveContextFloor is the served Ollama context window (tokens) below which a
// --skill drive on the compat path is warned about (P39.9). It matches the
// num_ctx the reference wrapper had to bake for the 35B MoE re-test: a
// skill-driven prompt reached ~34774 tokens, overflowing the stock 16384
// modelfile default.
const driveContextFloor = 32768

// warnCompatDriveWindow probes the served Ollama context window and, when a
// --skill drive is about to run on the OpenAI-compat (/v1) adapter with a window
// too small to hold a skill-driven prompt, prints a notice naming the fix
// (P39.9). Best-effort: a probe failure yields a softer notice (the compat path
// never honors context_window regardless), never an error.
func warnCompatDriveWindow(w io.Writer, cfg *config.Config) {
	if !providerfactory.IsLegacyOllamaCompat(cfg.Provider) {
		return
	}
	dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, ok := ollamainfo.Detect(dctx, ollamainfo.NativeBase(cfg.Provider.BaseURL), cfg.Provider.Model)
	served := 0
	want := driveContextFloor
	if ok {
		served = res.ContextWindow
		if rec := ollamainfo.RecommendContextWindow(res.ModelMax); rec > want {
			want = rec
		}
	}
	if msg := compatDriveWindowNotice(cfg.Provider.Model, served, want); msg != "" {
		fmt.Fprintf(w, "\n[notice: %s]\n", msg)
	}
}

// compatDriveWindowNotice is the pure decision behind warnCompatDriveWindow: it
// returns the notice text when the served window (served, 0 = undetectable) is
// below want, or "" when the served window is already large enough. Split out so
// the threshold and message are testable without a live Ollama server.
func compatDriveWindowNotice(model string, served, want int) string {
	if served >= want && served > 0 {
		return "" // served window already holds a skill-driven prompt
	}
	servedDesc := "unknown (could not probe the Ollama server)"
	if served > 0 {
		servedDesc = fmt.Sprintf("%d tokens", served)
	}
	notice := fmt.Sprintf("this --skill drive is on the /v1 compat adapter, which cannot send num_ctx; the served context window is %s and a skill-driven prompt can exceed it (Ollama then silently truncates it from the front). Switch to provider.default: ollama so context_window is honored", servedDesc)
	if recipe := providerfactory.LegacyOllamaModelfileRecipe(model, want); recipe != "" {
		notice += " — or " + recipe
	}
	return notice
}

// mutatingTools are the built-in tools whose successful call mutates a suite
// file on disk. The P39.7 no-progress guard uses a call to one of these (or a
// change in the PENDING marker set) as the signal that a drive turn made real
// progress, as opposed to only narrating, reading, or re-running recon. Kept
// deliberately narrow to the file-writing tools: recon/scaffold create files
// via `shell` (python), but those turns are still caught as progress because
// they change the PENDING set.
var mutatingTools = map[string]bool{
	"write_file": true,
	"edit_file":  true,
	"multi_edit": true,
}

// continuePrompt is the drive-to-completion continuation turn (P38.2): it names
// the files still carrying `<!-- PENDING -->` markers and tells the model to
// resume in dependency order without pausing to ask, matching the resume
// contract in the threat-modeling skill's SKILL.md §4.2. Only called with a
// non-empty list (the drive loop stops when no markers remain).
func continuePrompt(pending []string) string {
	return "Continue — the task is not finished. These files still contain `<!-- PENDING: … -->` markers and must be completed:\n- " +
		strings.Join(pending, "\n- ") +
		"\n\nResume from the first unfinished file in dependency order and keep working until NO `<!-- PENDING` marker remains in any file. Each marker is section-keyed (`<!-- PENDING: <section> -->`): edit that exact marker one at a time — never a bare `<!-- PENDING -->` and never `replace_all` on a marker, which would overwrite every section at once. Fill ONE section per `edit_file`; do not write a whole file or several sections in a single call — a monolithic write is slow and can truncate into a malformed edit. This is a non-interactive run: do not stop to ask whether to proceed, and do not return a partial result."
}
