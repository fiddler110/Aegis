package cli

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/spf13/cobra"
)

// newSecurityCmd is the P11.10/P11.11 CLI surface for the scanner
// availability layer: status shows how each tool would actually run right
// now (host binary, container fallback, or unavailable and why); install
// runs a tool's guided host install after showing the operator the exact
// command and getting explicit confirmation — installing software is a
// privileged, host-modifying action, so this must never happen silently or
// non-interactively without --yes.
func newSecurityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Manage security scanner availability (opengrep, gosec, bandit, brakeman, njsscan, trivy, gitleaks, trufflehog, kubescape, hadolint, grype, dockle, osv-scanner, syft)",
		Long: "Inspects and provisions the scanners behind `aegis scan`/the security_scan tool. " +
			"`status` reports whether each tool will run via its host binary, a configured " +
			"container image, or not at all (with the exact reason). `install` walks through " +
			"a guided, approval-gated host install for one tool. `verify-image` proves every scanner in " +
			"the built multiscanner image can actually run and detect, by scanning an embedded fixture " +
			"with planted findings. `baseline` shows the accepted-risk " +
			"suppression allowlist (.aegis/security-baseline.yaml) and each entry's status.",
	}
	cmd.AddCommand(newSecurityStatusCmd())
	cmd.AddCommand(newSecurityInstallCmd())
	cmd.AddCommand(newSecurityBuildImageCmd())
	cmd.AddCommand(newSecurityVerifyImageCmd())
	cmd.AddCommand(newSecurityUpdateDBCmd())
	cmd.AddCommand(newSecurityConfigCmd())
	cmd.AddCommand(newSecurityBaselineCmd())
	return cmd
}

// newSecurityBuildImageCmd builds the shared multiscanner image and records
// its identity in config.
//
// Unlike `install`, this doesn't prompt: `install` runs a descriptor-supplied
// shell command on the host, so the operator has to see it first, whereas this
// builds one Containerfile shipped inside the binary. Typing `build-image` is
// the intent. It still prints exactly what it's about to do, since the build
// downloads a few GB and rewrites a config block.
func newSecurityBuildImageCmd() *cobra.Command {
	var (
		runtimeName string
		profile     string
		image       string
		noCache     bool
		global      bool
		skipVerify  bool
	)
	cmd := &cobra.Command{
		Use:   "build-image",
		Short: "Build the bundled multiscanner container image and pin it in config",
		Long: "Builds a single local image carrying every bundled scanner, so container-method " +
			"scanning needs one image instead of a separately pinned image per tool, then records " +
			"the built image's ID into config. That ID is re-verified before every container run: " +
			"if the image is rebuilt or retagged behind Aegis's back, scans fail with a specific " +
			"reason rather than silently running something else.\n\n" +
			"Profiles: `full` (default) adds the Python (bandit/njsscan), Ruby (brakeman) " +
			"and network (nmap/nuclei) scanners on top of `core`'s static binaries. Expect roughly " +
			"3-4GB for full, and a long first build — vulnerability databases are baked in so scans " +
			"keep working with networking disabled.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			opts := security.MultiscannerBuildOptions{
				Runtime: sandbox.ContainerRuntime(strings.TrimSpace(runtimeName)),
				Profile: profile,
				Image:   image,
				NoCache: noCache,
			}
			res, err := security.BuildMultiscanner(cmd.Context(), opts, out)
			if err != nil {
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config to record the built image: %w", err)
			}
			// Every other security field is carried through unchanged:
			// patchSecurity rewrites the whole security: block.
			patch := config.SecurityPatch{
				EgressThenWrite:  cfg.Security.EgressThenWrite,
				NetworkAllowList: cfg.Security.NetworkAllowList,
				DefaultMethod:    cfg.Security.DefaultMethod,
				Tools:            cfg.Security.Tools,
				DAST:             cfg.Security.DAST,
				WSLDistro:        cfg.Security.WSLDistro,
				Debate:           cfg.Security.Debate,
				Multiscanner: config.MultiscannerConfig{
					Enabled: true,
					Image:   res.Image,
					ImageID: res.ImageID,
					// Recorded so resolution looks in the storage of the
					// runtime that actually built the image, rather than
					// whatever auto-detection would prefer.
					Runtime: string(res.Runtime),
					// Recorded next to the image ID so a later run can tell a
					// stale image from a rebuilt one: the ID proves the image
					// hasn't changed, this proves the source hasn't either.
					SourceFingerprint: res.SourceFingerprint,
					Concurrency:       cfg.Security.Multiscanner.Concurrency,
					Tools:             res.Tools,
				},
			}
			write, target := config.PatchProjectSecurity, config.ProjectConfigPath()
			if global {
				write, target = config.PatchGlobalSecurity, config.GlobalConfigPath()
			}
			if err := write(patch); err != nil {
				return fmt.Errorf("record the built image in config: %w", err)
			}

			fmt.Fprintf(out, "\nBuilt %s (profile: %s, runtime: %s)\n", res.Image, res.Profile, res.Runtime)
			fmt.Fprintf(out, "  image id: %s\n", res.ImageID)
			fmt.Fprintf(out, "  source:   %s\n", res.SourceFingerprint)
			fmt.Fprintf(out, "  scanners: %s\n", strings.Join(res.Tools, ", "))
			fmt.Fprintf(out, "  pinned in: %s\n", target)

			if skipVerify {
				fmt.Fprintf(out, "\nSkipped verification (--skip-verify). Run `aegis security verify-image` before trusting this image.\n")
				fmt.Fprintf(out, "Run `aegis security status` to see which scanners now resolve to it.\n")
				return nil
			}
			// Verification is part of the build, not an optional follow-up: a
			// build that "succeeded" while shipping a tool that cannot run is
			// exactly the state P55.3 exists to end, and an operator who has to
			// remember a second command will not (nobody did, for two releases).
			fmt.Fprintf(out, "\nVerifying the built image — each scanner is probed and then run against a fixture with planted findings.\n")
			if err := runVerifyImage(cmd, patch.Multiscanner, nil); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nRun `aegis security status` to see which scanners now resolve to it.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimeName, "runtime", "", "container runtime to build with (docker/podman); empty auto-detects")
	cmd.Flags().StringVar(&profile, "profile", security.MultiscannerProfileFull, "which scanners to include: "+strings.Join(security.MultiscannerProfiles(), " or "))
	cmd.Flags().StringVar(&image, "image", "", "image tag to build; empty uses "+security.MultiscannerDefaultImage)
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "rebuild every layer — the way to refresh the baked vulnerability databases")
	cmd.Flags().BoolVar(&global, "global", false, "pin in the user config (~/.config/aegis/config.yaml) instead of the project's .aegis/config.yaml")
	// No backticks in this usage string: cobra reads a backtick-quoted word as
	// the flag's argument placeholder, so "`verify-image`" would render as
	// "--skip-verify verify-image" on a boolean flag.
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "don't run verify-image after building (the image is pinned unverified — a scanner that can't run won't be noticed until a scan silently misses findings)")
	return cmd
}

// newSecurityVerifyImageCmd proves the built image's scanners actually work
// (P55.3).
//
// The version probe is the cheap half and catches a tool that isn't in the
// image at all. The canary scan is the half that matters: a scanner that never
// loaded its rules or its database does not fail — it reports zero findings and
// exits clean, which is indistinguishable from a clean repo unless the tree
// being scanned is known to be dirty. So the assertion is a non-zero finding
// count against an embedded fixture full of planted vulnerabilities, not exit 0.
func newSecurityVerifyImageCmd() *cobra.Command {
	var tools []string
	cmd := &cobra.Command{
		Use:   "verify-image",
		Short: "Prove every scanner in the built multiscanner image can actually run and detect",
		Long: "Runs two checks per scanner the pinned image claims to carry: a version probe (catches a " +
			"tool that isn't in the image), then a scan of a small embedded fixture with deliberately " +
			"planted vulnerabilities, asserting the scanner reports a non-zero number of findings.\n\n" +
			"The second check is the point. A scanner that never loaded its rule packs or its " +
			"vulnerability database doesn't error — it exits cleanly and reports nothing, which reads " +
			"exactly like a clean codebase. A version probe cannot see that; only running the tool " +
			"against something known to be dirty can.\n\n" +
			"Exits non-zero if any scanner fails, so this is usable as a provisioning gate. Tools that " +
			"cannot be canaried (nmap/nuclei need a network target) are reported as skipped with the " +
			"reason, never counted as passing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return runVerifyImage(cmd, cfg.Security.Multiscanner, tools)
		},
	}
	cmd.Flags().StringSliceVar(&tools, "tool", nil, "verify only these scanners (repeatable, or comma-separated); default is every tool the image claims")
	return cmd
}

// runVerifyImage is the shared body of `verify-image` and build-image's final
// step, so the two can never report differently about the same image.
func runVerifyImage(cmd *cobra.Command, mc config.MultiscannerConfig, tools []string) error {
	out := cmd.OutOrStdout()
	policy := security.MultiscannerPolicyFromConfig(mc)
	// Same reasoning as `update-db`: a stale image is a live cause of scanner
	// failures, and an operator who sees only the failing rows has no route to
	// that cause.
	if drift := security.MultiscannerSourceDrift(policy); drift != "" {
		fmt.Fprintf(out, "warning: %s\n\n", drift)
	}

	// The header is printed from the first row rather than up front, so a
	// preflight failure (no image, no runtime, a bad --tool name) reports as an
	// error instead of an error under an empty table.
	header := false
	results, err := security.VerifyMultiscanner(cmd.Context(), policy, tools, func(r security.VerifyResult) {
		if !header {
			fmt.Fprintf(out, "%-12s  %-8s  %-10s  %s\n", "TOOL", "STATUS", "FINDINGS", "VERSION")
			header = true
		}
		findings := "-"
		if r.Expected > 0 {
			findings = fmt.Sprintf("%d (>=%d)", r.Findings, r.Expected)
		}
		fmt.Fprintf(out, "%-12s  %-8s  %-10s  %s\n", r.Tool, r.Status, findings, defaultOr(r.Version, "-"))
		if r.Status != security.VerifyPass && r.Detail != "" {
			fmt.Fprintf(out, "%14s%s\n", "", r.Detail)
		}
	})
	if err != nil {
		return err
	}

	passed, failed, skipped, blocked := security.VerifyCounts(results)
	fmt.Fprintf(out, "\n%d scanner(s) checked: %d verified, %d failed", len(results), passed, failed)
	if skipped > 0 {
		fmt.Fprintf(out, ", %d skipped (no canary possible — see the reasons above)", skipped)
	}
	if blocked > 0 {
		fmt.Fprintf(out, ", %d blocked on an unpopulated database cache", blocked)
	}
	fmt.Fprintln(out, ".")
	if blocked > 0 {
		fmt.Fprintf(out, "Run `aegis security update-db` to populate %s, then re-run this command.\n", security.MultiscannerCacheVolume)
	}
	if failed > 0 {
		// Non-zero exit, deliberately: this is meant to gate provisioning, and
		// a gate that reports a problem and exits 0 is not a gate.
		return fmt.Errorf("%d scanner(s) in %s could not be verified — see the rows above", failed, policy.Image)
	}
	return nil
}

func newSecurityStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show how each security scanner would run right now",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			opts := security.OptionsFromConfig(cfg.Security)
			out := cmd.OutOrStdout()
			// Printed before the table rather than folded into a tool's DETAIL
			// column: drift affects every container-method scanner at once, and
			// repeating it on 16 rows would read as 16 problems.
			if drift := security.MultiscannerSourceDrift(opts.Multiscanner); drift != "" {
				fmt.Fprintf(out, "warning: %s\n\n", drift)
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TOOL\tCATEGORY\tMETHOD\tDETAIL")
			for _, d := range security.Descriptors() {
				method, rt, _, reason := security.Resolve(cmd.Context(), d.Name, opts)
				detail := reason
				switch method {
				case security.MethodHost:
					detail = "on PATH"
				case security.MethodContainer:
					detail = fmt.Sprintf("via %s", rt)
				case security.MethodWSL:
					detail = "via WSL"
				default:
					if note := security.AvailabilityNote(d.Name, reason); note != "" {
						detail = reason + "; " + note
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.Name, d.Category, methodLabel(method), detail)
			}
			tw.Flush()
			return nil
		},
	}
}

func methodLabel(m security.Method) string {
	switch m {
	case security.MethodHost:
		return "host"
	case security.MethodContainer:
		return "container"
	case security.MethodWSL:
		return "wsl"
	default:
		return "unavailable"
	}
}

func newSecurityInstallCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "install <tool>",
		Short: "Guided host install for one security scanner (approval-gated)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			d, ok := security.DescriptorFor(name)
			if !ok {
				return fmt.Errorf("unknown scanner %q (known: %s)", name, knownScannerNames())
			}
			command, ok := security.InstallCommand(name)
			if !ok {
				return fmt.Errorf("%s", security.NoGuidedInstallReason(d.Name))
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s — %s\n", d.Name, d.Summary)
			fmt.Fprintf(out, "\nThis will run the following command on your host:\n\n    %s\n\n", command)

			if !yes {
				fmt.Fprint(out, "Proceed? [y/N] ")
				reader := bufio.NewReader(cmd.InOrStdin())
				line, _ := reader.ReadString('\n')
				if !strings.EqualFold(strings.TrimSpace(line), "y") && !strings.EqualFold(strings.TrimSpace(line), "yes") {
					fmt.Fprintln(out, "Aborted — nothing was installed.")
					return nil
				}
			}

			if err := security.RunGuidedInstall(cmd.Context(), name, out); err != nil {
				return err
			}
			fmt.Fprintf(out, "\n%s installed. Run `aegis security status` to confirm.\n", d.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt (for non-interactive/scripted use)")
	return cmd
}

// newSecurityUpdateDBCmd refreshes the scanner databases in the cache volume.
//
// This exists so the image doesn't have to carry them: baked in, they were
// ~3.7GB of a 5.8GB image. It's also the only Aegis container run with network
// access — scans keep running --network none against whatever this leaves in
// the volume, so the workspace is never mounted into a networked container.
func newSecurityUpdateDBCmd() *cobra.Command {
	var skipJavaDB bool
	cmd := &cobra.Command{
		Use:   "update-db",
		Short: "Download/refresh the multiscanner's vulnerability databases",
		Long: "Populates the scanner cache volume (" + security.MultiscannerCacheVolume + ") with the trivy, " +
			"grype, and osv-scanner vulnerability databases. Run this once after `aegis security build-image`, and " +
			"again whenever you want fresher data — the databases are only as current as the last run.\n\n" +
			"This is the only Aegis container run that is given network access, and it mounts no workspace. " +
			"Scans themselves still run with --network none and read the databases from the volume.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			policy := security.MultiscannerPolicyFromConfig(cfg.Security.Multiscanner)
			// Warned before the run, not after: a stale image is a live cause
			// of update-db failures (a tool the current update script refreshes
			// may not exist in an older image at all), and an operator who sees
			// only the resulting error has no way to reach that cause.
			if drift := security.MultiscannerSourceDrift(policy); drift != "" {
				fmt.Fprintf(out, "warning: %s\n\n", drift)
			}
			if err := security.UpdateMultiscannerDB(cmd.Context(), policy, skipJavaDB, out); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nDatabases updated in volume %s. Scans will use them offline.\n", security.MultiscannerCacheVolume)
			return nil
		},
	}
	cmd.Flags().BoolVar(&skipJavaDB, "skip-java-db", false, "skip trivy's ~1.4GB Java database (fine unless you scan JVM code)")
	return cmd
}

func knownScannerNames() string {
	all := security.Descriptors()
	names := make([]string, len(all))
	for i, d := range all {
		names[i] = d.Name
	}
	return strings.Join(names, ", ")
}

func newSecurityConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print the resolved security.tools configuration",
		Long:  "View-only: edit security.tools/security.default_method directly in .aegis/config.yaml (project) or ~/.config/aegis/config.yaml (user) — see docs/configuration.md.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "default_method: %s\n", defaultOr(cfg.Security.DefaultMethod, "auto"))
			if len(cfg.Security.Tools) == 0 {
				fmt.Fprintln(out, "tools: (none configured — every scanner uses default_method)")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TOOL\tENABLED\tMETHOD\tINSTALL\tIMAGE")
			for name, tc := range cfg.Security.Tools {
				fmt.Fprintf(tw, "%s\t%v\t%s\t%s\t%s\n", name, tc.ToolEnabled(), defaultOr(tc.Method, "auto"), defaultOr(tc.Install, "prompt"), defaultOr(tc.Image, "(none)"))
			}
			tw.Flush()
			return nil
		},
	}
}

func defaultOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// newSecurityBaselineCmd is the P11.8 view for a project's accepted-risk
// allowlist (.aegis/security-baseline.yaml): view-only, same posture as
// `security config` — hand-edit the YAML directly (see docs/security.md)
// rather than through a mutating CLI, so an operator's suppression review
// happens in a real editor/PR, not a one-off command invocation.
func newSecurityBaselineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "baseline [path]",
		Short: "Show the accepted-risk suppression baseline and its status",
		Long: "Reads <path>/.aegis/security-baseline.yaml (default: current directory) and prints each " +
			"suppression entry's status: active (currently suppressing its matched finding), expired " +
			"(past its expires date — the finding it used to cover is back in scan reports), or invalid " +
			"(missing rule_id/reason, or an unparseable expires date — never applied). View-only: edit " +
			"the YAML file directly to add, change, or remove entries.",
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
			b, err := security.LoadBaseline(abs)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if b == nil || len(b.Suppressions) == 0 {
				fmt.Fprintf(out, "no baseline entries (%s not found or empty)\n", security.BaselinePath(abs))
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "STATUS\tRULE_ID\tLOCATION\tEXPIRES\tREASON")
			for _, e := range b.Suppressions {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", security.SuppressionStatusLabel(e, time.Now()), e.RuleID, defaultOr(e.Location, "(any)"), defaultOr(e.Expires, "(missing)"), e.Reason)
			}
			tw.Flush()
			return nil
		},
	}
}
