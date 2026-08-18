package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/tui/notify"
)

// applyStreamBatch applies a run of streamed events and refreshes the view
// exactly once (P21.1), so a burst of token deltas costs one markdown
// re-render instead of one per token. It returns a notification command if any
// applied event set one. Both the single-event (eventMsg) and coalesced
// (batchEventMsg) paths funnel through here so their bookkeeping is identical.
func (m *model) applyStreamBatch(evs []api.Event) tea.Cmd {
	// P18.3/P21.7: resume-on-return-to-bottom, one-way. If the pre-batch
	// scroll position is at the bottom, (re-)arm follow — checked before
	// applyEvent grows the transcript, since the content this batch appends
	// would itself make AtBottom() read false afterward. Never cleared here:
	// pausing follow is exclusively an explicit user scroll (wheel-up/pgup),
	// so a mid-stream geometry change (completion popup, approval dialog,
	// textarea growing a line — all of which shrink the pane and briefly
	// falsify AtBottom) can no longer silently kill auto-follow.
	if m.transcript.AtBottom() {
		m.followBottom = true
	}
	sawDone := false
	for _, ev := range evs {
		m.applyEvent(ev)
		if ev.Kind == api.KindDone {
			sawDone = true
		}
	}
	m.refresh()
	var cmds []tea.Cmd
	if m.pendingNotify != nil {
		cmds = append(cmds, m.notifyCmd(*m.pendingNotify))
		m.pendingNotify = nil
	}
	// A finished run may have improved the daemon's context-window detection
	// (first run loads the model into Ollama); re-fetch until authoritative.
	if sawDone && m.srvCtxWinSrc != "config" && m.srvCtxWinSrc != "ollama:loaded" {
		cmds = append(cmds, m.fetchStatusInfo())
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *model) applyEvent(ev api.Event) {
	// P66.15: two event fields carry text this harness did not author and
	// render it through lipgloss, which styles but never strips. A guard
	// reason is the *judge model's* own words (guard.parseVerdict returns
	// whatever the model wrote after the verdict keyword), and an error
	// string routinely quotes a provider or subprocess. ev is a value copy,
	// so this normalizes only what the branches below read.
	switch ev.Kind {
	case api.KindGuard:
		ev.Text = stripControlSeqs(ev.Text)
	case api.KindError:
		ev.Error = stripControlSeqs(ev.Error)
	}

	switch ev.Kind {
	case api.KindThinking:
		// Buffer extended-thinking text; flushed as a collapsible block when
		// the answer (or a tool call) begins. The first token starts the
		// "thought for Ns" clock.
		if m.thinkText.Len() == 0 {
			m.thinkStart = time.Now()
		}
		m.markModelOutput(len(ev.Text))
		m.thinkText.WriteString(ev.Text)

	case api.KindText:
		m.flushThinking() // reasoning is done once the answer starts
		// Buffer text in liveText; flushed through glamour at turn end.
		m.markModelOutput(len(ev.Text))
		m.liveText.WriteString(ev.Text)

	case api.KindToolCallStart:
		// P33.3: the model has named the tool but is still generating its
		// arguments — frequently the longest phase of a turn on a local
		// model, and one the shimmer phrase alone used to cover. The card
		// goes up now and the KindToolCall below fills in the arguments in
		// place. Only a repeat of a tool_use ID already on screen is dropped:
		// two starts can legitimately share an empty ID (the OpenAI wire
		// format can name a call in an earlier delta than it IDs it).
		if ev.Tool == "" {
			break
		}
		if c := m.pendingTools[ev.ToolID]; ev.ToolID != "" && c != nil {
			break
		}
		m.markModelOutput(0)
		m.flushThinking()
		m.flushLiveText() // render any preceding prose before the tool line
		key := m.toolCardKey(ev.ToolID)
		call := "\n" + renderToolCardStartCall(m.th, ev.Tool)
		if blk := m.transcript.AppendBlock(renderToolCardStart(m.th, call, m.animStep)); blk != nil {
			m.trackPendingTool(key, &toolCard{blk: blk, name: ev.Tool, call: call, awaitingCall: true})
		}

	case api.KindToolCall:
		// A daemon that emits no KindToolCallStart at all still leaves the
		// waiting phase here rather than at the tool's result.
		m.markModelOutput(0)
		m.flushThinking()
		m.flushLiveText() // render any preceding prose before the tool line
		// P21.2: a tool call gets one addressable card, appended pending and
		// later mutated in place to ok/err (see KindToolResult below) instead
		// of appending a second, independent block for the result. key
		// prefers the real tool_use ID so concurrent calls (engine.runTools
		// runs read/network tools concurrently) each own their own card; the
		// synthetic fallback only matters for an event with no ToolID (older
		// producers, or hand-built test events).
		call := "\n" + renderToolCall(m.th, ev.Tool, ev.ToolInput, m.transcript.Width())
		key, reconciled := m.reconcileStartedToolCard(ev, call)
		if !reconciled {
			key = m.toolCardKey(ev.ToolID)
			if blk := m.transcript.AppendBlock(renderToolCardPending(m.th, call, m.animStep)); blk != nil {
				m.trackPendingTool(key, &toolCard{blk: blk, name: ev.Tool, call: call})
			}
		}
		m.tools = append(m.tools, toolEntry{name: ev.Tool, status: "pending"})
		if len(m.tools) > maxToolHistory {
			m.tools = m.tools[1:]
		}
		// P2.3 + P2.4: track file access frecency and changed-file list.
		switch ev.Tool {
		case "read_file", "write_file", "edit_file", "multi_edit":
			var inp struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(ev.ToolInput, &inp) == nil && inp.Path != "" {
				if m.fileFrecency == nil {
					m.fileFrecency = make(map[string]int)
				}
				m.fileFrecency[inp.Path]++
				if ev.Tool == "read_file" {
					// Keyed by the same card key as pendingTools above (P21.2)
					// rather than a same-name FIFO queue, so concurrent
					// read_file calls can't cross-attribute paths if their
					// results arrive out of call order.
					if m.pendingReadPaths == nil {
						m.pendingReadPaths = make(map[string]string)
					}
					m.pendingReadPaths[key] = inp.Path
				} else {
					m.recordChangedFile(inp.Path)
				}
			}
		case "todo_add":
			// TQ7: capture the task text so we can match it when the result arrives.
			var inp struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(ev.ToolInput, &inp) == nil {
				m.pendingTodoText = inp.Text
			}
		}

	case api.KindToolResult:
		card, key := m.resolveToolCard(ev)
		path := ""
		if key != "" {
			if p, ok := m.pendingReadPaths[key]; ok {
				path = p
				delete(m.pendingReadPaths, key)
			}
		}
		if card != nil {
			m.transcript.SetItemRaw(card.blk, renderToolCardDone(m.th, card.call, ev.Tool, ev.ToolResult, ev.ToolIsError, m.transcript.Width(), m.toolMaxLines(), path))
		} else {
			// No matching pending card (e.g. a result event replayed or
			// synthesized without a preceding KindToolCall in this session) —
			// fall back to appending it as its own item rather than
			// silently dropping the result.
			m.transcript.Append(renderToolResult(m.th, ev.Tool, ev.ToolResult, ev.ToolIsError, m.transcript.Width(), m.toolMaxLines(), path) + "\n")
		}
		for i := len(m.tools) - 1; i >= 0; i-- {
			if m.tools[i].name == ev.Tool && m.tools[i].status == "pending" {
				if ev.ToolIsError {
					m.tools[i].status = "err"
				} else {
					m.tools[i].status = "ok"
				}
				break
			}
		}
		// TQ7: update the live todo strip from tool results.
		if !ev.ToolIsError {
			switch ev.Tool {
			case "todo_add":
				var id int
				fmt.Sscanf(ev.ToolResult, "added todo #%d", &id)
				if id > 0 {
					m.todoItems = append(m.todoItems, todoStripItem{id: id, text: m.pendingTodoText, status: "pending"})
					m.pendingTodoText = ""
				}
			case "todo_update":
				var id int
				var status string
				if parts := strings.SplitN(ev.ToolResult, " → ", 2); len(parts) == 2 {
					fmt.Sscanf(parts[0], "todo #%d", &id)
					status = strings.TrimSpace(parts[1])
				}
				if id > 0 && status != "" {
					for i := range m.todoItems {
						if m.todoItems[i].id == id {
							m.todoItems[i].status = status
							break
						}
					}
				}
			case "todo_list":
				m.todoItems = parseTodoList(ev.ToolResult)
			}
		}
		// P33.19: with this result the round's last tool has returned (nothing
		// left pending), so the model has been re-invoked with the enlarged
		// prompt and is now re-evaluating it. That wait — dead air the status
		// line otherwise labels "generating…" — begins now and runs until the
		// next model output calls markModelOutput. Gated on pendingTools being
		// empty so a result that still leaves concurrent tools running (they are
		// not a model wait) doesn't start the clock early.
		if m.streaming && len(m.pendingTools) == 0 {
			m.modelWaitAt = time.Now()
			m.status = statusReeval
		}

	case api.KindTurnDone:
		// Some local reasoning models (e.g. Gemma4 in Ollama) route their entire
		// output — both the reasoning chain and the final answer — through the
		// thinking/reasoning SSE field, leaving the content field empty. When that
		// happens, thinkText has content but liveText is empty. Promote the
		// thinking text to the response buffer so it renders with normal styling
		// rather than disappearing as dim unreachable text.
		if m.liveText.Len() == 0 && m.thinkText.Len() > 0 {
			m.liveText.WriteString(m.thinkText.String())
			m.thinkText.Reset()
			m.thinkStart = time.Time{}
		}
		m.flushThinking()
		m.flushLiveText() // render final prose through glamour
		if ev.OutputTokens > 0 || ev.TokensEstimated {
			m.inputTokens = ev.InputTokens
			m.inputTokensKnown = true
			m.outputTokens = ev.OutputTokens
			m.cacheReadTokens = ev.CacheReadTokens
			m.cacheCreationTokens = ev.CacheCreationTokens
			m.tokensEstimated = ev.TokensEstimated
			if !ev.TokensEstimated {
				m.costUSD += ev.CostUSD
			}
		}

	case api.KindApprovalRequest:
		// P66.6 (SEC-14): the tool name and its arguments are model-controlled
		// and land in the one dialog a human answers, so they are sanitized
		// here at ingestion rather than in each preview renderer — approval.go
		// routes four tools through renderToolCall and everything else through
		// renderApprovalPreview, and neither the diff path nor the generic
		// JSON excerpt strips anything, so a per-renderer fix would only hold
		// until the next branch is added. The reason string is daemon-authored,
		// but it renders in the same header and costs nothing to sanitize.
		toolName := stripControlSeqs(ev.Tool)
		input := sanitizeToolInputJSON(string(ev.ToolInput))
		m.approval = &approvalState{
			toolName: toolName,
			input:    input,
			reason:   stripControlSeqs(ev.ApprovalReason),
			id:       ev.ApprovalID,
			pattern:  suggestRulePattern(input),
		}
		m.status = "approval required"
		// Blur the composer so its cursor stops implying it's the thing
		// listening (P25.4a) — the approval dialog is the only visual and
		// input focus target until it's answered.
		m.ta.Blur()
		m.pendingNotify = &notify.Event{Title: "Aegis", Body: "Approval needed: " + toolName}

	case api.KindSteer:
		// A steering instruction was injected mid-run. Flush any partial model
		// output, show the steer as a user message, then open a new assistant bar
		// so the continuation renders under its own label.
		m.resolvePendingSteer(ev.Text)
		m.flushThinking()
		m.flushLiveText()
		sepW := max(m.transcript.Width()-2, 10)
		m.transcript.Append(m.th.turnSep.Render(strings.Repeat("─", sepW)) + "\n")
		m.transcript.Append(barLabel("You", colUserFg) + "\n" + ev.Text + "\n\n")
		m.transcript.Append(barLabel("Assistant", colAssistFg) + "\n")

	case api.KindSteerUnconsumed:
		// The run ended without the steer ever reaching a tool round (P33.2).
		// requeueSteer uses the resolved origin to keep a system-authored
		// steer (denial feedback) from being sent as the next user turn
		// (P33.15 #3).
		origin, _ := m.resolvePendingSteer(ev.Text)
		m.requeueSteer(ev.Text, origin)

	case api.KindGuard:
		m.flushThinking()
		m.flushLiveText()
		switch {
		case ev.GuardRetrying:
			// The engine is about to replace this answer with a corrective
			// retry (P25.3): withdraw the failed answer in place so the retry
			// renders as *the* answer, not as a second one below it.
			note := m.th.elapsedDim.Render("⚠ output guard: answer withdrawn ("+ev.Text+") — retrying…") + "\n\n"
			if m.lastAnswerBlock != nil {
				m.transcript.SetItemRaw(m.lastAnswerBlock, note)
				m.lastAnswerBlock = nil
				m.lastAssistantText = ""
			} else {
				m.transcript.Append(note)
			}
		case ev.Text != "":
			// Terminal validation failure (retries exhausted, answer surfaced
			// anyway); surface it as a dim warning.
			m.transcript.Append("\n" + m.th.elapsedDim.Render("⚠ output guard: "+ev.Text) + "\n")
		}
		// A pass event (empty Text) needs no transcript line at all.

	case api.KindCostAlert:
		// Spend crossed the configured alert threshold; surface it as a dim warning.
		m.flushThinking()
		m.flushLiveText()
		m.transcript.Append("\n" + m.th.elapsedDim.Render("⚠ "+ev.Text) + "\n")

	case api.KindNotice:
		// Engine advisory (context fill, compaction, step limit) — same dim
		// warning treatment as cost alerts.
		m.flushThinking()
		m.flushLiveText()
		m.transcript.Append("\n" + m.th.elapsedDim.Render("⚠ "+ev.Text) + "\n")

	case api.KindDone:
		// Flush any buffered text (safety net — normally flushed at KindTurnDone).
		// If the last action was a tool call with no follow-up text, this ensures
		// the transcript is fully rendered before the run is marked complete.
		m.flushLiveText()
		m.lastAnswerBlock = nil // the surfaced answer is final; nothing left to withdraw

	case api.KindError:
		m.flushThinking()
		m.flushLiveText()
		m.lastAnswerBlock = nil
		if m.approval != nil { // clear any pending approval if the run aborts
			m.approval = nil
			if !m.termFocused {
				m.ta.Focus()
			}
		}
		m.transcript.Append("\n" + m.th.errLine.Render("error: "+ev.Error) + "\n")
		m.pendingNotify = &notify.Event{Title: "Aegis", Body: "Error: " + truncate(ev.Error, 100)}
		// P21.2: a turn that errors mid-round can leave a concurrently-run
		// tool's card stuck pending (its own KindToolResult may simply never
		// arrive — see runTools/runToolsSequential, which stop emitting
		// further events once the round is abandoned). Resolve it here
		// rather than leaving it stuck; streamClosedMsg is the same
		// safety net for the interrupt path, which reaches neither
		// KindError nor KindDone (engine.ErrInterrupted returns silently).
		m.resolveStuckToolCards()
	}
}

// resolveToolCard looks up and removes the pending card matching a
// KindToolResult event (P21.2), returning it and the card key it was
// registered under (also the key pendingReadPaths uses) so the caller can
// pop that too. Prefers an exact tool_use-ID match (set on every
// live-engine-emitted event — see engine.Event.ToolID); falls back to the
// oldest pending card with a matching tool name for an event with no ID
// (the pre-P21.2 FIFO behavior), which keeps history-replay-shaped and
// hand-built test events working unchanged. Returns (nil, "") if nothing
// matches.
func (m *model) resolveToolCard(ev api.Event) (*toolCard, string) {
	if ev.ToolID != "" {
		if c, ok := m.pendingTools[ev.ToolID]; ok {
			m.removePendingTool(ev.ToolID)
			return c, ev.ToolID
		}
		return nil, ""
	}
	for _, k := range m.pendingToolOrder {
		if c := m.pendingTools[k]; c != nil && c.name == ev.Tool {
			m.removePendingTool(k)
			return c, k
		}
	}
	return nil, ""
}

// toolCardKey returns the pendingTools key for a tool event: its real
// tool_use ID, or a synthesized one for an event that carries none (P21.2).
func (m *model) toolCardKey(toolID string) string {
	if toolID != "" {
		return toolID
	}
	m.pendingToolSeq++
	return fmt.Sprintf("#%d", m.pendingToolSeq)
}

// trackPendingTool registers a freshly-appended card under key.
func (m *model) trackPendingTool(key string, c *toolCard) {
	if m.pendingTools == nil {
		m.pendingTools = make(map[string]*toolCard)
	}
	m.pendingTools[key] = c
	m.pendingToolOrder = append(m.pendingToolOrder, key)
}

// reconcileStartedToolCard folds a KindToolCall's rendered arguments into the
// provisional card its KindToolCallStart already put on screen (P33.3),
// returning the key the card ends up registered under. reconciled is false
// when there is no such card — a daemon that never emits KindToolCallStart,
// an adapter whose provider doesn't announce tool calls early, or a start
// event the SSE buffer dropped — and the caller appends a card as it always
// has, so nothing here can produce a second card for one call.
//
// Identity mirrors resolveToolCard's: an exact tool_use-ID match first, then
// the oldest still-provisional card of the same tool name. The fallback is
// load-bearing rather than legacy-only — the OpenAI wire format may name a
// tool call in an earlier delta than the one carrying its ID, so a start
// event can be ID-less even when the call that follows it isn't. Both events
// are emitted in stream order, so the i-th start of a name pairs with the
// i-th call of it.
func (m *model) reconcileStartedToolCard(ev api.Event, call string) (string, bool) {
	card, key := m.startedToolCard(ev)
	if card == nil {
		return "", false
	}
	card.call = call
	card.awaitingCall = false
	m.transcript.SetItemRaw(card.blk, renderToolCardPending(m.th, card.call, m.animStep))
	if ev.ToolID != "" && ev.ToolID != key {
		m.rekeyPendingTool(key, ev.ToolID)
		key = ev.ToolID
	}
	return key, true
}

// startedToolCard finds the still-provisional card belonging to ev and the
// key it is registered under, or (nil, "").
func (m *model) startedToolCard(ev api.Event) (*toolCard, string) {
	if c := m.pendingTools[ev.ToolID]; ev.ToolID != "" && c != nil && c.awaitingCall {
		return c, ev.ToolID
	}
	for _, k := range m.pendingToolOrder {
		if c := m.pendingTools[k]; c != nil && c.awaitingCall && c.name == ev.Tool {
			return c, k
		}
	}
	return nil, ""
}

// rekeyPendingTool re-registers a pending card under newKey without
// disturbing its position in pendingToolOrder. A card appended at
// KindToolCallStart is keyed by whatever the start event carried — a
// synthetic key when the provider hadn't assigned the tool_use ID that early
// — while the KindToolResult that eventually arrives looks it up by the real
// ID (P33.3).
func (m *model) rekeyPendingTool(oldKey, newKey string) {
	c := m.pendingTools[oldKey]
	if c == nil {
		return
	}
	delete(m.pendingTools, oldKey)
	m.pendingTools[newKey] = c
	for i, k := range m.pendingToolOrder {
		if k == oldKey {
			m.pendingToolOrder[i] = newKey
			break
		}
	}
}

// removePendingTool drops key from both pendingTools and pendingToolOrder.
// The order slice is small (bounded by maxParallelTools concurrent calls in
// one round), so a linear scan to remove it is cheap.
func (m *model) removePendingTool(key string) {
	delete(m.pendingTools, key)
	for i, k := range m.pendingToolOrder {
		if k == key {
			m.pendingToolOrder = append(m.pendingToolOrder[:i], m.pendingToolOrder[i+1:]...)
			break
		}
	}
}

// resolveStuckToolCards finalizes every still-pending tool card to a
// terminal "interrupted" state (P21.2). Called wherever a run can end
// without producing a KindToolResult for every KindToolCall it started —
// KindError above, and streamClosedMsg (the universal safety net: it fires
// for every run end, including a client-initiated cancel or a budget/loop
// abort, none of which necessarily emit any terminal event at all — see
// engine.ErrInterrupted's callers, which return before emitting anything).
// A no-op when nothing is pending.
func (m *model) resolveStuckToolCards() {
	if len(m.pendingTools) == 0 {
		return
	}
	for _, k := range m.pendingToolOrder {
		if c := m.pendingTools[k]; c != nil {
			m.transcript.SetItemRaw(c.blk, renderToolCardStuck(m.th, c.call))
		}
		delete(m.pendingReadPaths, k)
	}
	m.pendingTools = nil
	m.pendingToolOrder = nil
}

// updatePendingToolCards refreshes every still-pending tool card's shimmer
// frame in place (P21.2), driven by the same animation tick that already
// advances animStep for the "● thinking…" status line and the streaming
// write-head caret (P21.3) — so a long-running tool call visibly keeps
// working instead of sitting static. A no-op when nothing is pending.
func (m *model) updatePendingToolCards() {
	for _, c := range m.pendingTools {
		if c.awaitingCall {
			m.transcript.SetItemRaw(c.blk, renderToolCardStart(m.th, c.call, m.animStep))
			continue
		}
		m.transcript.SetItemRaw(c.blk, renderToolCardPending(m.th, c.call, m.animStep))
	}
}
