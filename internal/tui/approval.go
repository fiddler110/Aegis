package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/fiddler110/aegis/internal/api"
)

// Approval dialog options (TQ6), in display order.
const (
	apprAllowOnce = iota
	apprAllowAlways
	apprDeny
	apprDenyFeedback
	apprOptionCount
)

// approvalPreviewMaxLines caps the pending-change preview (diff/command block)
// inside the approval dialog.
const approvalPreviewMaxLines = 12

// suggestRulePattern derives the permission-rule glob offered by the "allow
// always" option from a pending tool call's input: shell commands are scoped
// to their first two words ("npm test *" → "npm test*"), file tools to the
// file's directory, network tools to the host. The pattern feeds a text rule
// "allow <tool>(<pattern>)" (permission.Rules syntax), deliberately narrower
// than the old whole-tool cache.
func suggestRulePattern(inputJSON string) string {
	var args struct {
		Command  string `json:"command"`
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
		URL      string `json:"url"`
	}
	_ = json.Unmarshal([]byte(inputJSON), &args)
	switch {
	case args.Command != "":
		f := strings.Fields(args.Command)
		if len(f) == 1 {
			return f[0] + "*"
		}
		if len(f) >= 2 {
			return f[0] + " " + f[1] + "*"
		}
	case args.Path != "" || args.FilePath != "":
		p := args.Path
		if p == "" {
			p = args.FilePath
		}
		return dirGlob(p)
	case args.URL != "":
		if u, err := url.Parse(args.URL); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host + "/*"
		}
		return args.URL
	}
	return "*"
}

// dirGlob widens a file path to its containing directory, preserving the
// path's own separator style so the glob matches future inputs verbatim
// (rule globs are matched against the raw path string, and "*" spans
// separators). A bare filename is returned as-is.
func dirGlob(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i < 0 {
		return p
	}
	return p[:i+1] + "*"
}

// handleApprovalKey processes a key press while the approval dialog is open.
// It owns option navigation, the y/a/n/f shortcuts, and the deny-with-feedback
// input line; unmatched keys fall through to viewport scrolling so the user
// can still review context behind the dialog.
func (m model) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.approval

	if a.feedbackMode {
		switch msg.String() {
		case "enter":
			feedback := strings.TrimSpace(a.feedback)
			return m.answerApproval(apprDeny, feedback)
		case "esc":
			a.feedbackMode = false
			a.feedback = ""
			m.applyViewportHeight()
			return m, nil
		case "backspace":
			if r := []rune(a.feedback); len(r) > 0 {
				a.feedback = string(r[:len(r)-1])
			}
			return m, nil
		case "space":
			a.feedback += " "
			return m, nil
		default:
			if r := []rune(msg.String()); len(r) == 1 {
				a.feedback += msg.String()
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "up", "shift+tab":
		a.selected = (a.selected + apprOptionCount - 1) % apprOptionCount
		return m, nil
	case "down", "tab":
		a.selected = (a.selected + 1) % apprOptionCount
		return m, nil
	case "enter":
		if a.selected == apprDenyFeedback {
			a.feedbackMode = true
			m.applyViewportHeight()
			return m, nil
		}
		return m.answerApproval(a.selected, "")
	case "y", "Y":
		return m.answerApproval(apprAllowOnce, "")
	case "a", "A":
		return m.answerApproval(apprAllowAlways, "")
	case "n", "N", "esc":
		return m.answerApproval(apprDeny, "")
	case "f", "F":
		a.feedbackMode = true
		m.applyViewportHeight()
		return m, nil
	}

	// Let viewport scroll keys fall through so the transcript stays reviewable.
	var vpCmd tea.Cmd
	m.vp, vpCmd = m.vp.Update(msg)
	m.followBottom = m.vp.AtBottom()
	return m, vpCmd
}

// answerApproval resolves the pending approval with the chosen option and
// fires the daemon request. Deny-with-feedback sends the denial and then
// injects the reason as a steering message so the model learns why it was
// refused instead of just seeing a bare denial.
func (m model) answerApproval(opt int, feedback string) (tea.Model, tea.Cmd) {
	a := m.approval
	req := api.ApproveRequest{ID: a.id}
	switch opt {
	case apprAllowOnce:
		req.Approved = true
	case apprAllowAlways:
		req.Approved = true
		req.AllowAlways = true
		req.Pattern = a.pattern
	}
	var steerText string
	if feedback != "" {
		steerText = fmt.Sprintf("The user denied the %s call. Feedback: %s", a.toolName, feedback)
	}
	m.approval = nil
	m.status = "thinking…"
	m.applyViewportHeight()

	cl, sessionID := m.cfg.Client, m.cfg.SessionID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := cl.SendApprovalReq(ctx, sessionID, req); err != nil {
			return errMsg{err: fmt.Errorf("approval: %w", err)}
		}
		if steerText != "" {
			// Best-effort: the steer channel is buffered and consumed between
			// tool rounds; if the run ends first the feedback is simply lost.
			_ = cl.Steer(ctx, sessionID, steerText)
		}
		return nil
	}
}

// renderApprovalDialog renders the option-list approval prompt (TQ6),
// replacing the old 3-line y/a/n banner: tool + reason header, a preview of
// the pending change (the real diff for edits/writes), and an arrow-key
// selectable option list with pattern-scoped "allow always".
func (m model) renderApprovalDialog() string {
	a := m.approval
	w := max(m.width-2, 20)

	sep := lipgloss.NewStyle().Foreground(colBorder).Render(strings.Repeat("─", w))
	header := " " + lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render("⚡ "+a.toolName) +
		"  " + lipgloss.NewStyle().Foreground(colTextMuted).Render(
		truncate(a.reason, max(w-len(a.toolName)-6, 12)))

	preview := m.renderApprovalBody(w)

	if a.feedbackMode {
		prompt := " " + lipgloss.NewStyle().Foreground(colDanger).Bold(true).Render("✗ deny — why? ") +
			lipgloss.NewStyle().Foreground(colFgBase).Render(a.feedback) +
			lipgloss.NewStyle().Foreground(colAccent).Render("▎") +
			lipgloss.NewStyle().Foreground(colTextMuted).Render("  enter send · esc back")
		return sep + "\n" + header + "\n" + preview + prompt
	}

	labels := [apprOptionCount]string{
		apprAllowOnce:    "Allow once",
		apprAllowAlways:  "Allow always  " + ruleLabel(a.toolName, a.pattern),
		apprDeny:         "Deny",
		apprDenyFeedback: "Deny with feedback…",
	}
	keys := [apprOptionCount]string{"y", "a", "n", "f"}

	var b strings.Builder
	b.WriteString(sep + "\n" + header + "\n" + preview)
	for i := 0; i < apprOptionCount; i++ {
		cursor := "   "
		style := lipgloss.NewStyle().Foreground(colTextDim)
		if i == a.selected {
			cursor = " ▸ "
			style = lipgloss.NewStyle().Foreground(colFgBase).Bold(true)
		}
		key := lipgloss.NewStyle().Foreground(colTextMuted).Render("  " + keys[i])
		b.WriteString(lipgloss.NewStyle().Foreground(colAccent).Render(cursor) +
			style.Render(truncate(labels[i], max(w-8, 16))) + key + "\n")
	}
	b.WriteString(" " + lipgloss.NewStyle().Foreground(colTextMuted).Render("↑/↓ select · enter confirm"))
	return b.String()
}

// ruleLabel renders the persistent rule an "allow always" choice would write.
func ruleLabel(tool, pattern string) string {
	return lipgloss.NewStyle().Foreground(colSuccessMore).Render(
		fmt.Sprintf("allow %s(%s)", tool, pattern))
}

// renderApprovalBody renders the pending call preview. File-mutating tools and
// shell reuse the transcript's rich tool-call rendering (unified diff/command
// block, TQ2); everything else falls back to the compact one-line preview.
// The result always ends with a newline.
func (m model) renderApprovalBody(w int) string {
	a := m.approval
	switch a.toolName {
	case "edit_file", "multi_edit", "write_file", "shell":
		body := renderToolCall(m.th, a.toolName, json.RawMessage(a.input), w-2)
		return " " + capLines(body, approvalPreviewMaxLines, m.th) + "\n"
	}
	return renderApprovalPreview(m.th, a.toolName, a.input, w) + "\n"
}

// capLines truncates s to at most maxLines lines, appending a dim "… N more
// lines" marker when content was dropped. Multi-line content keeps its
// internal newlines (indented one space by the caller's prefix only on the
// first line, matching transcript rendering).
func capLines(s string, maxLines int, th theme) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	hidden := len(lines) - maxLines
	lines = lines[:maxLines]
	return strings.Join(lines, "\n") + "\n  " + th.diffMeta.Render(fmt.Sprintf("… %d more lines", hidden))
}
