package server

import "context"

// toolCallingWarning returns the notice text for a model that can't drive the
// tool-calling loop, or "" when the model is fine, unknown, out of scope, or
// this session has already been told about this model.
//
// Scoped to local Ollama-style providers via the same isOllamaProvider gate
// probeProviderReachability and `aegis doctor` use: cloud providers have
// well-established tool-calling support, and skipping them keeps the probe off
// the paid-API path entirely.
//
// Called at run start rather than at the three model-selection points P34.2
// originally named (daemon start, PATCH /sessions/{id}, the TUI picker). Run
// start is the one choke point downstream of all of them — it sees the model
// after the persona pin, the per-session override, and P30's routing have been
// resolved, which no selection-site hook can — and it is also the moment the
// model is about to be loaded anyway, so the probe shares that cold load rather
// than adding one. The gate's cache makes the probe itself once per model per
// daemon.
//
// Warning once per session per model is a separate bound from that cache, and
// exists because the probe verdict is not the whole story: a tool-incapable
// model is still perfectly usable for conversation, and repeating a paragraph
// of warning on every single turn of such a session would be nagging rather
// than informing. Re-warning on a model switch is deliberate — that's new
// information about a new model.
func (s *Server) toolCallingWarning(ctx context.Context, sessionID, model string) string {
	if s.toolCalling == nil || !s.isOllamaProvider() {
		return ""
	}
	// The probe must ask for the *same* num_ctx the turn behind it will ask for
	// (P66.20/LLM-10). Its whole justification is that it shares the cold load
	// the run was about to pay for anyway — but toolcallprobe.Run sets no
	// NumCtx of its own, so a bare s.adapter falls through to the adapter's
	// construction-time value, which providerfactory only sets when
	// provider.context_window is configured explicitly. Under the documented
	// auto-detect posture that is zero, Ollama loads the model at its own
	// default 4096, and the first real turn then asks for the resolved window
	// (32768, say) and forces a full unload/reload of the weights — the probe
	// costing a cold load rather than sharing one. Wrapping it in the same
	// per-run decorator newEngine uses makes the two agree, and covers the
	// background conformance trials too: Gate.refine reuses this adapter.
	win, _ := s.effectiveContextWindowFor(ctx, model)
	warn := s.toolCalling.Warning(ctx, s.modelAdapter(win), model)
	if warn == "" {
		return ""
	}
	// Recorded only for models that actually failed the probe, so this map
	// stays bounded by the number of sessions using a broken model — in the
	// normal case it is never written to at all.
	key := sessionID + "\x00" + model
	s.toolCallWarnedMu.Lock()
	defer s.toolCallWarnedMu.Unlock()
	if _, told := s.toolCallWarned[key]; told {
		return ""
	}
	if s.toolCallWarned == nil {
		s.toolCallWarned = make(map[string]struct{})
	}
	s.toolCallWarned[key] = struct{}{}
	return warn
}
