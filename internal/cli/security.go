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
			"with planted findings.\n\n" +
			"There are two images, split by what the container is allowed to SEE. The multiscanner " +
			"(`build-image`) analyzes your source: workspace mounted, --network none. The netscanner " +
			"(`build-image --netscanner`) scans remote targets — a host list for nmap/nuclei, an image " +
			"reference for trivy/grype — so it runs with network access and no workspace mounted, ever. " +
			"Both take a --netscanner flag on build-image/verify-image.\n\n" +
			"`baseline` shows the accepted-risk " +
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
		project     bool
		global      bool
		skipVerify  bool
		netscanner  bool
	)
	cmd := &cobra.Command{
		Use:   "build-image",
		Short: "Build the bundled multiscanner container image and pin it in config",
		Long: "Builds a single local image carrying every bundled scanner, so container-method " +
			"scanning needs one image instead of a separately pinned image per tool, then records " +
			"the built image's ID into config. That ID is re-verified before every container run: " +
			"if the image is rebuilt or retagged behind Aegis's back, scans fail with a specific " +
			"reason rather than silently running something else.\n\n" +
			"The pin is written to the user config (~/.config/aegis/config.yaml, %AppData%\\aegis on " +
			"Windows) so every project on the machine uses the image — like the image itself and the " +
			"shared database volume, it is machine-wide. Pass --project to pin it in this repo's " +
			".aegis/config.yaml instead; the command always prints which file it wrote.\n\n" +
			"Profiles: `full` (default) adds the Python (bandit/njsscan), Ruby (brakeman), Go " +
			"(gosec, plus the Go toolchain it cannot work without) and network (nmap/nuclei) scanners " +
			"on top of `core`'s static binaries. Expect roughly " +
			"3-4GB for full, and a long first build — vulnerability databases are NOT in the image: " +
			"they live in the shared aegis-scanner-cache volume, so run `aegis security update-db` " +
			"after this or the database-backed scanners will be refused.\n\n" +
			"--netscanner builds the second, much smaller image instead (~570MB, most of it the " +
			"pinned nuclei template set): nmap, nuclei, and " +
			"image-reference scanning with trivy/grype. It is separate for one reason — mount posture. " +
			"Every tool in it needs network egress and none needs your source, so it runs with network " +
			"ON and no workspace mounted, ever, while this image keeps --network none with the workspace " +
			"mounted. It needs no update-db: having network, it refreshes its own databases into a " +
			"separate volume.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			// Checked before the build, not at the write: the build downloads
			// gigabytes and takes many minutes, so a contradictory pair of flags
			// has to fail now rather than after all of that.
			if project && global {
				return fmt.Errorf("--project and --global are mutually exclusive: --project pins in %s, while --global asks for the user config %s (which is now the default)",
					config.ProjectConfigPath(), config.GlobalConfigPath())
			}

			opts := security.MultiscannerBuildOptions{
				Runtime: sandbox.ContainerRuntime(strings.TrimSpace(runtimeName)),
				Profile: profile,
				Image:   image,
				NoCache: noCache,
			}
			if opts.Runtime == "" {
				opts.Runtime = stickyBuildRuntime(netscanner)
			}
			if netscanner {
				if profile != security.MultiscannerProfileFull {
					// Rejected rather than ignored: --profile means something
					// real for the other image, so silently dropping it here
					// would leave an operator believing they built a smaller
					// netscanner than they did.
					return fmt.Errorf("--profile applies only to the multiscanner image; the netscanner has no profiles (it is four tools with one posture)")
				}
				return runBuildNetscanner(cmd, opts, project, skipVerify)
			}
			res, err := security.BuildMultiscanner(cmd.Context(), opts, out)
			if err != nil {
				return err
			}

			pinned, target, err := recordMultiscannerPin(res, project)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "\nBuilt %s (profile: %s, runtime: %s)\n", res.Image, res.Profile, res.Runtime)
			fmt.Fprintf(out, "  image id: %s\n", res.ImageID)
			fmt.Fprintf(out, "  source:   %s\n", res.SourceFingerprint)
			fmt.Fprintf(out, "  scanners: %s\n", strings.Join(res.Tools, ", "))
			fmt.Fprintf(out, "  pinned in: %s\n", target)

			// Printed immediately under "pinned in", because it says that line
			// is not the whole truth for this directory.
			if !project {
				warn, err := multiscannerShadowWarning(res.ImageID)
				if err != nil {
					return err
				}
				if warn != "" {
					fmt.Fprintf(out, "\nwarning: %s\n", warn)
				}
			}

			if skipVerify {
				fmt.Fprintf(out, "\nSkipped verification (--skip-verify). Run `aegis security verify-image` before trusting this image.\n")
				if warn := siblingDriftWarning("multiscanner"); warn != "" {
					fmt.Fprintf(out, "\nnote: %s\n", warn)
				}
				fmt.Fprintf(out, "Run `aegis security status` to see which scanners now resolve to it.\n")
				return nil
			}
			// Verification is part of the build, not an optional follow-up: a
			// build that "succeeded" while shipping a tool that cannot run is
			// exactly the state P55.3 exists to end, and an operator who has to
			// remember a second command will not (nobody did, for two releases).
			fmt.Fprintf(out, "\nVerifying the built image — each scanner is probed and then run against a fixture with planted findings.\n")
			if err := runVerifyImage(cmd, pinned, nil); err != nil {
				return err
			}
			if warn := siblingDriftWarning("multiscanner"); warn != "" {
				fmt.Fprintf(out, "\nnote: %s\n", warn)
			}
			fmt.Fprintf(out, "\nRun `aegis security status` to see which scanners now resolve to it.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimeName, "runtime", "", "container runtime to build with (docker/podman); empty auto-detects")
	cmd.Flags().StringVar(&profile, "profile", security.MultiscannerProfileFull, "which scanners to include: "+strings.Join(security.MultiscannerProfiles(), " or "))
	cmd.Flags().StringVar(&image, "image", "", "image tag to build; empty uses "+security.MultiscannerDefaultImage)
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "rebuild every layer — the way to refresh the baked vulnerability databases")
	cmd.Flags().BoolVar(&project, "project", false, "pin in this project's .aegis/config.yaml instead of the user config — only for pinning a different image per repo")
	cmd.Flags().BoolVar(&global, "global", false, "deprecated no-op: the user config is now the default")
	// Kept rather than removed: --global was the documented way to get the
	// behavior that is now the default, so provisioning scripts pass it, and
	// deleting it would fail those runs *after* a multi-gigabyte build. It
	// still means what it always meant, so it stays accepted and silent about
	// anything but the deprecation. MarkDeprecated also hides it from --help,
	// which is the point: nobody new should learn it.
	_ = cmd.Flags().MarkDeprecated("global", "the user config is now the default; pass --project for a per-repo pin")
	// No backticks in this usage string: cobra reads a backtick-quoted word as
	// the flag's argument placeholder, so "`verify-image`" would render as
	// "--skip-verify verify-image" on a boolean flag.
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "don't run verify-image after building (the image is pinned unverified — a scanner that can't run won't be noticed until a scan silently misses findings)")
	cmd.Flags().BoolVar(&netscanner, "netscanner", false, "build the network-facing image instead: nmap, nuclei, and image-reference scanning with trivy/grype — it runs with network access and no workspace mounted")
	return cmd
}

// runBuildNetscanner is `build-image --netscanner`: the second image (P55.7).
//
// A flag on the same command rather than a command of its own, because the two
// builds are the same operation over the same build context — one Containerfile,
// one fetch script, one set of pinned tool versions, selected by --target. Two
// commands would invite them to drift, and every operator who built one would
// have to discover the other existed.
func runBuildNetscanner(cmd *cobra.Command, opts security.MultiscannerBuildOptions, project, skipVerify bool) error {
	out := cmd.OutOrStdout()
	res, err := security.BuildNetscanner(cmd.Context(), opts, out)
	if err != nil {
		return err
	}
	pinned, target, err := recordNetscannerPin(res, project)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "\nBuilt %s (runtime: %s)\n", res.Image, res.Runtime)
	fmt.Fprintf(out, "  image id: %s\n", res.ImageID)
	fmt.Fprintf(out, "  source:   %s\n", res.SourceFingerprint)
	fmt.Fprintf(out, "  scanners: %s\n", strings.Join(res.Tools, ", "))
	fmt.Fprintf(out, "  pinned in: %s\n", target)
	fmt.Fprintf(out, "\nThis image runs with network access and NO workspace mounted, ever — that is the whole\n"+
		"reason it is separate from the multiscanner, which runs with --network none and the\n"+
		"workspace mounted. Its runner takes no directory argument, so the split is structural.\n")

	if skipVerify {
		fmt.Fprintf(out, "\nSkipped verification (--skip-verify). Run `aegis security verify-image --netscanner` before trusting this image.\n")
		if warn := siblingDriftWarning("netscanner"); warn != "" {
			fmt.Fprintf(out, "\nnote: %s\n", warn)
		}
		return nil
	}
	fmt.Fprintf(out, "\nVerifying — each scanner is probed, and trivy/grype are run against a known-vulnerable\nimage (%s) to prove they load a vulnerability database. This needs network access.\n", security.NetscannerCanaryImage())
	if err := runVerifyNetscanner(cmd, pinned, nil); err != nil {
		return err
	}
	if warn := siblingDriftWarning("netscanner"); warn != "" {
		fmt.Fprintf(out, "\nnote: %s\n", warn)
	}
	return nil
}

// recordNetscannerPin writes a completed netscanner build into config, using
// the same target-file rules as recordMultiscannerPin (user config by default;
// --project for a repo that deliberately pins its own image).
func recordNetscannerPin(res security.NetscannerBuildResult, project bool) (config.NetscannerConfig, string, error) {
	write, target := config.PatchGlobalSecurity, config.GlobalConfigPath()
	if project {
		write, target = config.PatchProjectSecurity, config.ProjectConfigPath()
	}
	// Read from the file being rewritten rather than the merged config, for the
	// reason recordMultiscannerPin states: patchSecurity replaces that file's
	// whole security: block, so merged values would leak across layers.
	existing, err := config.FileSecurity(target)
	if err != nil {
		return config.NetscannerConfig{}, target, fmt.Errorf("read %s to record the built image: %w", target, err)
	}
	ns := config.NetscannerConfig{
		Enabled:           true,
		Image:             res.Image,
		ImageID:           res.ImageID,
		Runtime:           string(res.Runtime),
		SourceFingerprint: res.SourceFingerprint,
		Tools:             res.Tools,
	}
	patch := config.SecurityPatch{
		EgressThenWrite:  existing.EgressThenWrite,
		NetworkAllowList: existing.NetworkAllowList,
		DefaultMethod:    existing.DefaultMethod,
		Tools:            existing.Tools,
		DAST:             existing.DAST,
		WSLDistro:        existing.WSLDistro,
		Debate:           existing.Debate,
		// Carried, not rebuilt: this command builds one of the two images and
		// must not delete the other's pin as a side effect of rewriting the
		// block they share.
		Multiscanner: existing.Multiscanner,
		Netscanner:   ns,
	}
	if err := write(patch); err != nil {
		return config.NetscannerConfig{}, target, fmt.Errorf("record the built image in %s: %w", target, err)
	}
	return ns, target, nil
}

// recordMultiscannerPin writes a completed build into config and returns the
// block it wrote plus the file it wrote it to.
//
// The user config is the default target because everything else about the
// multiscanner is machine-wide: the image lives in the container runtime's
// storage, and the vulnerability databases live in one named volume shared by
// every scan in every project. A per-repo pin left the *configuration* as the
// only per-repo part of a machine-wide asset, so an operator who provisioned
// once was told scanners were "not installed" — with advice to build the image
// they had already built — in every directory but the one they built from
// (P55.5). --project keeps the narrow case: a repo deliberately pinned to a
// different image from the rest of the machine.
//
// Split out of the command body so the pin can be tested without a container
// runtime; everything above it in build-image needs a real multi-gigabyte
// build to reach.
func recordMultiscannerPin(res security.MultiscannerBuildResult, project bool) (config.MultiscannerConfig, string, error) {
	write, target := config.PatchGlobalSecurity, config.GlobalConfigPath()
	if project {
		write, target = config.PatchProjectSecurity, config.ProjectConfigPath()
	}
	// Read from the file being rewritten, not from the merged config:
	// patchSecurity replaces that file's whole security: block, so the fields
	// carried through unchanged have to be its own. Merged values leak across
	// layers — pinning the user config from inside a repo would copy that
	// repo's security.tools/wsl_distro into the user's config and apply them
	// to every other project on the machine.
	existing, err := config.FileSecurity(target)
	if err != nil {
		return config.MultiscannerConfig{}, target, fmt.Errorf("read %s to record the built image: %w", target, err)
	}
	ms := config.MultiscannerConfig{
		Enabled: true,
		Image:   res.Image,
		ImageID: res.ImageID,
		// Recorded so resolution looks in the storage of the runtime that
		// actually built the image, rather than whatever auto-detection would
		// prefer.
		Runtime: string(res.Runtime),
		// Recorded next to the image ID so a later run can tell a stale image
		// from a rebuilt one: the ID proves the image hasn't changed, this
		// proves the source hasn't either.
		SourceFingerprint: res.SourceFingerprint,
		Concurrency:       existing.Multiscanner.Concurrency,
		Tools:             res.Tools,
	}
	patch := config.SecurityPatch{
		EgressThenWrite:  existing.EgressThenWrite,
		NetworkAllowList: existing.NetworkAllowList,
		DefaultMethod:    existing.DefaultMethod,
		Tools:            existing.Tools,
		DAST:             existing.DAST,
		WSLDistro:        existing.WSLDistro,
		Debate:           existing.Debate,
		Multiscanner:     ms,
		// Carried, not rebuilt: this rewrites the whole security: block, so a
		// netscanner pin written by the other half of this command would be
		// deleted by whichever image was built second.
		Netscanner: existing.Netscanner,
	}
	if err := write(patch); err != nil {
		return config.MultiscannerConfig{}, target, fmt.Errorf("record the built image in %s: %w", target, err)
	}
	return ms, target, nil
}

// recordVerified marks an already-pinned image as having passed
// verification (P81.13), writing to whichever config file's pin the merged
// config actually resolved from — project if it pins a matching image ID,
// else the global (machine-wide) config, the same file
// recordMultiscannerPin/recordNetscannerPin default to.
//
// kind is "multiscanner" or "netscanner".
func recordVerified(kind string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	target := config.GlobalConfigPath()
	write := config.PatchGlobalSecurity
	projectSec, err := config.FileSecurity(config.ProjectConfigPath())
	if err == nil {
		matches := false
		switch kind {
		case "multiscanner":
			matches = strings.TrimSpace(projectSec.Multiscanner.ImageID) != "" &&
				strings.EqualFold(projectSec.Multiscanner.ImageID, cfg.Security.Multiscanner.ImageID)
		case "netscanner":
			matches = strings.TrimSpace(projectSec.Netscanner.ImageID) != "" &&
				strings.EqualFold(projectSec.Netscanner.ImageID, cfg.Security.Netscanner.ImageID)
		}
		if matches {
			target, write = config.ProjectConfigPath(), config.PatchProjectSecurity
		}
	}

	existing, err := config.FileSecurity(target)
	if err != nil {
		return fmt.Errorf("read %s to record verification: %w", target, err)
	}
	patch := config.SecurityPatch{
		EgressThenWrite:  existing.EgressThenWrite,
		NetworkAllowList: existing.NetworkAllowList,
		DefaultMethod:    existing.DefaultMethod,
		Tools:            existing.Tools,
		DAST:             existing.DAST,
		WSLDistro:        existing.WSLDistro,
		Debate:           existing.Debate,
		Multiscanner:     existing.Multiscanner,
		Netscanner:       existing.Netscanner,
	}
	now := time.Now().UTC().Format(time.RFC3339)
	switch kind {
	case "multiscanner":
		patch.Multiscanner.Verified = true
		patch.Multiscanner.VerifiedAt = now
	case "netscanner":
		patch.Netscanner.Verified = true
		patch.Netscanner.VerifiedAt = now
	}
	return write(patch)
}

// stickyBuildRuntime returns the container runtime a rebuild should reuse: the
// one already recorded in config, or "" to let BuildMultiscanner auto-detect.
//
// A rebuild with no --runtime used to auto-detect from scratch, and on a
// machine with two engines installed that is not a no-op — sandbox.DetectBest
// returns the first *available* one in priority order, not the one that built
// anything, so an operator with a working docker setup who simply re-ran
// `build-image` got the image rebuilt into *podman's* storage instead (podman
// leads the Windows order). Everything that makes the setup work is
// per-runtime: the image lives in one engine's store, and so do the
// aegis-scanner-cache and aegis-scanner-gocache volumes. So the rebuild
// succeeded, the pin was rewritten to the new engine, and the operator's
// populated vulnerability databases were left behind in podman — surfacing as
// every database-backed scanner reporting "cache not populated" right after a
// successful build, with nothing connecting that to the engine having changed.
//
// Observed on exactly that machine. The recorded runtime is the operator's
// effective choice whether or not they ever typed --runtime, so a rebuild
// honours it; --runtime still overrides, which is how they'd migrate on purpose.
func stickyBuildRuntime(netscanner bool) sandbox.ContainerRuntime {
	cfg, err := config.Load()
	if err != nil {
		return ""
	}
	recorded := cfg.Security.Multiscanner.Runtime
	if netscanner {
		recorded = cfg.Security.Netscanner.Runtime
		if strings.TrimSpace(recorded) == "" {
			// First netscanner build on a machine that already has a
			// multiscanner: follow it rather than auto-detecting, so the pair
			// doesn't end up split across two engines by default.
			recorded = cfg.Security.Multiscanner.Runtime
		}
	}
	return sandbox.ContainerRuntime(strings.TrimSpace(recorded))
}

// siblingDriftWarning reports that the image this command did *not* just build
// is pinned and now built from older source.
//
// It exists because the two images share one build context (that sharing is
// deliberate — one fetch script, one set of pinned versions, one fingerprint),
// which means editing either one's stages moves the fingerprint for both. So
// rebuilding one image leaves the other reporting drift, and nothing at the
// moment of rebuilding says so: the operator fixes the warning they were shown,
// and the next `aegis security status` shows them a fresh one. Twice is enough
// for a warning to become noise, and a drift warning that gets ignored is worse
// than no drift warning at all — the whole point of P55.1 is that this one gets
// read.
//
// built names the image just rebuilt, purely for the message. Returns "" when
// the sibling isn't pinned or isn't stale, so a caller can print unconditionally.
func siblingDriftWarning(built string) string {
	cfg, err := config.Load()
	if err != nil {
		// Not worth failing a successful multi-gigabyte build over: the
		// authoritative drift report is `aegis security status`, and this is a
		// convenience nudge on top of it.
		return ""
	}
	switch built {
	case "multiscanner":
		if drift := security.NetscannerSourceDrift(security.NetscannerPolicyFromConfig(cfg.Security.Netscanner)); drift != "" {
			return "the netscanner image is pinned too, and is now the stale one — both images are built from the same source, so this rebuild moved it out of date. Run `aegis security build-image --netscanner` to bring it along."
		}
	case "netscanner":
		if drift := security.MultiscannerSourceDrift(security.MultiscannerPolicyFromConfig(cfg.Security.Multiscanner)); drift != "" {
			return "the multiscanner image is pinned too, and is now the stale one — both images are built from the same source, so this rebuild moved it out of date. Run `aegis security build-image` to bring it along."
		}
	}
	return ""
}

// multiscannerShadowWarning reports the one upgrade state that makes a
// machine-wide pin invisible: a security.multiscanner block left in this
// repo's .aegis/config.yaml by an older `build-image`, which — since project
// config overrides user config — keeps winning here after the global pin is
// written. Every operator who ran the pre-P55.5 command in a repo has exactly
// this state, and the symptom (scans failing on an image ID that was just
// rewritten, or quietly using an older image) points nowhere near the cause.
//
// justBuilt is the image ID recorded in the global config a moment ago; when
// the project block happens to name the same one there is nothing wrong
// today, but it will go stale at the next rebuild, so that case is warned
// about differently rather than passed over in silence.
func multiscannerShadowWarning(justBuilt string) (string, error) {
	path := config.ProjectConfigPath()
	sec, err := config.FileSecurity(path)
	if err != nil {
		return "", err
	}
	pin, label := strings.TrimSpace(sec.Multiscanner.ImageID), "image_id"
	if pin == "" {
		pin, label = strings.TrimSpace(sec.Multiscanner.Image), "image"
	}
	if pin == "" {
		return "", nil
	}
	remedy := fmt.Sprintf("Delete the security.multiscanner block from %s so the machine-wide pin applies here too, or re-run with --project to keep this repo on its own image.", path)
	if label == "image_id" && pin == strings.TrimSpace(justBuilt) {
		return fmt.Sprintf("%s also pins security.multiscanner (%s %s — the same image just built). Project config overrides user config, so this repo will keep using the project pin, and the next rebuild will update only the user config and leave this one stale. %s", path, label, pin, remedy), nil
	}
	return fmt.Sprintf("%s also pins security.multiscanner (%s %s), and project config overrides user config — scans run from this directory will use that pin, not the one just written. %s", path, label, pin, remedy), nil
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
	var (
		tools      []string
		netscanner bool
	)
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
			if netscanner {
				return runVerifyNetscanner(cmd, cfg.Security.Netscanner, tools)
			}
			return runVerifyImage(cmd, cfg.Security.Multiscanner, tools)
		},
	}
	cmd.Flags().StringSliceVar(&tools, "tool", nil, "verify only these scanners (repeatable, or comma-separated); default is every tool the image claims")
	cmd.Flags().BoolVar(&netscanner, "netscanner", false, "verify the network-facing image instead; needs network access, since that is the thing being verified")
	return cmd
}

// runVerifyNetscanner is the netscanner half of verify-image, shared with
// `build-image --netscanner`'s final step so the two can never report
// differently about the same image.
func runVerifyNetscanner(cmd *cobra.Command, nc config.NetscannerConfig, tools []string) error {
	out := cmd.OutOrStdout()
	policy := security.NetscannerPolicyFromConfig(nc)
	if drift := security.NetscannerSourceDrift(policy); drift != "" {
		fmt.Fprintf(out, "warning: %s\n\n", drift)
	}

	header := false
	results, err := security.VerifyNetscanner(cmd.Context(), policy, tools, func(r security.VerifyResult) {
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

	passed, failed, skipped, _ := security.VerifyCounts(results)
	fmt.Fprintf(out, "\n%d scanner(s) checked: %d verified, %d failed", len(results), passed, failed)
	if skipped > 0 {
		fmt.Fprintf(out, ", %d skipped (no canary possible — see the reasons above)", skipped)
	}
	fmt.Fprintln(out, ".")
	if failed > 0 {
		return fmt.Errorf("%d scanner(s) in %s could not be verified — see the rows above", failed, policy.Image)
	}
	if err := recordVerified("netscanner"); err != nil {
		fmt.Fprintf(out, "\nwarning: verification passed but could not be recorded in config: %v\n", err)
	}
	return nil
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
	// P81.13: record the pass so verifyMultiscannerImage's scan-time gate
	// trusts this image without an operator having to remember a config edit.
	if err := recordVerified("multiscanner"); err != nil {
		fmt.Fprintf(out, "\nwarning: verification passed but could not be recorded in config: %v\n", err)
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
			if drift := security.NetscannerSourceDrift(opts.Netscanner); drift != "" {
				fmt.Fprintf(out, "warning: %s\n\n", drift)
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TOOL\tCATEGORY\tMETHOD\tDETAIL")
			fallbacks := map[string]string{}
			for _, d := range security.Descriptors() {
				r := security.ResolveDetailed(cmd.Context(), d.Name, opts)
				detail := r.Reason
				switch r.Method {
				case security.MethodHost:
					detail = "on PATH"
				case security.MethodContainer:
					detail = fmt.Sprintf("via %s", r.Runtime)
				case security.MethodWSL:
					detail = "via WSL"
				default:
					if note := security.AvailabilityNote(d.Name, r.Reason); note != "" {
						detail = r.Reason + "; " + note
					}
				}
				if r.FallbackWhy != "" {
					fallbacks[d.Name] = r.FallbackWhy
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.Name, d.Category, methodLabel(r.Method), detail)
			}
			tw.Flush()
			// After the table, not before it like the drift warning: this is a
			// footnote about rows the reader has just seen ("those seven say
			// host — here's why that isn't what you asked for"), whereas drift
			// invalidates the whole table and has to be read first.
			if advisory := security.HostFallbackAdvisory(fallbacks); advisory != "" {
				fmt.Fprintf(out, "\nnote: %s\n", advisory)
			}
			// A second table, because these tools resolve through a different
			// resolver against a different image with the opposite network
			// posture (P55.7). Folding them into the table above would report
			// nmap as "not installed" on a machine where it sits inside an image
			// Aegis built — the directory resolver has no opinion about the
			// network path, and never did. trivy and grype appear in both, which
			// is the truth: two paths, either of which can work alone.
			printNetworkFacingStatus(cmd, opts)
			now := time.Now()
			ages := security.DatabaseAges(cmd.Context(), opts)
			if table := security.FormatDatabaseAges(ages, now); table != "" {
				fmt.Fprintf(out, "\n%s", table)
			}
			if w := ages.Warning(now); w != "" {
				fmt.Fprintf(out, "\nwarning: %s\n", w)
			}
			return nil
		},
	}
}

// printNetworkFacingStatus renders how the remote-target tools would run:
// `aegis scan --image` (trivy/grype/dockle against a registry reference) and
// the recon_scan tool (nmap/nuclei against a host list), plus DAST's zap.
func printNetworkFacingStatus(cmd *cobra.Command, opts security.Options) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\nNetwork-facing tools — image scans (`aegis scan --image`) and network recon.\n")
	fmt.Fprintf(out, "These run from the netscanner image, which has network access and mounts no workspace.\n")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TOOL\tMETHOD\tDETAIL")
	fallbacks := map[string]string{}
	for _, name := range security.NetworkFacingTools() {
		r := security.ResolveNetworkDetailed(cmd.Context(), name, opts)
		detail := r.Reason
		switch r.Method {
		case security.MethodHost:
			detail = "on PATH"
		case security.MethodContainer:
			detail = fmt.Sprintf("via %s (netscanner)", r.Runtime)
		case security.MethodWSL:
			detail = "via WSL"
		}
		if r.FallbackWhy != "" {
			fallbacks[name] = r.FallbackWhy
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", name, methodLabel(r.Method), detail)
	}
	tw.Flush()
	if advisory := security.HostFallbackAdvisory(fallbacks); advisory != "" {
		fmt.Fprintf(out, "\nnote: %s\n", advisory)
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
