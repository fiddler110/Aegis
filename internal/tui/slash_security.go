package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/security"
)

func (d *SlashDispatcher) cmdSecurityConfig(args []string) SlashResult {
	if _, err := config.Load(); err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load config: %v", err), IsError: true}
	}
	global := len(args) > 0 && strings.ToLower(args[0]) == "global"
	return SlashResult{SecurityConfigGlobal: &global}
}

// cmdSecurity is the P14.2 in-session umbrella for the security-tooling
// surface: /security status, /security baseline, and /security install
// mirror `aegis security status/baseline/install`, which were previously
// CLI-only even though the model's security_scan tool and /security-config
// already ran in-session. /security config just delegates to the existing
// /security-config dialog rather than duplicating it. Like /sandbox and
// /security-config, this reads the TUI process's own config/workspace
// directly (no daemon round trip) — consistent with that existing pattern.
func (d *SlashDispatcher) cmdSecurity(args []string) SlashResult {
	sub := ""
	var rest []string
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
		rest = args[1:]
	}
	switch sub {
	case "", "status":
		return d.cmdSecurityStatus()
	case "baseline":
		return d.cmdSecurityBaseline(rest)
	case "config":
		return d.cmdSecurityConfig(rest)
	case "install":
		return d.cmdSecurityInstall(rest)
	default:
		return SlashResult{Output: fmt.Sprintf("Unknown /security subcommand %q.\nUsage: /security [status|install <tool>|baseline [path]|config [global]]", args[0]), IsError: true}
	}
}

// securityMethodLabel mirrors the CLI's methodLabel (internal/cli/security.go)
// — kept as a separate unexported copy rather than shared, since internal/cli
// isn't (and shouldn't become) an import of internal/tui.
func securityMethodLabel(m security.Method) string {
	switch m {
	case security.MethodHost:
		return "host"
	case security.MethodContainer:
		return "container"
	default:
		return "unavailable"
	}
}

// cmdSecurityStatus mirrors `aegis security status`: for each known scanner,
// shows whether it will actually run via host binary, a configured container
// image, or not at all (with the reason and, per P13.1, which other OSes have
// a guided host install when this one doesn't).
func (d *SlashDispatcher) cmdSecurityStatus() SlashResult {
	cfg, err := config.Load()
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load config: %v", err), IsError: true}
	}
	opts := security.OptionsFromConfig(cfg.Security)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TOOL\tCATEGORY\tMETHOD\tDETAIL")
	for _, dsc := range security.Descriptors() {
		method, rt, _, reason := security.Resolve(ctx, dsc.Name, opts)
		detail := reason
		switch method {
		case security.MethodHost:
			detail = "on PATH"
		case security.MethodContainer:
			detail = fmt.Sprintf("via %s", rt)
		default:
			if note := security.AvailabilityNote(dsc.Name, reason); note != "" {
				detail = reason + "; " + note
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", dsc.Name, dsc.Category, securityMethodLabel(method), detail)
	}
	tw.Flush()
	return SlashResult{Output: b.String()}
}

// cmdSecurityBaseline mirrors `aegis security baseline [path]`: view-only
// listing of .aegis/security-baseline.yaml's suppression entries and each
// one's status (active/expired/invalid).
func (d *SlashDispatcher) cmdSecurityBaseline(args []string) SlashResult {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to resolve path: %v", err), IsError: true}
	}
	bl, err := security.LoadBaseline(abs)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load baseline: %v", err), IsError: true}
	}
	if bl == nil || len(bl.Suppressions) == 0 {
		return SlashResult{Output: fmt.Sprintf("no baseline entries (%s not found or empty)", security.BaselinePath(abs))}
	}

	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tRULE_ID\tLOCATION\tEXPIRES\tREASON")
	now := time.Now()
	for _, e := range bl.Suppressions {
		loc, exp := e.Location, e.Expires
		if loc == "" {
			loc = "(any)"
		}
		if exp == "" {
			exp = "(missing)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", security.SuppressionStatusLabel(e, now), e.RuleID, loc, exp, e.Reason)
	}
	tw.Flush()
	return SlashResult{Output: b.String()}
}

// cmdSecurityInstall mirrors `aegis security install <tool>`'s guided,
// approval-gated host install, adapted to the slash-command shape: since a
// slash command returns one SlashResult with no interactive stdin prompt
// (unlike the CLI's y/N reader), the first invocation only shows the tool
// summary and exact host command; the caller must re-run with a trailing
// "confirm" to actually execute it. This keeps the same "never install
// silently" posture as the CLI/`/security-config` guided-install flow
// without adding new dialog/confirmation-view plumbing.
func (d *SlashDispatcher) cmdSecurityInstall(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "usage: /security install <tool> [confirm]", IsError: true}
	}
	name := args[0]
	confirmed := len(args) > 1 && strings.ToLower(args[1]) == "confirm"

	dsc, ok := security.DescriptorFor(name)
	if !ok {
		return SlashResult{Output: fmt.Sprintf("Unknown scanner %q. Run /security status to see known tools.", name), IsError: true}
	}
	command, ok := security.InstallCommand(name)
	if !ok {
		return SlashResult{Output: fmt.Sprintf("No guided install available for %s on this OS — configure security.tools.%s.image for a container fallback, or use /security-config.", dsc.Name, dsc.Name), IsError: true}
	}

	if !confirmed {
		return SlashResult{Output: fmt.Sprintf(
			"%s — %s\n\nThis will run the following command on your host:\n\n    %s\n\nRun `/security install %s confirm` to proceed, or use /security-config for the interactive dialog.",
			dsc.Name, dsc.Summary, command, name,
		)}
	}

	var out strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := security.RunGuidedInstall(ctx, name, &out); err != nil {
		return SlashResult{Output: fmt.Sprintf("%sInstall failed: %v", out.String(), err), IsError: true}
	}
	fmt.Fprintf(&out, "\n%s installed. Run /security status to confirm.\n", dsc.Name)
	return SlashResult{Output: out.String()}
}

// scanSelectorTokens splits raw on commas and resolves every token via
// security.ResolveSelector (an exact scanner name or a category alias like
// "secrets"/"sast"); returns the raw (unresolved) tokens if every one of them
// is recognized, nil otherwise. The tokens are sent as-is to the daemon,
// which resolves them again via the same security.ResolveSelector — this is
// purely a "does args[0] look like a scanner selector, or is it a path"
// dispatch decision, not the actual resolution.
func scanSelectorTokens(raw string) []string {
	parts := strings.Split(raw, ",")
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil
		}
		if _, ok := security.ResolveSelector(p); !ok {
			return nil
		}
		tokens = append(tokens, p)
	}
	return tokens
}

// cmdScan runs the security scanners directly against the daemon's workspace
// and prints the formatted report — no model turn spent, same underlying scan
// as `aegis scan`/the security_scan tool. Usage:
//
//	/scan                        preview the auto-detected plan for the whole workspace
//	/scan confirm                run the previewed plan
//	/scan <path>                 preview the auto-detected plan for a subdirectory
//	/scan <path> confirm         run it
//	/scan <scanner-or-category>[,<...>] [path]   run only the named scanner(s)/category(ies)
//	                             immediately, no preview — e.g. /scan trufflehog, /scan secrets,
//	                             /scan gitleaks,trufflehog src/
//	/scan image <ref>            scan a container image reference instead
//	/scan sbom [path]            generate a CycloneDX SBOM instead of a findings report
//	/scan network <target...>    run nmap+nuclei (recon_scan) against a host/IP/CIDR list
//	/scan list                  list every valid scanner name/category, with live availability
//
// A bare /scan (or /scan <path>) doesn't run anything on first invocation: it
// auto-detects the project's language, auto-enables the matching opt-in SAST
// engine (gosec/bandit/brakeman/njsscan), skips hadolint/kubescape when the
// workspace has no Dockerfile/Kubernetes manifest for them to analyze, and
// prints that plan for review — mirroring cmdSecurityInstall's "no
// interactive stdin, so require a second invocation with a trailing confirm"
// convention rather than adding new dialog/overlay plumbing. Naming specific
// scanners/categories is already an explicit choice, so that branch (and
// image/sbom/network/list) skips the preview and runs immediately, same as
// before.
//
// A scan can take a while (container fallback pulls, multiple scanner
// binaries), so this uses a long timeout rather than the 5s default other
// direct-daemon-call commands use.
func (d *SlashDispatcher) cmdScan(args []string) SlashResult {
	var req api.ScanRequest
	switch {
	case len(args) >= 1 && strings.ToLower(args[0]) == "image":
		if len(args) < 2 {
			return SlashResult{Output: "usage: /scan image <ref>", IsError: true}
		}
		req.Image = args[1]
	case len(args) >= 1 && strings.ToLower(args[0]) == "sbom":
		req.SBOM = true
		if len(args) >= 2 {
			req.Path = args[1]
		}
	case len(args) >= 1 && strings.ToLower(args[0]) == "network":
		if len(args) < 2 {
			return SlashResult{Output: "usage: /scan network <target> [target...]", IsError: true}
		}
		req.Targets = args[1:]
	case len(args) >= 1 && strings.ToLower(args[0]) == "list":
		return d.cmdScanList()
	case len(args) >= 1 && scanSelectorTokens(args[0]) != nil:
		req.Scanners = scanSelectorTokens(args[0])
		if len(args) >= 2 {
			req.Path = args[1]
		}
	default:
		rest := args
		confirmed := len(rest) > 0 && strings.ToLower(rest[len(rest)-1]) == "confirm"
		if confirmed {
			rest = rest[:len(rest)-1]
		}
		if len(rest) >= 1 {
			req.Path = rest[0]
		}
		if !confirmed {
			return d.cmdScanPreview(req.Path)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	resp, err := d.client.Scan(ctx, req)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Scan failed: %v", err), IsError: true}
	}
	return SlashResult{Output: resp.Report}
}

// cmdScanPreview computes and prints the same auto-detected plan `aegis
// scan`'s interactive picker previews (language detection +
// AutoEnableLanguageScanners + PlanScanners' relevance gating) for path
// (default: workspace root) — resolved entirely locally, same as
// cmdScanList/cmdSecurityStatus, since the plan doesn't need a live scan run
// to know what it would do.
func (d *SlashDispatcher) cmdScanPreview(path string) SlashResult {
	cfg, err := config.Load()
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load config: %v", err), IsError: true}
	}
	dir := "."
	if path != "" {
		dir = path
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to resolve path: %v", err), IsError: true}
	}
	if _, err := os.Stat(abs); err != nil {
		return SlashResult{Output: fmt.Sprintf("path not found: %s", dir), IsError: true}
	}
	opts := security.AutoEnableLanguageScanners(abs, security.OptionsFromConfig(cfg.Security))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var b strings.Builder
	if langs := security.DetectLanguageSummary(abs); len(langs) > 0 {
		parts := make([]string, len(langs))
		for i, l := range langs {
			if l.Scanner != "" {
				parts[i] = fmt.Sprintf("%s (%s)", l.Language, l.Scanner)
			} else {
				parts[i] = l.Language
			}
		}
		fmt.Fprintf(&b, "Detected: %s\n\n", strings.Join(parts, ", "))
	}
	fmt.Fprintln(&b, "Planned scan:")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, e := range security.PlanScanners(ctx, abs, security.DefaultScanners(), opts) {
		if e.Skipped {
			fmt.Fprintf(tw, "  --\t%s\tskip: %s\n", e.Scanner.Name(), e.Reason)
			continue
		}
		fmt.Fprintf(tw, "  ->\t%s\t%s\n", e.Scanner.Name(), e.Method)
	}
	tw.Flush()

	confirmArgs := "confirm"
	if path != "" {
		confirmArgs = path + " confirm"
	}
	fmt.Fprintf(&b, "\nRun `/scan %s` to proceed, or `/scan <scanner-or-category>[,...] [path]` to run only "+
		"specific scanners instead (e.g. /scan secrets, /scan gitleaks,trufflehog).\n", confirmArgs)
	return SlashResult{Output: b.String()}
}

// cmdScanList lists every scanner name and category alias usable with
// `/scan <selector>` (with live availability) — the same live-resolved
// TOOL/CATEGORY/METHOD/DETAIL shape cmdSecurityStatus prints, plus a
// DEFAULT column and the category-alias groupings /security status has no
// reason to know about. Resolved locally (config.Load + security.Resolve),
// same as cmdSecurityStatus, not via a daemon round trip.
func (d *SlashDispatcher) cmdScanList() SlashResult {
	cfg, err := config.Load()
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load config: %v", err), IsError: true}
	}
	opts := security.OptionsFromConfig(cfg.Security)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SCANNER\tCATEGORY\tDEFAULT\tSTATUS")
	for _, dsc := range security.Descriptors() {
		method, rt, _, reason := security.Resolve(ctx, dsc.Name, opts)
		status := reason
		switch method {
		case security.MethodHost:
			status = "on PATH"
		case security.MethodContainer:
			status = fmt.Sprintf("via %s", rt)
		default:
			if note := security.AvailabilityNote(dsc.Name, reason); note != "" {
				status = reason + "; " + note
			}
		}
		defaultLabel := "opt-in"
		if dsc.DefaultEnabled {
			defaultLabel = "enabled"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", dsc.Name, dsc.Category, defaultLabel, status)
	}
	tw.Flush()

	fmt.Fprintf(&b, "\nCategory aliases (/scan <alias> runs every scanner in the group):\n")
	catTw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range security.CategoryAliases() {
		fmt.Fprintf(catTw, "  %s\t-> %s\n", c.Name, strings.Join(c.Scanners, ", "))
	}
	catTw.Flush()
	return SlashResult{Output: b.String()}
}
