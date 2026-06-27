package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/spf13/cobra"
)

func newParallelCmd() *cobra.Command {
	var mode string
	var autoApprove bool

	cmd := &cobra.Command{
		Use:   "parallel [flags] \"prompt A\" \"prompt B\" ...",
		Short: "Run several prompts as concurrent, independent sessions",
		Long: "Launches each prompt as its own daemon session and runs them concurrently — " +
			"e.g. fix tests in one, update docs in another. Progress is interleaved per run; " +
			"each session persists and can be resumed with `aegis --resume <id>`.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if mode == "" {
				mode = cfg.Permission.Mode
			}
			if mode == "" {
				mode = "build"
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			cl, stopDaemon, err := ensureDaemon(ctx, cfg)
			if err != nil {
				return err
			}
			defer stopDaemon()

			out := cmd.OutOrStdout()
			var stdoutMu sync.Mutex
			logf := func(format string, a ...any) {
				stdoutMu.Lock()
				fmt.Fprintf(out, format, a...)
				stdoutMu.Unlock()
			}

			results := make([]parallelResult, len(args))

			var wg sync.WaitGroup
			for i, prompt := range args {
				wg.Add(1)
				go func(i int, prompt string) {
					defer wg.Done()
					results[i] = runOneParallel(ctx, cl, i+1, prompt, mode, autoApprove, logf)
				}(i, prompt)
			}
			wg.Wait()

			fmt.Fprintln(out, "\nSummary:")
			var failed int
			for _, r := range results {
				status := "ok"
				if r.err != nil {
					status = "error: " + r.err.Error()
					failed++
				}
				id := r.sessionID
				if id == "" {
					id = "(no session)"
				}
				fmt.Fprintf(out, "  [%d] %-12s %s (%d tool calls)\n", r.idx, status, id, r.tools)
				if r.sessionID != "" {
					fmt.Fprintf(out, "       resume: aegis --resume %s\n", r.sessionID)
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d runs failed", failed, len(args))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "permission mode for all sessions: plan, build (default), or auto")
	cmd.Flags().BoolVar(&autoApprove, "yes", false, "auto-approve tool calls that require confirmation (else they are denied)")
	return cmd
}

// parallelResult is the outcome of one fan-out run.
type parallelResult struct {
	idx       int
	sessionID string
	tools     int
	err       error
}

// runOneParallel creates a session, posts the prompt, and drains its event
// stream, answering approval prompts and reporting progress.
func runOneParallel(ctx context.Context, cl *client.Client, idx int, prompt, mode string, autoApprove bool, logf func(string, ...any)) (res parallelResult) {
	res.idx = idx
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: mode})
	if err != nil {
		res.err = err
		return
	}
	res.sessionID = meta.ID
	logf("[%d] started → %s\n", idx, meta.ID)

	events, err := cl.PostMessage(ctx, meta.ID, prompt)
	if err != nil {
		res.err = err
		return
	}
	for ev := range events {
		switch ev.Kind {
		case api.KindToolCall:
			res.tools++
			logf("[%d] 🔧 %s\n", idx, ev.Tool)
		case api.KindApprovalRequest:
			actx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = cl.SendApproval(actx, meta.ID, ev.ApprovalID, autoApprove)
			cancel()
			if autoApprove {
				logf("[%d] ✓ approved %s\n", idx, ev.Tool)
			} else {
				logf("[%d] ✗ denied %s (use --yes to allow)\n", idx, ev.Tool)
			}
		case api.KindError:
			res.err = fmt.Errorf("%s", ev.Error)
			logf("[%d] error: %s\n", idx, ev.Error)
		}
	}
	logf("[%d] done (%d tool calls)\n", idx, res.tools)
	return
}

func newRunsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "runs",
		Short: "List message runs currently in flight across all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			runs, err := cl.ListRuns(cmd.Context())
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no active runs")
				return nil
			}
			for _, r := range runs {
				title := r.Title
				if title == "" {
					title = "(untitled)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %3d tools  %-12s  %s\n",
					r.SessionID, time.Since(r.StartedAt).Truncate(time.Second), r.Tools, r.LastKind, title)
			}
			return nil
		},
	}
}
