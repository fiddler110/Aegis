package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/modelcatalog"
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

	// Drive is non-nil to start an unattended phased skill drive (P52.12)
	// instead of sending an ordinary message. The TUI streams it over the same
	// SSE path a message uses, so the transcript renders it identically — what
	// differs is server-side: fresh context per phase, auto verify + quality
	// pass, backend-liveness resume, hollow-body re-entry. Until this existed,
	// an unattended build meant dropping out of the TUI to `aegis chat --skill`.
	Drive *api.DriveRequest

	// ThreatModelTarget is non-nil to open the /threat-model framework picker
	// (forcing the choice up front instead of a model-turn clarifying
	// question); its value is the already-parsed target text ("" for the
	// whole project), carried through to re-dispatch /threat-model once a
	// framework is picked.
	ThreatModelTarget *string

	// ThreatModelUnattended is set alongside ThreatModelTarget when the user
	// asked for an unattended run before a framework was chosen, so the flag
	// survives the picker round-trip and the re-dispatched command still drives
	// (P52.12). Meaningless without ThreatModelTarget.
	ThreatModelUnattended bool

	// Transient marks output that should render in a dismissable overlay panel
	// (P33.11) instead of being appended to the transcript — set by the
	// dispatcher from the command's commandDef.transient flag, so a session of
	// informational housekeeping (/status, /help, /memory …) doesn't leave
	// stale blocks behind. Only ever consulted when the result carries plain
	// Output (no Message/picker/sentinel), so a transient command that still
	// needs to open a picker or send a message is unaffected.
	Transient bool

	// TransientTitle is the chip shown at the top of the transient panel — the
	// command name (e.g. "/status"), stamped by the dispatcher alongside
	// Transient so the panel can label itself without the TUI threading the
	// parsed command name into the result message.
	TransientTitle string

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
	client    *client.Client
	sessionID string
	mode      string
	model     string
	// baseModel is the model the TUI was started with (persona/global
	// default at boot) — the same fallback SlashResult.Model's doc describes.
	// d.model tracks "/model"'s per-session override and is kept non-empty by
	// falling back to this, so bare "/model", the picker's "current" marker,
	// and "/model default" all have a real id to show instead of "" once an
	// override has been cleared or a session switch lands on one with none.
	baseModel    string
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
		baseModel: model,
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
		d.builtins[c.name] = func(args []string) SlashResult {
			r := c.handler(d, args)
			// Stamp the transient flag from the single-source table (P33.11) so
			// the TUI routes this command's plain output into a dismissable
			// panel rather than the transcript. A command that opens a picker or
			// sends a message carries those fields regardless; the TUI only
			// honors Transient on plain Output.
			if c.transient {
				r.Transient = true
				r.TransientTitle = "/" + c.name
			}
			return r
		}
	}
	d.builtins["quit"] = d.cmdQuit // bare alias for "exit"; deliberately unlisted
	return d
}

// SetSession points the dispatcher at a different session (used when the TUI
// switches sessions via the picker, forks, or rewinds). model is the newly
// loaded session's own per-session /model override — "" when it has none, the
// same shape /model itself uses — so a switch onto a session with a different
// (or no) override doesn't leave d.model showing the previous session's,
// which is otherwise reported nowhere else once the switch is applied.
func (d *SlashDispatcher) SetSession(id, mode, model string) {
	d.sessionID = id
	d.mode = mode
	if model == "" {
		model = d.baseModel
	}
	d.model = model
}

// EffectiveModel returns the model currently in effect for this session:
// the per-session /model override, or the TUI's boot-time default when none
// is set. Callers that need to reflect the active model in UI state (the
// status bar, sidebar, the /models picker's "current" marker) after a
// session switch should read this rather than caching a copy that a later
// switch or "/model default" can silently invalidate.
func (d *SlashDispatcher) EffectiveModel() string {
	return d.model
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
// to become available in this one. Returns the skill's full body (for the
// caller to prepend to its synthetic message via skillTaskMessage) and a
// warning string to prepend to the command's Output on failure (e.g. daemon
// unreachable); both empty on the no-client unit-test path.
func (d *SlashDispatcher) activateSkill(name string) (body, warn string) {
	if d.client == nil { // unit tests exercise prompt-building without a live daemon
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, err := d.client.ActivateSkill(ctx, d.sessionID, name)
	if err != nil {
		return "", fmt.Sprintf("Warning: couldn't activate the %s skill (%v); asking anyway.\n\n", name, err)
	}
	return body, ""
}

// skillTaskMessage builds the synthetic user message a skill-triggering slash
// command sends. When the skill's body loaded (body != ""), it's prepended in
// a clearly delimited <skill> block so the model has the full top-level
// instructions up front — deterministically, rather than depending on it
// choosing to call the `skill` tool to fetch them, a round-trip small local
// models were observed to skip and then answer as if a stray directory
// listing were the whole task (P36.1). The task line still follows, so the
// model has both the instructions and the specific ask. On a load miss it
// degrades to the task line alone — today's name-only behavior, where the
// plain "Load the … skill" phrasing and the still-registered `skill` tool
// remain the fallback. The `skill` tool stays the path for the progressive
// reference/*.md files the skill loads later; only the initial top-level load
// moves here.
func skillTaskMessage(name, body, task string) string {
	if strings.TrimSpace(body) == "" {
		return task
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The %s skill has been loaded for you. Its full instructions are below — follow them for this task.\n\n", name)
	fmt.Fprintf(&b, "<skill name=%q>\n%s\n</skill>\n\n", name, body)
	b.WriteString(task)
	return b.String()
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

// cmdModels opens the interactive model picker (P16.6). When the daemon's
// configured provider is a local Ollama server, this now lists what is
// actually pulled there — real, loadable tags — instead of the static
// curated catalog. The catalog's local entries are family names ("qwen3",
// "deepseek-r1"), not IDs Ollama can load; picking one would 404 on the next
// turn, and it mixes in Anthropic/OpenAI/Gemini entries a local-only user
// never asked for (P71 follow-up, filed 2026-08-19). Falls back to the
// curated catalog when the provider isn't Ollama, isn't reachable, or has
// nothing pulled yet — the pre-existing behavior, unchanged.
func (d *SlashDispatcher) cmdModels(_ []string) SlashResult {
	if d.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if resp, err := d.client.ListLocalModels(ctx); err == nil && resp.Reachable && len(resp.Models) > 0 {
			return SlashResult{Models: localModelsToCatalog(resp.Models)}
		}
	}
	return SlashResult{Models: modelcatalog.Curated()}
}

// localModelsToCatalog adapts the daemon's live GET /models/local result into
// modelcatalog.Model entries the existing picker already knows how to render.
// A pulled tag commonly carries its serving window in a ":<n>k" suffix (this
// project's own aegis-*:16k/:32k convention) — shown there when present,
// since /api/tags doesn't report context length (only /api/ps does, for a
// model that's currently loaded, which most of the list won't be).
func localModelsToCatalog(models []api.LocalModelSummary) []modelcatalog.Model {
	out := make([]modelcatalog.Model, 0, len(models))
	for _, m := range models {
		ctxLabel := m.Quantization
		if i := strings.LastIndex(m.Name, ":"); i >= 0 {
			if tag := m.Name[i+1:]; tag != "" && tag != "latest" {
				if _, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(tag), "k")); err == nil && strings.HasSuffix(strings.ToLower(tag), "k") {
					ctxLabel = strings.ToUpper(tag)
				}
			}
		}
		var notes strings.Builder
		if m.ParameterSize != "" {
			fmt.Fprintf(&notes, "%s params", m.ParameterSize)
		}
		if m.Family != "" {
			if notes.Len() > 0 {
				notes.WriteString(", ")
			}
			fmt.Fprintf(&notes, "%s family", m.Family)
		}
		if m.SizeBytes > 0 {
			if notes.Len() > 0 {
				notes.WriteString(", ")
			}
			fmt.Fprintf(&notes, "%.1f GiB on disk", float64(m.SizeBytes)/(1<<30))
		}
		out = append(out, modelcatalog.Model{
			Provider: "ollama", ID: m.Name, Tier: modelcatalog.TierLocal,
			Context: ctxLabel, Notes: notes.String(),
		})
	}
	return out
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
		// The daemon-side override is gone, but the TUI still needs something
		// concrete to show (status bar, sidebar, the /models picker's "current"
		// marker) — fall back to the boot-time default rather than leaving
		// d.model empty, which used to make "/model" print a blank current
		// model and left every display showing whatever was picked before.
		d.model = d.baseModel
		return SlashResult{
			Output: "Cleared the session model override; reverts to the persona/global default on the next turn.",
			Model:  &d.baseModel,
		}
	}
	d.model = newModel
	return SlashResult{
		Output: fmt.Sprintf("Switched to model %q for this session. This must be a model id belonging to your currently configured provider (see /status) — a cross-provider id will fail on the next turn, not now.", newModel),
		Model:  &newModel,
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
		switch info.ContextWindowSource {
		case "ollama:default":
			b.WriteString("  ⚠ Ollama is serving its default context; raise OLLAMA_CONTEXT_LENGTH (or a modelfile num_ctx) for long agent tasks\n")
		case "ollama:compat-default":
			b.WriteString("  ⚠ the /v1 compat path never sends context_window, so Ollama is serving its default; switch to provider.default: ollama, or raise OLLAMA_CONTEXT_LENGTH (see `aegis doctor`)\n")
		}
	}

	// P81.22/FIND-22: info.SandboxBackend is the daemon-authoritative
	// *effective* backend (sandbox.Backend.Name()) — what will actually
	// contain the next command — not the configured value, which can differ
	// silently (an unavailable runtime, a fallback). Prefer it; fall back to
	// the local config's configured value only when talking to an older
	// daemon that predates this field.
	backend := info.SandboxBackend
	if backend == "" {
		backend = "local"
		if cfgErr == nil {
			if cfg.Sandbox.Backend != "" {
				backend = cfg.Sandbox.Backend
			}
			if cfg.Sandbox.Runtime != "" {
				backend = fmt.Sprintf("%s (runtime: %s)", backend, cfg.Sandbox.Runtime)
			}
		}
	}
	fmt.Fprintf(&b, "Sandbox: %s\n", backend)
	if backend == "local" {
		b.WriteString("  ⚠ commands run unconfined on the host (no container or OS-level isolation active)\n")
	}
	if info.SandboxFallback {
		fmt.Fprintf(&b, "  ⚠ fell back from the configured backend: %s\n", info.SandboxFallbackReason)
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
	case "ollama:compat-default":
		return "Ollama server default — the /v1 compat path cannot send context_window"
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
	body, warn := d.activateSkill(skill)
	return SlashResult{Output: warn, Message: skillTaskMessage(skill, body, prompt.String())}
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
	body, warn := d.activateSkill("deep-research")
	return SlashResult{Output: warn, Message: skillTaskMessage("deep-research", body, prompt)}
}

// cmdDocument sends a message that directly invokes the document-codebase
// skill, so writing or updating in-repo documentation is a discoverable entry
// point instead of relying on the model noticing a trigger phrase in free
// text — the same rationale as cmdReport and cmdResearch. Distinct from
// /report on purpose: that consolidates sources into a standalone deliverable,
// this maintains a file that lives next to the code.
func (d *SlashDispatcher) cmdDocument(args []string) SlashResult {
	target := strings.TrimSpace(strings.Join(args, " "))
	prompt := "Load the document-codebase skill and write or update documentation in this repository"
	if target != "" {
		prompt += " for: " + target
	} else {
		prompt += ". Ask me what to document and which document type fits before writing anything."
	}
	prompt += " Follow the skill: settle the audience and document type, check what already exists, ground every claim in code you actually read, write incrementally one section per edit, and verify paths and commands before delivering."
	body, warn := d.activateSkill("document-codebase")
	return SlashResult{Output: warn, Message: skillTaskMessage("document-codebase", body, prompt)}
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
	data, redactions, err := share.Render(sess, format)
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
	// P66.11: the count is stated at the moment the user decides whether to send
	// the file, not only inside it. A zero is reported too — "nothing matched" and
	// "the filter never ran" have to look different from here.
	return SlashResult{Output: fmt.Sprintf("Exported session → %s (%d credential-shaped value(s) redacted)", path, redactions)}
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
