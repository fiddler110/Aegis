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
	"github.com/fiddler110/aegis/internal/security"
)

// ─── Messages ─────────────────────────────────────────────────────────────────

type securityConfigResolvedMsg struct{ statuses map[string]string }
type securityConfigSavedMsg struct{ err error }

// ─── Phases ───────────────────────────────────────────────────────────────────

type securityConfigPhase int

const (
	scPhaseLoading securityConfigPhase = iota // async: resolve each scanner's live availability
	scPhaseList                               // huh form: pick a tool to edit, or save/cancel
	scPhaseEdit                               // huh form: edit one tool's settings
	scPhaseSaving                             // async: write config
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

	defaultMethod string
	tools         map[string]config.SecurityToolConfig // working copy, mutated as the user edits
	statuses      map[string]string                    // tool name -> live resolved status, computed once at open

	selected string // list-phase selection: a tool name, or "__save__"/"__cancel__"

	editingName string
	editEnabled bool
	editMethod  string
	editInstall string
	editImage   string

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
			method, rt, _, reason := security.Resolve(ctx, d.Name, opts)
			switch method {
			case security.MethodHost:
				statuses[d.Name] = "on PATH"
			case security.MethodContainer:
				statuses[d.Name] = "container (" + string(rt) + ")"
			default:
				statuses[d.Name] = "unavailable: " + reason
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

	return huh.NewForm(
		huh.NewGroup(
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
				Description("Only affects `aegis security install "+m.editingName+"`.").
				Options(installOpts...).
				Value(&m.editInstall).
				Height(5),
			huh.NewInput().
				Title("Container image (digest-pinned)").
				Description("Required to enable container fallback, e.g. name@sha256:... — leave empty to disable it. Aegis ships no built-in pin; see docs/security.md.").
				Placeholder("(none)").
				Value(&m.editImage),
		),
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
	case scPhaseEdit:
		return m.updateEdit(msg)
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
			m.startEdit(m.selected)
			return m.form.Init()
		}
	}
	return cmd
}

func (m *securityConfigModel) startEdit(name string) {
	m.editingName = name
	tc := m.tools[name] // zero value if not yet configured
	m.editEnabled = tc.ToolEnabled()
	m.editMethod = strOrDefault(tc.Method, "auto")
	m.editInstall = strOrDefault(tc.Install, "prompt")
	m.editImage = tc.Image
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
	m.tools[m.editingName] = config.SecurityToolConfig{
		Enabled: &enabled,
		Method:  m.editMethod,
		Install: m.editInstall,
		Image:   strings.TrimSpace(m.editImage),
	}
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
	}
	write := config.PatchProjectSecurity
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
	case m.phase == scPhaseSaving:
		body = m.sp.View() + " Saving configuration…"
	default:
		body = m.form.View()
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 3).
		Width(securityConfigPanelW).
		Render(header + body)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}
