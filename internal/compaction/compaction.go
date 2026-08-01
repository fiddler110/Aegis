// Package compaction keeps conversations within a token budget by summarizing
// older turns with an auxiliary model call (lineage-style compression, as in
// Hermes). Recent turns are preserved verbatim.
package compaction

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
)

const (
	// largeContextWindowThreshold is the token count above which a context window
	// is considered "large" and uses an absolute buffer instead of a ratio.
	largeContextWindowThreshold = 200_000
	// largeContextWindowBuffer is the minimum remaining tokens before compaction
	// triggers for large context windows.
	largeContextWindowBuffer = 20_000
	// smallContextWindowRatio is the fraction of the context window that must
	// remain free before compaction triggers for small context windows.
	smallContextWindowRatio = 0.20
)

// Summarizer implements engine.Compactor.
type Summarizer struct {
	adapter       provider.Adapter
	model         string
	maxBudget     int          // fallback fixed budget when contextWindow == 0; 0 = skip
	contextWindow atomic.Int64 // model context window in tokens; 0 = use maxBudget. Atomic: updatable after construction (late Ollama detection) while Compact runs concurrently.
	keepRecent    int          // minimum number of trailing messages kept verbatim
	summaryTokens int
}

// Options configures a Summarizer.
type Options struct {
	Adapter provider.Adapter
	Model   string
	// ContextWindow is the model's context window in tokens. When > 0 it drives
	// smart compaction thresholds. When 0, MaxBudget is used as a fixed fallback.
	ContextWindow int
	// MaxBudget is a fixed token budget. Used only when ContextWindow == 0.
	// A value of 0 means skip auto-compaction entirely (e.g. for local models
	// whose context size is not known). Defaults to 120 000 when ContextWindow
	// is also 0, for backward-compat with cloud providers.
	MaxBudget     int
	KeepRecent    int // default 8
	SummaryTokens int // default 1024
}

// New constructs a Summarizer.
func New(opts Options) *Summarizer {
	if opts.ContextWindow <= 0 && opts.MaxBudget <= 0 {
		// Neither set: keep the existing default for cloud providers.
		opts.MaxBudget = 120_000
	}
	if opts.KeepRecent <= 0 {
		opts.KeepRecent = 8
	}
	if opts.SummaryTokens <= 0 {
		opts.SummaryTokens = 1024
	}
	s := &Summarizer{
		adapter:       opts.Adapter,
		model:         opts.Model,
		maxBudget:     opts.MaxBudget,
		keepRecent:    opts.KeepRecent,
		summaryTokens: opts.SummaryTokens,
	}
	s.contextWindow.Store(int64(opts.ContextWindow))
	return s
}

// SetContextWindow updates the context window driving compaction thresholds.
// Safe to call while Compact is running; used when the effective window is
// only learned after construction (e.g. Ollama reports the loaded model's
// real allocation once the first request has loaded it).
func (s *Summarizer) SetContextWindow(tokens int) {
	s.contextWindow.Store(int64(tokens))
}

// ContextWindow reports the window currently driving compaction thresholds (0
// when none is known and the fixed MaxBudget applies instead). It exists so a
// caller that retunes the summarizer can assert which model's window it ended
// up with — the daemon tunes it from the model compaction actually runs on,
// which is not necessarily the global one (P52.1).
func (s *Summarizer) ContextWindow() int {
	return int(s.contextWindow.Load())
}

// shouldCompact reports whether the current estimated token count warrants
// compaction given the configured context window or fixed budget.
func (s *Summarizer) shouldCompact(estimated int) bool {
	if win := int(s.contextWindow.Load()); win > 0 {
		remaining := win - estimated
		if win > largeContextWindowThreshold {
			return remaining < largeContextWindowBuffer
		}
		return remaining < int(float64(win)*smallContextWindowRatio)
	}
	if s.maxBudget <= 0 {
		return false
	}
	return estimated > s.maxBudget
}

// EstimateTokens approximates token count using the shared script-aware
// heuristic (tokenest). It previously maintained a separate flat chars/4
// estimate, which undercounted CJK/non-ASCII-heavy conversations and could
// silently no-op a compaction the engine's own script-aware estimator had
// already decided was needed (P41.1) — so both now share one implementation.
func EstimateTokens(system string, msgs []provider.Message) int {
	return tokenest.Messages(system, msgs)
}

// Compact always runs the cheap deterministic prune pass (stale tool
// results, already-committed write/edit payloads — see pruneStaleToolResults)
// and additionally summarizes the older prefix of the conversation with an
// LLM call if it still exceeds the budget after that pass, returning the
// rewritten message list. It chooses a boundary that preserves
// tool_use/tool_result pairing by cutting only before an assistant message.
func (s *Summarizer) Compact(ctx context.Context, system string, msgs []provider.Message) ([]provider.Message, bool, error) {
	return s.compact(ctx, system, msgs, false)
}

// ForceCompact runs the same compaction pass unconditionally, skipping the
// budget checks Compact uses to decide whether compaction is warranted — for
// a user-triggered manual compaction (e.g. the TUI `/compact` command) ahead
// of a tool-heavy stretch the user knows is coming, rather than the automatic
// budget-driven path.
func (s *Summarizer) ForceCompact(ctx context.Context, system string, msgs []provider.Message) ([]provider.Message, bool, error) {
	return s.compact(ctx, system, msgs, true)
}

func (s *Summarizer) compact(ctx context.Context, system string, msgs []provider.Message, force bool) ([]provider.Message, bool, error) {
	// Deterministic pre-pass — drop stale tool results (superseded file
	// reads, old search dumps, already-committed write/edit payloads) on
	// every call, independent of the budget gate below. It costs no LLM call
	// and no I/O, only a scan over messages already in memory, so there is no
	// reason to defer it until the conversation is already near the context
	// window: a long tool-heavy run (many files written/edited in one
	// session — e.g. a skill driving a multi-file build to completion) would
	// otherwise keep resending every already-committed payload verbatim for
	// however many turns it takes to first cross the threshold, which is
	// exactly the peak-context pressure this pass exists to relieve.
	msgs, prunedChars := pruneStaleToolResults(msgs, s.keepRecent)
	changedByPrune := prunedChars > 0

	if !force && !s.shouldCompact(EstimateTokens(system, msgs)) {
		return msgs, changedByPrune, nil
	}

	boundary := s.boundary(msgs)
	if boundary <= 0 {
		return msgs, changedByPrune, nil // nothing more safe to compact
	}

	prefix := msgs[:boundary]
	summary, err := s.summarize(ctx, prefix)
	if err != nil {
		return msgs, changedByPrune, err
	}

	out := make([]provider.Message, 0, len(msgs)-boundary+1)
	out = append(out, provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: "Summary of earlier conversation (older turns were compacted):\n\n" + summary}},
	})
	out = append(out, msgs[boundary:]...)
	return out, true, nil
}

// FallbackCompact deterministically shortens the conversation without an LLM
// call, for use when the LLM summarizer has failed repeatedly (P28.4 —
// observed live against local models like qwythos:latest/gpt-oss:20b, which
// intermittently return empty output from the summarization prompt). It
// reuses the same boundary selection as Compact/ForceCompact — protecting
// the keepRecent tail and never splitting a tool_use/tool_result pair — but
// replaces the summarized prefix with a terse, deterministically generated
// note (message/tool-call counts) instead of an AI-generated summary, so a
// broken summarizer can never block context from shrinking. Unlike Compact,
// this cannot itself fail: worst case is changed=false when there is nothing
// safe to cut, matching Compact/ForceCompact's own no-op case.
func (s *Summarizer) FallbackCompact(msgs []provider.Message) ([]provider.Message, bool) {
	boundary := s.boundary(msgs)
	if boundary <= 0 {
		return msgs, false
	}
	prefix := msgs[:boundary]
	out := make([]provider.Message, 0, len(msgs)-boundary+1)
	out = append(out, provider.Message{
		Role: provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: "Earlier conversation was dropped by deterministic fallback " +
			"compaction (the AI summarizer failed repeatedly, so no AI-generated summary is available):\n\n" + fallbackNote(prefix)}},
	})
	out = append(out, msgs[boundary:]...)
	return out, true
}

// fallbackNote describes what a deterministic fallback compaction dropped —
// message and tool-call counts, and which tools were used — so the model at
// least knows something happened here, even without the verbatim content or
// an LLM's interpretation of it.
func fallbackNote(msgs []provider.Message) string {
	var userTurns, assistantTurns, toolCalls int
	var toolNames []string
	seen := make(map[string]bool)
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleUser:
			userTurns++
		case provider.RoleAssistant:
			assistantTurns++
		}
		for _, blk := range m.Content {
			if tu, ok := blk.(provider.ToolUseBlock); ok {
				toolCalls++
				if !seen[tu.Name] {
					seen[tu.Name] = true
					toolNames = append(toolNames, tu.Name)
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d earlier message(s) (%d user, %d assistant) covering %d tool call(s) were dropped.",
		len(msgs), userTurns, assistantTurns, toolCalls)
	if len(toolNames) > 0 {
		fmt.Fprintf(&b, " Tools used: %s.", strings.Join(toolNames, ", "))
	}
	b.WriteString(" Earlier file contents, decisions, and open tasks from this span are no longer available verbatim — re-read files or ask the user if needed.")
	return b.String()
}

// boundary returns the index of the first assistant message at or after the
// keep-recent cutoff, so the kept suffix starts cleanly and the summarized
// prefix never splits a tool_use/tool_result pair.
func (s *Summarizer) boundary(msgs []provider.Message) int {
	start := len(msgs) - s.keepRecent
	if start < 1 {
		return 0
	}
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role == provider.RoleAssistant {
			return i
		}
	}
	return 0
}

// summarize asks the model to condense the prefix transcript.
func (s *Summarizer) summarize(ctx context.Context, prefix []provider.Message) (string, error) {
	transcript := renderTranscript(prefix)
	req := provider.Request{
		Model:     s.model,
		MaxTokens: s.summaryTokens,
		System:    "You compress conversation history. Produce a concise but complete summary that preserves: decisions made, facts established, file paths and identifiers, tool results that matter, and any open tasks or unresolved questions. Use terse bullet points.",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "Summarize this conversation so far:\n\n" + transcript}}},
		},
	}
	stream, err := s.adapter.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range stream {
		switch ev.Type {
		case provider.EventTextDelta:
			b.WriteString(ev.Text)
		case provider.EventError:
			return "", ev.Err
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("summarizer returned empty output")
	}
	return out, nil
}

func renderTranscript(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		for _, blk := range m.Content {
			switch v := blk.(type) {
			case provider.TextBlock:
				fmt.Fprintf(&b, "%s: %s\n", m.Role, v.Text)
			case provider.ToolUseBlock:
				fmt.Fprintf(&b, "%s called tool %s(%s)\n", m.Role, v.Name, string(v.Input))
			case provider.ToolResultBlock:
				result := v.Content
				if len([]rune(result)) > 800 {
					result = string([]rune(result)[:800]) + "…"
				}
				fmt.Fprintf(&b, "tool result: %s\n", result)
			}
		}
	}
	return b.String()
}
