package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
)

func (m model) fetchSessions() tea.Cmd {
	cl := m.cfg.Client
	return fetchCmd(5*time.Second, cl.ListSessions, func(items []api.SessionMeta, err error) tea.Msg {
		return sessionsLoadedMsg{items: items, err: err}
	})
}

func (m model) switchSessionCmd(id string) tea.Cmd {
	cl := m.cfg.Client
	return fetchCmd(5*time.Second, func(ctx context.Context) (*session.Session, error) {
		return cl.GetSession(ctx, id)
	}, func(sess *session.Session, err error) tea.Msg {
		return sessionSwitchedMsg{sess: sess, err: err}
	})
}

// userMessageText extracts the concatenated text blocks of msgs[idx] if it is
// a user message, or "" otherwise (out-of-range idx, a non-user role, or an
// image/tool-result-only message with no text). Used to recover a checkpoint
// turn's verbatim original prompt: Checkpoint.Label is the same text but
// truncated to 120 runes, so it is only a reliable stand-in for short
// messages — this reads the real message content instead.
func userMessageText(msgs []provider.Message, idx int) string {
	if idx < 0 || idx >= len(msgs) {
		return ""
	}
	msg := msgs[idx]
	if msg.Role != provider.RoleUser {
		return ""
	}
	var sb strings.Builder
	for _, b := range msg.Content {
		if tb, ok := b.(provider.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

// fetchBacktrackTargets loads the P22.3 Esc-Esc picker's candidate list: the
// current session's checkpoints (newest first, one per turn) paired with each
// turn's verbatim user message recovered via userMessageText, falling back to
// the checkpoint's own truncated label if that message can't be found (e.g.
// a pre-P22.3 checkpoint layout edge case).
func (m model) fetchBacktrackTargets() tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cps, err := cl.ListCheckpoints(ctx, id)
		if err != nil {
			return backtrackTargetsMsg{err: err}
		}
		if len(cps) == 0 {
			return backtrackTargetsMsg{}
		}
		sess, err := cl.GetSession(ctx, id)
		if err != nil {
			return backtrackTargetsMsg{err: err}
		}
		items := make([]backtrackItem, 0, len(cps))
		for _, cp := range cps {
			text := userMessageText(sess.Messages, cp.Seq)
			if text == "" {
				text = cp.Label
			}
			items = append(items, backtrackItem{cpID: cp.ID, text: text, createdAt: cp.CreatedAt, fileCount: cp.FileCount})
		}
		return backtrackTargetsMsg{items: items}
	}
}

// forkAndSwitchCmd forks the current session at checkpointID (empty = current
// end of conversation) and loads the resulting session, same shape as
// switchSessionCmd but starting from a Fork call instead of a plain fetch.
// prefill is threaded through to forkedMsg unexamined — see its doc comment.
func (m model) forkAndSwitchCmd(checkpointID, prefill string) tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resp, err := cl.Fork(ctx, id, checkpointID)
		if err != nil {
			return forkedMsg{err: err}
		}
		sess, err := cl.GetSession(ctx, resp.SessionID)
		if err != nil {
			return forkedMsg{err: err}
		}
		return forkedMsg{sess: sess, title: resp.Title, prefill: prefill}
	}
}
