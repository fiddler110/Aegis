package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/share"
	"github.com/fiddler110/aegis/internal/trace"
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
	cmd.AddCommand(newSessionsArchiveCmd())
	cmd.AddCommand(newSessionsUnarchiveCmd())
	cmd.AddCommand(newSessionsPruneCmd())
	return cmd
}

// dialClient builds an authenticated client for one CLI invocation. It
// returns before that command's work is done, so it cannot scrub the token
// itself (FIND-33/P24.21) — every call site below is a one-shot command that
// does `cl, err := dialClient()` and then `defer cl.Zero()` once the error
// check passes.
func dialClient() (*client.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cl, err := client.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot reach daemon at %s: %w (start it with: aegis serve)", cfg.Server.Addr, err)
	}
	if err := cl.Health(context.Background()); err != nil {
		return nil, fmt.Errorf("cannot reach daemon at %s: %w (start it with: aegis serve)", cfg.Server.Addr, err)
	}
	return cl, nil
}

func newSessionsListCmd() *cobra.Command {
	var showArchived bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			defer cl.Zero()
			var metas []api.SessionMeta
			if showArchived {
				metas, err = cl.ListArchivedSessions(cmd.Context())
			} else {
				metas, err = cl.ListSessions(cmd.Context())
			}
			if err != nil {
				return err
			}
			if len(metas) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no sessions yet")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tMODE\tSTATUS\tUPDATED\tTITLE")
			for _, m := range metas {
				title := m.Title
				if title == "" {
					title = "(untitled)"
				}
				status := "active"
				if m.Archived {
					status = "archived"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					m.ID[:8], m.Mode, status,
					m.UpdatedAt.Local().Format("2006-01-02 15:04"), title)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&showArchived, "archived", false, "include archived sessions")
	return cmd
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
			defer cl.Zero()
			sess, err := cl.GetSession(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			data, redactions, err := share.Render(sess, f)
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
			// P66.11: the redaction count is reported here as well as inside the
			// artifact, because this is where the user decides whether to send it.
			// A stdout export ("-") returns above without this line rather than
			// corrupting the document with it.
			fmt.Fprintf(cmd.OutOrStdout(), "exported %s → %s (%d credential-shaped value(s) redacted)\n", sess.ID, abs, redactions)
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
			defer cl.Zero()
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
			// P66.11/GAP-01 widened the record; this column is what makes it
			// readable without a JSON export. WHY holds the three things that
			// explain a turn nobody asked for — the stop reason, whether compaction
			// fired, and which corrective the engine injected — because they are
			// read together or not at all: "why did this run take 40 turns" is
			// answered by max_tokens and a continuation appearing on every row.
			fmt.Fprintln(tw, "TURN\tMODEL\tIN\tOUT\tCOST\tWALL\tTOOLS\tWHY")
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
				fmt.Fprintf(tw, "%d\t%s\t%d\t%d\t$%.4f\t%s\t%s\t%s\n",
					i+1, model, t.InputTokens, t.OutputTokens, t.CostUSD,
					formatMS(t.WallMS), formatTools(t.ToolCalls), formatWhy(t))
			}
			fmt.Fprintf(tw, "\t\t\t\t\t\t\t\n")
			fmt.Fprintf(tw, "TOTAL\t%d turns\t%d\t%d\t$%.4f\t\t\t\n", len(sess.Traces), totIn, totOut, totCost)
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

// formatWhy renders the P66.11 fields that explain a turn: its stop reason, the
// compaction event, the guard verdict, and the correctives the engine injected.
//
// Compact rather than complete, because this is one column of a per-turn row and
// the full record is a field away in the JSON export. The selection is the part
// worth arguing about: end_turn and tool_use are the normal stop reasons and print
// as nothing, while max_tokens is the one that explains a long run — so only the
// values that tell you something earn the width.
func formatWhy(t trace.TurnTrace) string {
	var parts []string
	if t.StopReason != "" && t.StopReason != string(provider.StopEndTurn) && t.StopReason != string(provider.StopToolUse) {
		parts = append(parts, t.StopReason)
	}
	if c := t.Compaction; c != nil {
		switch {
		case c.Applied && c.Summarized:
			parts = append(parts, fmt.Sprintf("compacted %d→%d msgs", c.MessagesBefore, c.MessagesAfter))
		case c.Applied:
			parts = append(parts, fmt.Sprintf("pruned -%d tok", c.FreedTokens))
		case c.Suppressed:
			parts = append(parts, "compaction deferred")
		default:
			// Over the trigger and the compactor had nothing to give: the case the
			// context-full notice exists for, worth naming here too.
			parts = append(parts, "compaction no-op")
		}
	}
	if g := t.Guard; g != nil {
		switch {
		case g.Passed:
			parts = append(parts, "guard ok")
		case g.Retrying:
			parts = append(parts, "guard fail→retry")
		default:
			parts = append(parts, "guard fail")
		}
	}
	if len(t.Correctives) > 0 {
		parts = append(parts, strings.Join(t.Correctives, "+"))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
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
		Short: "Permanently delete a session and all its checkpoints",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			defer cl.Zero()
			if err := cl.DeleteSession(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", args[0])
			return nil
		},
	}
}

func newSessionsArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive <id>",
		Short: "Archive a session (hidden from normal listing; data preserved)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			defer cl.Zero()
			if err := cl.ArchiveSession(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "archived %s\n", args[0])
			return nil
		},
	}
}

func newSessionsUnarchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unarchive <id>",
		Short: "Restore an archived session to active status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			defer cl.Zero()
			if err := cl.UnarchiveSession(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unarchived %s\n", args[0])
			return nil
		},
	}
}

func newSessionsPruneCmd() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete non-archived sessions older than N days",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			defer cl.Zero()
			resp, err := cl.PruneSessions(cmd.Context(), days)
			if err != nil {
				return err
			}
			if resp.Deleted == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no sessions matched (nothing pruned)")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "pruned %d session(s)\n", resp.Deleted)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 0, "delete sessions not updated in this many days (overrides server config)")
	return cmd
}
