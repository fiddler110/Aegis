package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// scanProgressPrinter renders security.ScanEvent as one line per scanner
// start/finish/skip, so `aegis scan` shows live activity in the terminal
// instead of going silent until the whole (potentially multi-minute, one
// subprocess per tool) run completes.
func scanProgressPrinter(out io.Writer) func(security.ScanEvent) {
	return func(ev security.ScanEvent) {
		switch ev.Phase {
		case security.PhaseStart:
			fmt.Fprintf(out, "-> %s (%s)...\n", ev.Scanner, ev.Method)
		case security.PhaseDone:
			if ev.Err != nil {
				fmt.Fprintf(out, "   %s: error: %v (%s)\n", ev.Scanner, ev.Err, ev.Elapsed.Round(time.Millisecond))
				return
			}
			fmt.Fprintf(out, "   %s: %d finding(s) (%s)\n", ev.Scanner, ev.Findings, ev.Elapsed.Round(time.Millisecond))
		case security.PhaseSkipped:
			fmt.Fprintf(out, "-- %s: skipped (%s)\n", ev.Scanner, ev.Reason)
		}
	}
}

func newScanCmd() *cobra.Command {
	var scanners []string
	var list bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Run available security scanners and print normalized findings",
		Long: "Runs every enabled scanner (opengrep, trivy, gitleaks, kubescape, hadolint, osv-scanner, grype) over the " +
			"given path (default: current directory) and prints a unified findings report, persisted to " +
			".aegis/security/scan.json under it. The language-targeted engines (gosec/bandit/brakeman/" +
			"njsscan) are opt-in — enable via security.tools.<name>.enabled: true or `aegis security config` — but " +
			"a plain scan with no --scanner filter auto-detects the project's language (go.mod/*.go, " +
			"requirements.txt/*.py, Gemfile/*.rb, package.json/*.js, and more for display only) and auto-enables " +
			"the matching one for this run, without needing config, unless you've explicitly enabled/disabled it " +
			"yourself; hadolint/kubescape/brakeman are likewise skipped automatically when the workspace has no " +
			"Dockerfile, Kubernetes manifest, or Rails application for them to analyze. At a real terminal, a " +
			"plain `aegis scan` (no --scanner, " +
			"no --yes) previews this auto-detected plan and asks for confirmation before running anything — pass " +
			"--yes, or run non-interactively (CI/scripts), to skip the prompt and run the plan immediately. Pass " +
			"--scanner one or more times to run only specific scanners or categories instead (e.g. --scanner " +
			"trufflehog, --scanner secrets) — this force-enables them for the run regardless of config or " +
			"relevance, the same way `aegis scan image` already runs its own distinct scanner set on request, and " +
			"skips the confirmation prompt since the selection is already explicit. Run `aegis scan --list` to " +
			"see every valid --scanner name and category alias, with live availability. Falls back to a " +
			"configured container image (security.tools.<name>.image) for any enabled scanner not installed on " +
			"PATH. Findings are deduped across overlapping tools and, where confident, tagged with an OWASP ASVS " +
			"chapter; an accepted-risk .aegis/security-baseline.yaml (see `aegis security baseline`) can suppress " +
			"a specific, time-boxed finding.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return printScannerList(cmd)
			}
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
			opts := security.OptionsFromConfig(cfg.Security)
			selected := security.DefaultScanners()
			out := cmd.OutOrStdout()
			if len(scanners) > 0 {
				selected, opts, err = security.SelectScanners(selected, opts, scanners)
				if err != nil {
					return err
				}
			} else {
				opts = security.AutoEnableLanguageScanners(abs, opts)
				if !yes && isatty.IsTerminal(os.Stdin.Fd()) {
					var run bool
					selected, opts, run, err = confirmScanPlan(cmd, abs, selected, opts)
					if err != nil {
						return err
					}
					if !run {
						fmt.Fprintln(out, "Aborted — no scan run.")
						return nil
					}
				}
			}
			report := security.RunWithProgress(cmd.Context(), abs, selected, opts, scanProgressPrinter(out))
			security.WriteReportArtifact(abs, "scan", report)
			fmt.Fprintln(out, report.Format())
			fmt.Fprintf(out, "\nReport written to %s\n", security.ReportArtifactPath(abs, "scan"))
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&scanners, "scanner", "s", nil, "run only this scanner or category (repeatable) — e.g. --scanner trufflehog --scanner secrets; see --list for valid names")
	cmd.Flags().BoolVar(&list, "list", false, "list every scanner name and category alias usable with --scanner (with live availability), then exit")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the interactive scanner-plan confirmation and run the auto-detected set immediately (for scripts/CI)")
	cmd.AddCommand(newScanImageCmd())
	cmd.AddCommand(newScanSBOMCmd())
	cmd.AddCommand(newScanDASTCmd())
	cmd.AddCommand(newScanNetworkCmd())
	return cmd
}

// formatScanPlan renders a PlanScanners result as a human-readable preview:
// one line per scanner, "->" for what would run (with its method) and "--"
// for what's skipped (with why) — the same shape scanProgressPrinter uses
// for the live run, so the preview and the real output read consistently.
func formatScanPlan(plan []security.ScanPlanEntry) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, e := range plan {
		if e.Skipped {
			fmt.Fprintf(tw, "  --\t%s\tskip: %s\n", e.Scanner.Name(), e.Reason)
			continue
		}
		fmt.Fprintf(tw, "  ->\t%s\t%s\n", e.Scanner.Name(), e.Method)
	}
	tw.Flush()
	return b.String()
}

// applyScanPlanChoice interprets one line of picker input against the
// auto-detected plan: blank/"y"/"yes" accepts it as-is, "n"/"no" aborts, and
// anything else is treated as a comma-separated scanner/category selection
// (the same syntax --scanner accepts) that overrides the plan entirely. A
// pure function of (line, all, opts) — separated from confirmScanPlan's I/O
// so the parsing logic is unit-testable without a real terminal.
func applyScanPlanChoice(line string, all []security.Scanner, opts security.Options) (selected []security.Scanner, newOpts security.Options, run bool, err error) {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return all, opts, true, nil
	case "n", "no":
		return nil, opts, false, nil
	default:
		tokens := strings.Split(line, ",")
		for i := range tokens {
			tokens[i] = strings.TrimSpace(tokens[i])
		}
		sel, selOpts, err := security.SelectScanners(all, opts, tokens)
		if err != nil {
			return nil, opts, false, err
		}
		return sel, selOpts, true, nil
	}
}

// confirmScanPlan previews, at a real terminal, exactly which scanners the
// auto-detected set (language detection + hadolint/kubescape relevance
// gating) would run right now, and lets the operator accept it, decline, or
// type a specific scanner/category selection instead of always silently
// running the full set the moment `aegis scan` is invoked. Skipped entirely
// (by the --yes flag or a non-interactive stdin) so scripts/CI keep today's
// immediate-run behavior.
func confirmScanPlan(cmd *cobra.Command, dir string, all []security.Scanner, opts security.Options) (selected []security.Scanner, newOpts security.Options, run bool, err error) {
	out := cmd.OutOrStdout()
	if langs := security.DetectLanguageSummary(dir); len(langs) > 0 {
		parts := make([]string, len(langs))
		for i, l := range langs {
			if l.Scanner != "" {
				parts[i] = fmt.Sprintf("%s (%s)", l.Language, l.Scanner)
			} else {
				parts[i] = l.Language
			}
		}
		fmt.Fprintf(out, "Detected: %s\n\n", strings.Join(parts, ", "))
	}
	plan := security.PlanScanners(cmd.Context(), dir, all, opts)
	fmt.Fprintln(out, "Planned scan:")
	fmt.Fprint(out, formatScanPlan(plan))
	fmt.Fprint(out, "\nRun this plan? [Y/n, or a comma-separated scanner/category list, e.g. \"secrets\" or \"gitleaks,trufflehog\"] ")
	reader := bufio.NewReader(cmd.InOrStdin())
	line, _ := reader.ReadString('\n')
	return applyScanPlanChoice(line, all, opts)
}

func newScanNetworkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network <target> [target...]",
		Short: "Run nmap + nuclei against a network target host list (attack-surface mapping)",
		Long: "Runs nmap (port/service/version discovery) and nuclei (template-driven vulnerability/misconfiguration " +
			"matching) against a bare host/IP/CIDR list and prints a unified findings report — same underlying scan as " +
			"the standalone recon_scan tool (P13.5). Each target must be loopback/private (allowed by default) or " +
			"explicitly declared in security.dast.allowed_targets — the same shared gate `aegis scan dast` uses; " +
			"anything else is rejected before either scanner runs. security.dast.allow_active (the same flag `scan " +
			"dast`'s --mode active/api requires) unlocks more aggressive probes: nmap's OS detection/full port " +
			"range/default scripts, and nuclei's full template set (dos/fuzz/intrusive templates excluded " +
			"otherwise). nuclei also requires security.tools.nuclei.templates_version (a pinned nuclei-templates " +
			"release tag) — see docs/security.md.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			report, err := security.RunRecon(cmd.Context(), security.ReconOptions{
				Targets:        args,
				AllowedTargets: cfg.Security.DAST.AllowedTargets,
				AllowActive:    cfg.Security.DAST.AllowActive,
			}, security.OptionsFromConfig(cfg.Security))
			if err != nil {
				return err
			}
			cwd, err := filepath.Abs(".")
			if err == nil {
				security.WriteReportArtifact(cwd, "network", report)
			}
			fmt.Fprintln(cmd.OutOrStdout(), report.Format())
			return nil
		},
	}
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
			cwd, err := filepath.Abs(".")
			if err == nil {
				security.WriteReportArtifact(cwd, "dast", report)
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
			cwd, err := filepath.Abs(".")
			if err == nil {
				security.WriteReportArtifact(cwd, "image", report)
			}
			fmt.Fprintln(cmd.OutOrStdout(), report.Format())
			return nil
		},
	}
}

// printScannerList prints every scanner name and category alias usable with
// `aegis scan --scanner`/`/scan <selector>`, with live availability — the
// same live-resolved TOOL/CATEGORY/STATUS shape `aegis security status`
// already prints, plus a DEFAULT column (would this tool run without an
// explicit --scanner/config change) and the category-alias groupings that
// `security status` has no reason to know about.
func printScannerList(cmd *cobra.Command) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	opts := security.OptionsFromConfig(cfg.Security)
	out := cmd.OutOrStdout()

	if drift := security.MultiscannerSourceDrift(opts.Multiscanner); drift != "" {
		fmt.Fprintf(out, "warning: %s\n\n", drift)
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SCANNER\tCATEGORY\tDEFAULT\tSTATUS")
	for _, d := range security.Descriptors() {
		method, rt, _, reason := security.Resolve(cmd.Context(), d.Name, opts)
		status := reason
		switch method {
		case security.MethodHost:
			status = "on PATH"
		case security.MethodContainer:
			status = fmt.Sprintf("via %s", rt)
		case security.MethodWSL:
			status = "via WSL"
		default:
			if note := security.AvailabilityNote(d.Name, reason); note != "" {
				status = reason + "; " + note
			}
		}
		defaultLabel := "opt-in"
		if d.DefaultEnabled {
			defaultLabel = "enabled"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.Name, d.Category, defaultLabel, status)
	}
	tw.Flush()

	fmt.Fprintln(out, "\nCategory aliases (--scanner <alias> runs every scanner in the group):")
	catTw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, c := range security.CategoryAliases() {
		fmt.Fprintf(catTw, "  %s\t-> %s\n", c.Name, strings.Join(c.Scanners, ", "))
	}
	catTw.Flush()
	return nil
}
