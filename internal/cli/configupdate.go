package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/spf13/cobra"
	yamlv3 "go.yaml.in/yaml/v3"
)

// ─── Shared addition content ───────────────────────────────────────────────
//
// New template content is written once here and spliced into
// globalConfigTemplateRaw (for fresh --first-init) AND into configAdditions
// below (for `aegis config update` against a config file created by an
// older version), so the two paths never drift apart. The project
// template's own copy is illustrative-only static text (see
// projectConfigTemplateRaw) since it lives inside an already-commented
// example block there, not a live section — it isn't wired through this
// shared const.
//
// Every addition is wrapped in a "aegis:added <id>"/"aegis:end <id>" marker
// comment pair. The marker text is what makes `config update` idempotent
// (skip if already present) — never reuse an id for a different meaning
// once it has shipped.
//
// The block always assumes it's being spliced as a sibling of other 2-space-
// indented, single-`#`-commented fields directly under an ACTIVE top-level
// key (e.g. a real `security:` mapping) — that's the only case `config
// update` ever auto-splices into (see insertIntoSection).

const securityScannerConfigID = "security-scanner-config"

const securityScannerAdditionBlock = `  # ── Per-scanner config for ` + "`aegis scan`" + ` / the security_scan tool ──────────
  # aegis:added security-scanner-config
  # default_method: auto        # host | container | auto (default).
  #                             # Under "auto", a tool the multiscanner image carries
  #                             # prefers the CONTAINER over a host binary (P55.4):
  #                             # host binaries are unpinned and unconfined, so two
  #                             # machines can silently scan with different rule sets.
  #                             # A refused container falls back to host, and says so.
  # wsl_distro: kali-linux      # Windows only: target this WSL distro for nmap/nuclei/
  #                             # opengrep/kubescape (P14.x) instead of the WSL default —
  #                             # recommended for red-team/recon work (see docs/security.md)
  # tools:                      # keyed by scanner name (aegis scan --list)
  #   opengrep:   { enabled: true, method: host }
  #   trivy:      { enabled: true, image: "trivy@sha256:..." }
  #   gitleaks:   { enabled: true }
  #   trufflehog: { enabled: true, verify: false }   # live credential check, host-only
  #   nmap:       { method: wsl }                    # force WSL over a flaky native Windows install
  #   nuclei:     { method: wsl, templates_version: "v9.62.0" }
  # dast:                       # dast_scan target-authorization policy
  #   allowed_targets: []       # hostnames/".suffix" wildcards/CIDRs
  #   allow_active: false       # opt-in for active/api scan modes
  # debate:                     # route findings through a P12 debate round
  #   threat_model: false
  #   triage: false
  # aegis:end security-scanner-config`

const securityContainerImagesID = "security-container-images"

// securityContainerImagesAdditionBlock documents the two locally-built scanner
// images (P55.x). Both blocks are written by `aegis security build-image` and
// are not meant to be hand-edited — they are documented here anyway because
// where the pin lives is an operator decision with a sharp edge (a project-level
// block shadows the machine-wide one and fails closed), and a config file that
// never mentions the images at all is how an operator concludes that installing
// fifteen host binaries is still the only way to scan.
const securityContainerImagesAdditionBlock = `  # ── Container scanner images (` + "`aegis security build-image`" + `) ───────────────
  # aegis:added security-container-images
  # Both blocks below are WRITTEN BY ` + "`aegis security build-image`" + `, not hand-edited.
  # image_id is re-verified via "image inspect" before every container run, so a
  # stale value fails scans closed with a specific reason rather than silently
  # running some other image.
  #
  # The pins are MACHINE-WIDE: they belong in this user config, because the
  # images and the shared database volume are machine-wide too. A multiscanner
  # or netscanner block left behind in a repo's .aegis/config.yaml SHADOWS this
  # one (project config wins) and the next scan fails with "no longer matches
  # the ID recorded in config". Only use "build-image --project" when a repo
  # deliberately runs a different image.
  #
  # multiscanner — runs with --network none and the workspace mounted. Carries
  #   the filesystem scanners: trivy, gitleaks, trufflehog, syft, osv-scanner,
  #   grype, kubescape, hadolint, opengrep, and (profile "full") bandit,
  #   brakeman, njsscan, gosec, nmap, nuclei. Its vulnerability databases are
  #   NOT baked into the image — run "aegis security update-db" afterwards to
  #   fill the shared volume, and re-run it periodically: a stale database
  #   under-reports rather than failing. "aegis security status" shows its age.
  # multiscanner:
  #   enabled: true
  #   image: "localhost/aegis-multiscanner:v1"
  #   image_id: "sha256:..."     # recorded at build time; re-verified before use
  #   runtime: docker            # the engine that BUILT it — a locally-built image
  #                              # exists only in that engine's storage, so
  #                              # auto-detection could look in the wrong one
  #   concurrency: 3             # scanners running at once; 1 = strictly sequential
  #   tools: []                  # what the built profile actually carries
  #
  # netscanner — the second image ("build-image --netscanner"). It runs with
  #   network ON and the workspace NEVER mounted; the split is mount posture,
  #   not tool category. nmap, nuclei, and image-reference scanning with
  #   trivy/grype each scan a REMOTE target, so none of them needs your source
  #   and all of them need egress. It needs no update-db — having network, it
  #   refreshes its own databases into a separate volume.
  # netscanner:
  #   enabled: true
  #   image: "localhost/aegis-netscanner:v1"
  #   image_id: "sha256:..."
  #   runtime: docker
  #
  # Host-only carve-outs, in neither image: dockle (inspects images through the
  # container engine socket — effectively host root, which no scanner image
  # grants) and zap (a large Java app with its own official image and mount
  # contract). Install those with "aegis security install <tool>".
  # aegis:end security-container-images`

// configAddition is one incremental piece of template content that a config
// file generated by an older version of Aegis may be missing.
type configAddition struct {
	id         string // marker id — never reused once shipped
	sectionKey string // top-level YAML key this extends, e.g. "security"
	block      string // 2-space-indented, commented block to splice in
}

// configAdditions lists every incremental addition `aegis config update`
// knows how to reconcile. Append to this list (never edit an existing
// entry's id) whenever SecurityConfig or another top-level section grows a
// field the templates don't yet document.
var configAdditions = []configAddition{
	{
		id:         securityScannerConfigID,
		sectionKey: "security",
		block:      securityScannerAdditionBlock,
	},
	{
		id:         securityContainerImagesID,
		sectionKey: "security",
		block:      securityContainerImagesAdditionBlock,
	},
}

// updateConfigFile reconciles a single existing config file against
// configAdditions, extending only sections that already exist as an active,
// uncommented top-level key, and only with content not already present
// (detected via the "aegis:added <id>" marker). It never touches anything
// else in the file — no reformatting, no rewriting of existing keys — and
// refuses to write if the result doesn't parse as valid YAML.
//
// When a section isn't active in the file (missing entirely, or still only
// a commented-out example), the addition is not guessed at — it's reported
// so the user can add it by hand, since splicing into a comment-only example
// block reliably is a much harder text-matching problem than extending a
// real mapping, and guessing wrong risks a confusing result.
func updateConfigFile(path string, dryRun bool) (report []string, changed bool, err error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{fmt.Sprintf("%s: not found, nothing to update (run --first-init/--init first)", path)}, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	hasCRLF := strings.Contains(string(raw), "\r\n")
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")

	var applied []string
	for _, add := range configAdditions {
		marker := "aegis:added " + add.id
		if strings.Contains(text, marker) {
			continue // already applied in an earlier run
		}
		newText, ok := insertIntoSection(text, add.sectionKey, add.block)
		if !ok {
			report = append(report, fmt.Sprintf(
				"%s: no active %q section found to extend — add this manually (inside a `%s:` mapping):\n%s",
				path, add.sectionKey, add.sectionKey, add.block))
			continue
		}
		text = newText
		applied = append(applied, add.id)
	}

	if len(applied) == 0 {
		return report, false, nil
	}

	// Safety net: never write a file that no longer parses as YAML, no
	// matter how confident the text-splice logic above was.
	var probe map[string]any
	if err := yamlv3.Unmarshal([]byte(text), &probe); err != nil {
		return nil, false, fmt.Errorf("update to %s would produce invalid YAML, aborting without writing: %w", path, err)
	}

	out := text
	if hasCRLF {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}

	if dryRun {
		report = append([]string{fmt.Sprintf("%s: would add %s (dry run, not written)", path, strings.Join(applied, ", "))}, report...)
		return report, true, nil
	}

	backup := fmt.Sprintf("%s.bak-%d", path, time.Now().Unix())
	if err := os.WriteFile(backup, raw, 0o600); err != nil {
		return nil, false, fmt.Errorf("write backup %s: %w", backup, err)
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return nil, false, fmt.Errorf("write %s: %w", path, err)
	}
	report = append([]string{fmt.Sprintf("%s: added %s (backup saved to %s)", path, strings.Join(applied, ", "), backup)}, report...)
	return report, true, nil
}

// sectionBoundaryRe matches the first column-0, non-blank line, which is where
// a top-level section ends. Constant, so it is compiled once at package load
// rather than on every insertIntoSection call — it was the only per-call
// MustCompile left in the tree outside table construction. startRe below is
// key-dependent and stays inline.
var sectionBoundaryRe = regexp.MustCompile(`(?m)^\S`)

// insertIntoSection finds the top-level (column-0), active "<key>:" line in
// text and splices block in just before the section ends — the first
// subsequent line that itself starts at column 0 (the next top-level key or
// a banner comment), or EOF if the section runs to the end of the file.
// Every line belonging to a section in these templates is blank or indented,
// so a column-0, non-blank line reliably marks the boundary. Returns
// ok=false without modifying text if no active "<key>:" line is found (this
// intentionally does not match a commented-out "# <key>:" example line —
// see the doc comment on updateConfigFile).
func insertIntoSection(text, key, block string) (string, bool) {
	startRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:[ \t]*(#.*)?$`)
	loc := startRe.FindStringIndex(text)
	if loc == nil {
		return "", false
	}
	rest := text[loc[1]:]
	insertPos := len(text)
	if b := sectionBoundaryRe.FindStringIndex(rest); b != nil {
		insertPos = loc[1] + b[0]
	}
	head := strings.TrimRight(text[:insertPos], "\n")
	tail := text[insertPos:]
	if tail == "" {
		return head + "\n" + block + "\n", true
	}
	return head + "\n" + block + "\n\n" + tail, true
}

func newConfigUpdateCmd() *cobra.Command {
	var (
		global  bool
		project bool
		dryRun  bool
	)
	c := &cobra.Command{
		Use:   "update",
		Short: "Reconcile an existing config file with newer template content, without discarding customizations",
		Long: `Merges template content added to Aegis since a config file was created
(e.g. new security-scanner options) into that existing file. Existing keys,
comments, and formatting are left untouched — only genuinely new, previously
absent content is spliced in, and only into a section you're already using
(an active top-level key). A backup of the original is written alongside it
before any change.

With no flags, both the global config (` + config.GlobalConfigPath() + `) and the
project config (./` + config.ProjectConfigPath() + `) are reconciled if they exist.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !global && !project {
				global, project = true, true
			}
			var anyChanged bool
			if global {
				report, changed, err := updateConfigFile(config.GlobalConfigPath(), dryRun)
				if err != nil {
					return err
				}
				for _, line := range report {
					fmt.Fprintln(cmd.OutOrStdout(), line)
				}
				anyChanged = anyChanged || changed
			}
			if project {
				report, changed, err := updateConfigFile(config.ProjectConfigPath(), dryRun)
				if err != nil {
					return err
				}
				for _, line := range report {
					fmt.Fprintln(cmd.OutOrStdout(), line)
				}
				anyChanged = anyChanged || changed
			}
			if !anyChanged {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing to update")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&global, "global", false, "only reconcile the global config")
	c.Flags().BoolVar(&project, "project", false, "only reconcile the project config")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	return c
}
