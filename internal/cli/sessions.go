package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/share"
	"github.com/scottymacleod/aegis/internal/trace"
	"github.com/spf13/cobra"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sessions",
		Aliases: []string{"session"},
		Short:   "Manage stored sessions via the daemon",
	}
	cmd.AddCommand(newSessionsListCmd())
	cmd.AddCommand(newSessionsDeleteCmd())
	cmd.AddCommand(newSessionsExportCmd())
	cmd.AddCommand(newSessionsTraceCmd())
	return cmd
}

func dialClient() (*client.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cl := client.New(cfg.Server.Addr).WithTokenFile(cfg.AuthTokenPath())
	if err := cl.Health(context.Background()); err != nil {
		return nil, fmt.Errorf("cannot reach daemon at %s: %w (start it with: aegis serve)", cfg.Server.Addr, err)
	}
	return cl, nil
}

func newSessionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			metas, err := cl.ListSessions(cmd.Context())
			if err != nil {
				return err
			}
			if len(metas) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no sessions yet")
				return nil
			}
			for _, m := range metas {
				title := m.Title
				if title == "" {
					title = "(untitled)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %-5s  %s  %s\n",
					m.ID, m.Mode, m.UpdatedAt.Local().Format("2006-01-02 15:04"), title)
			}
			return nil
		},
	}
}

func newSessionsExportCmd() *cobra.Command {
	var format, out string
	cmd := &cobra.Command{
		Use:   "export <id>",
		Short: "Export a session as a shareable transcript (html, md, or json)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := share.ParseFormat(format)
			if err != nil {
				return err
			}
			cl, err := dialClient()
			if err != nil {
				return err
			}
			sess, err := cl.GetSession(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			data, err := share.Render(sess, f)
			if err != nil {
				return err
			}
			if out == "-" {
				_, err := cmd.OutOrStdout().Write(data)
				return err
			}
			if out == "" {
				out = fmt.Sprintf("aegis-session-%s.%s", shortID(sess.ID), f.Ext())
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return err
			}
			abs, _ := filepath.Abs(out)
			fmt.Fprintf(cmd.OutOrStdout(), "exported %s → %s\n", sess.ID, abs)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "html", "output format: html, md, or json")
	cmd.Flags().StringVar(&out, "out", "", "output file (default aegis-session-<id>.<ext>; use - for stdout)")
	return cmd
}

func newSessionsTraceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trace <id>",
		Short: "Print the per-turn trace (tokens, cost, tools, timing) for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			sess, err := cl.GetSession(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(sess.Traces) == 0 {
				fmt.Fprintln(out, "no trace recorded for this session")
				return nil
			}

			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TURN\tMODEL\tIN\tOUT\tCOST\tWALL\tTOOLS")
			var (
				totIn, totOut int
				totCost       float64
			)
			for i, t := range sess.Traces {
				totIn += t.InputTokens
				totOut += t.OutputTokens
				totCost += t.CostUSD
				model := t.Model
				if t.Estimated {
					model += " (est)"
				}
				fmt.Fprintf(tw, "%d\t%s\t%d\t%d\t$%.4f\t%s\t%s\n",
					i+1, model, t.InputTokens, t.OutputTokens, t.CostUSD,
					formatMS(t.WallMS), formatTools(t.ToolCalls))
			}
			fmt.Fprintf(tw, "\t\t\t\t\t\t\n")
			fmt.Fprintf(tw, "TOTAL\t%d turns\t%d\t%d\t$%.4f\t\t\n", len(sess.Traces), totIn, totOut, totCost)
			return tw.Flush()
		},
	}
}

// formatMS renders a millisecond duration compactly (e.g. "820ms", "3.4s").
func formatMS(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// formatTools summarizes a turn's tool calls as "name(820ms), name(1.2s)".
func formatTools(calls []trace.ToolCall) string {
	if len(calls) == 0 {
		return "-"
	}
	parts := make([]string, len(calls))
	for i, c := range calls {
		name := c.Name
		if c.IsError {
			name += "✗"
		}
		parts[i] = fmt.Sprintf("%s(%s)", name, formatMS(c.DurationMS))
	}
	return strings.Join(parts, ", ")
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func newSessionsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			if err := cl.DeleteSession(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", args[0])
			return nil
		},
	}
}
