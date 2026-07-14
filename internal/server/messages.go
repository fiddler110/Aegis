package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/notify"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/trace"
)

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.PostMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Text) == "" && len(req.Images) == 0 {
		writeError(w, http.StatusBadRequest, "text or images required")
		return
	}
	// resumable (P28.5): this run should survive an SSE connection drop, like
	// an explicitly-backgrounded session already does. Both cases need the
	// same daemon-rooted context + event buffering, so they share one flag
	// below rather than duplicating every check against sess.Background.
	resumable := req.Resumable

	imageBlocks, err := buildImageBlocks(req.Images)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Serialize runs within the same session: at most one active run at a time.
	// Concurrent requests queue here rather than racing to mutate the session.
	sem := s.sessionSemaphore(id)
	select {
	case sem <- struct{}{}:
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "request cancelled while waiting for active run to finish")
		return
	}
	defer func() { <-sem }()

	// Global concurrency ceiling across every session (P21.5): unlike the
	// per-session semaphore above, this bounds total daemon-wide active runs
	// so a caller that fans out across many sessions (e.g. a hostile or
	// misbehaving aegis mcp-serve client) can't exhaust host resources.
	// Non-blocking: a full daemon rejects immediately rather than queuing.
	if s.runSem != nil {
		select {
		case s.runSem <- struct{}{}:
			defer func() { <-s.runSem }()
		default:
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("daemon at max concurrent runs (%d); try again shortly", cap(s.runSem)))
			return
		}
	}

	sess, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}

	if cap := s.cfg.Cost.SessionCapUSD; cap > 0 && sess.CostUSD >= cap {
		writeError(w, http.StatusPaymentRequired, fmt.Sprintf("session spend cap reached: $%.4f of $%.2f limit", sess.CostUSD, cap))
		return
	}
	spend, dailyCapErr := s.beginDailySpend(r.Context())
	if dailyCapErr != nil {
		writeError(w, http.StatusPaymentRequired, dailyCapErr.Error())
		return
	}
	sessionTokensBefore := sess.InputTokens + sess.OutputTokens
	if cap := s.cfg.Cost.SessionTokenCap; cap > 0 && sessionTokensBefore >= cap {
		writeError(w, http.StatusPaymentRequired, fmt.Sprintf("session token cap reached: %d of %d limit", sessionTokensBefore, cap))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// All writes to w (events + heartbeat) go through writeMu so the two
	// goroutines never interleave a frame.
	var writeMu sync.Mutex

	// Events are queued and flushed by a dedicated writer goroutine rather
	// than written synchronously from the engine's own goroutine (P21.5): a
	// slow or stalled SSE consumer (TUI, web UI, or an mcp-serve client)
	// falling behind drops its oldest queued event instead of growing memory
	// without bound or blocking the run — see sseWriter.
	sseBufSize := s.cfg.Server.SSEBufferSize
	if sseBufSize <= 0 {
		sseBufSize = config.DefaultSSEBufferSize
	}
	var sseDropWarnOnce sync.Once
	sw := newSSEWriter(w, flusher, &writeMu, sseBufSize, func() {
		sseDropWarnOnce.Do(func() {
			s.logger.Warn("sse buffer full for run; dropping oldest queued events", "session", id, "buffer_size", sseBufSize)
		})
	})
	defer sw.Close()
	send := sw.send

	// Heartbeat: emit an SSE comment periodically so idle long-running tool
	// calls don't get dropped by intermediaries. The goroutine is joined before
	// returning so it never writes to w after the handler exits.
	hbCtx, hbCancel := context.WithCancel(r.Context())
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				writeMu.Lock()
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
				writeMu.Unlock()
			}
		}
	}()
	defer func() { hbCancel(); <-hbDone }()

	// Register a per-run approval channel keyed by a unique run id so a
	// concurrent run on the same session can't consume this run's answer.
	runID := newRunID()
	approvalCh := make(chan approvalDecision, 1)
	s.pendingApprovals.Store(runID, approvalCh)
	defer s.pendingApprovals.Delete(runID)

	// Track this run so concurrent parallel sessions are observable via /runs.
	if s.runs != nil {
		runTitle := sess.Title
		if runTitle == "" {
			runTitle = deriveTitle(req.Text)
		}
		s.runs.start(runID, id, runTitle)
		defer s.runs.finish(runID)
		baseSend := send
		send = func(ev api.Event) {
			s.runs.observe(runID, ev.Kind)
			baseSend(ev)
		}
	}

	var runApprover permission.Approver
	if s.cfg.Permission.AutoApproveExec {
		runApprover = permission.AutoApprove{}
	} else {
		runApprover = &sseApprover{
			send:        send,
			ch:          approvalCh,
			runID:       runID,
			sessionID:   id,
			permCache:   &s.sessionPermCache,
			persistRule: s.addPermissionRule,
		}
	}

	// Steer channel: the TUI can POST /sessions/{id}/steer while the run is
	// active to inject a course-correction message between tool rounds.
	steerCh := make(chan string, 8)
	s.pendingSteers.Store(id, steerCh)
	defer s.pendingSteers.Delete(id)

	s.refreshPersonas() // pick up persona file edits without a daemon restart
	p, _ := persona.Get(sess.Persona)
	guardEnabled := s.cfg.OutputGuard.Enabled
	if req.GuardEnabled != nil {
		guardEnabled = *req.GuardEnabled
	}

	// Shared across this engine and every sub-agent it spawns (D1): embedding
	// the same tracker in runCtx below means fan-out draws from one ledger
	// instead of each spawned agent getting its own fresh BudgetUSD allowance.
	tracker := cost.NewTracker()
	// Session-scoped tool registry clone (P9): tool_search loads a deferred
	// tool onto this session's own exposure state, not the daemon-wide
	// registry every other session and persona shares.
	sessionTools := s.sessionToolRegistry(id)
	workdir := s.workdirFor(id)
	eng, err := s.newEngine(sess.Mode, runApprover, steerCh, p, guardEnabled, tracker, sessionTools, sess.Model, workdir, req.Text, sess.Messages)
	if err != nil {
		send(api.Event{Kind: api.KindError, Error: err.Error()})
		return
	}

	conv := &engine.Conversation{System: s.effectiveSystem(sess.System, id), Messages: sess.Messages, Persisted: len(sess.Messages)}

	// P3.2/P28.5: a background session or a resumable run keeps executing on a
	// server-level context (below) after the HTTP client disconnects. Either
	// way, every event is also buffered to SQLite so a client can reattach
	// and catch up via GET /sessions/{id}/events?since=N — a resumable run's
	// web UI reconnect (P28.5) and a background session's reattach (P3.2) are
	// literally the same catch-up mechanism, just triggered differently.
	detached := sess.Background || resumable
	if detached {
		origSend := send
		send = func(ev api.Event) {
			origSend(ev) // best-effort SSE while client is connected
			if data, jerr := json.Marshal(ev); jerr == nil {
				_ = s.store.AppendBGEvent(context.Background(), id, string(data))
			}
		}
	}

	// Create a checkpoint for this turn before appending the user message, so a
	// rewind restores the conversation to just before this turn and undoes any
	// file changes the turn makes. seq is the pre-turn message count.
	var snap *checkpoint.Snapshotter
	if s.checkpoints != nil {
		if cp, err := s.checkpoints.Create(context.Background(), id, len(sess.Messages), req.Text); err != nil {
			s.logger.Warn("create checkpoint", "session", id, "err", err)
		} else {
			snap = s.checkpoints.NewSnapshotter(cp.ID)
			// P3.4: capture the HEAD commit SHA asynchronously so rollback can reset to it.
			go func(cpID string) {
				if sha := captureGitSHA(context.Background(), workdir); sha != "" {
					if err := s.checkpoints.SetGitSHA(context.Background(), cpID, sha); err != nil {
						s.logger.Warn("set checkpoint git sha", "checkpoint", cpID, "err", err)
					}
				}
			}(cp.ID)
		}
	}

	content := make([]provider.Block, 0, 1+len(imageBlocks))
	if strings.TrimSpace(req.Text) != "" {
		// P5.5: expand @path#L10-40 file mentions to inline file excerpts.
		content = append(content, provider.TextBlock{Text: expandFileMentions(req.Text, workdir)})
	}
	content = append(content, imageBlocks...)
	conv.Append(provider.Message{Role: provider.RoleUser, Content: content})

	// Carry the session's permission mode so the `agent` tool can clamp any
	// sub-agents it spawns to no more than this posture. P3.2/P28.5: a
	// detached (background or resumable) run uses a server-level context so
	// it continues after the HTTP client drops.
	baseRunCtx := r.Context()
	if detached {
		baseRunCtx = context.Background()
	}
	// Optional wall-clock ceiling (P21.5): off by default (0). A coarse
	// last-resort backstop for a run that never trips the token/dollar
	// budgets — e.g. a local model stuck in a near-zero-cost tool-call loop,
	// or a hostile caller trying to hold a run (and the session/global
	// concurrency slot it occupies) open forever. The engine already treats
	// context cancellation as an interruption (engine.ErrInterrupted), so a
	// timeout aborts the run the same clean way a client-initiated cancel does.
	if d := s.cfg.Server.MaxRunDurationSec; d > 0 {
		var cancel context.CancelFunc
		baseRunCtx, cancel = context.WithTimeout(baseRunCtx, time.Duration(d)*time.Second)
		defer cancel()
	}
	// A detached run's context is no longer tied to the HTTP request, so
	// disconnecting can no longer stop it — register an explicit cancel func
	// so POST /sessions/{id}/stop has something to call (P28.5). A plain run
	// keeps stopping the way it always has: the client tears down its
	// request, which cancels baseRunCtx (= r.Context()) directly.
	if detached {
		var stopCancel context.CancelFunc
		baseRunCtx, stopCancel = context.WithCancel(baseRunCtx)
		defer stopCancel()
		if s.runs != nil {
			s.runs.setCancel(runID, stopCancel)
		}
	}
	runCtx := swarm.WithParentMode(baseRunCtx, sess.Mode)
	runCtx = swarm.WithCostTracker(runCtx, tracker)
	if snap != nil {
		runCtx = checkpoint.WithSnapshotter(runCtx, snap)
	}
	if s.execHook != nil {
		s.execHook.SessionStart(runCtx, id)
		defer s.execHook.Stop(context.Background(), id)
	}
	var (
		totalIn   int
		totalOut  int
		totalCost float64
		traces    []trace.TurnTrace
	)
	// Deferred (not called inline at the bottom of the handler) so every
	// return path below this point — including one a future edit adds — is
	// covered without anyone needing to remember to add the call there too.
	defer func() { spend.Finish(totalCost, totalIn+totalOut) }()
	// flushMessages durably saves whatever of conv.Messages hasn't been saved
	// yet. It's called after every tool round (not just once at the very end)
	// so a crash mid-run loses at most the current turn's in-flight model
	// call, not the whole turn's transcript — tool side effects (files
	// written, shell commands executed) already happened on disk by the time
	// their result messages are appended, so leaving them unpersisted until
	// eng.Run fully returns let history desync from real repo state with no
	// record of what actually ran. Safe to call from the emit callback: it
	// runs on the same goroutine as eng.Run, synchronously between conv
	// mutations, never concurrently with them.
	flushMessages := func() {
		if conv.Persisted < 0 {
			if err := s.store.SaveMessages(context.Background(), id, conv.Messages); err != nil {
				s.logger.Error("save messages", "session", id, "err", err)
				return
			}
			conv.Persisted = len(conv.Messages)
			return
		}
		if conv.Persisted >= len(conv.Messages) {
			return
		}
		if err := s.store.AppendMessages(context.Background(), id, conv.Messages[conv.Persisted:]); err != nil {
			s.logger.Error("append messages", "session", id, "err", err)
			return
		}
		conv.Persisted = len(conv.Messages)
	}
	runErr := eng.Run(runCtx, conv, func(ev engine.Event) {
		// Trace events are server-internal observability records — collect them
		// for persistence but never forward them to the SSE client.
		if ev.Kind == engine.KindTrace {
			if ev.Trace != nil {
				traces = append(traces, *ev.Trace)
			}
			flushMessages()
			return
		}
		apiEv := toAPIEvent(ev)
		send(apiEv)
		if ev.Kind == engine.KindTurnDone {
			flushMessages()
			if ev.Usage != nil {
				// Tokens count even for estimated usage (local/Ollama models
				// report no real usage) — only the dollar figure is skipped
				// for those, since pricing an estimate would be misleading
				// (P10.5). Before this fix, AddUsage/AddDailyTokens never saw
				// a local model's turns at all, so session/daily token caps
				// had nothing to check against.
				totalIn += ev.Usage.InputTokens
				totalOut += ev.Usage.OutputTokens
				if !ev.Usage.IsEstimated {
					totalCost += apiEv.CostUSD
				}
			}
		}
	})

	// For non-interrupt aborts (max iterations, cost budget, loop detected) inject
	// a note so the model knows on the next turn what happened and what remains.
	if runErr != nil && !errors.Is(runErr, engine.ErrInterrupted) {
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
			provider.TextBlock{Text: fmt.Sprintf("[System: run aborted — %v. On your next message, summarize what completed and what still needs to be done.]", runErr)},
		}})
	}
	// Final flush: covers the abort note just appended above, plus anything
	// from the last turn the incremental flushes above hadn't caught yet.
	flushMessages()
	if totalIn > 0 || totalOut > 0 {
		_ = s.store.AddUsage(context.Background(), id, totalIn, totalOut, totalCost)
	}
	totalTokens := totalIn + totalOut
	s.alertOnCostThreshold(send, sess.CostUSD, totalCost, spend.dailyCostBefore)
	s.alertOnTokenThreshold(send, sessionTokensBefore, totalTokens, spend.dailyTokensBefore)
	if len(traces) > 0 {
		if err := s.store.AppendTraces(context.Background(), id, traces); err != nil {
			s.logger.Warn("save traces", "session", id, "err", err)
		}
	}
	// The run just loaded the model into Ollama (if that's the backend), so
	// /api/ps can now report the real serving context; re-detect while the
	// current value is non-authoritative. No-op for cloud providers.
	go s.maybeRefreshContextWindow(context.Background())
	if sess.Title == "" {
		go s.generateTitle(id, req.Text)
	}
	if runErr != nil {
		s.logger.Warn("run ended with error", "session", id, "err", runErr)
	}

	// Notify when a detached (background) session finishes: the user is not
	// watching the TUI, so surface completion/failure out-of-band (P5.4).
	if sess.Background && s.notifier != nil {
		ev := notify.Event{
			SessionID: id,
			Title:     sess.Title,
			Status:    notify.StatusCompleted,
			Message:   fmt.Sprintf("Background session %q completed", displayTitle(sess.Title, id)),
			CostUSD:   totalCost,
		}
		if runErr != nil && !errors.Is(runErr, engine.ErrInterrupted) {
			ev.Status = notify.StatusError
			ev.Message = fmt.Sprintf("Background session %q failed: %v", displayTitle(sess.Title, id), runErr)
		}
		s.notifier.Notify(context.Background(), ev)
	}
}

// spendGuard is the only way to gate a handler against the cross-session
// daily dollar/token caps (P9.5/P10.5). It replaces a previous pair of free
// functions (checkDailyCaps / recordDailySpend) that a handler had to
// remember to call together, in the right order, on every return path — a
// convention with no compiler backing, which is exactly how /debate once
// shipped spending real model turns without ever being gated or recorded
// against the daily caps (see routes() doc comment). Folding both halves
// into one type doesn't stop a new handler from skipping beginDailySpend
// entirely, but it does mean that once a handler calls it, there is no
// exported way to read dailyCostBefore/dailyTokensBefore without also
// holding the guard whose Finish records spend — and deferring Finish
// immediately, as both call sites below do, means a later edit that adds an
// early return in between can't reintroduce the "checked but never
// recorded" gap by accident.
type spendGuard struct {
	s                 *Server
	dailyCostBefore   float64
	dailyTokensBefore int
}

// beginDailySpend checks the cross-session daily totals and, if neither
// configured cap is already exhausted, returns a guard. Defer guard.Finish
// immediately so it fires on every return path (including a panic unwinding
// through the defer stack), regardless of how many early returns the
// handler body adds later. A read failure is logged and treated as "not yet
// exceeded" rather than blocking the caller.
func (s *Server) beginDailySpend(ctx context.Context) (*spendGuard, error) {
	g := &spendGuard{s: s}
	if cap := s.cfg.Cost.DailyCapUSD; cap > 0 {
		cost, err := s.store.TodayCost(ctx)
		if err != nil {
			s.logger.Warn("read daily cost", "err", err)
		} else {
			g.dailyCostBefore = cost
			if cost >= cap {
				return nil, fmt.Errorf("daily spend cap reached: $%.4f of $%.2f limit", cost, cap)
			}
		}
	}
	if cap := s.cfg.Cost.DailyTokenCap; cap > 0 {
		tokens, err := s.store.TodayTokens(ctx)
		if err != nil {
			s.logger.Warn("read daily tokens", "err", err)
		} else {
			g.dailyTokensBefore = tokens
			if tokens >= cap {
				return nil, fmt.Errorf("daily token cap reached: %d of %d limit", tokens, cap)
			}
		}
	}
	return g, nil
}

// Finish records costUSD/tokens spent under this guard against the current
// day's cross-session totals, so a later beginDailySpend call — from a
// normal session turn, another /debate call, or any future model-spending
// endpoint — sees this run's spend.
func (g *spendGuard) Finish(costUSD float64, tokens int) {
	if costUSD > 0 && g.s.cfg.Cost.DailyCapUSD > 0 {
		if err := g.s.store.AddDailyCost(context.Background(), costUSD); err != nil {
			g.s.logger.Warn("add daily cost", "err", err)
		}
	}
	if tokens > 0 && g.s.cfg.Cost.DailyTokenCap > 0 {
		if err := g.s.store.AddDailyTokens(context.Background(), tokens); err != nil {
			g.s.logger.Warn("add daily tokens", "err", err)
		}
	}
}

// alertOnCostThreshold sends a KindCostAlert event when this turn's spend
// pushes either the session or the daily total across the configured alert
// fraction of its cap, but only on the turn that crosses it (not every turn
// past the threshold) — checked by comparing the before/after totals (P9.5).
func (s *Server) alertOnCostThreshold(send func(api.Event), sessionCostBefore, turnCost, dailyCostBefore float64) {
	if turnCost <= 0 {
		return
	}
	frac := s.cfg.Cost.AlertThreshold
	if frac <= 0 {
		return
	}
	if cap := s.cfg.Cost.SessionCapUSD; cap > 0 {
		threshold := cap * frac
		after := sessionCostBefore + turnCost
		if sessionCostBefore < threshold && after >= threshold {
			send(api.Event{Kind: api.KindCostAlert, Text: fmt.Sprintf("session spend at $%.4f — %.0f%% of the $%.2f cap", after, after/cap*100, cap)})
		}
	}
	if cap := s.cfg.Cost.DailyCapUSD; cap > 0 {
		threshold := cap * frac
		after := dailyCostBefore + turnCost
		if dailyCostBefore < threshold && after >= threshold {
			send(api.Event{Kind: api.KindCostAlert, Text: fmt.Sprintf("daily spend at $%.4f — %.0f%% of the $%.2f cap", after, after/cap*100, cap)})
		}
	}
}

// alertOnTokenThreshold is the token-denominated counterpart to
// alertOnCostThreshold (P10.5) — same crossing-edge logic, checked against
// SessionTokenCap/DailyTokenCap instead of the dollar caps, so it still fires
// for local/unpriced models whose turnCost is always 0.
func (s *Server) alertOnTokenThreshold(send func(api.Event), sessionTokensBefore, turnTokens, dailyTokensBefore int) {
	if turnTokens <= 0 {
		return
	}
	frac := s.cfg.Cost.AlertThreshold
	if frac <= 0 {
		return
	}
	if cap := s.cfg.Cost.SessionTokenCap; cap > 0 {
		threshold := float64(cap) * frac
		after := sessionTokensBefore + turnTokens
		if float64(sessionTokensBefore) < threshold && float64(after) >= threshold {
			send(api.Event{Kind: api.KindCostAlert, Text: fmt.Sprintf("session tokens at %d — %.0f%% of the %d cap", after, float64(after)/float64(cap)*100, cap)})
		}
	}
	if cap := s.cfg.Cost.DailyTokenCap; cap > 0 {
		threshold := float64(cap) * frac
		after := dailyTokensBefore + turnTokens
		if float64(dailyTokensBefore) < threshold && float64(after) >= threshold {
			send(api.Event{Kind: api.KindCostAlert, Text: fmt.Sprintf("daily tokens at %d — %.0f%% of the %d cap", after, float64(after)/float64(cap)*100, cap)})
		}
	}
}

// displayTitle returns a human-friendly session label, falling back to a
// truncated id when the session has no title yet.
func displayTitle(title, id string) string {
	if title != "" {
		return title
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// addPermissionRule installs a pattern-scoped allow rule from an interactive
// "allow always" approval (TQ6): it takes effect for subsequent runs in this
// daemon immediately and is appended to the project config
// (.aegis/config.yaml → permission.rules) so it survives restarts. A rule
// that fails to parse or persist is logged, never fatal — the approval that
// produced it has already been granted.
func (s *Server) addPermissionRule(toolName, pattern string) {
	line := fmt.Sprintf("allow %s(%s)", toolName, pattern)
	rule, err := permission.ParseRule(line)
	if err != nil {
		s.logger.Warn("ignoring invalid approval-derived permission rule", "rule", line, "err", err)
		return
	}
	s.permMu.Lock()
	s.permRules = append(s.permRules, rule)
	s.permMu.Unlock()
	if err := config.AppendProjectPermissionRule(s.workspace, line); err != nil {
		s.logger.Warn("permission rule active for this daemon but not persisted", "rule", line, "err", err)
		return
	}
	s.logger.Info("persisted permission rule from approval", "rule", line)
}

// handleApprove answers a pending interactive approval request. The body must
// be {"approved": bool, "id": "<run id from the approval event>"}. Returns 204
// on success, 404 if no approval is pending for that run id, or 409 if it was
// already answered.
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.ApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "approval id is required")
		return
	}
	val, ok := s.pendingApprovals.Load(req.ID)
	if !ok {
		writeError(w, http.StatusNotFound, "no pending approval for run")
		return
	}
	ch := val.(chan approvalDecision)
	select {
	case ch <- approvalDecision{Approved: req.Approved, AllowAlways: req.AllowAlways, Pattern: req.Pattern}:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusConflict, "approval already answered or not yet requested")
	}
}

// handleSteer injects a mid-run instruction into an active session run. The
// text is delivered to the engine between tool rounds via the steer channel;
// if no run is active for the session the request returns 404.
func (s *Server) handleSteer(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	id := r.PathValue("id")
	var req api.SteerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	val, ok := s.pendingSteers.Load(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no active run for session")
		return
	}
	ch := val.(chan string)
	select {
	case ch <- req.Text:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusTooManyRequests, "steer buffer full; try again momentarily")
	}
}

// handleStopRun cancels the active resumable run for a session (P28.5).
// Needed because a resumable run's context is deliberately decoupled from its
// HTTP request context (so a dropped connection doesn't kill it) — a plain
// (non-resumable) run has no registered cancel and is instead stopped the
// usual way, by the client tearing down its request. Returns 404 if no
// resumable run is currently active for the session.
func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.runs == nil || !s.runs.stopSession(id) {
		writeError(w, http.StatusNotFound, "no resumable run active for session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newRunID returns a short random identifier for a single message run.
func newRunID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func toAPIEvent(ev engine.Event) api.Event {
	out := api.Event{
		Kind:        api.EventKind(ev.Kind),
		Text:        ev.Text,
		Tool:        ev.ToolName,
		ToolInput:   ev.ToolInput,
		ToolID:      ev.ToolID,
		ToolResult:  ev.ToolResult,
		ToolIsError: ev.ToolIsError,
	}
	if ev.Err != nil {
		out.Error = ev.Err.Error()
	}
	if ev.Kind == engine.KindGuard {
		out.Text = ev.GuardReason
		out.GuardRetrying = ev.GuardRetrying
	}
	if ev.Usage != nil {
		out.InputTokens = ev.Usage.InputTokens
		out.OutputTokens = ev.Usage.OutputTokens
		out.CacheReadTokens = ev.Usage.CacheReadTokens
		out.CacheCreationTokens = ev.Usage.CacheCreationTokens
		out.TokensEstimated = ev.Usage.IsEstimated
	}
	out.CostUSD = ev.CostUSD
	return out
}

// sessionSemaphore returns the buffered channel used to serialize runs for a
// session (capacity 1 — only one goroutine holds it at a time).
func (s *Server) sessionSemaphore(id string) chan struct{} {
	v, _ := s.sessionSems.LoadOrStore(id, make(chan struct{}, 1))
	return v.(chan struct{})
}

// expandFileMentions replaces @path#L10-40 tokens in text with the referenced
// file lines so the model sees the content directly (P5.5).
// Tokens must be preceded by whitespace or start-of-text and have the form
// @<relpath>#L<start>-<end> or @<relpath>#<start>-<end>.
// If a token cannot be resolved (file missing, range invalid) it is left as-is.
func expandFileMentions(text, workspace string) string {
	if !strings.Contains(text, "@") || !strings.Contains(text, "#") {
		return text
	}
	fields := strings.Fields(text)
	changed := false
	for i, f := range fields {
		if !strings.HasPrefix(f, "@") || !strings.Contains(f, "#") {
			continue
		}
		atPath, rangeStr, _ := strings.Cut(strings.TrimPrefix(f, "@"), "#")
		if atPath == "" || rangeStr == "" {
			continue
		}
		// Parse #L10-40 or #10-40.
		rangeStr = strings.TrimPrefix(rangeStr, "L")
		var start, end int
		if _, err := fmt.Sscanf(rangeStr, "%d-%d", &start, &end); err != nil || start < 1 || end < start {
			continue
		}
		abs := filepath.Join(workspace, filepath.FromSlash(atPath))
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		if end > len(lines) {
			end = len(lines)
		}
		if start > end {
			// start was past the end of the file even before clamping end
			// (e.g. a stale line reference, or a mention naming a range far
			// beyond a short file) — leave the token as-is rather than
			// slicing with a start past the (now-clamped) end.
			continue
		}
		excerpt := strings.Join(lines[start-1:end], "\n")
		fields[i] = fmt.Sprintf("```\n// @%s#L%d-%d\n%s\n```", atPath, start, end, excerpt)
		changed = true
	}
	if !changed {
		return text
	}
	return strings.Join(fields, " ")
}
