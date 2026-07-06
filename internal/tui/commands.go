package tui

// commandDef is the single source of truth for one built-in slash command.
// The dispatch table (d.builtins), the general /help listing, /help <name>'s
// detailed text, and the completion-popup/command-palette entry all derive
// from commandDefs instead of three separately hand-maintained lists (P14.10)
// — the structural fix for the drift class that let /security-config, /scan,
// /debate, /rollback, /detach, /archive, and /humor silently fall out of the
// completion popup while still being fully dispatchable (P14.1). Any future
// built-in command should be added here once; it then automatically appears
// everywhere it needs to.
type commandDef struct {
	name    string // dispatch key, e.g. "mode"
	argHint string // shown after the name in /help's general listing, e.g. "<plan|build|auto>"

	// shortDesc is the one-line description shared by /help's general listing
	// and the completion popup/palette.
	shortDesc string

	// detailedHelp is the full text for /help <name>.
	detailedHelp string

	// needsArgs completes the command with a trailing space in the popup so
	// the user can type arguments immediately, rather than accepting the bare
	// command. "persona" is deliberately absent (needsArgs: false): Tab/Enter
	// on /persona dispatches it immediately, which opens the interactive
	// picker.
	needsArgs bool

	handler func(*SlashDispatcher, []string) SlashResult
}

// commandDefs is the ordered list of every built-in slash command except
// "quit" — a bare alias for "exit" registered separately in
// NewSlashDispatcher so it isn't listed twice (matching the prior behavior
// documented in help_test.go and completion_test.go).
//
// This is a function rather than a package-level var: several handlers
// (e.g. cmdHelp) range over the command list, and a var initializer that
// references those same handler values would create a compile-time
// initialization cycle (var commandDefs -> handler funcs -> commandDefs).
func commandDefs() []commandDef {
	return []commandDef{
		{
			name: "help", argHint: "[cmd]", needsArgs: true,
			shortDesc:    "Show this help or detail for a command",
			detailedHelp: "/help [command]\n  Show available commands, or detailed help for a specific command.\n  No args: also lists keyboard shortcuts, including keybind-only features that have no slash-command equivalent (e.g. ctrl+x terminal pane, ctrl+t sub-agent list, ctrl+r session switcher).",
			handler:      (*SlashDispatcher).cmdHelp,
		},
		{
			name: "persona", argHint: "[name]",
			shortDesc:    "Pick persona interactively, or switch directly by name",
			detailedHelp: "/persona [name]\n  No args: open an interactive list to pick a persona.\n  With name: switch directly, e.g. /persona security.",
			handler:      (*SlashDispatcher).cmdPersona,
		},
		{
			name: "mode", argHint: "<plan|build|auto>", needsArgs: true,
			shortDesc:    "Switch permission mode",
			detailedHelp: "/mode <plan|build|auto>\n  Switch the permission mode for the current session.\n  plan = read-only\n  build = file edits allowed, shell execution requires approval\n  auto  = all capabilities allowed without prompting",
			handler:      (*SlashDispatcher).cmdMode,
		},
		{
			name: "guard", argHint: "[on|off|status]", needsArgs: true,
			shortDesc:    "Toggle output validation for this session",
			detailedHelp: "/guard [on|off|status]\n  Toggle output validation for the current session.\n  Defaults to the configured output_guard.enabled; resets on restart.",
			handler:      (*SlashDispatcher).cmdGuard,
		},
		{
			name: "tools", argHint: "<compact|full>", needsArgs: true,
			shortDesc:    "Set tool-output line cap (compact=10 lines, full=unlimited)",
			detailedHelp: "/tools <compact|full>\n  compact: cap multi-line tool output at 10 lines (default).\n  full: show complete tool output without truncation.\n  Applies to new results; toggle resets on TUI restart.",
			handler:      (*SlashDispatcher).cmdTools,
		},
		{
			name:         "clear",
			shortDesc:    "Clear the transcript",
			detailedHelp: "/clear\n  Clear the conversation transcript (session history is preserved).",
			handler:      (*SlashDispatcher).cmdClear,
		},
		{
			name:         "config",
			shortDesc:    "Interactive configuration wizard",
			detailedHelp: "/config\n  Open the interactive configuration wizard to change provider, model, tokens, and think settings.\n  Changes are written to the global config file and take effect on next restart.",
			handler:      (*SlashDispatcher).cmdConfig,
		},
		{
			name:         "memory",
			shortDesc:    "Show saved memories",
			detailedHelp: "/memory\n  Display saved project and user memory entries.",
			handler:      (*SlashDispatcher).cmdMemory,
		},
		{
			name: "remember", argHint: "<text>", needsArgs: true,
			shortDesc:    "Save a memory entry",
			detailedHelp: "/remember <text>\n  Save a fact to project memory for future sessions.",
			handler:      (*SlashDispatcher).cmdRemember,
		},
		{
			name: "skills", argHint: "[enable|disable <name> [global]]",
			shortDesc: "List skills, or toggle a built-in skill on/off",
			detailedHelp: "/skills\n  List active skills (project/user skill files, plus any enabled built-ins) and the full built-in catalog with on/off status.\n" +
				"/skills enable <name> [global]\n  Turn on a built-in skill shipped with Aegis. Writes to the project config (.aegis/config.yaml) by default; add 'global' to write to the user config instead. Takes effect on restart.\n" +
				"/skills disable <name> [global]\n  Turn off a built-in skill the same way.",
			handler: (*SlashDispatcher).cmdSkills,
		},
		{
			name:         "commands",
			shortDesc:    "List custom commands",
			detailedHelp: "/commands\n  List custom user-defined commands from .aegis/commands/.",
			handler:      (*SlashDispatcher).cmdCommands,
		},
		{
			name:         "models",
			shortDesc:    "Show current model info",
			detailedHelp: "/models\n  Show the current model and provider.",
			handler:      (*SlashDispatcher).cmdModels,
		},
		{
			name: "model", argHint: "<model-id|default>", needsArgs: true,
			shortDesc:    "Switch this session's model mid-session",
			detailedHelp: "/model <model-id>\n  Switch this session's model. Persisted as a per-session override that outranks the active persona's own model and the global default on every subsequent turn.\n  Must be an id for your currently configured provider (see /status) — a cross-provider id fails on the next turn, not at switch time.\n  /model default: clear the override, reverting to the persona/global default.\n  No args: show the current model.",
			handler:      (*SlashDispatcher).cmdModel,
		},
		{
			name:         "status",
			shortDesc:    "Show daemon health, sandbox backend, and cost caps/spend",
			detailedHelp: "/status\n  Show daemon reachability, provider/model, active sandbox backend and any fallback reason, this session's cumulative spend against its caps, and cross-session today's spend against the daily caps.",
			handler:      (*SlashDispatcher).cmdStatus,
		},
		{
			name: "sandbox", argHint: "[use <target>]",
			shortDesc:    "Show or switch the shell-execution sandbox",
			detailedHelp: "/sandbox [use <target>]\n  No args: show the configured sandbox backend and detected container runtimes (docker, podman, wslc, container).\n  use <local|auto|docker|podman|wslc|container>: set the backend (written to global config; takes effect on restart).",
			handler:      (*SlashDispatcher).cmdSandbox,
		},
		{
			name: "security", argHint: "[status|install <tool>|baseline [path]|config [global]]",
			shortDesc:    "Show scanner status, manage the suppression baseline, or install/configure scanners",
			detailedHelp: "/security [status|install <tool>|baseline [path]|config [global]]\n  No args or 'status': show how each security scanner would run right now (host binary, container fallback, or unavailable and why) — same as `aegis security status`.\n  'install <tool> [confirm]': show the guided host-install command for a scanner; add 'confirm' to actually run it (never runs without the explicit confirm word) — same as `aegis security install`.\n  'baseline [path]': show the accepted-risk suppression baseline (.aegis/security-baseline.yaml) and each entry's active/expired/invalid status — same as `aegis security baseline`.\n  'config [global]': open the interactive /security-config dialog (see /help security-config).",
			handler:      (*SlashDispatcher).cmdSecurity,
		},
		{
			name: "security-config", argHint: "[global]", needsArgs: true,
			shortDesc:    "Interactively configure, enable, and install security scanners",
			detailedHelp: "/security-config [global]\n  Opens an interactive dialog to configure the security scanners (opengrep, semgrep, gosec, bandit, brakeman, njsscan, trivy, gitleaks, trufflehog, kubescape, hadolint, grype, dockle, osv-scanner, syft) used by /scan and the security_scan tool: toggle enabled (including the opt-in language-specific SAST engines), pick host/container/auto, set the install policy, and set a digest-pinned container image. trufflehog additionally offers a warning-labelled \"verify\" toggle for live credential verification (host-only). Selecting a tool now also offers \"Install now (guided)\" — shows the exact host command and runs it after you confirm, no separate CLI trip required.\n  No args: edits the project's .aegis/config.yaml. 'global': edits ~/.config/aegis/config.yaml instead.\n  Written immediately; restart Aegis to apply.",
			handler:      (*SlashDispatcher).cmdSecurityConfig,
		},
		{
			name: "scan", argHint: "[path|scanner[,scanner...] [path]|image <ref>|sbom [path]|network <target...>]", needsArgs: true,
			shortDesc:    "Run security scanners now and print the findings report",
			detailedHelp: "/scan [path|scanner[,scanner...] [path]|image <ref>|sbom [path]|network <target...>]\n  Runs the security scanners directly and prints the findings report — no model turn spent, same scan `aegis scan`/the security_scan tool runs. Every findings report is also persisted under .aegis/security/ (scan.json, image.json, or network.json).\n  No args: scan the whole workspace with every enabled scanner, auto-detecting the project's language (go.mod/*.go, requirements.txt/*.py, Gemfile/*.rb, package.json/*.js) to auto-enable the matching opt-in SAST engine (gosec/bandit/brakeman/njsscan) for this run.\n  /scan <path>: scan just a workspace-relative subdirectory (same language auto-detection, scoped to that path).\n  /scan <scanner-or-category>[,<...>] [path]: run only the named scanner(s), force-enabled regardless of config — e.g. /scan trufflehog, /scan secrets, /scan gitleaks,trufflehog src/. Categories: secrets, sast, sca/deps, iac, misconfig.\n  /scan image <ref>: scan a container image reference instead (e.g. /scan image alpine:3.20).\n  /scan sbom [path]: generate a CycloneDX SBOM instead of a findings report.\n  /scan network <target> [target...]: run nmap+nuclei (recon_scan) against a bare host/IP/CIDR list — same shared security.dast.allowed_targets gate as /scan and dast_scan; a disallowed target is rejected before either scanner runs.\n  Use /security-config first to enable/install the scanners you want included.",
			handler:      (*SlashDispatcher).cmdScan,
		},
		{
			name: "knowledge", argHint: "[index|query <text>]", needsArgs: true,
			shortDesc:    "Rebuild or search the project knowledge base",
			detailedHelp: "/knowledge [index|query <text>]\n  index: rebuild the project knowledge base FTS5 index — same as `aegis knowledge index`.\n  query <text>: search the index — same as the model's project_knowledge tool.\n  Runs directly against the daemon's own store, no model turn spent.",
			handler:      (*SlashDispatcher).cmdKnowledge,
		},
		{
			name:         "index",
			shortDesc:    "Rebuild the repository map for the system prompt",
			detailedHelp: "/index\n  Rebuild the compact repository map (top-level symbols per file) and refresh it in the daemon's system prompt — same build as `aegis index`, no restart needed.",
			handler:      (*SlashDispatcher).cmdIndex,
		},
		{
			name: "debate", argHint: "[--domain generic] [--file <path>]... <claim>", needsArgs: true,
			shortDesc:    "Adversarially debate any claim (propose/critique/rebut/arbitrate) and print the verdict",
			detailedHelp: "/debate [--domain generic] [--file <path>]... [--proposer|--critic|--arbiter <persona>] [--max-rounds <n>] <claim>\n  Runs a multi-agent debate directly against the daemon's configured model — a critic challenges the claim (grounded in cited evidence or an explicit CONCEDE), the proposer rebuts, this repeats for up to 2 rounds (or --max-rounds), then an arbiter issues a final UPHOLD/REVISE/REJECT verdict with a confidence label. Unlike /scan, this does spend model turns (one per role per round) since the debate itself is model-driven.\n  Defaults to the security-researcher/security-critic/security-arbiter personas; pass --domain generic to use general/critic/arbiter instead for non-security claims (documents, plans, decisions). --file points the roles at specific files to read for grounding instead of relying on recall — useful for debating the accuracy of a document or implementation plan. --proposer/--critic/--arbiter override individual role personas regardless of --domain.\n  Same underlying mechanism as the `agent` tool's mode:\"debate\", exposed to run directly on a claim you already have in hand without a conversational turn first.",
			handler:      (*SlashDispatcher).cmdDebate,
		},
		{
			name: "threat-model", argHint: "[system or feature]",
			shortDesc:    "Threat-model a system or feature (STRIDE/LINDDUN/PASTA/Trike/VAST/NIST 800-154)",
			detailedHelp: "/threat-model [system or feature]\n  Loads the threat-modeling skill and starts a threat model. Asks which framework to use (STRIDE, LINDDUN, PASTA, Trike, VAST, or NIST 800-154) if it isn't already clear from context, then explores the workspace and applies it.\n  No args: threat-models the whole project.\n  With args: scopes to the named system/feature, e.g. /threat-model the auth service.\n  Spends a model turn, same as /debate — a discoverable entry point into the skill instead of relying on the model noticing a trigger phrase in free text.",
			handler:      (*SlashDispatcher).cmdThreatModel,
		},
		{
			name: "session", argHint: "[list]", needsArgs: true,
			shortDesc:    "Show session info or list sessions",
			detailedHelp: "/session [list]\n  No args: show current session info.\n  list: show all sessions.",
			handler:      (*SlashDispatcher).cmdSession,
		},
		{
			name: "rewind", argHint: "[n] [scope]", needsArgs: true,
			shortDesc:    "List or restore checkpoints (code/conversation/both)",
			detailedHelp: "/rewind [n] [code|conversation|both]\n  No args: list checkpoints (rewind points) for this session, newest first.\n  /rewind <n>: restore checkpoint n (both files and conversation by default).\n  Scope: 'code' restores only files, 'conversation' only the transcript, 'both' (default) does both.\n  Each checkpoint is the state just before a user turn; rewinding undoes that turn's file changes and/or messages.",
			handler:      (*SlashDispatcher).cmdRewind,
		},
		{
			name: "rollback", argHint: "[n]", needsArgs: true,
			shortDesc:    "Restore checkpoint n and run git reset --hard to pre-turn HEAD",
			detailedHelp: "/rollback [n]\n  No args: list checkpoints (rollback points) for this session, newest first.\n  /rollback <n>: restore checkpoint n's conversation AND run `git reset --hard` to that checkpoint's commit — a harder reset than /rewind's 'both' scope, which restores files by rewriting them rather than resetting git history.\n  Use this when a turn's file changes need to be fully undone at the git level, not just reverted in the working tree.",
			handler:      (*SlashDispatcher).cmdRollback,
		},
		{
			name: "detach", argHint: "[on|off]", needsArgs: true,
			shortDesc:    "Run this session in the background (turn continues after TUI closes)",
			detailedHelp: "/detach [on|off]\n  Toggle background (detached) mode for this session.\n  on (default): turns continue running after the TUI closes. Use `aegis bg events <id>` to check progress.\n  off: revert to normal foreground execution.",
			handler:      (*SlashDispatcher).cmdDetach,
		},
		{
			name: "humor", argHint: "[on|off]", needsArgs: true,
			shortDesc:    "Toggle D&D-themed thinking phrases in the response area",
			detailedHelp: "/humor [on|off]\n  Toggle D&D-themed thinking phrases in the response area.\n  on: show phrases like \"Rolling for Initiative\", \"Consulting the Tome\", etc.\n  off: show plain \"thinking…\" status.\n  No args: toggle current state.\n  Set tui.humor_mode in your config file to make this permanent.",
			handler:      (*SlashDispatcher).cmdHumor,
		},
		{
			name: "archive", argHint: "[off|list]", needsArgs: true,
			shortDesc:    "Archive this session (hidden from listings; data kept). /archive off to restore",
			detailedHelp: "/archive [off|list]\n  Archive the current session — it is hidden from normal session listings but all data is preserved.\n  /archive off: restore an archived session to active status.\n  /archive list: list all archived sessions.\n  To permanently remove a session, use `aegis sessions delete <id>` from the CLI.",
			handler:      (*SlashDispatcher).cmdArchive,
		},
		{
			name: "prune", argHint: "[days]", needsArgs: true,
			shortDesc:    "Delete non-archived sessions older than N days",
			detailedHelp: "/prune [days]\n  Delete non-archived sessions not updated in the last N days.\n  No args: use the server's configured TTL.\n  Same operation as `aegis sessions prune`.",
			handler:      (*SlashDispatcher).cmdPrune,
		},
		{
			name: "bundle", argHint: "[install|info <path-or-url>]", needsArgs: true,
			shortDesc:    "Install or inspect a persona/skill/command bundle",
			detailedHelp: "/bundle [install|info <path-or-url>]\n  'info <path-or-url>': show a bundle's manifest, artifacts, and content hash — same as `aegis bundle info`. A git URL (https/git@/ssh) is shallow-cloned to a temp dir first.\n  'install <path-or-url> [global] [sha256:<hash>] [confirm]': install a bundle's commands/agents/skills. No 'confirm': preview only (manifest, artifacts, target scope, hash) — nothing is written. 'global' installs into the user data dir instead of the project's .aegis/ (default). 'sha256:<hash>' pins the P7.6 content-hash provenance check (see /bundle info) and aborts if it doesn't match — same as the CLI's --expect-sha256.\n  Same underlying package as `aegis bundle install/info`.",
			handler:      (*SlashDispatcher).cmdBundle,
		},
		{
			name:         "runs",
			shortDesc:    "List message runs currently in flight across all sessions",
			detailedHelp: "/runs\n  List message runs currently in flight across all sessions (session id, elapsed time, tool-call count, last event kind, title) — same data as `aegis runs`.",
			handler:      (*SlashDispatcher).cmdRuns,
		},
		{
			name: "bg", argHint: "[list|events [session-id]]",
			shortDesc:    "List background (detached) sessions, or print one's buffered events",
			detailedHelp: "/bg [list|events [session-id]]\n  No args or 'list': list sessions currently running in background (detached) mode.\n  'events [session-id]': print buffered engine events from a background session (defaults to the current session) — same data as `aegis bg events`.",
			handler:      (*SlashDispatcher).cmdBG,
		},
		{
			name:         "timeline",
			shortDesc:    "Jump to a past turn in the conversation timeline",
			detailedHelp: "/timeline\n  Opens an interactive picker of past turns in this conversation. Selecting one jumps the transcript view to that point — a navigation aid, not a rewind (use /rewind or /rollback to actually restore state).",
			handler:      (*SlashDispatcher).cmdTimeline,
		},
		{
			name:         "sidebar",
			shortDesc:    "Toggle the sidebar panel on/off (also ctrl+b)",
			detailedHelp: "/sidebar\n  Toggles the sidebar panel (context %, cost, agent count) on/off. Same as pressing ctrl+b. Hidden by default; folds into the status bar when off.",
			handler:      (*SlashDispatcher).cmdSidebar,
		},
		{
			name: "theme", argHint: "<dark|light>", needsArgs: true,
			shortDesc:    "Switch the color scheme live, no restart needed",
			detailedHelp: "/theme <dark|light>\n  Switch the TUI color scheme immediately — no restart needed.\n  No args: show the current theme.\n  This session only; set tui.theme: <name> in config (project or global) to make it the default on restart.",
			handler:      (*SlashDispatcher).cmdTheme,
		},
		{
			name: "copy", argHint: "[N]",
			shortDesc:    "Copy last assistant message, or Nth code block, to clipboard",
			detailedHelp: "/copy [N]\n  No args: copy the last assistant message to the clipboard.\n  /copy <N>: copy the Nth fenced code block from the last assistant message instead.\n  Uses pbcopy/xclip/clip.exe depending on platform; shows a toast confirming what was copied.",
			handler:      (*SlashDispatcher).cmdCopy,
		},
		{
			name: "share", argHint: "[html|md|json]", needsArgs: true,
			shortDesc:    "Export this session to a shareable transcript file",
			detailedHelp: "/share [html|md|json]\n  Export this session as a shareable transcript file in the current directory.\n  html (default): a self-contained page with styling and inline images.\n  md: Markdown. json: the raw session.\n  Use `aegis sessions export <id>` for the same from the CLI.",
			handler:      (*SlashDispatcher).cmdShare,
		},
		{
			name:         "exit",
			shortDesc:    "Exit Aegis",
			detailedHelp: "/quit\n  Exit Aegis.",
			handler:      (*SlashDispatcher).cmdQuit,
		},
	}
}
