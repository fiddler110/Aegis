package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/permission"
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
// always" option from a pending tool call's input: shell commands are
// reduced to their binary (or binary+subcommand — see suggestShellPattern),
// file tools to the file's directory, network tools to the host. The pattern
// feeds a text rule "allow <tool>(<pattern>)" (permission.Rules syntax),
// deliberately narrower than the old whole-tool cache. An empty return means
// no safe suggestion exists (P25.4b) — the caller must not offer an
// allow-always rule in that case.
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
		return suggestShellPattern(args.Command)
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

// cdPrefixRE matches one leading "cd <dir> &&" segment (P25.4b): the shell's
// working directory at the moment of the call isn't part of what the user is
// approving, and keeping it in the suggested pattern is what produced rules
// narrow enough to never match again — e.g.
// "cd /private/tmp/…/demo-project && python3 temps.py*".
var cdPrefixRE = regexp.MustCompile(`^cd\s+\S+\s*&&\s*`)

// envPrefixRE matches one leading "VAR=value" environment assignment.
var envPrefixRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=\S*\s+`)

// suggestShellPattern reduces a shell command to a rule pattern scoped to its
// binary — widened to binary+subcommand only when the second word reads as a
// verb rather than a file/argument, e.g. "git status*" but "python3 *" (not
// "python3 temps.py*", which never matches again once the script name
// changes) — after stripping leading cd/env prefixes (P25.4b). Returns "" if
// the command, once stripped, still contains a shell chaining/redirection/
// substitution metacharacter: no wildcard pattern can be offered safely for
// it, so the caller must fall back to requiring a hand-written rule instead
// of ever suggesting one that bakes in a redirection/pipe/backtick/
// substitution/chaining character (">", "|", a backtick, "$(...)", ";", "&").
func suggestShellPattern(command string) string {
	rest := strings.TrimSpace(command)
	for {
		if m := cdPrefixRE.FindString(rest); m != "" {
			rest = rest[len(m):]
			continue
		}
		if m := envPrefixRE.FindString(rest); m != "" {
			rest = rest[len(m):]
			continue
		}
		break
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "*"
	}
	if strings.ContainsAny(rest, permission.ShellChainMetaChars) {
		return ""
	}
	f := strings.Fields(rest)
	if len(f) == 1 {
		return f[0] + "*"
	}
	if looksLikeSubcommand(f[1]) {
		return f[0] + " " + f[1] + "*"
	}
	return f[0] + " *"
}

// looksLikeSubcommand reports whether a shell argument reads as a verb
// subcommand (git's "status", npm's "test") rather than a file path or
// script argument (python3's "temps.py", which varies every call and
// shouldn't be baked into the suggested rule).
func looksLikeSubcommand(word string) bool {
	if word == "" || strings.HasPrefix(word, "-") {
		return false
	}
	return !strings.ContainsAny(word, `./\`)
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
	// shift+tab here is the dialog's own "move selection up" binding, and
	// takes precedence over the global CycleMode binding because the
	// approval dialog gets first refusal on every message (see update.go's
	// modal-overlay dispatch order). This is intended — do not "fix" it into
	// falling through to mode-cycling while a dialog is open.
	case "up", "shift+tab":
		a.selected = (a.selected + apprOptionCount - 1) % apprOptionCount
		return m, nil
	case "down", "tab":
		a.selected = (a.selected + 1) % apprOptionCount
		return m, nil
	case "enter":
		if a.selected == apprDenyFeedback {
			a.feedbackMode = true
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
		return m, nil
	}

	// Let transcript scroll keys fall through so the transcript stays
	// reviewable. The textarea isn't capturing input here, so the full
	// vi-style scroll set applies; follow state only changes when a key
	// actually scrolled (P21.7 — a non-scroll key must not re-derive follow
	// from geometry the approval dialog itself just perturbed).
	if m.transcript.HandleKey(msg) {
		m.followBottom = m.transcript.AtBottom()
	}
	return m, nil
}

// approvalByID reports whether a call with this id is already known —
// showing (m.approval) or queued (m.approvalQueue) — so a repeated
// ApprovalBatch listing (P81.33) doesn't duplicate an entry.
func (m model) approvalByID(id string) *approvalState {
	if m.approval != nil && m.approval.id == id {
		return m.approval
	}
	for _, a := range m.approvalQueue {
		if a.id == id {
			return a
		}
	}
	return nil
}

// pendingApprovalCount reports how many calls are currently blocked on an
// answer — the showing dialog (if any) plus everything queued behind it —
// for the sidebar/footer's passive indicator, which must stay accurate
// whether or not the approval dialog itself is open.
func (m model) pendingApprovalCount() int {
	n := len(m.approvalQueue)
	if m.approval != nil {
		n++
	}
	return n
}

// approvalQueueSummary renders the "+N more pending" line shown under the
// active approval dialog when a parallel round left other calls waiting
// behind it (P81.33/FIND-33) — the reviewable-summary half of the finding:
// the operator sees the whole round is queued, not just the one call in
// front of them.
func approvalQueueSummary(queue []*approvalState) string {
	if len(queue) == 0 {
		return ""
	}
	names := make([]string, 0, len(queue))
	for _, a := range queue {
		names = append(names, a.toolName)
	}
	return fmt.Sprintf("+%d more waiting: %s", len(queue), strings.Join(names, ", "))
}

// buildApproveRequest translates a dialog option into the request sent to
// the daemon. Split out of answerApproval so the AllowAlways/Pattern
// decision — in particular the P25.4b empty-pattern case — is directly
// testable without a live daemon connection.
func buildApproveRequest(a *approvalState, opt int) api.ApproveRequest {
	req := api.ApproveRequest{ID: a.id}
	switch opt {
	case apprAllowOnce:
		req.Approved = true
	case apprAllowAlways:
		req.Approved = true
		// An empty pattern means suggestRulePattern found no safe glob for
		// this call (P25.4b: shell metacharacters). Sending AllowAlways with
		// no pattern would fall back to the daemon's whole-tool session
		// cache (sseApprover.Approve) — exactly the "one keypress whitelists
		// arbitrary shell redirection" hazard this exists to close — so
		// approve this call only and leave AllowAlways/Pattern unset.
		if a.pattern != "" {
			req.AllowAlways = true
			req.Pattern = a.pattern
		}
	}
	return req
}

// answerApproval resolves the pending approval with the chosen option and
// fires the daemon request. Deny-with-feedback sends the denial and then
// injects the reason as a steering message so the model learns why it was
// refused instead of just seeing a bare denial.
func (m model) answerApproval(opt int, feedback string) (tea.Model, tea.Cmd) {
	a := m.approval
	req := buildApproveRequest(a, opt)
	var steerText string
	if feedback != "" {
		steerText = fmt.Sprintf("The user denied the %s call. Feedback: %s", a.toolName, feedback)
	}
	// Advance to the next queued call, if this round left any behind
	// (P81.33/FIND-33), rather than always returning focus to the composer.
	if len(m.approvalQueue) > 0 {
		m.approval = m.approvalQueue[0]
		m.approvalQueue = m.approvalQueue[1:]
	} else {
		m.approval = nil
		m.status = m.phaseStatus()
		if !m.termFocused {
			m.ta.Focus()
		}
	}
	if steerText != "" {
		// Tagged steerOriginDenialFeedback (P33.15 #3): this is system-phrased
		// text, not something the user typed, so if it ever comes back as
		// KindSteerUnconsumed it must not be requeued as the next user turn
		// the way a plain steer would be — see requeueSteer.
		m.pendingSteers = append(m.pendingSteers, pendingSteerEntry{text: steerText, origin: steerOriginDenialFeedback})
	}

	cl, sessionID := m.cfg.Client, m.cfg.SessionID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := cl.SendApprovalReq(ctx, sessionID, req); err != nil {
			return errMsg{err: fmt.Errorf("approval: %w", err)}
		}
		if steerText != "" {
			// The steer channel is buffered and consumed between tool
			// rounds; a failure here (e.g. the run ended first, or the
			// buffer's full) doesn't mean the approval itself failed, so it
			// reports back as steerFailedMsg rather than errMsg — same
			// reasoning as sendSteerCmd (P33.15 #2).
			if err := cl.Steer(ctx, sessionID, steerText); err != nil {
				return steerFailedMsg{text: steerText, origin: steerOriginDenialFeedback, err: fmt.Errorf("steer: %w", err)}
			}
		}
		return nil
	}
}

// approvalDialogW is the content width of the approval overlay, bounded like
// the list pickers' own frames (newSessionPicker et al.) so the dialog family
// keeps one silhouette.
func (m model) approvalDialogW() int {
	return max(min(m.width-6, 74), 20)
}

// renderApprovalDialog renders the option-list approval prompt (TQ6),
// replacing the old 3-line y/a/n banner: tool + reason header, a preview of
// the pending change (the real diff for edits/writes), and an arrow-key
// selectable option list with pattern-scoped "allow always". P33.6 moved it
// into the shared dialog frame — render() composites it over the chat via
// renderOverlay rather than inserting it into the vertical stack, which used
// to shrink the transcript by the dialog's whole height mid-run.
func (m model) renderApprovalDialog() string {
	a := m.approval
	w := m.approvalDialogW()

	header := " " + lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render("⚡ "+a.toolName) +
		"  " + lipgloss.NewStyle().Foreground(colTextMuted).Render(
		truncate(a.reason, max(w-len(a.toolName)-6, 12)))

	preview := m.renderApprovalBody(w)

	if a.feedbackMode {
		prompt := " " + lipgloss.NewStyle().Foreground(colDanger).Bold(true).Render("✗ deny — why? ") +
			lipgloss.NewStyle().Foreground(colFgBase).Render(a.feedback) +
			lipgloss.NewStyle().Foreground(colAccent).Render("▎") +
			lipgloss.NewStyle().Foreground(colTextMuted).Render("  enter send · esc back")
		return dialogFrame(header + "\n" + preview + prompt)
	}

	allowAlwaysLabel := "Allow always (once only — " + lipgloss.NewStyle().Foreground(colWarning).Render("no safe rule; write one by hand") + ")"
	if a.pattern != "" {
		allowAlwaysLabel = "Allow always  " + ruleLabel(a.toolName, a.pattern)
	}
	labels := [apprOptionCount]string{
		apprAllowOnce:    "Allow once",
		apprAllowAlways:  allowAlwaysLabel,
		apprDeny:         "Deny",
		apprDenyFeedback: "Deny with feedback…",
	}
	keys := [apprOptionCount]string{"y", "a", "n", "f"}

	var b strings.Builder
	b.WriteString(header + "\n" + preview)
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
	if summary := approvalQueueSummary(m.approvalQueue); summary != "" {
		b.WriteString("\n " + lipgloss.NewStyle().Foreground(colTextMuted).Render(truncate(summary, max(w-2, 16))))
	}
	return dialogFrame(b.String())
}

// apprPointer and apprCaret are the glyphs approvalCursorPos looks for to
// place the real terminal cursor (P74.7): the selected option's arrow in the
// normal view, or the typed-feedback caret in feedbackMode.
const (
	apprPointer = "▸"
	apprCaret   = "▎"
)

// approvalCursorPos locates the position within content (the already-rendered
// dialog, as returned by renderApprovalDialog) that the real terminal cursor
// belongs at — the selected option's arrow, or the end of the typed feedback
// text while feedbackMode is active.
func approvalCursorPos(content string, feedbackMode bool) (x, y int, ok bool) {
	if feedbackMode {
		return findGlyphPos(content, apprCaret)
	}
	return findGlyphPos(content, apprPointer)
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
