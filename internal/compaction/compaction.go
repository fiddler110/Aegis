// Package compaction keeps conversations within a token budget by summarizing
// older turns with an auxiliary model call (lineage-style compression, as in
// Hermes). Recent turns are preserved verbatim.
package compaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/scottymacleod/agentharness/internal/provider"
)

// Summarizer implements engine.Compactor.
type Summarizer struct {
	adapter      provider.Adapter
	model        string
	maxBudget    int // approximate token budget that triggers compaction
	keepRecent   int // minimum number of trailing messages kept verbatim
	summaryTokens int
}

// Options configures a Summarizer.
type Options struct {
	Adapter       provider.Adapter
	Model         string
	MaxBudget     int // default 120000
	KeepRecent    int // default 8
	SummaryTokens int // default 1024
}

// New constructs a Summarizer.
func New(opts Options) *Summarizer {
	if opts.MaxBudget <= 0 {
		opts.MaxBudget = 120000
	}
	if opts.KeepRecent <= 0 {
		opts.KeepRecent = 8
	}
	if opts.SummaryTokens <= 0 {
		opts.SummaryTokens = 1024
	}
	return &Summarizer{
		adapter:       opts.Adapter,
		model:         opts.Model,
		maxBudget:     opts.MaxBudget,
		keepRecent:    opts.KeepRecent,
		summaryTokens: opts.SummaryTokens,
	}
}

// EstimateTokens approximates token count using a 4-chars-per-token heuristic.
func EstimateTokens(system string, msgs []provider.Message) int {
	chars := len(system)
	for _, m := range msgs {
		for _, b := range m.Content {
			switch v := b.(type) {
			case provider.TextBlock:
				chars += len(v.Text)
			case provider.ToolUseBlock:
				chars += len(v.Name) + len(v.Input)
			case provider.ToolResultBlock:
				chars += len(v.Content)
			}
		}
	}
	return chars / 4
}

// Compact summarizes the older prefix of the conversation if it exceeds the
// budget, returning the rewritten message list. It chooses a boundary that
// preserves tool_use/tool_result pairing by cutting only before an assistant
// message.
func (s *Summarizer) Compact(ctx context.Context, system string, msgs []provider.Message) ([]provider.Message, bool, error) {
	if EstimateTokens(system, msgs) <= s.maxBudget {
		return msgs, false, nil
	}
	boundary := s.boundary(msgs)
	if boundary <= 0 {
		return msgs, false, nil // nothing safe to compact
	}

	prefix := msgs[:boundary]
	summary, err := s.summarize(ctx, prefix)
	if err != nil {
		return msgs, false, err
	}

	out := make([]provider.Message, 0, len(msgs)-boundary+1)
	out = append(out, provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: "Summary of earlier conversation (older turns were compacted):\n\n" + summary}},
	})
	out = append(out, msgs[boundary:]...)
	return out, true, nil
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
				if len(result) > 800 {
					result = result[:800] + "…"
				}
				fmt.Fprintf(&b, "tool result: %s\n", result)
			}
		}
	}
	return b.String()
}
