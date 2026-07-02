package cli

// bg.go — "aegis bg" command group for background (detached) session management (P3.2).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/spf13/cobra"
)

func newBGCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bg",
		Short: "Manage background (detached) sessions",
	}
	cmd.AddCommand(newBGListCmd())
	cmd.AddCommand(newBGEventsCmd())
	return cmd
}

func newBGListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List sessions that have background mode enabled",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cl, stop, err := ensureDaemon(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer stop()

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			sessions, err := cl.ListSessions(ctx)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tMODE\tBACKGROUND\tUPDATED\tTITLE")
			for _, s := range sessions {
				if !s.Background {
					continue
				}
				bg := "yes"
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					s.ID[:8], s.Mode, bg,
					s.UpdatedAt.Format("2006-01-02 15:04"),
					truncate80(s.Title))
			}
			return w.Flush()
		},
	}
}

func newBGEventsCmd() *cobra.Command {
	var since int64
	cmd := &cobra.Command{
		Use:   "events <session-id>",
		Short: "Print buffered engine events from a background session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cl, stop, err := ensureDaemon(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer stop()

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			events, err := cl.GetBGEvents(ctx, args[0], since)
			if err != nil {
				return err
			}
			if len(events) == 0 {
				fmt.Println("no buffered events (since=" + strconv.FormatInt(since, 10) + ")")
				return nil
			}
			for _, e := range events {
				var ev api.Event
				if json.Unmarshal([]byte(e.Data), &ev) == nil {
					switch ev.Kind {
					case api.KindText:
						fmt.Print(ev.Text)
					case api.KindToolCall:
						fmt.Printf("\n[tool] %s\n", ev.Tool)
					case api.KindToolResult:
						fmt.Printf("[result] %s\n", truncate80(ev.ToolResult))
					case api.KindTurnDone:
						fmt.Printf("\n[done] in=%d out=%d\n", ev.InputTokens, ev.OutputTokens)
					case api.KindError:
						fmt.Printf("[error] %s\n", ev.Error)
					}
				}
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().Int64Var(&since, "since", 0, "return events with id > since")
	return cmd
}

func truncate80(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:77] + "..."
}
