package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Run available security scanners and print normalized findings",
		Long:  "Runs every installed scanner (semgrep, trivy, gitleaks, kubescape, hadolint) over the given path (default: current directory) and prints a unified findings report. Falls back to a configured container image (security.tools.<name>.image) for any scanner not installed on PATH.",
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
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			report := security.RunWithOptions(cmd.Context(), abs, security.DefaultScanners(), security.OptionsFromConfig(cfg.Security))
			fmt.Fprintln(cmd.OutOrStdout(), report.Format())
			return nil
		},
	}
	cmd.AddCommand(newScanImageCmd())
	return cmd
}

func newScanImageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "image <ref>",
		Short: "Scan a container image for vulnerabilities and best-practice violations",
		Long: "Runs image-oriented scanners (trivy image, grype, dockle) against a container image reference " +
			"(e.g. \"alpine:3.20\" or a registry ref) and prints a unified findings report. Host-binary only " +
			"for now: an image scanner that would otherwise run via a container is reported skipped, since " +
			"scanner containers run network-isolated and can't pull the target image (see docs/security.md).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			report := security.ScanImage(cmd.Context(), ref, security.DefaultImageScanners(), security.OptionsFromConfig(cfg.Security))
			fmt.Fprintln(cmd.OutOrStdout(), report.Format())
			return nil
		},
	}
}
