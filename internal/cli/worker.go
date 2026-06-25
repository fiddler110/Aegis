package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/cost"
	"github.com/scottymacleod/aegis/internal/engine"
	"github.com/scottymacleod/aegis/internal/permission"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/providerfactory"
	"github.com/scottymacleod/aegis/internal/swarm"
	"github.com/scottymacleod/aegis/internal/tool"
	"github.com/scottymacleod/aegis/internal/tool/builtin"
	"github.com/spf13/cobra"
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
	adapter, err := providerfactory.Build(cfg)
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
