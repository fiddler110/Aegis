package cli

import (
	"context"
	"fmt"

	"github.com/scottymacleod/agentharness/internal/client"
	"github.com/scottymacleod/agentharness/internal/config"
	"github.com/spf13/cobra"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage stored sessions via the daemon",
	}
	cmd.AddCommand(newSessionsListCmd())
	cmd.AddCommand(newSessionsDeleteCmd())
	return cmd
}

func dialClient() (*client.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cl := client.New(cfg.Server.Addr)
	if err := cl.Health(context.Background()); err != nil {
		return nil, fmt.Errorf("cannot reach daemon at %s: %w (start it with: harness serve)", cfg.Server.Addr, err)
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
