package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/bundle"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/modelcatalog"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/share"
	"github.com/fiddler110/aegis/internal/skills"
)

// SlashResult describes what a slash command produced for the TUI to render.
type SlashResult struct {
	Output   string            // text to append to the transcript
	IsError  bool              // render in error style
	Quit     bool              // signal the TUI to exit
	Message  string            // if non-empty, send this text to the daemon as a normal message
	Personas []api.PersonaInfo // if non-nil, open the persona picker with these entries

	// ReloadSession asks the TUI to refetch the current session and replay its
	// (possibly truncated) transcript — used after a /rewind that changes the
	// conversation. Output, if set, is shown as a toast rather than appended,
	// since the reload resets the transcript.
	ReloadSession bool

	// SecurityConfigGlobal is non-nil to open the /security-config dialog
	// (P11.11): *true edits the global config, *false the project config.
	SecurityConfigGlobal *bool

	// Models is non-nil to open the P16.6 model picker with these curated
	// entries.
	Models []modelcatalog.Model

	// Model is non-nil after a successful /model switch (not on "/model
	// default"), so the TUI can update its own display copy (title bar,
	// sidebar, context-window sizing) to match — those all read the model
	// the TUI was started with otherwise, which /model's daemon-side
	// override alone never touched.
	Model *string

	// ThreatModelTarget is non-nil to open the /threat-model framework picker
	// (forcing the choice up front instead of a model-turn clarifying
	// question); its value is the already-parsed target text ("" for the
	// whole project), carried through to re-dispatch /threat-model once a
	// framework is picked.
	ThreatModelTarget *string

	// SwitchToSession is non-empty after a successful /fork (P22.3): the TUI
	// must load this session id as the active one, the same "fetch and
	// replace the live view" path Ctrl+Y's session picker and ReloadSession
	// both already use — reused here rather than duplicated, since a fork's
	// "switch into a different, already-fully-formed session" is exactly the
	// Ctrl+Y case, not the "refetch this same session" ReloadSession case.
	SwitchToSession string
}

// SlashDispatcher dispatches slash commands to built-in handlers or custom
// command templates.
type SlashDispatcher struct {
	client       *client.Client
	sessionID    string
	mode         string
	model        string
	workDir      string // project root; used to validate/list project-local theme files (P16.7) and to name the workspace in /threat-model's default prompt
	guardEnabled *bool  // per-session output-guard toggle; nil = server default
	builtins     map[string]func(args []string) SlashResult
	customs      []api.CommandInfo
	// keys backs /help's keyboard-shortcuts section (P13.3.5): defaults to
	// defaultKeyMap() here but is overwritten with the model's actual (possibly
	// remapped) keys by newModel, so /help matches the real bindings.
	keys keyMap
}

// NewSlashDispatcher creates a dispatcher for the given session.
func NewSlashDispatcher(cl *client.Client, sessionID, mode, model, workDir string) *SlashDispatcher {
	d := &SlashDispatcher{
		client:    cl,
		sessionID: sessionID,
		mode:      mode,
		model:     model,
		workDir:   workDir,
		keys:      defaultKeyMap(),
	}
	// d.builtins is derived from commandDefs (P14.10) rather than hand-listed,
	// so a command added to that single table is automatically dispatchable
	// here, listed in /help, described in the completion popup/palette, and
	// covered by builtinHelp — no second or third place to remember.
	defs := commandDefs()
	d.builtins = make(map[string]func(args []string) SlashResult, len(defs)+1)
	for _, c := range defs {
		c := c
		d.builtins[c.name] = func(args []string) SlashResult { return c.handler(d, args) }
	}
	d.builtins["quit"] = d.cmdQuit // bare alias for "exit"; deliberately unlisted
	return d
}

// SetSession points the dispatcher at a different session (used when the TUI
// switches sessions via the picker).
func (d *SlashDispatcher) SetSession(id, mode string) {
	d.sessionID = id
	d.mode = mode
}

// Dispatch executes a parsed slash command. It checks builtins first, then
// custom commands.
func (d *SlashDispatcher) Dispatch(parsed *commands.ParsedCommand) SlashResult {
	if handler, ok := d.builtins[parsed.Name]; ok {
		return handler(parsed.Args)
	}
	return d.tryCustom(parsed)
}

func (d *SlashDispatcher) tryCustom(parsed *commands.ParsedCommand) SlashResult {
	if d.customs == nil {
		d.refreshCustoms()
	}
	for _, c := range d.customs {
		if c.Name == parsed.Name {
			argText := strings.Join(parsed.Args, " ")
			prompt := c.Description
			if argText != "" {
				prompt = c.Description + "\n\nContext: " + argText
			}
			if prompt == "" {
				prompt = "Execute the /" + parsed.Name + " command"
			}
			return SlashResult{Message: prompt}
		}
	}
	return SlashResult{
		Output:  fmt.Sprintf("Unknown command: /%s\nType /help for available commands.", parsed.Name),
		IsError: true,
	}
}

// Customs returns the cached custom command list, refreshing it once if it
// has not yet been loaded. Used by the inline completion popup and palette.
func (d *SlashDispatcher) Customs() []api.CommandInfo {
	if d.customs == nil {
		d.refreshCustoms()
	}
	return d.customs
}

func (d *SlashDispatcher) refreshCustoms() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmds, err := d.client.ListCommands(ctx)
	if err != nil {
		d.customs = []api.CommandInfo{}
		return
	}
	d.customs = cmds
}

// --- built-in handlers ---

func (d *SlashDispatcher) cmdHelp(args []string) SlashResult {
	if len(args) > 0 {
		name := strings.ToLower(args[0])
		if _, ok := d.builtins[name]; ok {
			return SlashResult{Output: builtinHelp(name)}
		}
		return SlashResult{Output: fmt.Sprintf("Unknown command: /%s", name), IsError: true}
	}

	var b strings.Builder
	b.WriteString("Available commands:\n")
	for _, c := range commandDefs() {
		name := c.name
		if c.argHint != "" {
			name = c.name + " " + c.argHint
		}
		fmt.Fprintf(&b, "  /%-22s %s\n", name, c.shortDesc)
	}

	if d.customs == nil {
		d.refreshCustoms()
	}
	if len(d.customs) > 0 {
		b.WriteString("\nCustom commands:\n")
		for _, c := range d.customs {
			argStr := ""
			if len(c.Args) > 0 {
				argStr = " <" + strings.Join(c.Args, "> <") + ">"
			}
			fmt.Fprintf(&b, "  /%-22s %s\n", c.Name+argStr, c.Description)
		}
	}

	// P14.9: several features (terminal pane, sub-agent list, session
	// switcher, thinking expand/collapse, message queueing, newline
	// insertion) are keybind-only with no slash-command equivalent, so
	// listing slash commands alone left them undiscoverable without reading
	// the docs. Reusing keyMap.helpEntries() keeps this in sync with the F1
	// overlay (renderHelpOverlay in tui.go) — one list, not two.
	b.WriteString("\nKeyboard shortcuts (also shown via f1):\n")
	for _, e := range d.keys.helpEntries() {
		fmt.Fprintf(&b, "  %-14s %s\n", e.Key, e.Desc)
	}
	return SlashResult{Output: b.String()}
}

// builtinHelp looks up a command's detailed /help <name> text from
// commandDefs (P14.10); "quit" resolves to "exit"'s entry since it's a bare
// alias not separately listed.
func builtinHelp(name string) string {
	if name == "quit" {
		name = "exit"
	}
	for _, c := range commandDefs() {
		if c.name == name {
			return c.detailedHelp
		}
	}
	return "No help available for /" + name
}

func (d *SlashDispatcher) cmdPersona(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	personas, err := d.client.ListPersonas(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to list personas: %v", err), IsError: true}
	}

	if len(args) == 0 {
		// No name given — signal the TUI to open the interactive picker.
		if len(personas) == 0 {
			return SlashResult{Output: "No personas available."}
		}
		return SlashResult{Personas: personas}
	}

	name := strings.ToLower(args[0])
	var found *api.PersonaInfo
	for _, p := range personas {
		if p.Name == name {
			found = &p
			break
		}
	}
	if found == nil {
		var names []string
		for _, p := range personas {
			names = append(names, p.Name)
		}
		return SlashResult{
			Output:  fmt.Sprintf("Unknown persona %q. Available: %s", name, strings.Join(names, ", ")),
			IsError: true,
		}
	}

	meta, err := d.client.UpdateSession(ctx, d.sessionID, api.UpdateSessionRequest{Persona: &name})
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to switch persona: %v", err), IsError: true}
	}
	out := fmt.Sprintf("Switched to %s persona: %s", found.Name, found.Description)
	if meta != nil && meta.Mode != "" && meta.Mode != d.mode {
		out += fmt.Sprintf("\nPermission mode changed: %s → %s (persona default)", d.mode, meta.Mode)
		d.mode = meta.Mode
	}
	return SlashResult{Output: out}
}

func (d *SlashDispatcher) cmdMode(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: fmt.Sprintf("Current mode: %s\nUsage: /mode <plan|build|auto>", d.mode)}
	}
	mode := strings.ToLower(args[0])
	if mode != "plan" && mode != "build" && mode != "auto" {
		return SlashResult{Output: "Mode must be 'plan', 'build', or 'auto'.", IsError: true}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := d.client.UpdateSession(ctx, d.sessionID, api.UpdateSessionRequest{Mode: &mode})
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to switch mode: %v", err), IsError: true}
	}
	d.mode = mode
	if mode == "auto" {
		return SlashResult{Output: "Switched to auto mode.\n⚠ auto runs all tools — including shell commands — without asking. Unless a container sandbox is configured, commands execute directly on this host."}
	}
	return SlashResult{Output: fmt.Sprintf("Switched to %s mode.", mode)}
}

// cmdGuard toggles per-session output validation. Unlike /mode the toggle is
// not persisted server-side; it is sent with each message turn via
// PostMessageRequest.GuardEnabled, so it resets when the TUI restarts.
func (d *SlashDispatcher) cmdGuard(args []string) SlashResult {
	switch arg := strings.ToLower(strings.TrimSpace(firstArg(args))); arg {
	case "on", "true":
		v := true
		d.setGuard(&v)
		return SlashResult{Output: "Output guard: on (this session)"}
	case "off", "false":
		v := false
		d.setGuard(&v)
		return SlashResult{Output: "Output guard: off (this session)"}
	default:
		return SlashResult{Output: "Output guard: " + d.guardStatus() + "\nUsage: /guard [on|off|status]"}
	}
}

// setGuard records the per-session output-guard override (nil = server default).
func (d *SlashDispatcher) setGuard(v *bool) { d.guardEnabled = v }

// guardStatus reports the current per-session toggle: "default" when no override
// is set (the configured output_guard.enabled applies), else "on"/"off".
func (d *SlashDispatcher) guardStatus() string {
	if d.guardEnabled == nil {
		return "default"
	}
	if *d.guardEnabled {
		return "on"
	}
	return "off"
}

// cmdTools toggles per-session tool-output display between compact (10-line cap)
// and full (no cap). The result carries a sentinel string handled by the TUI.
func (d *SlashDispatcher) cmdTools(args []string) SlashResult {
	switch strings.ToLower(firstArg(args)) {
	case "compact":
		return SlashResult{Output: "\x00tools-compact"}
	case "full":
		return SlashResult{Output: "\x00tools-full"}
	default:
		return SlashResult{Output: "Usage: /tools <compact|full>\n  compact  cap tool output at 10 lines (default)\n  full     show complete tool output"}
	}
}

// firstArg returns the first argument or "" when none were given.
func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func (d *SlashDispatcher) cmdClear(_ []string) SlashResult {
	return SlashResult{Output: "\x00clear"} // special marker handled by TUI
}

func (d *SlashDispatcher) cmdMemory(_ []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mem, err := d.client.GetMemory(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load memory: %v", err), IsError: true}
	}
	var b strings.Builder
	if mem.ProjectMemory != "" {
		b.WriteString("Project memory:\n" + mem.ProjectMemory + "\n")
	}
	if mem.UserMemory != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("User memory:\n" + mem.UserMemory + "\n")
	}
	if b.Len() == 0 {
		b.WriteString("No memories saved yet. Use /remember <text> to save one.")
	}
	return SlashResult{Output: b.String()}
}

func (d *SlashDispatcher) cmdRemember(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "Usage: /remember <text to remember>", IsError: true}
	}
	entry := strings.Join(args, " ")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.client.AppendMemory(ctx, api.AppendMemoryRequest{Entry: entry, Scope: "project"}); err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to save: %v", err), IsError: true}
	}
	return SlashResult{Output: "Saved to project memory."}
}

func (d *SlashDispatcher) cmdSkills(args []string) SlashResult {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "enable", "disable":
			return d.cmdSkillsToggle(args)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mem, err := d.client.GetMemory(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load skills: %v", err), IsError: true}
	}

	cfg, err := config.Load()
	enabled := map[string]bool{}
	if err == nil {
		for _, n := range cfg.Skills.BuiltinEnabled {
			enabled[strings.ToLower(n)] = true
		}
	}

	var b strings.Builder
	if len(mem.Skills) == 0 {
		b.WriteString("No active skills (none enabled, no project/user skill files).\n")
	} else {
		b.WriteString("Active skills:\n")
		for _, s := range mem.Skills {
			fmt.Fprintf(&b, "  %s\n", s)
		}
	}
	b.WriteString("\nBuilt-in skills (ship with Aegis, dormant until enabled):\n")
	for _, bi := range skills.Builtins() {
		status := "off"
		if enabled[strings.ToLower(bi.Name)] {
			status = "on"
		}
		fmt.Fprintf(&b, "  [%3s] %-22s %s\n", status, bi.Name, bi.Description)
	}
	b.WriteString("\nUsage: /skills enable <name> [global] | /skills disable <name> [global]")
	return SlashResult{Output: b.String()}
}

// activateSkill turns on a dormant embedded built-in skill for this session
// only, right before a command like /threat-model sends a message that
// invokes it — the skill stays dormant (no system-prompt cost) for every
// session that never asks for it, and needs no config edit or daemon restart
// to become available in this one. Returns a warning string to prepend to the
// command's Output on failure (e.g. daemon unreachable); empty on success, so
// the model still gets a shot at the skill via a plain-text fallback.
func (d *SlashDispatcher) activateSkill(name string) string {
	if d.client == nil { // unit tests exercise prompt-building without a live daemon
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.client.ActivateSkill(ctx, d.sessionID, name); err != nil {
		return fmt.Sprintf("Warning: couldn't activate the %s skill (%v); asking anyway.\n\n", name, err)
	}
	return ""
}

// cmdSkillsToggle enables or disables a built-in skill by writing the full
// desired enabled set back to config. Like /sandbox use, the change is
// written immediately but applies on the next restart.
func (d *SlashDispatcher) cmdSkillsToggle(args []string) SlashResult {
	if len(args) < 2 {
		return SlashResult{Output: "Usage: /skills enable <name> [global] | /skills disable <name> [global]", IsError: true}
	}
	enable := strings.ToLower(args[0]) == "enable"
	name := strings.ToLower(strings.TrimSpace(args[1]))
	global := len(args) > 2 && strings.ToLower(args[2]) == "global"

	if !skills.IsBuiltin(name) {
		return SlashResult{Output: fmt.Sprintf("Unknown built-in skill %q. Run /skills to see the list.", name), IsError: true}
	}

	cfg, err := config.Load()
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load config: %v", err), IsError: true}
	}
	set := make(map[string]bool)
	for _, n := range cfg.Skills.BuiltinEnabled {
		set[strings.ToLower(n)] = true
	}
	if enable {
		set[name] = true
	} else {
		delete(set, name)
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)

	write := config.PatchProjectSkillsEnabled
	scope := "project"
	if global {
		write = config.PatchGlobalSkillsEnabled
		scope = "global"
	}
	if err := write(names); err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to write config: %v", err), IsError: true}
	}
	verb := "enabled"
	if !enable {
		verb = "disabled"
	}
	return SlashResult{Output: fmt.Sprintf("%s %q (%s config, written). Restart Aegis to apply.", verb, name, scope)}
}

func (d *SlashDispatcher) cmdCommands(_ []string) SlashResult {
	d.customs = nil // force refresh
	d.refreshCustoms()
	if len(d.customs) == 0 {
		return SlashResult{Output: "No custom commands found.\nAdd .md files to .aegis/commands/ to create commands."}
	}
	var b strings.Builder
	b.WriteString("Custom commands:\n")
	for _, c := range d.customs {
		argStr := ""
		if len(c.Args) > 0 {
			argStr = " <" + strings.Join(c.Args, "> <") + ">"
		}
		fmt.Fprintf(&b, "  /%-22s %s\n", c.Name+argStr, c.Description)
	}
	return SlashResult{Output: b.String()}
}

// cmdModels opens the interactive model picker (P16.6): curated models
// grouped by provider with the current model marked. Use /model <id>
// directly if the exact id is already known.
func (d *SlashDispatcher) cmdModels(_ []string) SlashResult {
	return SlashResult{Models: modelcatalog.Curated()}
}

// cmdModel switches this session's model (P14.7). Unlike /mode, this isn't
// validated against a fixed set of choices: any non-empty id is accepted and
// persisted as a per-session override that takes precedence over the active
// persona's own Model and the global provider.model on every subsequent turn
// — the same precedence a persona-level override already has, and just as
// unvalidated against the configured provider's actual model list. Switching
// to a model belonging to a different provider than the daemon's configured
// adapter will surface as a provider error on the next turn, not here.
// "/model default" clears the override, reverting to the persona/global
// default.
func (d *SlashDispatcher) cmdModel(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: fmt.Sprintf("Current model: %s\nUsage: /model <model-id>\n  /model default clears a session override, reverting to the persona/global default.", d.model)}
	}
	target := args[0]
	newModel := target
	if strings.EqualFold(target, "default") {
		newModel = ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := d.client.UpdateSession(ctx, d.sessionID, api.UpdateSessionRequest{Model: &newModel}); err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to switch model: %v", err), IsError: true}
	}

	if newModel == "" {
		d.model = ""
		return SlashResult{Output: "Cleared the session model override; reverts to the persona/global default on the next turn."}
	}
	d.model = newModel
	return SlashResult{
		Output: fmt.Sprintf("Switched to model %q for this session. This must be a model id belonging to your currently configured provider (see /status) — a cross-provider id will fail on the next turn, not now.", newModel),
		Model:  &newModel,
	}
}

func (d *SlashDispatcher) cmdSandbox(args []string) SlashResult {
	cfg, err := config.Load()
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load config: %v", err), IsError: true}
	}
	priority := sandbox.ParseRuntimes(cfg.Sandbox.Priority)

	// /sandbox use <target>: persist a new backend choice.
	if len(args) >= 2 && strings.ToLower(args[0]) == "use" {
		patch := config.SandboxPatch{Image: cfg.Sandbox.Image, Network: cfg.Sandbox.Network, Priority: cfg.Sandbox.Priority}
		target := strings.ToLower(strings.TrimSpace(args[1]))
		switch target {
		case "local", "auto":
			patch.Backend = target
		case "wsl", "wslc":
			patch.Backend, patch.Runtime = "container", "wslc"
		case "docker", "podman", "container":
			patch.Backend, patch.Runtime = "container", target
		default:
			return SlashResult{Output: fmt.Sprintf("Unknown sandbox target %q (want local, auto, docker, podman, wslc, or container).", args[1]), IsError: true}
		}
		if err := config.PatchGlobalSandbox(patch); err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to write config: %v", err), IsError: true}
		}
		return SlashResult{Output: fmt.Sprintf("Sandbox backend set to %q. Restart Aegis to apply.", target)}
	}

	// /sandbox: show current config and detected runtimes.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	backend := cfg.Sandbox.Backend
	if backend == "" {
		backend = "local"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Sandbox backend: %s", backend)
	if cfg.Sandbox.Runtime != "" {
		fmt.Fprintf(&b, " (runtime: %s)", cfg.Sandbox.Runtime)
	}
	b.WriteString("\n\n")
	b.WriteString(sandbox.Report(ctx, priority))
	b.WriteString("\n\nChange with: /sandbox use <local|auto|docker|podman|wslc|container>")
	return SlashResult{Output: b.String()}
}

// cmdTheme switches the TUI color scheme live (P14.8; generalized beyond
// dark/light to any loaded theme by P16.7). Unlike /sandbox and /model,
// there's no daemon round trip: the theme is purely a TUI rendering concern,
// so the dispatcher only validates the name and hands off to the model via
// the same "\x00"-prefixed sentinel Output convention /humor and /sidebar
// use for local (non-server) UI state changes — the model is the one that
// actually rebinds the package-level colors, rebuilds m.th, and recreates
// the markdown renderer (see the slashResultMsg case in tui.go). Validation
// here checks d.workDir's project theme directory plus the user's and the
// embedded builtins (availableThemeNames) — a name valid here is guaranteed
// to load in tui.go's actual switch. This session only: set tui.theme in
// config to persist across restarts, same as the config-only /sandbox use.
func (d *SlashDispatcher) cmdTheme(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "\x00theme-show"}
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	valid := availableThemeNames(d.workDir)
	found := false
	for _, v := range valid {
		if v == name {
			found = true
			break
		}
	}
	if !found {
		return SlashResult{Output: fmt.Sprintf("Unknown theme %q (want one of: %s).", args[0], strings.Join(valid, ", ")), IsError: true}
	}
	return SlashResult{Output: "\x00theme " + name}
}

// cmdNotify sets the P16.1 attention-system mode live, same pattern as
// cmdTheme: purely local TUI state, validated here and applied via the
// "\x00notify "-prefixed sentinel (see the slashResultMsg case in tui.go).
// This session only: set tui.notifications in config to persist.
func (d *SlashDispatcher) cmdNotify(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "\x00notify-show"}
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	switch name {
	case "off", "bell", "desktop", "both":
		return SlashResult{Output: "\x00notify " + name}
	default:
		return SlashResult{Output: fmt.Sprintf("Unknown notify mode %q (want off, bell, desktop, or both).", args[0]), IsError: true}
	}
}

// cmdStatus is the P14.5 daemon/session health surface: daemon reachability,
// provider/model, sandbox backend + any fallback reason (previously only
// ever shown once, to stderr, before the TUI took over the terminal — see
// warnSandboxFallback in internal/cli/root.go), this session's cumulative
// spend against its caps, and cross-session daily spend against the P9.5/
// P10.5 daily caps. Sandbox backend name comes from the local config (same
// no-daemon-round-trip convention as /sandbox and /security); everything
// else is daemon-authoritative via the new GET /status endpoint.
func (d *SlashDispatcher) cmdStatus(_ []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := d.client.StatusInfo(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to reach daemon: %v", err), IsError: true}
	}

	cfg, cfgErr := config.Load()

	var b strings.Builder
	b.WriteString("Daemon: ok\n")
	fmt.Fprintf(&b, "Provider: %s · Model: %s\n", info.Provider, info.Model)
	if info.ContextWindow > 0 {
		fmt.Fprintf(&b, "Context window: %d tokens (%s)\n", info.ContextWindow, describeCtxWinSource(info.ContextWindowSource))
		if info.ContextWindowSource == "ollama:default" {
			b.WriteString("  ⚠ Ollama is serving its default context; raise OLLAMA_CONTEXT_LENGTH (or a modelfile num_ctx) for long agent tasks\n")
		}
	}

	backend := "local"
	if cfgErr == nil {
		if cfg.Sandbox.Backend != "" {
			backend = cfg.Sandbox.Backend
		}
		if cfg.Sandbox.Runtime != "" {
			backend = fmt.Sprintf("%s (runtime: %s)", backend, cfg.Sandbox.Runtime)
		}
	}
	fmt.Fprintf(&b, "Sandbox: %s\n", backend)
	if info.SandboxFallback {
		fmt.Fprintf(&b, "  ⚠ fell back to unsandboxed local execution: %s\n", info.SandboxFallbackReason)
	}

	if sess, err := d.client.GetSession(ctx, d.sessionID); err == nil {
		fmt.Fprintf(&b, "Session (%s): %d tokens · $%.4f\n", sess.Mode, sess.InputTokens+sess.OutputTokens, sess.CostUSD)
	}
	if cfgErr == nil {
		if cfg.Cost.SessionCapUSD > 0 || cfg.Cost.SessionTokenCap > 0 {
			fmt.Fprintf(&b, "  session cap: $%.2f / %d tokens\n", cfg.Cost.SessionCapUSD, cfg.Cost.SessionTokenCap)
		}
		if cfg.Cost.BudgetUSD > 0 || cfg.Cost.MaxTokensPerRun > 0 {
			fmt.Fprintf(&b, "  per-run cap: $%.2f / %d tokens\n", cfg.Cost.BudgetUSD, cfg.Cost.MaxTokensPerRun)
		}
	}

	fmt.Fprintf(&b, "Today (all sessions): %d tokens · $%.4f\n", info.DailyTokens, info.DailyCostUSD)
	if info.DailyCapUSD > 0 || info.DailyTokenCap > 0 {
		fmt.Fprintf(&b, "  daily cap: $%.2f / %d tokens\n", info.DailyCapUSD, info.DailyTokenCap)
	}

	fmt.Fprintf(&b, "Sub-agent concurrency: %d (adaptive, max %d)\n", info.AgentConcurrency, info.AgentConcurrencyMax)

	return SlashResult{Output: strings.TrimRight(b.String(), "\n")}
}

// describeCtxWinSource renders the /status context_window_source values in
// user-facing terms.
func describeCtxWinSource(src string) string {
	switch src {
	case "config":
		return "from config context_window"
	case "ollama:loaded":
		return "reported by Ollama for the loaded model"
	case "ollama:modelfile":
		return "Ollama modelfile num_ctx"
	case "ollama:default":
		return "Ollama server default, assumed"
	default:
		return src
	}
}

// cmdSecurityConfig opens the interactive security-scanner config dialog
// (P11.11) — lets the user toggle enabled/method/install/image per scanner
// without hand-editing security.tools in config.yaml. Like /sandbox use and
// /skills enable, it defaults to the project config; pass "global" to edit
// the user-level config instead.
// cmdKnowledge is the P14.3 in-session surface for the project knowledge base
// (previously only reachable via `aegis knowledge index` and the model's
// project_knowledge tool): /knowledge rebuilds the FTS5 index the same way
// `aegis knowledge index` does, and /knowledge query searches it the same way
// the project_knowledge tool does — both via the daemon's own live store
// rather than opening a second sqlite connection from the TUI process.
func (d *SlashDispatcher) cmdKnowledge(args []string) SlashResult {
	sub := ""
	var rest []string
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
		rest = args[1:]
	}
	switch sub {
	case "index":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		resp, err := d.client.Knowledge(ctx, api.KnowledgeRequest{Action: "index"})
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Index failed: %v", err), IsError: true}
		}
		out := fmt.Sprintf("Indexed %d documents → %s", resp.DocCount, resp.DBPath)
		if resp.EmbeddingsEnabled {
			out += "\nSemantic embeddings: enabled"
		}
		return SlashResult{Output: out}
	case "query":
		query := strings.TrimSpace(strings.Join(rest, " "))
		if query == "" {
			return SlashResult{Output: "usage: /knowledge query <text>", IsError: true}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		resp, err := d.client.Knowledge(ctx, api.KnowledgeRequest{Action: "query", Query: query})
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Query failed: %v", err), IsError: true}
		}
		if resp.Count == 0 {
			return SlashResult{Output: fmt.Sprintf("no results for %q (run /knowledge index to rebuild)", query)}
		}
		var b strings.Builder
		for i, res := range resp.Results {
			fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n\n", i+1, res.Path, res.Title, res.Snippet)
		}
		return SlashResult{Output: strings.TrimRight(b.String(), "\n")}
	case "":
		return SlashResult{Output: "usage: /knowledge [index|query <text>]", IsError: true}
	default:
		return SlashResult{Output: fmt.Sprintf("Unknown /knowledge subcommand %q.\nUsage: /knowledge [index|query <text>]", args[0]), IsError: true}
	}
}

// cmdIndex rebuilds the repository map (P2.3/P14.3) directly against the
// daemon's workspace, refreshing both the on-disk cache
// (.aegis/repomap.json) and the daemon's cached system-prompt block — the
// same build `aegis index` runs, without needing a restart to pick it up.
func (d *SlashDispatcher) cmdIndex(_ []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := d.client.RepoMapIndex(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Index failed: %v", err), IsError: true}
	}
	return SlashResult{Output: fmt.Sprintf("Indexed %d files → %s", resp.FileCount, resp.Path)}
}

// cmdDebate runs a multi-agent debate directly against the daemon's
// configured model and prints the formatted transcript — no session/prior
// conversational turn needed, same underlying mechanism as the `agent` tool's
// mode:"debate". Usage:
//
//	/debate <claim text>                     debate the given claim with the default (security) roles
//	/debate --domain generic <claim text>    use the general/critic/arbiter personas instead
//	/debate --file <path> [--file <path>...] <claim text>   ground the debate in specific documents
//	/debate --proposer/--critic/--arbiter <persona> <claim text>   override individual roles
//	/debate --max-rounds <n> <claim text>    override the critique/rebuttal round bound (default 2)
//
// Leading --flag value pairs are consumed before the remaining args are
// joined into the claim, mirroring the `aegis debate` CLI's flag names.
//
// Unlike /scan, a debate spends real model turns (one per role per round), so
// it uses the same long timeout /scan uses rather than the 5s default other
// direct-daemon-call commands use.
func (d *SlashDispatcher) cmdDebate(args []string) SlashResult {
	req, rest, err := parseDebateArgs(args)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	req.Claim = strings.TrimSpace(strings.Join(rest, " "))
	if req.Claim == "" {
		return SlashResult{Output: "usage: /debate [--domain generic] [--file <path>]... [--proposer|--critic|--arbiter <persona>] [--max-rounds <n>] <claim>", IsError: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	resp, err := d.client.Debate(ctx, req)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Debate failed: %v", err), IsError: true}
	}
	return SlashResult{Output: resp.Report}
}

// parseDebateArgs consumes leading --flag value pairs recognized by /debate,
// returning the populated request (Claim left empty) and the remaining args
// to be joined as the claim text.
func parseDebateArgs(args []string) (api.DebateRequest, []string, error) {
	var req api.DebateRequest
	i := 0
	for i < len(args) {
		flag := args[i]
		if !strings.HasPrefix(flag, "--") {
			break
		}
		if i+1 >= len(args) {
			return req, nil, fmt.Errorf("flag %s requires a value", flag)
		}
		val := args[i+1]
		switch flag {
		case "--domain":
			req.Domain = val
		case "--file":
			req.Files = append(req.Files, val)
		case "--proposer":
			req.ProposerPersona = val
		case "--critic":
			req.CriticPersona = val
		case "--arbiter":
			req.ArbiterPersona = val
		case "--max-rounds":
			n, err := strconv.Atoi(val)
			if err != nil {
				return req, nil, fmt.Errorf("--max-rounds: %v", err)
			}
			req.MaxRounds = n
		default:
			// Not a recognized flag — treat it and everything after as claim text.
			return req, args[i:], nil
		}
		i += 2
	}
	return req, args[i:], nil
}

// threatModelFrameworkAliases maps a lowercase leading /threat-model token to
// its canonical framework name. "nist" alone is accepted since "800-154" is
// awkward to type and unambiguous in this context; if it's followed by a
// literal "800-154" token that token is consumed too.
var threatModelFrameworkAliases = map[string]string{
	"stride":       "STRIDE",
	"linddun":      "LINDDUN",
	"pasta":        "PASTA",
	"trike":        "Trike",
	"vast":         "VAST",
	"nist":         "NIST 800-154",
	"nist800-154":  "NIST 800-154",
	"nist-800-154": "NIST 800-154",
	"nist800154":   "NIST 800-154",
}

// extractThreatModelFramework consumes a recognized framework name from the
// front of /threat-model's args (e.g. "PASTA the auth service" ->
// ("PASTA", ["the", "auth", "service"])), so the framework can be given
// up front instead of only via the skill's clarifying question.
func extractThreatModelFramework(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	name, ok := threatModelFrameworkAliases[strings.ToLower(args[0])]
	if !ok {
		return "", args
	}
	rest := args[1:]
	if strings.EqualFold(args[0], "nist") && len(rest) > 0 && rest[0] == "800-154" {
		rest = rest[1:]
	}
	return name, rest
}

// cmdThreatModel sends a message that directly invokes the threat-modeling
// skill, so its framework-selection clarifying question is asked as part of
// the resulting turn rather than depending on the model noticing a trigger
// phrase in free text (P13.6 TUI-surface requirement). A leading framework
// name (STRIDE, LINDDUN, PASTA, Trike, VAST, NIST 800-154) is recognized and
// passed through as an explicit choice; without one, it opens a picker
// dialog instead of spending a model turn on the clarifying question.
func (d *SlashDispatcher) cmdThreatModel(args []string) SlashResult {
	framework, rest := extractThreatModelFramework(args)
	target := strings.TrimSpace(strings.Join(rest, " "))
	if framework == "" {
		return SlashResult{ThreatModelTarget: &target}
	}
	// The common case is "model the thing Aegis is actually running against"
	// — naming the workspace explicitly (and its path, when known) grounds
	// that instead of leaving the model to guess what "this project" means,
	// and matches the skill's own §2 instruction to explore the real
	// workspace rather than an assumed architecture.
	prompt := "Load the threat-modeling skill and produce a threat model for "
	if target != "" {
		prompt += target + ", scoped to the current workspace"
	} else {
		prompt += "the application and codebase in the current workspace"
	}
	if d.workDir != "" {
		prompt += fmt.Sprintf(" (%s)", d.workDir)
	}
	prompt += fmt.Sprintf(". Use the %s framework — this has already been decided, so skip the framework-selection clarifying question.", framework)
	warn := d.activateSkill("threat-modeling")
	return SlashResult{Output: warn, Message: prompt}
}

// cmdReport sends a message that directly invokes the html-report or
// latex-report skill (P13.7 TUI-surface requirement), so a user driving the
// TUI has a discoverable entry point into report consolidation instead of
// depending on the model noticing a trigger phrase in free text.
func (d *SlashDispatcher) cmdReport(args []string) SlashResult {
	skill := "html-report"
	if len(args) > 0 && strings.EqualFold(args[0], "latex") {
		skill = "latex-report"
		args = args[1:]
	}
	sources := strings.TrimSpace(strings.Join(args, " "))

	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Load the %s skill and consolidate ", skill)
	if sources != "" {
		fmt.Fprintf(&prompt, "the following source docs into one coherent report: %s.", sources)
	} else {
		prompt.WriteString("the relevant existing markdown docs in this project into one coherent report. Ask me which docs to include if it isn't already clear from context.")
	}
	warn := d.activateSkill(skill)
	return SlashResult{Output: warn, Message: prompt.String()}
}

// cmdResearch sends a message that directly invokes the deep-research skill
// (P20.1 TUI-surface requirement), so a structured research run — planned
// rounds, source-quality vetting, a findings log with an audit trail, and a
// cited report — is a discoverable entry point instead of relying on the
// model noticing a trigger phrase in free text.
func (d *SlashDispatcher) cmdResearch(args []string) SlashResult {
	topic := strings.TrimSpace(strings.Join(args, " "))
	prompt := "Load the deep-research skill and run a structured research workflow"
	if topic != "" {
		prompt += " on: " + topic
	} else {
		prompt += ". Ask me what to research before running any searches."
	}
	prompt += " Follow the skill's round structure, source-quality bar, and citation discipline; end with the cited report."
	warn := d.activateSkill("deep-research")
	return SlashResult{Output: warn, Message: prompt}
}

func (d *SlashDispatcher) cmdSession(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(args) > 0 && strings.ToLower(args[0]) == "list" {
		sessions, err := d.client.ListSessions(ctx)
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to list sessions: %v", err), IsError: true}
		}
		if len(sessions) == 0 {
			return SlashResult{Output: "No sessions."}
		}
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for _, s := range sessions {
			marker := "  "
			if s.ID == d.sessionID {
				marker = "* "
			}
			fmt.Fprintf(&b, "%s%-8s  %-6s  %s  %s\n", marker, s.ID[:8], s.Mode, s.UpdatedAt.Format("2006-01-02 15:04"), s.Title)
		}
		return SlashResult{Output: b.String()}
	}

	sess, err := d.client.GetSession(ctx, d.sessionID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to get session: %v", err), IsError: true}
	}
	return SlashResult{Output: fmt.Sprintf("Session: %s\nTitle: %s\nMode: %s\nMessages: %d\nCreated: %s",
		sess.ID, sess.Title, sess.Mode, len(sess.Messages), sess.CreatedAt.Format(time.RFC3339))}
}

// cmdCompact forces context compaction now rather than waiting for the
// automatic budget-driven trigger inside engine.Run (P19.2) — e.g. ahead of a
// long tool-heavy stretch the user knows is coming.
func (d *SlashDispatcher) cmdCompact(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	resp, err := d.client.Compact(ctx, d.sessionID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Compaction failed: %v", err), IsError: true}
	}
	if !resp.Compacted {
		return SlashResult{Output: "Nothing to compact — the conversation is too short to safely summarize."}
	}
	return SlashResult{
		Output:        fmt.Sprintf("Compacted conversation: %d messages -> %d.", resp.MessagesBefore, resp.MessagesAfter),
		ReloadSession: true,
	}
}

func (d *SlashDispatcher) cmdRewind(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cps, err := d.client.ListCheckpoints(ctx, d.sessionID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to list checkpoints: %v", err), IsError: true}
	}
	if len(cps) == 0 {
		return SlashResult{Output: "No checkpoints yet. One is captured at the start of each turn once you send a message."}
	}

	if len(args) == 0 {
		var b strings.Builder
		b.WriteString("Checkpoints (newest first):\n")
		for i, cp := range cps {
			files := ""
			if cp.FileCount > 0 {
				files = fmt.Sprintf(" · %d file(s)", cp.FileCount)
			}
			label := strings.ReplaceAll(cp.Label, "\n", " ")
			fmt.Fprintf(&b, "  %2d  %s%s\n      %s\n", i+1, cp.CreatedAt.Format("15:04:05"), files, label)
		}
		b.WriteString("\nUse /rewind <n> [code|conversation|both] to restore.")
		return SlashResult{Output: b.String()}
	}

	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > len(cps) {
		return SlashResult{Output: fmt.Sprintf("Invalid checkpoint number %q. Use /rewind to see the list (1–%d).", args[0], len(cps)), IsError: true}
	}
	scope := "both"
	if len(args) > 1 {
		scope = strings.ToLower(args[1])
		if scope != "code" && scope != "conversation" && scope != "both" {
			return SlashResult{Output: "Scope must be 'code', 'conversation', or 'both'.", IsError: true}
		}
	}

	cp := cps[n-1]
	resp, err := d.client.Rewind(ctx, d.sessionID, cp.ID, scope)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Rewind failed: %v", err), IsError: true}
	}

	summary := fmt.Sprintf("Rewound to checkpoint %d (%s)", n, scope)
	switch scope {
	case "code":
		summary += fmt.Sprintf(": restored %d file(s).", resp.FilesRestored)
	case "conversation":
		summary += fmt.Sprintf(": kept %d message(s).", resp.MessagesKept)
	default:
		summary += fmt.Sprintf(": restored %d file(s), kept %d message(s).", resp.FilesRestored, resp.MessagesKept)
	}

	// A code-only rewind leaves the transcript intact; otherwise the
	// conversation changed and the TUI must reload it.
	if scope == "code" {
		return SlashResult{Output: summary}
	}
	return SlashResult{Output: summary, ReloadSession: true}
}

// cmdFork creates a new session that branches off the current one (P22.3) —
// the non-destructive counterpart to /rewind: the source session is never
// truncated, so this is safe to use for "let me try something risky" without
// any risk to the conversation you're forking from.
//
// No args forks at the current end of the conversation — a clean sandbox
// branch point. /fork <n> instead truncates the new session to the nth
// checkpoint (newest first, same numbering /rewind and /rollback already
// use): the state just before that turn's user message, ready to receive a
// fresh or edited message picking up from there. Either way, on success the
// TUI switches into the new session, mirroring what picking an entry from
// Ctrl+Y's session switcher already does.
func (d *SlashDispatcher) cmdFork(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	checkpointID := ""
	if len(args) > 0 {
		// Validate the argument shape locally before ever touching the
		// daemon — "/fork abc" should fail immediately rather than paying
		// for a checkpoint list fetch just to reject it.
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return SlashResult{Output: fmt.Sprintf("Invalid checkpoint number %q. Use /rewind to see the list.", args[0]), IsError: true}
		}
		cps, err := d.client.ListCheckpoints(ctx, d.sessionID)
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to list checkpoints: %v", err), IsError: true}
		}
		if len(cps) == 0 {
			return SlashResult{Output: "No checkpoints yet. One is captured at the start of each turn once you send a message."}
		}
		if n > len(cps) {
			return SlashResult{Output: fmt.Sprintf("Invalid checkpoint number %d. Use /rewind to see the list (1–%d).", n, len(cps)), IsError: true}
		}
		checkpointID = cps[n-1].ID
	}

	resp, err := d.client.Fork(ctx, d.sessionID, checkpointID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Fork failed: %v", err), IsError: true}
	}

	return SlashResult{
		Output:          fmt.Sprintf("Forked into new session %s (%q), %d message(s) — switching to it.", short(resp.SessionID), resp.Title, resp.MessagesKept),
		SwitchToSession: resp.SessionID,
	}
}

// cmdRollback is like rewind but also resets git HEAD to the pre-turn commit,
// providing a true "undo everything" for multi-file task failures (P3.4).
func (d *SlashDispatcher) cmdRollback(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cps, err := d.client.ListCheckpoints(ctx, d.sessionID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to list checkpoints: %v", err), IsError: true}
	}
	if len(cps) == 0 {
		return SlashResult{Output: "No checkpoints yet. Send a message first."}
	}

	if len(args) == 0 {
		var b strings.Builder
		b.WriteString("Checkpoints available for rollback (newest first):\n")
		for i, cp := range cps {
			git := ""
			if cp.GitSHA != "" {
				git = fmt.Sprintf(" · git:%s", cp.GitSHA[:8])
			}
			label := strings.ReplaceAll(cp.Label, "\n", " ")
			fmt.Fprintf(&b, "  %2d  %s%s\n      %s\n", i+1, cp.CreatedAt.Format("15:04:05"), git, label)
		}
		b.WriteString("\nUse /rollback <n> to restore files AND git reset --hard to pre-turn HEAD.")
		return SlashResult{Output: b.String()}
	}

	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > len(cps) {
		return SlashResult{Output: fmt.Sprintf("Invalid number %q (1–%d).", args[0], len(cps)), IsError: true}
	}
	cp := cps[n-1]

	noGit := ""
	if cp.GitSHA == "" {
		noGit = " (no git SHA recorded — file-only restore)"
	}

	resp, err := d.client.Rollback(ctx, d.sessionID, cp.ID, "both")
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Rollback failed: %v", err), IsError: true}
	}

	summary := fmt.Sprintf("Rolled back to checkpoint %d%s: restored %d file(s), kept %d message(s).",
		n, noGit, resp.FilesRestored, resp.MessagesKept)
	return SlashResult{Output: summary, ReloadSession: true}
}

// cmdDetach toggles the background (detached) flag on the current session.
// When on, subsequent message turns run with a server-level context so they
// continue even after the TUI disconnects (P3.2).
func (d *SlashDispatcher) cmdDetach(args []string) SlashResult {
	on := true // default to enabling
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "off", "false", "0":
			on = false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.client.SetBackground(ctx, d.sessionID, on); err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to set background mode: %v", err), IsError: true}
	}
	if on {
		return SlashResult{Output: "Background mode: on\nThis session now runs detached from the TUI. Close the TUI at any time — the turn will continue running.\nUse `aegis bg list` to check status or /detach off to disable."}
	}
	return SlashResult{Output: "Background mode: off\nThis session is back to normal (foreground) execution."}
}

// cmdHumor toggles D&D-themed thinking phrases in the response area.
// Uses the "\x00humor-*" magic output protocol (same as /tools compact|full).
func (d *SlashDispatcher) cmdHumor(args []string) SlashResult {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "off", "false", "0":
			return SlashResult{Output: "\x00humor-off"}
		case "on", "true", "1":
			return SlashResult{Output: "\x00humor-on"}
		default:
			return SlashResult{Output: "Usage: /humor [on|off]", IsError: true}
		}
	}
	// No arg: toggle current state
	return SlashResult{Output: "\x00humor-toggle"}
}

// cmdArchive archives or unarchives the current session, or lists archived
// sessions. Archived sessions are hidden from normal listings but their data
// is preserved. Use /archive off to restore. To permanently remove a session,
// use `aegis sessions delete <id>`.
func (d *SlashDispatcher) cmdArchive(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(args) > 0 && strings.ToLower(args[0]) == "list" {
		metas, err := d.client.ListArchivedSessions(ctx)
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to list archived sessions: %v", err), IsError: true}
		}
		var archived []api.SessionMeta
		for _, m := range metas {
			if m.Archived {
				archived = append(archived, m)
			}
		}
		if len(archived) == 0 {
			return SlashResult{Output: "No archived sessions."}
		}
		var b strings.Builder
		b.WriteString("Archived sessions:\n")
		for _, m := range archived {
			title := m.Title
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(&b, "  %-8s  %-6s  %s  %s\n", m.ID[:8], m.Mode, m.UpdatedAt.Local().Format("2006-01-02 15:04"), title)
		}
		return SlashResult{Output: b.String()}
	}

	unarchive := len(args) > 0 && (strings.ToLower(args[0]) == "off" || strings.ToLower(args[0]) == "false")
	if unarchive {
		if err := d.client.UnarchiveSession(ctx, d.sessionID); err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to unarchive session: %v", err), IsError: true}
		}
		return SlashResult{Output: "Session unarchived. It will now appear in normal session listings."}
	}
	if err := d.client.ArchiveSession(ctx, d.sessionID); err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to archive session: %v", err), IsError: true}
	}
	return SlashResult{Output: fmt.Sprintf("Session %s archived.\nIt is now hidden from normal listings but all data is preserved.\nUse /archive off to restore, or `aegis sessions delete %s` to permanently remove.", d.sessionID[:8], d.sessionID[:8])}
}

// cmdPrune deletes non-archived sessions older than the given number of days
// (or the server's configured TTL if no argument is given) — same operation
// as `aegis sessions prune`.
func (d *SlashDispatcher) cmdPrune(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	days := 0
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 {
			return SlashResult{Output: fmt.Sprintf("Invalid days %q.", args[0]), IsError: true}
		}
		days = n
	}

	resp, err := d.client.PruneSessions(ctx, days)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Prune failed: %v", err), IsError: true}
	}
	if resp.Deleted == 0 {
		return SlashResult{Output: "No sessions matched (nothing pruned)."}
	}
	return SlashResult{Output: fmt.Sprintf("Pruned %d session(s).", resp.Deleted)}
}

// cmdRuns lists message runs currently in flight across all sessions — same
// data as `aegis runs`.
func (d *SlashDispatcher) cmdRuns(_ []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runs, err := d.client.ListRuns(ctx)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to list runs: %v", err), IsError: true}
	}
	if len(runs) == 0 {
		return SlashResult{Output: "No active runs."}
	}
	var b strings.Builder
	b.WriteString("Active runs:\n")
	for _, r := range runs {
		title := r.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "  %-8s  %s  %3d tools  %-12s  %s\n",
			r.SessionID[:8], time.Since(r.StartedAt).Truncate(time.Second), r.Tools, r.LastKind, title)
	}
	return SlashResult{Output: b.String()}
}

// cmdBG lists sessions running in background (detached) mode, or prints
// buffered engine events from one — same data as `aegis bg list`/`aegis bg
// events`.
func (d *SlashDispatcher) cmdBG(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "list":
		sessions, err := d.client.ListSessions(ctx)
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to list sessions: %v", err), IsError: true}
		}
		var b strings.Builder
		b.WriteString("Background sessions:\n")
		found := false
		for _, s := range sessions {
			if !s.Background {
				continue
			}
			found = true
			fmt.Fprintf(&b, "  %-8s  %-6s  %s  %s\n", s.ID[:8], s.Mode, s.UpdatedAt.Local().Format("2006-01-02 15:04"), s.Title)
		}
		if !found {
			return SlashResult{Output: "No sessions running in background mode."}
		}
		return SlashResult{Output: b.String()}
	case "events":
		id := d.sessionID
		if len(args) > 1 {
			id = args[1]
		}
		events, err := d.client.GetBGEvents(ctx, id, 0)
		if err != nil {
			return SlashResult{Output: fmt.Sprintf("Failed to get events: %v", err), IsError: true}
		}
		if len(events) == 0 {
			return SlashResult{Output: "No buffered events."}
		}
		var b strings.Builder
		for _, e := range events {
			var ev api.Event
			if json.Unmarshal([]byte(e.Data), &ev) == nil {
				switch ev.Kind {
				case api.KindText:
					b.WriteString(ev.Text)
				case api.KindToolCall:
					fmt.Fprintf(&b, "\n[tool] %s\n", ev.Tool)
				case api.KindToolResult:
					fmt.Fprintf(&b, "[result] %s\n", ev.ToolResult)
				case api.KindTurnDone:
					fmt.Fprintf(&b, "\n[done] in=%d out=%d\n", ev.InputTokens, ev.OutputTokens)
				case api.KindError:
					fmt.Fprintf(&b, "[error] %s\n", ev.Error)
				}
			}
		}
		return SlashResult{Output: b.String()}
	default:
		return SlashResult{Output: "Usage: /bg [list|events [session-id]]", IsError: true}
	}
}

// cmdBundle dispatches /bundle's two subcommands — info and install — same
// operations as `aegis bundle info`/`aegis bundle install`, run directly
// against the local filesystem (no daemon round trip, matching /sandbox and
// /security's local-computation convention).
func (d *SlashDispatcher) cmdBundle(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "Usage: /bundle [install|info <path-or-url>]", IsError: true}
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "info":
		return d.cmdBundleInfo(rest)
	case "install":
		return d.cmdBundleInstall(rest)
	default:
		return SlashResult{Output: fmt.Sprintf("Unknown /bundle subcommand %q.\nUsage: /bundle [install|info <path-or-url>]", args[0]), IsError: true}
	}
}

// bundleIsGitURL mirrors internal/cli/bundle.go's isGitURL — kept as a
// separate unexported copy rather than shared, since internal/cli isn't (and
// shouldn't become) an import of internal/tui.
func bundleIsGitURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "git://") ||
		strings.HasPrefix(s, "ssh://")
}

// bundleResolveSource mirrors internal/cli/bundle.go's resolveBundle: a git
// URL is shallow-cloned into a temp dir (the caller must call cleanup once
// done reading from the returned path); a local path is returned as-is.
func bundleResolveSource(src string) (path string, cleanup func(), err error) {
	if !bundleIsGitURL(src) {
		return src, nil, nil
	}
	dir, err := os.MkdirTemp("", "aegis-bundle-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	rm := func() { os.RemoveAll(dir) }
	out, cloneErr := exec.Command("git", "clone", "--depth=1", src, dir).CombinedOutput()
	if cloneErr != nil {
		rm()
		return "", nil, fmt.Errorf("git clone %s: %v\n%s", src, cloneErr, strings.TrimSpace(string(out)))
	}
	return dir, rm, nil
}

// bundleScopeDir mirrors internal/cli/bundle.go's scopeDir: project scope is
// ./.aegis relative to the daemon's workspace, user scope is the configured
// data dir.
func bundleScopeDir(global bool) (string, error) {
	if !global {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, ".aegis"), nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.DataDir, nil
}

func (d *SlashDispatcher) cmdBundleInfo(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "Usage: /bundle info <path-or-url>", IsError: true}
	}
	path, cleanup, err := bundleResolveSource(args[0])
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	if cleanup != nil {
		defer cleanup()
	}
	b, err := bundle.Load(path)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%s %s\n", b.Manifest.Name, b.Manifest.Version)
	if b.Manifest.Description != "" {
		fmt.Fprintf(&out, "%s\n", b.Manifest.Description)
	}
	if b.Manifest.Author != "" {
		fmt.Fprintf(&out, "by %s\n", b.Manifest.Author)
	}
	fmt.Fprintf(&out, "\n%d artifact(s):\n", len(b.Artifacts))
	for _, a := range b.Artifacts {
		fmt.Fprintf(&out, "  %s/%s\n", a.Kind, a.Rel)
	}
	if hash, err := b.ContentHash(); err == nil {
		fmt.Fprintf(&out, "\ncontent hash: %s\n(pin with `/bundle install %s global sha256:%s confirm` to detect upstream changes)\n", hash, args[0], strings.TrimPrefix(hash, "sha256:"))
	}
	return SlashResult{Output: out.String()}
}

// cmdBundleInstall mirrors `aegis bundle install`, adapted to the slash-command
// shape (no interactive flags): trailing tokens after the path may appear in
// any order — "global" installs into the user data dir instead of the
// project's .aegis/ (default), "sha256:<hash>" pins the P7.6 content-hash
// provenance check the same as the CLI's --expect-sha256, and "confirm" is
// required to actually write anything. Without "confirm" this only previews
// the manifest/artifacts/hash, same posture as /security install.
func (d *SlashDispatcher) cmdBundleInstall(args []string) SlashResult {
	if len(args) == 0 {
		return SlashResult{Output: "Usage: /bundle install <path-or-url> [global] [sha256:<hash>] [confirm]", IsError: true}
	}
	src := args[0]
	global := false
	confirmed := false
	expectHash := ""
	for _, a := range args[1:] {
		switch {
		case strings.EqualFold(a, "global"):
			global = true
		case strings.EqualFold(a, "confirm"):
			confirmed = true
		case strings.HasPrefix(strings.ToLower(a), "sha256:"):
			expectHash = a
		default:
			expectHash = "sha256:" + a // allow the bare hex digest too
		}
	}

	path, cleanup, err := bundleResolveSource(src)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	if cleanup != nil {
		defer cleanup()
	}
	b, err := bundle.Load(path)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}

	hash, hashErr := b.ContentHash()
	if expectHash != "" {
		if hashErr != nil {
			return SlashResult{Output: fmt.Sprintf("compute content hash: %v", hashErr), IsError: true}
		}
		if !strings.EqualFold(hash, expectHash) {
			return SlashResult{Output: fmt.Sprintf("Bundle content hash mismatch: got %s, expected %s — refusing to install (the source may have changed).", hash, expectHash), IsError: true}
		}
	}

	dest, err := bundleScopeDir(global)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}

	if !confirmed {
		scopeLabel := "project"
		if global {
			scopeLabel = "user (global)"
		}
		var preview strings.Builder
		fmt.Fprintf(&preview, "%s %s — %d artifact(s), installing into %s scope (%s):\n", b.Manifest.Name, b.Manifest.Version, len(b.Artifacts), scopeLabel, dest)
		for _, a := range b.Artifacts {
			fmt.Fprintf(&preview, "  %s/%s\n", a.Kind, a.Rel)
		}
		if hashErr == nil {
			fmt.Fprintf(&preview, "\ncontent hash: %s\n", hash)
		}
		scopeArg := ""
		if global {
			scopeArg = " global"
		}
		fmt.Fprintf(&preview, "\nRun `/bundle install %s%s confirm` to proceed.", src, scopeArg)
		return SlashResult{Output: preview.String()}
	}

	written, err := b.Install(dest, false)
	if err != nil {
		return SlashResult{Output: err.Error(), IsError: true}
	}
	if len(written) == 0 {
		return SlashResult{Output: "Nothing installed (all artifacts already exist)."}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Installed %q (%d file(s)) into %s:\n", b.Manifest.Name, len(written), dest)
	for _, w := range written {
		fmt.Fprintf(&out, "  %s\n", w)
	}
	skipped := len(b.Artifacts) - len(written)
	if skipped > 0 {
		fmt.Fprintf(&out, "(%d already present, skipped)\n", skipped)
	}
	if hashErr == nil {
		fmt.Fprintf(&out, "content hash: %s\n", hash)
	}
	return SlashResult{Output: out.String()}
}

func (d *SlashDispatcher) cmdShare(args []string) SlashResult {
	format := share.FormatHTML
	if len(args) > 0 {
		f, err := share.ParseFormat(args[0])
		if err != nil {
			return SlashResult{Output: err.Error(), IsError: true}
		}
		format = f
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := d.client.GetSession(ctx, d.sessionID)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Failed to load session: %v", err), IsError: true}
	}
	data, err := share.Render(sess, format)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Export failed: %v", err), IsError: true}
	}

	id := d.sessionID
	if len(id) > 8 {
		id = id[:8]
	}
	dir, _ := os.Getwd()
	path := filepath.Join(dir, fmt.Sprintf("aegis-session-%s.%s", id, format.Ext()))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return SlashResult{Output: fmt.Sprintf("Write failed: %v", err), IsError: true}
	}
	return SlashResult{Output: fmt.Sprintf("Exported session → %s", path)}
}

func (d *SlashDispatcher) cmdConfig(_ []string) SlashResult {
	return SlashResult{Output: "\x00wizard"}
}

func (d *SlashDispatcher) cmdTimeline(_ []string) SlashResult {
	return SlashResult{Output: "\x00timeline"}
}

// cmdSidebar toggles the sidebar panel. Uses the "\x00sidebar-toggle" protocol.
func (d *SlashDispatcher) cmdSidebar(_ []string) SlashResult {
	return SlashResult{Output: "\x00sidebar-toggle"}
}

// cmdScrollback toggles raw scrollback mode (P22.6): plain, unclipped
// transcript text with alt-screen and mouse capture released, so the
// terminal emulator's own scrollback/selection/search work natively.
func (d *SlashDispatcher) cmdScrollback(args []string) SlashResult {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "off", "false", "0":
			return SlashResult{Output: "\x00scrollback-off"}
		case "on", "true", "1":
			return SlashResult{Output: "\x00scrollback-on"}
		default:
			return SlashResult{Output: "Usage: /scrollback [on|off]", IsError: true}
		}
	}
	// No arg: toggle current state
	return SlashResult{Output: "\x00scrollback-toggle"}
}

// cmdCopy copies the last assistant message (or Nth code block) to the clipboard.
func (d *SlashDispatcher) cmdCopy(args []string) SlashResult {
	if len(args) > 0 {
		return SlashResult{Output: "\x00copy " + args[0]}
	}
	return SlashResult{Output: "\x00copy"}
}

// cmdPasteImage reads an image off the OS clipboard and attaches it to the
// draft — a slash-command fallback for terminals that intercept ctrl+v for
// their own paste binding before it ever reaches Aegis (P16.8).
func (d *SlashDispatcher) cmdPasteImage(_ []string) SlashResult {
	return SlashResult{Output: "\x00paste-image"}
}

func (d *SlashDispatcher) cmdQuit(_ []string) SlashResult {
	return SlashResult{Quit: true}
}
