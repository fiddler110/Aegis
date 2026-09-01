package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/reqorigin"
	"github.com/fiddler110/aegis/internal/security"
)

// ─── Messages ─────────────────────────────────────────────────────────────────

type securityConfigResolvedMsg struct{ statuses map[string]string }
type securityConfigSavedMsg struct{ err error }
type securityInstallDoneMsg struct {
	name   string
	output string
	err    error
}

// ─── Phases ───────────────────────────────────────────────────────────────────

type securityConfigPhase int

const (
	scPhaseLoading        securityConfigPhase = iota // async: resolve each scanner's live availability
	scPhaseList                                      // huh form: pick a tool to edit, or save/cancel
	scPhaseAction                                    // huh form: a tool was picked — edit settings, install, or back
	scPhaseEdit                                      // huh form: edit one tool's settings
	scPhaseInstallConfirm                            // huh form: confirm the exact guided-install command
	scPhaseInstalling                                // async: run the guided install
	scPhaseSaving                                    // async: write config
)

// ─── Model ────────────────────────────────────────────────────────────────────

const securityConfigPanelW = 76

// securityConfigModel is the `/security-config` interactive dialog (P11.11):
// lets the user toggle enabled/method/install/image per scanner without
// hand-editing YAML, then writes the result via config.PatchProjectSecurity/
// PatchGlobalSecurity — the same splice-preserving mechanism every other
// config-writing command (/sandbox use, /skills enable) already uses.
type securityConfigModel struct {
	phase securityConfigPhase
	form  *huh.Form
	sp    spinner.Model

	global bool // true = write ~/.config/aegis/config.yaml, false = project .aegis/config.yaml

	// original contextual-security fields, carried through unchanged on save
	// (patchSecurity replaces the whole security: block, so these must not be
	// silently dropped just because this dialog only edits scanner config).
	egressThenWrite  bool
	networkAllowList []string
	dast             config.DASTConfig
	wslDistro        string
	debate           config.DebateIntegrationConfig
	multiscanner     config.MultiscannerConfig
	// netscanner is carried, never edited here, for the same reason
	// multiscanner is: saving replaces the whole security: block, so a pin this
	// form never shows would be deleted by the save.
	netscanner config.NetscannerConfig

	defaultMethod string
	tools         map[string]config.SecurityToolConfig // working copy, mutated as the user edits
	statuses      map[string]string                    // tool name -> live resolved status, computed once at open

	selected string // list-phase selection: a tool name, or "__save__"/"__cancel__"

	editingName string
	editEnabled bool
	editMethod  string
	editInstall string
	editImage   string
	editVerify  bool // trufflehog only (P13.2): live credential verification, off by default

	action           string // action-phase selection: "edit", "install", or "back"
	installCmd       string // guided-install command shown for confirmation
	installConfirmed bool
	notice           string // one-line result banner shown on the list after an install attempt

	done    bool
	saved   bool
	saveErr string

	width, height int
	th            theme
}

func newSecurityConfigModel(width, height int, th theme, global bool) *securityConfigModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)

	var sec config.SecurityConfig
	if cfg, err := config.Load(); err == nil {
		sec = cfg.Security
	}
	tools := make(map[string]config.SecurityToolConfig, len(sec.Tools))
	for name, tc := range sec.Tools {
		tools[name] = tc
	}

	return &securityConfigModel{
		width:            width,
		height:           height,
		th:               th,
		sp:               sp,
		global:           global,
		egressThenWrite:  sec.EgressThenWrite,
		networkAllowList: sec.NetworkAllowList,
		dast:             sec.DAST,
		wslDistro:        sec.WSLDistro,
		debate:           sec.Debate,
		multiscanner:     sec.Multiscanner,
		netscanner:       sec.Netscanner,
		defaultMethod:    strOrDefault(sec.DefaultMethod, "auto"),
		tools:            tools,
		statuses:         map[string]string{},
	}
}

func (m *securityConfigModel) init() tea.Cmd {
	return tea.Batch(m.sp.Tick, m.resolveCmd())
}

func strOrDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// resolveCmd probes live availability (host binary / container runtime) for
// every built-in scanner under the config as it stood when the dialog
// opened. Runs once, off the main loop (container-runtime probing can take
// a couple of seconds), rather than on every edit — the user is choosing
// policy here, not watching a live scan.
func (m *securityConfigModel) resolveCmd() tea.Cmd {
	opts := security.OptionsFromConfig(config.SecurityConfig{
		Tools:         m.tools,
		DefaultMethod: m.defaultMethod,
	})
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		statuses := make(map[string]string)
		for _, d := range security.Descriptors() {
			r := security.ResolveDetailed(ctx, d.Name, opts)
			switch r.Method {
			case security.MethodHost:
				statuses[d.Name] = "on PATH"
				// The one site that can't take the collapsed advisory the CLI
				// and /security status print: this status is a single cell in
				// a one-line-per-tool picker, so it gets the shortest true
				// statement instead — enough to notice the row isn't what the
				// operator configured, with the full reason a `/security
				// status` away.
				if r.FallbackWhy != "" {
					statuses[d.Name] = "on PATH (multiscanner container unavailable)"
				}
			case security.MethodContainer:
				statuses[d.Name] = "container (" + string(r.Runtime) + ")"
			case security.MethodWSL:
				statuses[d.Name] = "via WSL"
			default:
				statuses[d.Name] = "unavailable: " + r.Reason
				if note := security.AvailabilityNote(d.Name, r.Reason); note != "" {
					statuses[d.Name] += "; " + note
				}
			}
		}
		return securityConfigResolvedMsg{statuses: statuses}
	}
}

// ─── Form builders ────────────────────────────────────────────────────────────

func (m *securityConfigModel) buildListForm() *huh.Form {
	descs := security.Descriptors()
	opts := make([]huh.Option[string], 0, len(descs)+2)
	for _, d := range descs {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-9s %s  —  %s", d.Name, m.toolBadge(d.Name), m.statuses[d.Name]), d.Name))
	}
	opts = append(opts, huh.NewOption("─── Save & exit ───", "__save__"))
	opts = append(opts, huh.NewOption("─── Cancel (discard changes) ───", "__cancel__"))

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Security scanner configuration (%s config)", m.scopeLabel())).
				Description("Select a tool to edit its enabled/method/install/image settings.").
				Options(opts...).
				Value(&m.selected).
				Height(len(opts) + 2),
		),
	).WithWidth(securityConfigPanelW - 8).WithTheme(aegisHuhTheme())
}

// toolBadge summarizes a tool's pending (in-memory, possibly-edited) settings
// for the list view: [on|off] + its effective method.
func (m *securityConfigModel) toolBadge(name string) string {
	state, method := "on", m.defaultMethod
	if tc, ok := m.tools[name]; ok {
		if !tc.ToolEnabled() {
			state = "off"
		}
		if tc.Method != "" {
			method = tc.Method
		}
		if name == "trufflehog" && tc.Verify {
			return fmt.Sprintf("[%3s %-9s verify:ON]", state, method)
		}
	}
	return fmt.Sprintf("[%3s %-9s]", state, method)
}

func (m *securityConfigModel) buildEditForm() *huh.Form {
	d, _ := security.DescriptorFor(m.editingName)

	methodOpts := []huh.Option[string]{
		huh.NewOption("Auto (host binary if present, else container)", "auto"),
		huh.NewOption("Host only (never fall back to a container)", "host"),
		huh.NewOption("Container only", "container"),
	}
	installOpts := []huh.Option[string]{
		huh.NewOption("Prompt before installing (default)", "prompt"),
		huh.NewOption("Always (pre-authorize `aegis security install`)", "always"),
		huh.NewOption("Never (use only if already present)", "never"),
	}

	fields := []huh.Field{
		huh.NewNote().
			Title(m.editingName).
			Description(d.Summary),
		huh.NewConfirm().
			Title("Enabled").
			Affirmative("Yes").
			Negative("No").
			Value(&m.editEnabled),
		huh.NewSelect[string]().
			Title("Run method").
			Options(methodOpts...).
			Value(&m.editMethod).
			Height(5),
		huh.NewSelect[string]().
			Title("Install policy").
			Description("Only affects `aegis security install " + m.editingName + "`.").
			Options(installOpts...).
			Value(&m.editInstall).
			Height(5),
		huh.NewInput().
			Title("Container image (digest-pinned)").
			Description("Required to enable container fallback, e.g. name@sha256:... — leave empty to disable it. Aegis ships no built-in pin; see docs/security.md.").
			Placeholder("(none)").
			Value(&m.editImage),
	}
	// trufflehog-only (P13.2): live credential verification is a distinct,
	// explicitly warning-labelled opt-in — it makes real calls to third-party
	// provider APIs using the actual discovered secret, and forces host-only
	// execution (Resolve refuses container mode when this is set).
	if m.editingName == "trufflehog" {
		fields = append(fields, huh.NewConfirm().
			Title("⚠ Verify (live credential check)").
			Description("Confirms each detected secret against the real provider API (AWS/GitHub/etc.) using the actual discovered credential — a real outbound call, not a local check. Forces host-only execution (no container fallback). Off by default.").
			Affirmative("Yes, enable verification").
			Negative("No").
			Value(&m.editVerify))
	}

	return huh.NewForm(
		huh.NewGroup(fields...),
	).WithWidth(securityConfigPanelW - 8).WithTheme(aegisHuhTheme())
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m *securityConfigModel) update(msg tea.Msg) tea.Cmd {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+c" {
		m.done = true
		return nil
	}

	switch m.phase {
	case scPhaseLoading:
		return m.updateLoading(msg)
	case scPhaseList:
		return m.updateList(msg)
	case scPhaseAction:
		return m.updateAction(msg)
	case scPhaseEdit:
		return m.updateEdit(msg)
	case scPhaseInstallConfirm:
		return m.updateInstallConfirm(msg)
	case scPhaseInstalling:
		return m.updateInstalling(msg)
	case scPhaseSaving:
		return m.updateSaving(msg)
	}
	return nil
}

func (m *securityConfigModel) updateLoading(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return cmd
	case securityConfigResolvedMsg:
		m.statuses = msg.statuses
		m.phase = scPhaseList
		m.form = m.buildListForm()
		return m.form.Init()
	}
	return nil
}

func (m *securityConfigModel) updateList(msg tea.Msg) tea.Cmd {
	f, cmd := m.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		m.form = ff
	}
	switch m.form.State {
	case huh.StateAborted:
		m.done = true
	case huh.StateCompleted:
		switch m.selected {
		case "__cancel__":
			m.done = true
		case "__save__":
			m.phase = scPhaseSaving
			return tea.Batch(m.sp.Tick, m.saveCmd())
		default:
			m.notice = ""
			m.editingName = m.selected
			m.phase = scPhaseAction
			m.form = m.buildActionForm()
			return m.form.Init()
		}
	}
	return cmd
}

// buildActionForm lets the user choose what to do with the tool picked from
// the list: edit its config, run its guided install (only offered when one
// exists for the current OS), or go back without changing anything.
func (m *securityConfigModel) buildActionForm() *huh.Form {
	opts := []huh.Option[string]{
		huh.NewOption("Edit settings (enabled / method / install policy / image)", "edit"),
	}
	if _, ok := security.InstallCommand(m.editingName); ok {
		opts = append(opts, huh.NewOption("Install now (guided)", "install"))
	}
	opts = append(opts, huh.NewOption("← Back to list", "back"))

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(m.editingName).
				Description(m.statuses[m.editingName]).
				Options(opts...).
				Value(&m.action).
				Height(len(opts) + 2),
		),
	).WithWidth(securityConfigPanelW - 8).WithTheme(aegisHuhTheme())
}

func (m *securityConfigModel) updateAction(msg tea.Msg) tea.Cmd {
	f, cmd := m.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		m.form = ff
	}
	switch m.form.State {
	case huh.StateAborted:
		m.backToList()
		return m.form.Init()
	case huh.StateCompleted:
		switch m.action {
		case "edit":
			m.startEdit(m.editingName)
			return m.form.Init()
		case "install":
			cmdStr, _ := security.InstallCommand(m.editingName)
			m.installCmd = cmdStr
			m.installConfirmed = false
			m.phase = scPhaseInstallConfirm
			m.form = m.buildInstallConfirmForm()
			return m.form.Init()
		default: // "back"
			m.backToList()
			return m.form.Init()
		}
	}
	return cmd
}

// buildInstallConfirmForm shows the exact host command before running it —
// installing software is a privileged, host-modifying action that must never
// happen without the operator seeing what will run first (same posture as
// `aegis security install`).
func (m *securityConfigModel) buildInstallConfirmForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Install "+m.editingName).
				Description("This will run the following command on your host:\n\n    "+m.installCmd),
			huh.NewConfirm().
				Title("Proceed?").
				Affirmative("Install").
				Negative("Cancel").
				Value(&m.installConfirmed),
		),
	).WithWidth(securityConfigPanelW - 8).WithTheme(aegisHuhTheme())
}

func (m *securityConfigModel) updateInstallConfirm(msg tea.Msg) tea.Cmd {
	f, cmd := m.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		m.form = ff
	}
	switch m.form.State {
	case huh.StateAborted:
		m.backToList()
		return m.form.Init()
	case huh.StateCompleted:
		if !m.installConfirmed {
			m.backToList()
			return m.form.Init()
		}
		m.phase = scPhaseInstalling
		return tea.Batch(m.sp.Tick, m.installCmdFunc())
	}
	return cmd
}

// installCmdFunc runs the confirmed guided install off the main loop —
// package managers/build steps can take a while — and reports the combined
// output back as a message.
func (m *securityConfigModel) installCmdFunc() tea.Cmd {
	name := m.editingName
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var buf strings.Builder
		err := security.RunGuidedInstall(ctx, name, &buf)
		return securityInstallDoneMsg{name: name, output: buf.String(), err: err}
	}
}

func (m *securityConfigModel) updateInstalling(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return cmd
	case securityInstallDoneMsg:
		if msg.err != nil {
			m.notice = "✗ install failed for " + msg.name + ": " + msg.err.Error()
		} else {
			m.notice = "✓ " + msg.name + " installed."
		}
		// Re-resolve every tool's live status so the list badge reflects the
		// newly-installed binary instead of the stale "unavailable" reason.
		m.phase = scPhaseLoading
		return tea.Batch(m.sp.Tick, m.resolveCmd())
	}
	return nil
}

func (m *securityConfigModel) startEdit(name string) {
	m.editingName = name
	tc := m.tools[name] // zero value if not yet configured
	m.editEnabled = tc.ToolEnabled()
	m.editMethod = strOrDefault(tc.Method, "auto")
	m.editInstall = strOrDefault(tc.Install, "prompt")
	m.editImage = tc.Image
	m.editVerify = tc.Verify
	m.phase = scPhaseEdit
	m.form = m.buildEditForm()
}

func (m *securityConfigModel) updateEdit(msg tea.Msg) tea.Cmd {
	f, cmd := m.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		m.form = ff
	}
	switch m.form.State {
	case huh.StateAborted:
		m.backToList() // discard this tool's in-progress edits
		return m.form.Init()
	case huh.StateCompleted:
		m.applyEdit()
		m.backToList()
		return m.form.Init()
	}
	return cmd
}

func (m *securityConfigModel) applyEdit() {
	enabled := m.editEnabled
	tc := config.SecurityToolConfig{
		Enabled: &enabled,
		Method:  m.editMethod,
		Install: m.editInstall,
		Image:   strings.TrimSpace(m.editImage),
	}
	if m.editingName == "trufflehog" {
		tc.Verify = m.editVerify
	}
	m.tools[m.editingName] = tc
}

func (m *securityConfigModel) backToList() {
	m.editingName = ""
	m.selected = ""
	m.phase = scPhaseList
	m.form = m.buildListForm()
}

func (m *securityConfigModel) saveCmd() tea.Cmd {
	patch := config.SecurityPatch{
		EgressThenWrite:  m.egressThenWrite,
		NetworkAllowList: m.networkAllowList,
		DefaultMethod:    m.defaultMethod,
		Tools:            m.tools,
		DAST:             m.dast,
		WSLDistro:        m.wslDistro,
		Debate:           m.debate,
		Multiscanner:     m.multiscanner,
		Netscanner:       m.netscanner,
	}
	write := func(p config.SecurityPatch) error {
		return config.PatchProjectSecurityWithOrigin(p, reqorigin.TUI)
	}
	if m.global {
		write = config.PatchGlobalSecurity
	}
	return func() tea.Msg {
		return securityConfigSavedMsg{err: write(patch)}
	}
}

func (m *securityConfigModel) updateSaving(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return cmd
	case securityConfigSavedMsg:
		if msg.err != nil {
			m.saveErr = msg.err.Error()
		} else {
			m.saved = true
		}
		m.done = true
	}
	return nil
}

func (m *securityConfigModel) scopeLabel() string {
	if m.global {
		return "global"
	}
	return "project"
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m *securityConfigModel) view() string {
	header := renderBrandMark() + " " +
		m.th.statusDim.Render("Security Scanner Configuration") + "\n\n"

	var body string
	switch {
	case m.saveErr != "":
		body = m.th.errLine.Render("Failed to save:") + "\n" +
			m.th.statusDim.Render("  "+m.saveErr)
	case m.phase == scPhaseLoading:
		body = m.sp.View() + " Checking scanner availability…"
	case m.phase == scPhaseInstalling:
		body = m.sp.View() + " Installing " + m.editingName + "…"
	case m.phase == scPhaseSaving:
		body = m.sp.View() + " Saving configuration…"
	default:
		body = m.form.View()
		if m.phase == scPhaseList && m.notice != "" {
			style := m.th.statusDim
			if strings.HasPrefix(m.notice, "✗") {
				style = m.th.errLine
			}
			body = style.Render(m.notice) + "\n\n" + body
		}
	}

	return fixedPanelFrame(header+body, securityConfigPanelW)
}
