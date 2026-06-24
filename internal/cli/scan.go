package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/scottymacleod/agentharness/internal/security"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan [path]",
		Short: "Run available security scanners and print normalized findings",
		Long:  "Runs every installed scanner (semgrep, trivy, gitleaks) over the given path (default: current directory) and prints a unified findings report.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if _, err := os.Stat(abs); err != nil {
				return fmt.Errorf("path not found: %s", dir)
			}
			report := security.RunAll(cmd.Context(), abs, security.DefaultScanners())
			fmt.Fprintln(cmd.OutOrStdout(), report.Format())
			return nil
		},
	}
}
