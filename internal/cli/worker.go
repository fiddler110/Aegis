package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
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

	output, runErr := executeWorker(ctx, spec)

	// Record the result durably so the parent can read it after we exit.
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
			Payload:   map[string]any{"error": errStr},
		})
	}
	return runErr
}

// executeWorker builds a sub-engine and runs the teammate to completion,
// returning its final text. Workers do not get the `agent` tool, so they are
// leaf nodes (no nested subprocess spawning).
func executeWorker(ctx context.Context, spec swarm.WorkerSpec) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	adapter, err := providerfactory.Build(cfg, nil)
	if err != nil {
		return "", err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL}); err != nil {
		return "", err
	}

	model := spec.Config.Model
	if model == "" {
		model = cfg.Provider.Model
	}
	var approver permission.Approver = permission.AutoDeny{}
	if cfg.Permission.AutoApproveExec {
		approver = permission.AutoApprove{}
	}
	gate := permission.New(permission.ParseMode(spec.Config.Mode), approver)

	eng, err := engine.New(engine.Options{
		Adapter:   adapter,
		Tools:     reg,
		Gate:      gate,
		Cost:      cost.NewTracker(),
		BudgetUSD: cfg.Cost.BudgetUSD,
		Model:     model,
		MaxTokens: cfg.Provider.MaxTokens,
	})
	if err != nil {
		return "", err
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
	return strings.TrimSpace(sb.String()), runErr
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
	db, err := sql.Open("sqlite", spec.SessionDBPath)
	if err != nil {
		return nil, func() {}
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, func() {}
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
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
