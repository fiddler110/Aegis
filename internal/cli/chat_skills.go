package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/spf13/cobra"
)

// prepareChatSkills self-heals stale built-in skills, materializes the enabled
// ones (plus any named by --skill) both to the data dir and into the workspace,
// and installs the bundled-skill scanner. It returns the enabled built-in set
// the registry and the <skills_available> index are built from.
//
// Self-heal runs on every run. Both the per-user <dataDir>/builtin-skills copy
// and a project's <cwd>/.aegis/builtin-skills copy are extracted snapshots of
// the embedded skills; after a binary upgrade those on-disk copies can lag what
// this binary ships (e.g. a fixed verify.py), and nothing refreshed a skill
// unless that exact skill was invoked — so a project that materialized
// threat-modeling under an older binary kept running the stale bundled scripts.
// Reconcile whatever is already materialized against the embedded content
// regardless of which skill (if any) this run uses: overwrite-on-diff, only
// files that drifted, only shipped builtins (a user's own bundled skill is
// untouched), never creating skills that aren't already present.
func prepareChatSkills(cmd *cobra.Command, cfg *config.Config, cwd, skillName string, logger *slog.Logger) ([]string, error) {
	refreshBuiltins := func(where string, changed []string, rerr error) {
		if rerr != nil {
			logger.Warn("chat: refreshing stale built-in skills failed", "where", where, "err", rerr)
			return
		}
		if len(changed) > 0 {
			logger.Info("chat: refreshed stale built-in skill files to match this binary", "where", where, "count", len(changed), "files", changed)
			fmt.Fprintf(cmd.ErrOrStderr(), "[notice: refreshed %d stale built-in skill file(s) in the %s to match this binary's embedded copies]\n", len(changed), where)
		}
	}
	dc, de := skills.RefreshDataDirBuiltins(cfg.DataDir)
	refreshBuiltins("data dir", dc, de)
	pc, pe := skills.RefreshProjectBuiltins(cwd)
	refreshBuiltins("project", pc, pe)

	// P38.2: --skill preloads a specific skill for this run. Enable it on
	// top of config's builtin list (so the `skill` tool and the
	// <skills_available> index both see it) and materialize the embedded
	// built-ins to <dataDir>/builtin-skills — the daemon does this at
	// startup, but `aegis chat` runs in-process and never did, so without
	// it a freshly-installed binary's builtin skill body and its bundled
	// scripts (recon.py, verify.py, …) wouldn't be on disk to read.
	enabledBuiltins := cfg.Skills.BuiltinEnabled
	if skillName != "" && skills.IsBuiltin(skillName) {
		enabledBuiltins = appendUnique(enabledBuiltins, skillName)
	}
	if skillName != "" || len(enabledBuiltins) > 0 {
		if err := skills.MaterializeBuiltins(cfg.DataDir); err != nil {
			return nil, fmt.Errorf("materialize built-in skills: %w", err)
		}
		// P39.10: the <dataDir>/builtin-skills copy above is outside the
		// workspace root, so the sandboxed file tools (confined to cwd)
		// reject reading a builtin skill's bundled scripts/skeletons —
		// the model can't reach recon.py/scaffold.py and bails before the
		// build even starts. Mirror the daemon (server.sessions) and also
		// materialize the enabled builtins into <cwd>/.aegis/builtin-skills
		// so the <skill_assets> manifest resolves to a workspace-relative
		// path the file tools accept. skills.Load then prefers this project
		// copy, so skillDir/verify-script paths point inside the workspace.
		if err := skills.MaterializeBuiltinsToProject(cwd, enabledBuiltins); err != nil {
			return nil, fmt.Errorf("materialize built-in skills into workspace: %w", err)
		}
	}

	// Screen untrusted bundled skill directories through the same scan
	// `aegis security scan` drives (P44.1); silent no-op when no scanner
	// is available.
	chatScanOpts := security.OptionsFromConfig(cfg.Security)
	skills.SetBundleScanner(func(ctx context.Context, dir string) []string {
		return security.ScanBundleWarnings(ctx, dir, chatScanOpts)
	})
	return enabledBuiltins, nil
}

// skillPreamble frames a preloaded skill body as authoritative instructions
// ahead of the task, mirroring the TUI's skillTaskMessage (internal/tui) so the
// scripted --skill path and the interactive /threat-model path present the skill
// to the model identically.
func skillPreamble(name, body string) string {
	return fmt.Sprintf("The %s skill has been loaded for you. Its full instructions are below — follow them for this task.\n\n<skill name=%q>\n%s\n</skill>\n\n", name, name, body)
}

// compactFirstSkillMessage rewrites the drive's first user message in place,
// replacing the full preloaded SKILL.md body with a compact pointer plus the
// original task (P39.5). It reports whether it actually rewrote anything: it is
// a no-op (returning false) unless the first message is still the user
// preamble message skillPreamble produced — engine compaction may have already
// rewritten it, in which case there is nothing to do. After a rewrite it calls
// conv.Invalidate so the cached token estimate and persistence offset stay
// correct.
func compactFirstSkillMessage(conv *engine.Conversation, skillName, taskPrompt, skillDir string) bool {
	if skillName == "" || len(conv.Messages) == 0 {
		return false
	}
	first := conv.Messages[0]
	if first.Role != provider.RoleUser || len(first.Content) == 0 {
		return false
	}
	txt, ok := first.Content[0].(provider.TextBlock)
	if !ok || !strings.Contains(txt.Text, "<skill name=") {
		return false // not (or no longer) the full preamble message
	}
	conv.Messages[0].Content = []provider.Block{provider.TextBlock{Text: compactSkillPreamble(skillName, taskPrompt, skillDir)}}
	conv.Invalidate()
	return true
}

// compactSkillPreamble is the slimmed replacement for skillPreamble used after
// the opening drive turn (P39.5): it reminds the model the skill's instructions
// were already given and remain in force, names the on-disk file to re-read for
// any specific rule, and re-states the task — without re-sending the ~9K-token
// body every turn.
func compactSkillPreamble(name, taskPrompt, skillDir string) string {
	ref := "its bundled instructions"
	if skillDir != "" {
		ref = fmt.Sprintf("`%s` (and its `references/`)", filepath.ToSlash(filepath.Join(skillDir, "SKILL.md")))
	}
	return fmt.Sprintf("The %s skill's full instructions were provided at the start of this task and remain in effect — keep following them. They are not repeated here so the context stays small enough to work in; re-read %s if you need a specific rule.\n\n", name, ref) + taskPrompt
}

// scanPendingMarkers walks root (typically <cwd>/.aegis) and returns the
// root-relative paths of text files that still contain a `<!-- PENDING`
// marker — the stub-first pattern multi-phase skills use to mark unfinished
// files. The match is the marker *prefix*, not the exact `<!-- PENDING -->`
// literal: scaffold.py now emits section-keyed markers
// (`<!-- PENDING: deployment-classification -->`, P38.7) so an `edit_file` can
// target one section without a `replace_all` file-nuke, and the prefix catches
// those as well as any bare legacy marker. Only small text-ish files are read
// (the marker only ever appears in generated markdown/yaml/mmd), so the walk
// stays cheap. A missing root yields no matches.
func scanPendingMarkers(root string) []string {
	const marker = "<!-- PENDING"
	const maxFileSize = 1 << 20 // 1 MiB — generated report files are far smaller
	var hits []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// P39.11: the materialized builtin-skills assets (skeleton
			// templates, SKILL.md/README) carry their own `<!-- PENDING`
			// markers as examples; counting them would keep the drive's
			// completion oracle permanently non-empty (it never converges,
			// phase-6 verify never fires). Skip that subtree — it is skill
			// source, not build output.
			if d.Name() == pendingSkipDir {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".mmd", ".markdown", ".yaml", ".yml", ".txt":
		default:
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > maxFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), marker) {
			if rel, err := filepath.Rel(root, path); err == nil {
				hits = append(hits, filepath.ToSlash(rel))
			} else {
				hits = append(hits, filepath.ToSlash(path))
			}
		}
		return nil
	})
	sort.Strings(hits)
	return hits
}

// suiteFileCount returns how many text-ish files exist anywhere under root
// (typically <cwd>/.aegis). The P38.6 drive-to-completion floor check uses it to
// tell "finished — every marker resolved" from "nothing was ever written" —
// both of which leave scanPendingMarkers empty, but only the latter is a
// fabricated success. A missing root yields zero.
func suiteFileCount(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// P39.11: exclude the materialized builtin-skills source (see
			// scanPendingMarkers) — otherwise its asset files count as "suite
			// written", defeating the P38.6 fabricated-success floor check that
			// distinguishes "finished" from "nothing was ever written".
			if d.Name() == pendingSkipDir {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".mmd", ".markdown", ".yaml", ".yml", ".txt":
			n++
		}
		return nil
	})
	return n
}

// pendingSkipDir is the subdirectory of <cwd>/.aegis holding builtin skill
// source materialized into the workspace (P39.10) — skeleton templates and
// SKILL.md bodies that carry their own `<!-- PENDING` example markers. Both
// drive-completion scans skip it so skill source is never mistaken for build
// output. Mirrors skills.builtinSkillsDirName (kept as a local literal to avoid
// exporting an internal constant across the package boundary).
const pendingSkipDir = "builtin-skills"

// skillPhaseSpecs returns the named skill's declared `phases:` plan (P52.12),
// or nil when the skill has none, cannot be found, or none was named.
//
// Read here — before the workspace is otherwise set up — only so the P47.5(a)
// up-front window sizing knows whether the run will be phased. skills.Discover
// memoizes its walk, so this costs nothing beyond the first call; the drive
// itself uses the specs carried on the Skill it loads later.
func skillPhaseSpecs(cfg *config.Config, skillName string) []skills.PhaseSpec {
	if skillName == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	sk, ok := skills.Load(cwd, cfg.DataDir, appendUnique(cfg.Skills.BuiltinEnabled, skillName), skillName)
	if !ok {
		return nil
	}
	return sk.Phases
}
