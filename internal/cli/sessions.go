package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/share"
	"github.com/spf13/cobra"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage stored sessions via the daemon",
	}
	cmd.AddCommand(newSessionsListCmd())
	cmd.AddCommand(newSessionsDeleteCmd())
	cmd.AddCommand(newSessionsExportCmd())
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
