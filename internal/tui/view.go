package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/tui/notify"
)

// --- view ---

// View wraps the rendered content in a tea.View, setting the v2 terminal modes
// (alt-screen, mouse, background) that were previously program options.
//
// P22.6: rawScrollback drops both alt-screen and mouse capture. Alt-screen
// alone isn't the blocker for native terminal scrollback — bubbletea's
// non-alt-screen renderer already resizes its frame to the content height and
// lets genuinely new lines scroll through the terminal's own history (see
// cursed_renderer.go's flush: "the frame height can change based on the
// content... different from the alt screen buffer, which has a fixed
// height"). What actually defeats it in this app's normal mode is that the
// transcript is a bounded, in-place-redrawn viewport (transcriptPane) that
// clips to a fixed visible window regardless of alt-screen — see
// applyViewportHeight's rawScrollback branch, which is the other half of this
// mode: it renders every segment unclipped so the frame truly grows. Mouse
// capture is released too, since MouseModeCellMotion alone is enough to stop
// a terminal emulator from offering its own click-drag text selection.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = !m.rawScrollback
	v.BackgroundColor = colSurface
	if m.rawScrollback {
		v.MouseMode = tea.MouseModeNone
	} else {
		v.MouseMode = tea.MouseModeCellMotion
	}
	v.WindowTitle = m.windowTitle() // P16.1: OSC 0/2, reflects session state
	v.ReportFocus = true            // P16.1: enables tea.FocusMsg/BlurMsg
	return v
}

// windowTitle reflects session state in the terminal title (P16.1) so a
// tabbed-away user can tell streaming/ready/awaiting-approval apart from the
// tab/window list alone.
func (m model) windowTitle() string {
	switch {
	case m.approval != nil:
		return "Aegis — approval needed"
	case m.streaming:
		return "Aegis — working…"
	default:
		return "Aegis — ready"
	}
}

// notifyCmd returns a tea.Cmd that fires the P16.1 attention system for ev —
// bell and/or OS desktop notification per m.notifyMode — or nil if the
// terminal is known to be focused or the mode has nothing to send.
func (m model) notifyCmd(ev notify.Event) tea.Cmd {
	if m.focused {
		return nil
	}
	seq := notify.Sequence(m.notifyMode, ev)
	if seq == "" {
		return nil
	}
	return tea.Raw(seq)
}

// render dispatches to whichever overlay is active. The wizard and
// security-config dialogs (P33.12) used to replace the frame outright; they
// now composite over the live chat view via renderOverlay (P16.6), same as
// the filterable-list dialogs, help, and quit-confirm, so closing them
// doesn't lose your place — the transcript keeps running underneath a
// long multi-step form exactly as it does behind the approval dialog.
func (m model) render() string {
	if !m.ready {
		return "initializing…"
	}

	base := m.renderChat()
	if m.wizard != nil {
		return renderOverlay(base, m.wizard.view(), m.width, m.height)
	}
	if m.securityConfig != nil {
		return renderOverlay(base, m.securityConfig.view(), m.width, m.height)
	}

	if m.completion.active {
		// P33.18: the completion popup used to insert into the vertical layout
		// and shrink the transcript pane by its own height, the same reflow
		// jump P33.6 fixed for the approval dialog. Unlike that dialog it is
		// non-modal and anchored (the user is still typing behind it, not
		// looking at a centered form), so it composites via
		// renderAnchoredOverlay — no centering, no dimming — positioned just
		// above the composer instead of the screen center.
		popup, x, y := m.renderCompletionPopup()
		base = renderAnchoredOverlay(base, popup, x, y, m.width, m.height)
	}
	if m.approval != nil {
		// P33.6: the approval prompt used to sit between transcript and input,
		// shrinking the pane by its own height every time the engine asked —
		// the loudest layout jump in the normal flow. Compositing it leaves the
		// transcript's geometry alone; modality is unchanged, since the
		// composer was already blurred while one is pending (P25.4a).
		base = renderOverlay(base, m.renderApprovalDialog(), m.width, m.height)
	}
	switch {
	case m.helpOpen:
		return renderOverlay(base, renderHelpBox(m.keys), m.width, m.height)
	case m.quitConfirm:
		return renderOverlay(base, renderQuitConfirmBox(), m.width, m.height)
	case m.dialog != nil:
		return renderOverlay(base, m.dialog.View(), m.width, m.height)
	case m.transientPanel != nil:
		// P33.11: the informational panel composites over the live chat (dimmed
		// behind it) rather than replacing the frame, so dismissing it drops the
		// user straight back where they were.
		return renderOverlay(base, m.transientPanel.View(), m.width, m.height)
	}
	return base
}

// renderChat renders the normal chat frame: title bar, transcript/sidebar/
// terminal pane, todo strip, and input area. Split out of render() so overlay
// dialogs — and, since P33.18, the completion popup — composite over it
// instead of being laid out inline.
func (m model) renderChat() string {
	titleBar := m.renderTitleBar()
	inputArea := m.renderInputArea()

	var content string
	if m.rawScrollback {
		// P22.6: no scrollbar column (nothing to indicate — the terminal owns
		// scroll position) and no sidebar/terminal pane (both assume a
		// fixed-height dashboard next to a bounded transcript; here the
		// transcript's own height is unbounded and grows with content, so
		// joining a fixed-height column beside it would either misalign or,
		// for the sidebar's Height()-padded block, emit thousands of blank
		// lines). Plain sequential text gets the full body width instead.
		content = lipgloss.NewStyle().PaddingLeft(1).Render(m.renderTranscriptContent())
	} else {
		main := lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().PaddingLeft(1).Render(m.renderTranscriptContent()),
			m.renderScrollbar(),
		)
		if m.sidebarOpen && m.width >= sidebarMinTermW {
			sidebar := m.renderSidebar(m.transcript.Height())
			if m.termOpen {
				content = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main, m.term.view(m.th, m.termFocused, m.keys.Diagnose.Help().Key))
			} else {
				content = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
			}
		} else {
			if m.termOpen {
				content = lipgloss.JoinHorizontal(lipgloss.Top, main, m.term.view(m.th, m.termFocused, m.keys.Diagnose.Help().Key))
			} else {
				content = main
			}
		}
	}

	parts := []string{titleBar, content}
	if len(m.todoItems) > 0 {
		parts = append(parts, m.renderTodoStrip())
	}
	parts = append(parts, inputArea)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderCompletionPopup renders the inline completion popup and the (x, y)
// screen position render() should composite it at (P33.18): left-aligned,
// bottom-anchored just above the composer — above the todo strip too, when
// one is showing, matching the popup's old position in the vertical layout
// before it moved to compositing. Only meaningful when m.completion.active;
// callers must check that first.
func (m model) renderCompletionPopup() (popup string, x, y int) {
	popupW := min(m.width-2, 72)
	popup = lipgloss.NewStyle().PaddingLeft(1).Render(m.completion.view(popupW))

	inputAreaH := m.ta.Height() + 2 + 1 // border(2) + belowBar(1), mirrors fixedH()
	todoH := 0
	if len(m.todoItems) > 0 {
		todoH = 1
	}
	y = m.height - inputAreaH - todoH - lipgloss.Height(popup)
	return popup, 0, y
}

func (m model) renderTitleBar() string {
	brand := renderBrandMark()
	brandW := lipgloss.Width(brand)

	// P16.5: the scroll-position indicator moved to a rendered scrollbar
	// glyph column beside the transcript (see renderScrollbar) — this bar no
	// longer carries scroll state.
	rightW := max(m.width-brandW, 0)
	// P28.7: a colored dot (+ latency once measured) ahead of the model name
	// gives an always-visible, glance-only connection/model-health signal —
	// no /status command or wasted "is the model connected" prompt needed.
	right := lipgloss.NewStyle().
		Background(colSurface).
		Foreground(colTextMuted).
		Width(rightW).
		Align(lipgloss.Right).
		Render(m.renderConnBadge(colSurface) + " " + m.cfg.Model + " ")

	return brand + right
}

func (m model) renderSidebar(h int) string {
	var b strings.Builder
	w := sidebarInnerW - 2 // usable text width (inner - left padding)

	add := func(s string) { b.WriteString(s + "\n") }
	// Section headers carry a small diamond marker (Crush-style) so the panel
	// reads as a set of labelled groups rather than a flat column of words.
	section := func(title string) {
		add(m.th.sideSection.Render("◇ " + title))
	}

	add("")
	section("SESSION")
	add(m.th.sideValue.Render(short(m.cfg.SessionID)))
	add("")

	section("MODE")
	add(m.renderModeBadge())
	add("")

	section("MODEL")
	add(m.th.sideMuted.Render(truncate(m.cfg.Model, w)))
	add(m.renderConnDetail()) // P28.7: reachable/unreachable + latency at a glance
	add("")

	if m.streaming && !m.streamStart.IsZero() {
		if m.firstTokenAt.IsZero() {
			section("WAITING")
		} else {
			section("GENERATING")
		}
		secs := int(time.Since(m.streamStart).Seconds())
		add(m.th.elapsedDim.Render(fmt.Sprintf("%ds elapsed", secs)))
		add("")
	}

	if len(m.tools) > 0 {
		section("TOOLS")
		for _, t := range m.tools {
			tag, style := "●", m.th.tool
			switch t.status {
			case "ok":
				tag, style = "✓", m.th.sideValue
			case "err":
				tag, style = "×", m.th.toolErr
			}
			add(style.Render(tag + " " + truncate(t.name, w-2)))
		}
		add("")
	}

	// P2.4: show files edited this session.
	if len(m.changedFiles) > 0 {
		section("FILES")
		for _, f := range m.changedFiles {
			add(m.th.sideValue.Render("✎ " + truncate(filepath.Base(f), w-2)))
		}
		add("")
	}

	// P2.5: show running sub-agents.
	var runningAgents []api.Teammate
	for _, tm := range m.teammates {
		if tm.Status == "running" {
			runningAgents = append(runningAgents, tm)
		}
	}
	if len(runningAgents) > 0 {
		section("AGENTS")
		for _, tm := range runningAgents {
			id := tm.AgentID
			if len(id) > 8 {
				id = id[:8]
			}
			label := "⚇ " + id
			if tm.Summary != "" {
				label += ": " + oneLine(tm.Summary)
			}
			add(m.th.tool.Render(truncate(label, w)))
		}
		add("")
	}

	// promptTokens approximates the last-turn prompt size: uncached input plus
	// any cache reads/writes (Anthropic reports these separately). P35.10
	// caveat: on the native-Ollama path m.inputTokens is uncached prefill, not
	// full prompt size, so on a KV-cache-hit turn (P35.4/P35.7) this meter
	// understates how full the context window is. Truthful for the work done,
	// but not a reliable fullness gauge there; a correct fix would need an
	// estimated-context number the daemon does not currently surface to the UI.
	promptTokens := m.inputTokens + m.cacheReadTokens + m.cacheCreationTokens
	if promptTokens > 0 {
		section("CONTEXT")
		add(renderContextBar(promptTokens, m.contextWindowSize(), w))
		if m.cacheReadTokens > 0 {
			hit := int(float64(m.cacheReadTokens)/float64(promptTokens)*100 + 0.5)
			add(m.th.sideMuted.Render(fmt.Sprintf("cache %d%% hit", hit)))
		}
		add("")
	}

	if promptTokens > 0 || m.costUSD > 0 {
		section("COST")
		if m.costUSD > 0 {
			add(m.th.costText.Render(fmt.Sprintf("$%.4f", m.costUSD)))
		}
		if promptTokens > 0 {
			add(m.th.sideMuted.Render(fmt.Sprintf("in  %d", promptTokens)))
			add(m.th.sideMuted.Render(fmt.Sprintf("out %d", m.outputTokens)))
		}
	}

	return lipgloss.NewStyle().
		Width(sidebarInnerW).
		Height(h).
		MaxHeight(h). // prevent overflow: lipgloss Height() pads but never truncates
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colBorder).
		PaddingLeft(1).
		Render(b.String())
}

func (m model) renderInputArea() string {
	// Left side: streaming indicator with elapsed time, toast, or ready dot.
	var statusLeft string
	if m.approval != nil {
		// P25.4a: the composer is blurred while the dialog is open (no
		// blinking cursor down here) — spell out where input goes instead of
		// leaving that to be inferred from the missing cursor alone.
		statusLeft = lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render("⏸ respond to the approval dialog")
	} else if !m.streaming && m.backtrackArmed {
		// P22.3: armed by a first ESC press on an already-empty input box;
		// a second press opens the backtrack picker.
		statusLeft = lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render("⚠  ESC again to backtrack")
	} else if m.streaming {
		// P33.4: the transcript tail loses its hint the moment live text starts
		// flowing, so the status bar carries the token/throughput readout for
		// the whole run — on a local model the rate is the vital sign, and it
		// is worth least when it disappears at the first token.
		hint := m.th.elapsedDim.Render(formatStreamHint(m.streamStats()))
		statusLeft = shimmerText("● "+m.status, m.animStep, colTextMuted, colAccent) + hint
	} else if m.activeToast != nil {
		tag, fg, bg := toastTag(m.activeToast.level)
		statusLeft = statusTag(tag, fg, bg) + " " + m.toastStyle(m.activeToast.level).Render(m.activeToast.message)
	} else {
		statusLeft = statusTag("READY", colBgLess, colSuccess)
	}
	leftW := lipgloss.Width(statusLeft)

	// Right side segments, highest → lowest priority. The loop drops from the
	// tail so lower-value segments disappear first on narrow terminals.
	//   badge (always)  →  hints  →  stats  →  context/agents (sidebar off)  →  cwd
	segs := []string{m.renderModeBadge()}
	segs = append(segs, m.th.statusDim.Render("ctrl+k · f1 · ctrl+e"))
	if stats := m.renderStats(); stats != "" {
		segs = append(segs, m.th.statusDim.Render(stats))
	}
	if !m.sidebarOpen {
		// Fold glanceable sidebar data into the status bar when sidebar is hidden.
		promptTokens := m.inputTokens + m.cacheReadTokens + m.cacheCreationTokens
		if promptTokens > 0 {
			segs = append(segs, renderContextBar(promptTokens, m.contextWindowSize(), 14))
		}
		running := 0
		for _, tm := range m.teammates {
			if tm.Status == "running" {
				running++
			}
		}
		if running > 0 {
			segs = append(segs, m.th.tool.Render(fmt.Sprintf("⚇%d", running)))
		}
	}
	segs = append(segs, m.th.cwdStyle.Render(shortenPath(m.workDir)))

	budget := m.width - leftW - 3 // 2 outer spaces + 1 minimum gap
	for len(segs) > 1 && joinedWidth(segs) > budget {
		segs = segs[:len(segs)-1]
	}
	right := strings.Join(segs, "  ")

	pad := max(m.width-leftW-lipgloss.Width(right)-2, 0)
	belowBar := " " + statusLeft + strings.Repeat(" ", pad) + right + " "

	return m.ta.View() + "\n" + belowBar
}

// joinedWidth returns the rendered width of segments joined by a two-space
// separator, used to decide how many status segments fit on one line.
func joinedWidth(segs []string) int {
	if len(segs) == 0 {
		return 0
	}
	w := 2 * (len(segs) - 1)
	for _, s := range segs {
		w += lipgloss.Width(s)
	}
	return w
}

// statusTag renders a Crush-style padded, coloured indicator chip (e.g. READY,
// ERROR) — bold foreground on a solid status background.
func statusTag(label string, fg, bg color.Color) string {
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(true).Padding(0, 1).Render(label)
}

// toastTag maps a toast level to its indicator chip label and colours, mirroring
// Crush's Status.{Success,Warn,Error}Indicator pairings.
func toastTag(level toastLevel) (label string, fg, bg color.Color) {
	switch level {
	case toastWarn:
		return "WARN", colBgMost, colWarnSubtle
	case toastError:
		return "ERROR", colOnPrimary, colError
	default:
		return "INFO", colBgLess, colInfo
	}
}

func (m model) toastStyle(level toastLevel) lipgloss.Style {
	switch level {
	case toastWarn:
		return lipgloss.NewStyle().Foreground(colWarning)
	case toastError:
		return m.th.errLine
	default:
		return m.th.statusText
	}
}

// connColor picks the P28.7 connection-indicator color: muted while the
// first /status round trip is still in flight, green when the daemon
// reports its configured provider reachable, red otherwise.
func (m model) connColor() color.Color {
	switch {
	case !m.connKnown:
		return colTextMuted
	case m.connReachable:
		return colSuccess
	default:
		return colError
	}
}

// renderConnBadge renders the compact P28.7 connection/model-health glyph
// used in the always-visible title bar: a colored dot, plus a latency
// suffix once one has been measured. bg must match the enclosing
// Background() so the nested style's reset doesn't leave a stray
// mismatched-background segment on the line.
func (m model) renderConnBadge(bg color.Color) string {
	dot := lipgloss.NewStyle().Foreground(m.connColor()).Background(bg).Bold(true).Render("●")
	if m.connKnown && m.connReachable && m.connLatencyMS > 0 {
		dot += lipgloss.NewStyle().Foreground(colTextMuted).Background(bg).Render(fmt.Sprintf(" %dms", m.connLatencyMS))
	}
	return dot
}

// renderConnDetail renders the fuller P28.7 connection-status line for the
// sidebar's MODEL section: reachable/unreachable/checking, plus latency once
// measured (0/unmeasured for a cloud provider, where reachability is just an
// API-key-present check — see Server.probeProviderReachability).
func (m model) renderConnDetail() string {
	style := lipgloss.NewStyle().Foreground(m.connColor())
	switch {
	case !m.connKnown:
		return style.Render("◌ checking…")
	case m.connReachable && m.connLatencyMS > 0:
		return style.Render(fmt.Sprintf("● reachable · %dms", m.connLatencyMS))
	case m.connReachable:
		return style.Render("● reachable")
	default:
		return style.Render("● unreachable")
	}
}

func (m model) renderModeBadge() string {
	switch m.slash.mode {
	case "build":
		return m.th.sideValue.Render("build")
	case "auto":
		return m.th.sideValue.Render("auto")
	default:
		return m.th.sideValue.Render("plan")
	}
}

func (m model) renderStats() string {
	if m.inputTokens == 0 && m.outputTokens == 0 {
		return ""
	}
	est := ""
	if m.tokensEstimated {
		est = "~"
	}
	s := fmt.Sprintf("%sin:%d out:%d", est, m.inputTokens, m.outputTokens)
	if m.costUSD > 0 {
		s += fmt.Sprintf("  $%.4f", m.costUSD)
	}
	return s
}

// toolMaxLines returns the effective per-result line cap based on compact mode.
func (m *model) toolMaxLines() int {
	if m.toolCompact {
		return toolMaxLinesCompact
	}
	return 9999
}

// --- todo strip ---

// renderTodoStrip renders a compact one-line plan progress strip (TQ7).
// Format: ▣▣▢▢ 2/4  → refactor session store
func (m model) renderTodoStrip() string {
	done, inProg, total := 0, 0, len(m.todoItems)
	var activeText string
	for _, it := range m.todoItems {
		switch it.status {
		case "done":
			done++
		case "in_progress":
			inProg++
			if activeText == "" {
				activeText = it.text
			}
		}
	}

	var dots strings.Builder
	for _, it := range m.todoItems {
		switch it.status {
		case "done":
			dots.WriteString(m.th.sideValue.Render("▣"))
		case "in_progress":
			dots.WriteString(m.th.tool.Render("▶"))
		default:
			dots.WriteString(m.th.sideMuted.Render("▢"))
		}
	}

	counter := m.th.sideMuted.Render(fmt.Sprintf(" %d/%d ", done+inProg, total))
	active := ""
	if activeText != "" {
		maxW := max(m.width-total-8, 10)
		active = m.th.statusDim.Render("→ " + truncate(activeText, maxW))
	}
	sep := lipgloss.NewStyle().Foreground(colBorder).Render(strings.Repeat("─", m.width))
	return sep + "\n" + " " + dots.String() + counter + active
}

// parseTodoList parses the formatted output of todo_list into strip items.
func parseTodoList(result string) []todoStripItem {
	var items []todoStripItem
	for _, line := range strings.Split(strings.TrimRight(result, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 5 {
			continue
		}
		var status string
		rest := line
		switch {
		case strings.HasPrefix(line, "[x]"):
			status = "done"
			rest = strings.TrimPrefix(line, "[x]")
		case strings.HasPrefix(line, "[~]"):
			status = "in_progress"
			rest = strings.TrimPrefix(line, "[~]")
		case strings.HasPrefix(line, "[ ]"):
			status = "pending"
			rest = strings.TrimPrefix(line, "[ ]")
		default:
			continue
		}
		rest = strings.TrimSpace(rest)
		var id int
		var text string
		if n, _ := fmt.Sscanf(rest, "%d.", &id); n == 1 {
			if dot := strings.Index(rest, "."); dot >= 0 {
				text = strings.TrimSpace(rest[dot+1:])
			}
		}
		if text == "" {
			text = rest
		}
		items = append(items, todoStripItem{id: id, text: text, status: status})
	}
	return items
}

// --- clipboard ---

// extractCodeBlocks returns the contents of fenced code blocks in text.
func extractCodeBlocks(text string) []string {
	var blocks []string
	var current strings.Builder
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") {
			if inBlock {
				blocks = append(blocks, strings.TrimRight(current.String(), "\n"))
				current.Reset()
				inBlock = false
			} else {
				inBlock = true
			}
			continue
		}
		if inBlock {
			current.WriteString(line + "\n")
		}
	}
	return blocks
}

// copyToClipboardCmd returns a tea.Cmd that copies text to the system clipboard.
func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		return clipboardResultMsg{err: copyToClipboard(text)}
	}
}

// copyToClipboard writes text to the platform clipboard using native tools.
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		for _, tool := range [][]string{
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
			{"wl-copy"},
		} {
			if _, err := exec.LookPath(tool[0]); err == nil {
				cmd = exec.Command(tool[0], tool[1:]...)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("no clipboard tool found (install xclip, xsel, or wl-copy)")
		}
	case "windows":
		cmd = exec.Command("clip.exe")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// --- stash (P5.6) ---

// loadStash reads a previously saved draft from path, returning "" on any error.
func loadStash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var v struct {
		Draft string `json:"draft"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return ""
	}
	return v.Draft
}

// saveStash persists the draft text to path, silently ignoring errors.
func saveStash(path, draft string) {
	if path == "" {
		return
	}
	draft = strings.TrimSpace(draft)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if draft == "" {
		_ = os.Remove(path)
		return
	}
	data, _ := json.Marshal(struct {
		Draft string `json:"draft"`
	}{Draft: draft})
	_ = os.WriteFile(path, data, 0o600)
}

// --- help overlay ---

// renderHelpBox renders just the keyboard-shortcuts box; render() composites
// it over the chat via renderOverlay (P16.6) rather than placing it on a
// blank frame.
func renderHelpBox(keys keyMap) string {
	entries := keys.helpEntries()

	keyStyle := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Width(14)
	descStyle := lipgloss.NewStyle().Foreground(colTextDim)

	var rows strings.Builder
	for _, e := range entries {
		rows.WriteString(keyStyle.Render(e.Key) + "  " + descStyle.Render(e.Desc) + "\n")
	}

	heading := lipgloss.NewStyle().Foreground(colBrandFg).Bold(true).Render("Keyboard Shortcuts")
	footer := lipgloss.NewStyle().Foreground(colTextMuted).Render("press f1 or esc to close")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Background(colSurface).
		Padding(1, 3).
		Width(50).
		Render(heading + "\n\n" + rows.String() + "\n" + footer)
}
