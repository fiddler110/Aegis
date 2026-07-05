package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
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
}

// SlashDispatcher dispatches slash commands to built-in handlers or custom
// command templates.
type SlashDispatcher struct {
	client       *client.Client
	sessionID    string
	mode         string
	model        string
	guardEnabled *bool // per-session output-guard toggle; nil = server default
	builtins     map[string]func(args []string) SlashResult
	customs      []api.CommandInfo
}

// NewSlashDispatcher creates a dispatcher for the given session.
func NewSlashDispatcher(cl *client.Client, sessionID, mode, model string) *SlashDispatcher {
	d := &SlashDispatcher{
		client:    cl,
		sessionID: sessionID,
		mode:      mode,
		model:     model,
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

func (d *SlashDispatcher) cmdModels(_ []string) SlashResult {
	return SlashResult{Output: fmt.Sprintf("Model: %s\nMode: %s", d.model, d.mode)}
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

// cmdSecurityConfig opens the interactive security-scanner config dialog
// (P11.11) — lets the user toggle enabled/method/install/image per scanner
// without hand-editing security.tools in config.yaml. Like /sandbox use and
// /skills enable, it defaults to the project config; pass "global" to edit
// the user-level config instead.
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

// cmdScan runs the security scanners directly against the daemon's workspace
// and prints the formatted report — no model turn spent, same underlying scan
// as `aegis scan`/the security_scan tool. Usage:
//
//	/scan                run every enabled scanner over the whole workspace
//	/scan <path>          scan just a workspace-relative subdirectory
//	/scan image <ref>      scan a container image reference instead
//	/scan sbom [path]      generate a CycloneDX SBOM instead of a findings report
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
	case len(args) >= 1:
		req.Path = args[0]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	resp, err := d.client.Scan(ctx, req)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Scan failed: %v", err), IsError: true}
	}
	return SlashResult{Output: resp.Report}
}

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
//	/debate <claim text>   debate the given claim with the default roles
//
// Unlike /scan, a debate spends real model turns (one per role per round), so
// it uses the same long timeout /scan uses rather than the 5s default other
// direct-daemon-call commands use.
func (d *SlashDispatcher) cmdDebate(args []string) SlashResult {
	claim := strings.TrimSpace(strings.Join(args, " "))
	if claim == "" {
		return SlashResult{Output: "usage: /debate <claim>", IsError: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	resp, err := d.client.Debate(ctx, api.DebateRequest{Claim: claim})
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("Debate failed: %v", err), IsError: true}
	}
	return SlashResult{Output: resp.Report}
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

// cmdArchive archives or unarchives the current session. Archived sessions are
// hidden from normal listings but their data is preserved. Use /archive off to
// restore. To permanently remove a session, use `aegis sessions delete <id>`.
func (d *SlashDispatcher) cmdArchive(args []string) SlashResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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

// cmdCopy copies the last assistant message (or Nth code block) to the clipboard.
func (d *SlashDispatcher) cmdCopy(args []string) SlashResult {
	if len(args) > 0 {
		return SlashResult{Output: "\x00copy " + args[0]}
	}
	return SlashResult{Output: "\x00copy"}
}

func (d *SlashDispatcher) cmdQuit(_ []string) SlashResult {
	return SlashResult{Quit: true}
}
