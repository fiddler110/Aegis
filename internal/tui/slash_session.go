package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/reqorigin"
)

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

// cmdSide runs a quick, unrelated question through a brand-new session
// (P22.5) — a scratch pad, not a full session switch. Unlike /fork (which
// branches the current conversation and switches the TUI into it), /side
// creates a genuinely isolated session with no accumulated history, asks it
// exactly one question, and blocks (same non-interactive round-trip pattern
// as /debate above) until the answer comes back — the main session's
// messages, token/cost accounting, and on-screen view are never touched.
//
// The side session is a real, persisted session rather than something
// deleted right after use: an abrupt delete would lose the Q&A if the user
// later wants to revisit it, and it stays revisitable with /session list,
// /fork, /rewind, etc. like any other session. To keep it from cluttering
// that list indistinguishably from real conversations, its title is
// prefixed "[side] " so it reads as disposable scratch work and is easy to
// find and bulk-delete later — deliberately not a dedicated Ephemeral field
// on SessionMeta, which would mean threading a new concept through the
// store, the session list UI, and every session-management command for what
// a title prefix already accomplishes.
//
// It always runs in plan (read-only) mode with the default persona/system
// prompt: plan mode never raises interactive tool-approval prompts in the
// first place (any that do slip through — e.g. network egress, still
// gated in plan mode — are auto-denied below, since this handler runs
// synchronously with no way to surface an approval dialog mid-flight), and
// the default persona keeps a "quick, unrelated question" self-contained
// rather than inheriting whatever persona/mode/model the main conversation
// happens to be in.
func (d *SlashDispatcher) cmdSide(args []string) SlashResult {
	question := strings.TrimSpace(strings.Join(args, " "))
	if question == "" {
		return SlashResult{
			Output:  "Usage: /side <question>\n  Ask a quick, unrelated question in a fresh throwaway session — the main conversation's history and context are left untouched.",
			IsError: true,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	title := "[side] " + truncate(oneLine(question), 60)
	meta, err := d.client.CreateSession(ctx, api.CreateSessionRequest{Title: title, Mode: "plan", Origin: reqorigin.TUI})
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("/side: failed to open a side session: %v", err), IsError: true}
	}

	events, err := d.client.PostMessage(ctx, meta.ID, question)
	if err != nil {
		return SlashResult{Output: fmt.Sprintf("/side: failed to ask: %v", err), IsError: true}
	}

	var answer strings.Builder
	var runErr string
	for ev := range events {
		switch ev.Kind {
		case api.KindText:
			answer.WriteString(ev.Text)
		case api.KindApprovalRequest:
			// No interactive surface mid-dispatch (see doc comment above) —
			// deny so the side turn never hangs waiting on a prompt nobody
			// can answer.
			actx, acancel := context.WithTimeout(ctx, 10*time.Second)
			_ = d.client.SendApproval(actx, meta.ID, ev.ApprovalID, false, false)
			acancel()
		case api.KindError:
			runErr = ev.Error
		}
	}

	if runErr != "" {
		return SlashResult{
			Output:  fmt.Sprintf("[side %s] %s\n\n(failed: %s)\n\nSide session kept: %s (/session list to find it, /fork %s to continue it).", short(meta.ID), question, runErr, meta.ID, short(meta.ID)),
			IsError: true,
		}
	}
	ans := strings.TrimSpace(answer.String())
	if ans == "" {
		ans = "(no answer)"
	}
	return SlashResult{
		Output: fmt.Sprintf("[side %s] %s\n\n%s\n\n(kept as a separate session — /session list to find it later)", short(meta.ID), question, ans),
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
