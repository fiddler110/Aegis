package server

import (
	"regexp"
	"strings"

	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
)

// P9.4 per-turn model routing.
//
// The three existing SmallModel call sites (guardModel in engine_build.go,
// generateTitle in sessions.go, compaction model selection in server.go) all
// route small, non-user-facing auxiliary calls. None of them route the
// actual user-facing agent turn — that's the gap this file closes: an
// opt-in (Provider.TaskRouting) heuristic that sends "simple" turns to
// Provider.SmallModel and leaves everything else on Provider.Model.
//
// The classifier below is a purely local heuristic — regexes and word
// counts, never an extra model call. An LLM call to decide "which LLM to
// call" would spend the very latency/cost this feature exists to save. Every
// signal is deliberately cheap to evaluate and biased toward "complex":
// a false negative (big model on an easy turn) only costs a bit more money,
// while a false positive (small model on a hard turn) produces a wrong or
// unhelpful answer, which is strictly worse. When in doubt, stay expensive.

const (
	// complexWordThreshold: long messages tend to be detailed specs or
	// multi-step instructions rather than quick questions — bias to the
	// primary model past this length even if no other signal fires.
	complexWordThreshold = 40
	// complexCharThreshold catches messages whose length a naive word split
	// would undercount — a single long pasted stack trace, diff, or URL is
	// one "word" but is exactly the kind of dense content that benefits from
	// the stronger model.
	complexCharThreshold = 320
	// complexSentenceThreshold: several sentences bundled into one message
	// usually means several sub-requests stacked together (a compound ask),
	// not one simple question.
	complexSentenceThreshold = 3
	// complexListItemThreshold: two or more list-style lines (numbered or
	// bulleted) is a strong signal the user wrote out an explicit multi-step
	// plan and expects it followed precisely.
	complexListItemThreshold = 2
)

// listItemRe matches a line starting with a bullet ("-", "*", "+") or a
// numbered-list marker ("1.", "2)", ...).
var listItemRe = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+[.)])\s+`)

// sentenceEndRe approximates sentence boundaries by counting terminal
// punctuation followed by whitespace or end-of-string. It's a heuristic, not
// a parser — good enough to distinguish "one quick question" from "several
// asks in one message".
var sentenceEndRe = regexp.MustCompile(`[.!?]+(\s|$)`)

// classifyTurn decides whether a user turn looks "simple" (safe to route to
// the cheaper SmallModel) or "complex" (must stay on the primary Model).
// priorMessages is the session's message history *before* this turn's
// message is appended: if it already contains tool calls, this session is
// mid-flight on something the model judged worth using tools for, and
// bouncing down to a smaller model partway through a task risks breaking it
// for a saving that's marginal at best — so that signal is checked first and
// short-circuits the rest of the message-content analysis entirely.
func classifyTurn(userText string, priorMessages []provider.Message) (complex bool, reason string) {
	if hasPriorToolCalls(priorMessages) {
		return true, "mid-flight session with prior tool calls"
	}

	text := strings.TrimSpace(userText)
	if text == "" {
		// Nothing to classify confidently from (e.g. an image-only message)
		// — stay on the primary model rather than guess.
		return true, "no text to classify"
	}

	if strings.Contains(text, "```") {
		return true, "contains a code fence"
	}

	if n := len(listItemRe.FindAllString(text, -1)); n >= complexListItemThreshold {
		return true, "looks like a multi-step list"
	}

	if words := len(strings.Fields(text)); words > complexWordThreshold {
		return true, "long message (word count)"
	}
	if len(text) > complexCharThreshold {
		return true, "long message (char count)"
	}

	if n := len(sentenceEndRe.FindAllString(text, -1)); n >= complexSentenceThreshold {
		return true, "multiple sentences (compound request)"
	}

	return false, "short, single-part message"
}

// hasPriorToolCalls reports whether any message in the session's history so
// far carries a tool_use or tool_result block.
func hasPriorToolCalls(messages []provider.Message) bool {
	for _, m := range messages {
		for _, b := range m.Content {
			switch b.(type) {
			case provider.ToolUseBlock, provider.ToolResultBlock:
				return true
			}
		}
	}
	return false
}

// routeModel applies P9.4 task routing on top of a base model already
// resolved by resolveModel/personaModel. It only ever changes anything when
// both Provider.TaskRouting is enabled and Provider.SmallModel is
// configured — the same "no small_model configured = no behavior change"
// precedent guardModel and generateTitle already follow — otherwise base is
// returned unchanged with an empty reason (nothing to log). routedToSmall
// distinguishes "classifier ran and kept the primary model" from "routing
// wasn't applicable at all", so callers can log the former without noise
// from the latter.
func (s *Server) routeModel(base, userText string, priorMessages []provider.Message) (model, reason string, routedToSmall bool) {
	if !s.cfg.Provider.TaskRouting || s.cfg.Provider.SmallModel == "" {
		return base, "", false
	}
	complex, reason := classifyTurn(userText, priorMessages)
	if complex {
		return base, reason, false
	}
	return s.cfg.Provider.SmallModel, reason, true
}

// turnModel resolves the model id for one user-facing engine turn: it's
// resolveModel's existing precedence (session /model override > persona
// config override > persona file Model > global Model) with P9.4 routing
// layered on top. Routing never runs when modelOverride is set — an explicit
// per-session /model override (P14.7) always wins over routing, the same way
// it already wins over everything resolveModel would otherwise pick.
func (s *Server) turnModel(p persona.Persona, modelOverride, userText string, priorMessages []provider.Message) (model, reason string, routedToSmall bool) {
	base := s.resolveModel(p, modelOverride)
	if modelOverride != "" {
		return base, "", false
	}
	return s.routeModel(base, userText, priorMessages)
}
