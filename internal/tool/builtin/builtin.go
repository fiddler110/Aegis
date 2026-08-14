// Package builtin provides the harness's built-in tools: file access, search,
// shell execution, and web access. File and shell tools are confined to a
// workspace root so the agent cannot reach outside the project by default.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/longmem"
	"github.com/fiddler110/aegis/internal/lsp"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/toolpath"
)

// Options configures the built-in tool set.
type Options struct {
	// Root is the workspace directory file/search/shell tools are confined to.
	Root string
	// DataDir is the per-user data directory; when set, memory/skill tools are
	// registered.
	DataDir string
	// ShellTimeoutSec bounds how long a shell command may run (0 -> default).
	ShellTimeoutSec int
	// HTTPUserAgent is sent by the web tools.
	HTTPUserAgent string
	// KrokiURL is the diagram rendering endpoint.
	KrokiURL string
	// Tasks, when set, enables background jobs: the shell tool's background
	// option and the task_* management tools.
	Tasks *task.Manager
	// Cron, when set, enables recurring-job tools (cron_create, etc.).
	Cron *cron.Scheduler
	// Sandbox, when set, routes shell execution through a sandbox backend
	// (local, Docker, Podman, etc.). Nil = direct local execution.
	Sandbox sandbox.Backend
	// FileTracker, when set, enables file staleness detection. Write/edit
	// tools reject edits to files modified externally since last read.
	FileTracker *filetracker.Tracker
	// Commands resolves optional external host binaries (ripgrep and friends)
	// honoring the user's `commands:` config. Nil means every such tool is
	// treated as absent and the built-in fallbacks are used — which is what
	// tests want, and keeps a missing resolver from silently reaching for
	// whatever happens to be on the host's PATH.
	Commands *toolpath.Resolver
	// LSP, when set, enables code intelligence tools (diagnostics, references).
	LSP *lsp.Manager
	// TodoList, when set, enables planning tools (todo_add, todo_update, todo_list).
	TodoList *TodoList
	// Questioner, when set, enables the ask_user tool for structured questions.
	Questioner Questioner
	// Knowledge, when set, enables the project_knowledge search tool (P3.3).
	Knowledge *knowledge.Store
	// KnowledgeProvider, when set, resolves a session-scoped knowledge store
	// per call from the effective root (P25.9) instead of always using the
	// fixed Knowledge store above. Optional — nil keeps today's behavior.
	KnowledgeProvider KnowledgeProvider
	// LongMem, when set, enables entity_remember and entity_recall tools (P3.1).
	LongMem *longmem.Store
	// Search selects the web_search provider (P5.3). Empty provider uses the
	// zero-config DuckDuckGo scrape.
	Search SearchOptions
	// TeamTasks, when set, enables the agent-team coordination tools (P5.1):
	// shared task list + peer messaging.
	TeamTasks *swarm.TaskList
	// MailboxRoot is the on-disk root for team mailboxes (P5.1); required for
	// the peer-messaging tools.
	MailboxRoot string
	// BuiltinSkills names which embedded built-in skills (shipped in the
	// binary; see internal/skills) are active. Empty keeps them all dormant.
	BuiltinSkills []string
	// SecurityScan configures the security_scan tool's per-scanner policy
	// (host vs container execution, enable/disable) — P11.11's config
	// surface, translated from config.SecurityConfig via
	// security.OptionsFromConfig. Zero value runs every built-in scanner in
	// "auto" mode (host binary if present, else skip — no container image
	// configured by default).
	SecurityScan security.Options
	// DASTAllowedTargets and DASTAllowActive configure the hard,
	// mode-independent target-authorization gate (P11.7) shared by both
	// dast_scan and recon_scan (P13.5.1/.2) — config.SecurityConfig.DAST,
	// translated by the caller. Zero value only permits loopback/private
	// targets and passive (baseline) scans.
	DASTAllowedTargets []string
	DASTAllowActive    bool
	// LocalProfile is P25.6's local-model prompt profile: when true,
	// web_search/web_fetch/security_scan/git_pr are registered deferred
	// (name+description only, loaded on demand via tool_search) instead of
	// always-exposed, cutting per-turn schema tokens for small local models
	// that pay for every always-exposed schema in prompt-processing latency.
	// False (the default profile) keeps today's behavior unchanged.
	LocalProfile bool
	// GitPreCommitTestCommand, when set, is a shell command the git_commit tool
	// runs in the workspace before every commit; a non-zero exit aborts the
	// commit (P46.2, config.GitConfig.PreCommitTestCommand). Empty = no gate.
	GitPreCommitTestCommand string
	// GitPreCommitTestTimeout bounds GitPreCommitTestCommand; 0 uses
	// config.DefaultPreCommitTestTimeoutSec.
	GitPreCommitTestTimeout time.Duration
	// ToolFamilies re-enables deferred tool families LocalProfile would
	// otherwise omit (P62.6, config.ToolsConfig.Families). Ignored under the
	// default profile, which registers every family. See ToolFamilies for the
	// names and localOmittedFamilies for what the profile drops.
	ToolFamilies []string
}

// ToolFamilies are the optional deferred tool families the local prompt profile
// can omit, and the names config.ToolsConfig.Families accepts.
var ToolFamilies = []string{familyTeam, familyCron, familyEntity}

const (
	familyTeam   = "team"   // team_send/team_inbox/team_task_* — swarm coordination
	familyCron   = "cron"   // cron_create/list/toggle/delete/history — background scheduling
	familyEntity = "entity" // entity_remember/entity_recall — long-term memory
)

// localOmittedFamilies are the families the local prompt profile drops (P62.6).
//
// Between them they are thirteen of the local profile's twenty-six deferred
// tools and ~570 tokens of every turn's prompt, spent advertising three
// capabilities a small-model, file-scoped task does not reach for: coordinating
// with teammates that a solo session does not have, scheduling background jobs,
// and writing to a cross-session entity store. None of the three is *unusable*
// in a local session, which is why this is a profile default with an additive
// override rather than a deletion — see config.ToolsConfig.Families.
//
// Deliberately not extended to latex/security/lsp: those are the families a
// local run genuinely does reach for, and the P34.3 preload path already
// depends on a persona being able to name a deferred security tool.
var localOmittedFamilies = []string{familyTeam, familyCron, familyEntity}

// familyEnabled reports whether family should be registered under opts.
func (o Options) familyEnabled(family string) bool {
	if !o.LocalProfile {
		return true
	}
	omitted := false
	for _, f := range localOmittedFamilies {
		if f == family {
			omitted = true
			break
		}
	}
	if !omitted {
		return true
	}
	for _, f := range o.ToolFamilies {
		if strings.EqualFold(strings.TrimSpace(f), family) {
			return true
		}
	}
	return false
}

// SearchOptions configures the web_search tool's provider.
type SearchOptions struct {
	Provider string
	APIKey   string
	BaseURL  string
	// ScanOutput opts web_fetch/web_search output into the heuristic
	// prompt-injection scan (FIND-04). The untrusted-content provenance
	// marker is always applied regardless of this setting.
	ScanOutput bool
}

// Register adds all built-in tools to the registry.
func Register(reg *tool.Registry, opts Options) error {
	if opts.Root == "" {
		opts.Root = "."
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	if opts.HTTPUserAgent == "" {
		opts.HTTPUserAgent = "aegis/0.1"
	}
	if opts.KrokiURL == "" {
		opts.KrokiURL = "https://kroki.io"
	}

	ft := opts.FileTracker
	// Core tools are always exposed: file ops, search, shell, git, and the two
	// meta-tools (skill, tool_search) that unlock the rest.
	tools := []tool.Tool{
		&readTool{root: root, tracker: ft},
		&writeTool{root: root, tracker: ft},
		&editTool{root: root, tracker: ft},
		&fillMarkerTool{root: root, tracker: ft},
		&editSectionTool{root: root, tracker: ft},
		&multieditTool{root: root, tracker: ft},
		&lsTool{root: root},
		&globTool{root: root, cmds: opts.Commands},
		&grepTool{root: root, cmds: opts.Commands},
		&gitTool{root: root},
		&gitCommitTool{root: root, preCommitTest: opts.GitPreCommitTestCommand, preCommitTestTimeout: opts.GitPreCommitTestTimeout},
		newShellTool(root, opts.ShellTimeoutSec, opts.Tasks, opts.Sandbox),
		&modelsTool{},
		NewSkillTool(root, opts.DataDir, opts.BuiltinSkills),
		&toolSearchTool{reg: reg},
	}
	// Deferred tools are niche: registered but advertised only as a name+
	// description line in the system prompt, loaded on demand via tool_search
	// (P4.6). This keeps per-turn schema tokens low.
	deferred := []tool.Tool{
		&diagramTool{root: root, krokiURL: opts.KrokiURL},
		&repomapTool{root: root},
		&latexBuildTool{root: root},
		&latexNewDocumentTool{root: root},
		&dastScanTool{root: root, opts: opts.SecurityScan, allowedTargets: opts.DASTAllowedTargets, allowActive: opts.DASTAllowActive},
		&reconScanTool{root: root, opts: opts.SecurityScan, allowedTargets: opts.DASTAllowedTargets, allowActive: opts.DASTAllowActive},
		&adviseTool{root: opts.DataDir},
		&scopeTool{},
		&yamlValidateTool{root: root},
	}
	// web_search/web_fetch/security_scan/git_pr are always-exposed in the
	// default profile but move to deferred under LocalProfile (P25.6): they're
	// the least likely tools a small-model, file-scoped local task needs on
	// turn one, and their schemas are some of the heaviest always-exposed
	// ones.
	networkAndScanTools := []tool.Tool{
		&gitPRTool{root: root},
		&fetchTool{userAgent: opts.HTTPUserAgent, scanOutput: opts.Search.ScanOutput},
		&searchTool{userAgent: opts.HTTPUserAgent, provider: opts.Search.Provider, apiKey: opts.Search.APIKey, baseURL: opts.Search.BaseURL, scanOutput: opts.Search.ScanOutput},
		&securityScanTool{root: root, opts: opts.SecurityScan},
	}
	if opts.LocalProfile {
		deferred = append(deferred, networkAndScanTools...)
	} else {
		tools = append(tools, networkAndScanTools...)
	}
	if opts.DataDir != "" {
		src := memory.Sources{ProjectRoot: root, DataDir: opts.DataDir}
		tools = append(tools, &rememberTool{src: src}, &saveSkillTool{src: src})
	}
	if opts.Tasks != nil {
		tools = append(tools, TaskTools(opts.Tasks, root, opts.ShellTimeoutSec, opts.Sandbox)...)
	}
	if opts.Cron != nil && opts.familyEnabled(familyCron) {
		deferred = append(deferred, CronTools(opts.Cron)...)
	}
	if opts.LSP != nil {
		deferred = append(deferred, LSPTools(opts.LSP, root)...)
	}
	if opts.TodoList != nil {
		tools = append(tools, TodoTools(opts.TodoList)...)
	}
	if opts.Questioner != nil {
		tools = append(tools, &askTool{questioner: opts.Questioner})
	}
	if opts.Knowledge != nil {
		tools = append(tools, KnowledgeTools(opts.Knowledge, opts.KnowledgeProvider, root)...)
	}
	if opts.LongMem != nil && opts.familyEnabled(familyEntity) {
		deferred = append(deferred, LongMemTools(opts.LongMem, root)...)
	}
	if opts.TeamTasks != nil && opts.MailboxRoot != "" && opts.familyEnabled(familyTeam) {
		deferred = append(deferred, TeamTools(opts.TeamTasks, opts.MailboxRoot)...)
	}
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	for _, t := range deferred {
		if err := reg.RegisterDeferred(t); err != nil {
			return err
		}
	}
	return nil
}

// resolvePath joins p against root and rejects paths that escape it. It
// delegates to the sandbox path validator which resolves symlinks to prevent
// symlink-based workspace escapes.
//
// This is the single-root form, for the handful of callers with no request
// context to consult. Anything reachable from a tool's Execute should use
// resolveRead/resolveWrite instead so the session's additional roots (P52.13)
// are honored.
func resolvePath(root, p string) (string, error) {
	return sandbox.ValidatePath(root, p)
}

// resolveRead resolves p for reading, against the session workdir plus any
// additional roots configured for the session (P52.13).
func resolveRead(ctx context.Context, root, p string) (string, error) {
	return sandbox.ValidatePathIn(effectiveRoots(ctx, root), p, sandbox.AccessRead)
}

// resolveWrite resolves p for writing. Identical to resolveRead except that an
// additional root only satisfies it when that root was explicitly marked
// writable — additional roots are read-only by default, so the common
// "read research from A, write the document into B" setup cannot accidentally
// scribble into A.
func resolveWrite(ctx context.Context, root, p string) (string, error) {
	abs, err := sandbox.ValidatePathIn(effectiveRoots(ctx, root), p, sandbox.AccessWrite)
	if err != nil {
		return "", err
	}
	if err := denyMaterializedSkillWrite(effectiveRoots(ctx, root), abs); err != nil {
		return "", err
	}
	return abs, nil
}

// materializedSkillsRel is the workspace-relative directory built-in skills are
// extracted into (skills.MaterializeBuiltinsToProject). Kept as literal segments
// rather than importing internal/skills, which would make internal/tool/builtin
// depend on the skills package purely for a path constant.
var materializedSkillsRel = []string{".aegis", "builtin-skills"}

// denyMaterializedSkillWrite refuses a write that lands inside a root's
// <root>/.aegis/builtin-skills tree.
//
// That tree is harness-owned: it is an extraction of content compiled into the
// binary, re-materialized on demand, and nothing in it is meant to be authored
// in the workspace. It is only inside the workspace root at all so the model's
// sandboxed *read* tools can reach a skill's scripts and skeletons — which
// incidentally put it in reach of the write tools too.
//
// Observed live (P38.1 re-test, 2026-08-09): a model wrote the command line it
// meant to *run* into recon.py, replacing the script with 295 bytes of shell
// text. Every later `python recon.py` then raised SyntaxError, and the agent
// could see the damage but had no copy to restore from — the phase could not
// proceed, and no error along the way pointed at the cause. An agent destroying
// the tooling it was given is a failure mode worth making structurally
// impossible rather than recoverable.
//
// This deliberately does not cover .aegis/skills/ — user- and project-authored
// skills are ordinary workspace content an agent may legitimately be asked to
// write. Nor does it cover the shell tool, which can still reach the tree; the
// per-phase refresh in internal/drive repairs that case instead.
func denyMaterializedSkillWrite(roots []sandbox.Root, abs string) error {
	for _, r := range roots {
		if r.Path == "" {
			continue
		}
		guarded := filepath.Join(append([]string{r.Path}, materializedSkillsRel...)...)
		rel, err := filepath.Rel(guarded, abs)
		if err != nil {
			continue
		}
		// Inside the tree, or the tree itself — but not a sibling that merely
		// shares a prefix (filepath.Rel would yield a leading "..").
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return fmt.Errorf("%s is a built-in skill file: this tree is extracted from the binary and is read-only to tools — copy it elsewhere in the workspace if you need to modify it", filepath.ToSlash(filepath.Join(append(append([]string{}, materializedSkillsRel...), rel)...)))
		}
	}
	return nil
}

// effectiveRoot returns the working directory a tool call should be confined
// to: the session-scoped override carried on ctx (see tool.WithWorkdir), or
// fallback — the tool's own construction-time root — when no override is
// set (P25.1). This lets one daemon-wide Registry serve sessions rooted at
// different directories without rebuilding the registry (and its MCP/
// plugin/swarm wiring) per session.
func effectiveRoot(ctx context.Context, fallback string) string {
	if wd, ok := tool.WorkdirFromContext(ctx); ok {
		return wd
	}
	return fallback
}

// effectiveRoots returns the full confinement set for a call: the working
// directory from effectiveRoot as the primary writable root, followed by the
// session's additional roots. With none configured — the overwhelmingly common
// case — the result is a one-element set and validation is bit-for-bit the
// old single-root behavior.
//
// root is passed rather than read from the tool because callers frequently
// already hold the resolved workdir; passing the raw fallback works too, since
// effectiveRoot is applied here.
func effectiveRoots(ctx context.Context, root string) []sandbox.Root {
	primary := effectiveRoot(ctx, root)
	extra := tool.ExtraRootsFromContext(ctx)
	if len(extra) == 0 {
		return []sandbox.Root{{Path: primary, Writable: true}}
	}
	roots := make([]sandbox.Root, 0, len(extra)+1)
	roots = append(roots, sandbox.Root{Path: primary, Writable: true})
	for _, r := range extra {
		// The session workdir is always primary and always writable; a stale
		// additional root naming it must not be able to demote it.
		if r.Path == primary {
			continue
		}
		roots = append(roots, r)
	}
	return roots
}

// parseArgs unmarshals tool input into v, returning a friendly error.
func parseArgs(input json.RawMessage, v any) error {
	if len(input) == 0 {
		return nil
	}
	if err := json.Unmarshal(input, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// schema is a tiny helper to declare a tool's JSON Schema inline.
func schema(s string) json.RawMessage { return json.RawMessage(s) }
