package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCronCmd is the operator-facing review view for persisted cron jobs
// (P27.15/FIND-08): jobs are otherwise only visible to the model itself via
// the cron_list tool, so an operator auditing what fires unattended — in
// particular which jobs carry auto_approve, since those bypass interactive
// approval entirely at fire time — had no way to see them without asking the
// model to run a turn.
func newCronCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Inspect persisted cron jobs (background scheduled tasks)",
	}
	cmd.AddCommand(newCronListCmd())
	return cmd
}

func newCronListCmd() *cobra.Command {
	var autoApproveOnly bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cron jobs, flagging which ones carry auto_approve",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := dialClient()
			if err != nil {
				return err
			}
			defer cl.Zero()
			jobs, err := cl.ListCronJobs(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			nShown := 0
			for _, j := range jobs {
				if autoApproveOnly && !j.AutoApprove {
					continue
				}
				nShown++
				state := "enabled"
				if !j.Enabled {
					state = "disabled"
				}
				approve := ""
				if j.AutoApprove {
					approve = "  [AUTO_APPROVE — fires unattended, bypassing interactive approval]"
				}
				if j.Notify {
					approve += "  [NOTIFY — delivers its output over the configured notify channels]"
				}
				if !j.Confirmed {
					approve += "  [UNCONFIRMED — created from a non-interactive surface; will not fire until cron_confirm]"
				}
				title := j.Title
				if title == "" {
					title = "(untitled)"
				}
				fmt.Fprintf(out, "%s  %-8s  %-14s  %s  %s%s\n", j.ID, state, j.Schedule, j.Command, title, approve)
			}
			if nShown == 0 {
				if autoApproveOnly {
					fmt.Fprintln(out, "no cron jobs with auto_approve set")
				} else {
					fmt.Fprintln(out, "no cron jobs")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&autoApproveOnly, "auto-approve-only", false, "show only jobs with auto_approve set")
	return cmd
}
