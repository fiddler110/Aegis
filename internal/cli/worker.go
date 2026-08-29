package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/server"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

// newWorkerCmd builds the hidden headless-worker command used by the swarm
// SubprocessBackend. It runs one sub-agent to completion from a spec file and
// records the result in the teammate's mailbox.
func newWorkerCmd() *cobra.Command {
	var specPath string
	cmd := &cobra.Command{
		Use:           "__worker",
		Short:         "Internal: run a headless sub-agent worker",
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorker(cmd.Context(), specPath)
		},
	}
	cmd.Flags().StringVar(&specPath, "spec", "", "path to the worker spec JSON file")
	return cmd
}

func runWorker(ctx context.Context, specPath string) error {
	if specPath == "" {
		return fmt.Errorf("worker: --spec is required")
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("worker: read spec: %w", err)
	}
	var spec swarm.WorkerSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("worker: parse spec: %w", err)
	}

	output, snap, runErr := executeWorker(ctx, spec)

	// Record the result durably so the parent can read it after we exit.
	// cost_usd/tokens (P10.3) let SubprocessBackend fold this worker's actual
	// spend into the shared fan-out tracker once it exits, so a sibling
	// spawned afterward computes its own remaining budget against the
	// updated total instead of the daemon's full configured cap again.
	if mb, e := swarm.OpenMailbox(spec.MailboxRoot, spec.Identity); e == nil {
		errStr := ""
		if runErr != nil {
			errStr = runErr.Error()
		}
		_ = mb.Send(swarm.Message{
			Type:      swarm.MsgResult,
			Sender:    spec.Identity.AgentID,
			Recipient: spec.Config.ParentSessionID,
			Text:      output,
			Payload: map[string]any{
				"error":    errStr,
				"cost_usd": snap.TotalUSD,
				"tokens":   snap.Usage.InputTokens + snap.Usage.OutputTokens + snap.Usage.CacheCreationTokens + snap.Usage.CacheReadTokens,
			},
		})
	}
	return runErr
}

// resolveWorkerCwd picks the directory a subprocess worker's tools and
// sandbox root against (P25.8). A subprocess worker starts a whole separate
// process with its own cwd — inherited from the daemon's own working
// directory, not necessarily the spawning session's — so without this a
// subprocess-backend teammate silently operated in the daemon root
// regardless of the parent session's workdir, the exact failure mode P25.1
// fixed for top-level sessions. specWorkdir is threaded through explicitly
// by the spawning SpawnConfig; falls back to processCwd if unset or if the
// directory no longer exists.
func resolveWorkerCwd(processCwd, specWorkdir string, logger *slog.Logger) string {
	if specWorkdir == "" {
		return processCwd
	}
	if info, err := os.Stat(specWorkdir); err == nil && info.IsDir() {
		return specWorkdir
	}
	logger.Warn("worker: spec workdir does not exist, falling back to process cwd", "workdir", specWorkdir)
	return processCwd
}

// executeWorker builds a sub-engine and runs the teammate to completion,
// returning its final text and its own cost.Snapshot (P10.3 — the caller
// reports this back to the parent via the mailbox so a sibling spawned
// afterward sees the updated shared spend). Workers do not get the `agent`
// tool, so they are leaf nodes (no nested subprocess spawning).
func executeWorker(ctx context.Context, spec swarm.WorkerSpec) (string, cost.Snapshot, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", cost.Snapshot{}, err
	}
	adapter, err := providerfactory.Build(cfg, nil, providerfactory.WithModelCaps(cfg.OpenModelCaps()))
	if err != nil {
		return "", cost.Snapshot{}, err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	processCwd, err := os.Getwd()
	if err != nil {
		return "", cost.Snapshot{}, err
	}
	cwd := resolveWorkerCwd(processCwd, spec.Config.Workdir, logger)
	// Reconstruct the daemon's configured sandbox backend instead of running
	// this worker's shell tool directly on the host: the subprocess backend is
	// sold as giving real OS-level isolation, but without this it actually
	// provided *less* isolation than the in-process backend, and its default
	// (no-Sandbox) local exec never stripped provider API keys from the shell
	// env either (P10.2). Errors are non-fatal here — a worker is a background
	// process with no interactive operator to show a strict-mode error to, so
	// it logs and falls back rather than failing the whole spawn.
	workerSandbox, _, fallbackReason, sbErr := server.SelectSandbox(cfg.Sandbox, cwd, logger)
	if sbErr != nil {
		logger.Warn("worker: sandbox selection failed, running unsandboxed", "err", sbErr)
		workerSandbox = nil
	} else if fallbackReason != "" {
		logger.Warn("worker: sandbox fallback", "reason", fallbackReason)
	}
	if workerSandbox != nil {
		// P60.2: a persistent container is owned state, and this worker is the
		// process that owns it. Without this the container would outlive the
		// worker until its TTL — recoverable (the reaper finds it once this pid
		// is gone) but wasteful, and one leaked container per spawned teammate
		// adds up fast.
		defer workerSandbox.Close()
	}

	reg := tool.NewRegistry()
	// P62.10: carry the daemon's prompt profile and option set across the
	// process boundary, like the gate stack (P10.1) and the sandbox (P10.2).
	regOpts := enginecfg.BuiltinOptions(cfg, cwd)
	regOpts.Sandbox = workerSandbox
	// LocalProfile is decided by enginecfg.BuiltinOptions (P62.10/P66.13): this
	// worker reconstructs cfg from disk and then has to actually consult it —
	// without that it registered the cloud surface regardless of the configured
	// model, so a teammate spawned under a local model paid ~1,300 schema tokens
	// the daemon had already decided that model should not spend. Nothing becomes
	// unreachable — the profile defers rather than removes, and tool_search loads
	// any of it on demand.
	if err := builtin.Register(reg, regOpts); err != nil {
		return "", cost.Snapshot{}, err
	}

	model := spec.Config.Model
	if model == "" {
		model = cfg.Provider.Model
	}
	var approver permission.Approver = permission.AutoDeny{}
	if cfg.Permission.AutoApproveExec {
		approver = permission.AutoApprove{}
	}
	// Same gate stack as every other engine Aegis builds (enginecfg.BuildGate,
	// P10.1/P66.13): a bare mode gate here let a subprocess teammate route
	// straight around an operator's egress-then-write policy or deny rule,
	// exactly the bypass P10.1 closed for in-process sub-agents — this backend
	// needed the identical fix since it builds its engine in a wholly separate
	// process with no access to the daemon's live Server state. It had been
	// hand-rolling three of the five layers, and had drifted two behind: the
	// per-task write scope (P46.1) and the persona-tool gate were absent, so a
	// `scope` call in a subprocess teammate silently confined nothing.
	//
	// cfg.Permission.Rules covers persisted rules (project/global config); a
	// rule added via an "allow always" approval that hasn't been persisted yet
	// is the one gap a separate process can't see. A subprocess teammate has no
	// persona of its own, so the persona layers stay inert here — but they are
	// now inert by the stack's own rule rather than by absence.
	gate, engineHooks := enginecfg.BuildGate(enginecfg.GateOptions{
		Mode:     spec.Config.Mode,
		Approver: approver,
		Security: cfg.Security,
		Registry: reg,
		Rules:    enginecfg.ConfigRules(cfg, logger),
		Hooks:    enginecfg.EngineHooks(enginecfg.ExecHooks(cfg, logger)),
		Logger:   logger,
	})

	tracker := cost.NewTracker()
	opts := engine.Options{
		Adapter:        adapter,
		Tools:          reg,
		Gate:           gate,
		Hooks:          engineHooks,
		Cost:           tracker,
		Purpose:        provider.PurposeSubAgent, // P67.3
		RoundResultCap: roundCapFor(cwd),         // P67.1
		Model:          model,
		ExtraRoots:     driveExtraRoots(cwd, cfg, logger),
	}
	// P66.13/ARCH-06: one shared reading of the run bounds. The WorkerSpec's
	// remaining-allowance computation carries exactly two dimensions, so those
	// two are replaced when the parent computed them and every other bound —
	// generated tokens, wall clock, stall, iterations, loop threshold, secret
	// redaction, cold cache — is inherited whole. Elapsed time in particular
	// isn't divisible across siblings the way spend is.
	enginecfg.CostLimits(cfg).
		WithRemainingAllowance(spec.RemainingBudgetUSD, spec.RemainingTokens).
		Apply(&opts)
	// A subprocess teammate talks to the same model server as its parent, so it
	// inherits the sampling parameters, the tool-calling fallback (P53.6 — a
	// shim that applied only to the parent would leave every spawned agent
	// unable to act) and the backend identification (P66.14/LLM-03).
	enginecfg.ModelBackend(cfg).Apply(&opts)
	eng, err := engine.New(opts)
	if err != nil {
		return "", cost.Snapshot{}, err
	}

	ctx = swarm.WithParentMode(ctx, spec.Config.Mode)
	if snap, closeDB := openCheckpointSnapshotter(spec); snap != nil {
		defer closeDB()
		ctx = checkpoint.WithSnapshotter(ctx, snap)
	}
	conv := &engine.Conversation{System: spec.Config.SystemPrompt}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: spec.Config.Prompt}}})

	const maxOutput = 1 << 20 // 1 MiB
	var sb strings.Builder
	runErr := eng.Run(ctx, conv, func(ev engine.Event) {
		if ev.Kind == engine.KindText && sb.Len() < maxOutput {
			sb.WriteString(ev.Text)
		}
	})
	return strings.TrimSpace(sb.String()), tracker.Snapshot(), runErr
}

// openCheckpointSnapshotter reconstructs a Snapshotter for spec's checkpoint
// (P9), if the parent daemon has checkpointing configured and this worker was
// spawned mid-turn. The in-process swarm backend gets this for free — its ctx
// keeps the same Snapshotter value all the way down to a spawned sub-agent —
// but this worker started an entirely separate process with its own ctx
// tree, so it opens its own connection to the same session database the
// daemon uses and records file writes against the same checkpoint id. Errors
// opening that connection are non-fatal: the worker still runs normally,
// just without this turn's file writes being restorable by /rewind.
//
// Returns (nil, no-op) when spec carries no checkpoint id or db path — the
// common case for a daemon with checkpointing disabled, or a spawn that
// wasn't captured with one (e.g. no in-flight Snapshotter on the parent ctx).
func openCheckpointSnapshotter(spec swarm.WorkerSpec) (*checkpoint.Snapshotter, func()) {
	if spec.Config.CheckpointID == "" || spec.SessionDBPath == "" {
		return nil, func() {}
	}
	// busy_timeout rides on the DSN, not a `PRAGMA busy_timeout` Exec (P63.4):
	// unlike journal_mode=WAL it is per-connection state that is not persisted
	// in the database file, so an Exec covers only whichever pooled connection
	// served it and any connection opened later reverts to the immediate-fail
	// default. modernc.org/sqlite applies `_pragma=` params to every new
	// connection. Same value and mechanism as session/longmem/knowledge.
	db, err := sql.Open("sqlite", spec.SessionDBPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, func() {}
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, func() {}
	}
	store, err := checkpoint.NewStore(db)
	if err != nil {
		_ = db.Close()
		return nil, func() {}
	}
	return store.NewSnapshotter(spec.Config.CheckpointID), func() { _ = db.Close() }
}
