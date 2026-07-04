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
		Long: "Runs every enabled scanner (opengrep, trivy, gitleaks, kubescape, hadolint, osv-scanner, grype) over the " +
			"given path (default: current directory) and prints a unified findings report. semgrep and the " +
			"language-targeted engines (gosec/bandit/brakeman/njsscan) are opt-in — enable via " +
			"security.tools.<name>.enabled: true or `aegis security config`. Falls back to a configured container " +
			"image (security.tools.<name>.image) for any enabled scanner not installed on PATH. Findings are " +
			"deduped across overlapping tools and, where confident, tagged with an OWASP ASVS chapter; an " +
			"accepted-risk .aegis/security-baseline.yaml (see `aegis security baseline`) can suppress a specific, " +
			"time-boxed finding.",
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
	cmd.AddCommand(newScanSBOMCmd())
	cmd.AddCommand(newScanDASTCmd())
	return cmd
}

func newScanDASTCmd() *cobra.Command {
	var mode, apiDefinition string
	cmd := &cobra.Command{
		Use:   "dast <target-url>",
		Short: "Run OWASP ZAP against a running application (DAST)",
		Long: "Crawls (and, in --mode active/api, actively attacks) a *running* application via OWASP ZAP and prints " +
			"a unified findings report. Container-only (security.tools.zap.image, digest-pinned) — see docs/security.md. " +
			"The target must be loopback/private (allowed by default) or explicitly declared in " +
			"security.dast.allowed_targets; anything else is rejected before ZAP ever runs. --mode active/api sends " +
			"real attack payloads and additionally requires security.dast.allow_active: true.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			report, err := security.RunDAST(cmd.Context(), security.DASTOptions{
				Target:         args[0],
				Mode:           security.DASTMode(mode),
				APIDefinition:  apiDefinition,
				AllowedTargets: cfg.Security.DAST.AllowedTargets,
				AllowActive:    cfg.Security.DAST.AllowActive,
			}, security.OptionsFromConfig(cfg.Security))
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), report.Format())
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "baseline", "scan mode: baseline (passive), active (+ attack payloads), api (OpenAPI + active)")
	cmd.Flags().StringVar(&apiDefinition, "api-definition", "", "OpenAPI spec URL or path; required when --mode=api")
	return cmd
}

func newScanSBOMCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "sbom [path]",
		Short: "Generate a CycloneDX SBOM for the given path via syft",
		Long: "Runs syft over the given path (default: current directory) and writes a CycloneDX JSON SBOM — " +
			"a persisted supply-chain artifact, and the same generation grype's directory scan (`aegis scan`) " +
			"prefers feeding from instead of re-cataloging. Falls back to a configured container image " +
			"(security.tools.syft.image) if syft isn't installed on PATH.",
		Args: cobra.MaximumNArgs(1),
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
			sbom, _, err := security.GenerateSBOM(cmd.Context(), abs, security.OptionsFromConfig(cfg.Security))
			if err != nil {
				return fmt.Errorf("generate sbom: %w", err)
			}
			dest := out
			if dest == "" {
				dest = filepath.Join(abs, ".aegis", "sbom.cdx.json")
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dest, sbom, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "SBOM written to %s (%d bytes)\n", dest, len(sbom))
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output file path (default: <path>/.aegis/sbom.cdx.json)")
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
