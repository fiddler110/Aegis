package provider

import "context"

// Purpose names the *kind* of call a provider request is, independent of what
// the request says (P67.3). Everything Aegis sends goes through one adapter
// seam — the user's turn, a compaction summary, the output guard's second pass,
// a debate role, a swarm sub-agent, the tool-call probe, a session title, an
// MCP sampling request — and until this tag existed the seam could not tell
// them apart. One retry policy therefore served all of them, which is wrong in
// both directions at once: a title generation nobody is waiting on backs off
// four times against a rate-limited backend and amplifies exactly the load that
// rate-limited it, while the user's own turn — the one call a human is watching
// — gets no more patience than the title does.
//
// The tag is deliberately about the *caller*, not the content. Two identical
// message lists sent by the engine and by the summarizer are different calls,
// and only the caller knows which is which.
//
// P67.6 needs this for a second reason: a compaction trigger that fires on the
// main conversation must not fire for a sub-agent or an analysis-only caller,
// and the purpose is the correct discriminator for that decision. Keep new
// purposes descriptive of the caller rather than of the policy they happen to
// get today — the policy table below is allowed to change; the caller is a fact.
type Purpose string

const (
	// PurposeUnspecified is the zero value: a request whose caller has not
	// declared itself. It resolves to the baseline policy — today's behavior,
	// unchanged — so adding this tag regresses nothing that was not updated,
	// and a call path added later cannot silently acquire the aggressive
	// setting by forgetting to say what it is.
	PurposeUnspecified Purpose = ""

	// PurposeForeground is the user's own turn: a human is watching this
	// specific stream and its failure is the failure they see.
	PurposeForeground Purpose = "foreground"

	// PurposeCompaction is the compaction summarizer.
	PurposeCompaction Purpose = "compaction"

	// PurposeGuard is the output guard's second pass.
	PurposeGuard Purpose = "guard"

	// PurposeSubAgent is a swarm sub-agent's turn (goroutine or subprocess).
	PurposeSubAgent Purpose = "subagent"

	// PurposeDebate is a debate role — proposer, critic, rebutter, arbitrator.
	PurposeDebate Purpose = "debate"

	// PurposeProbe is the tool-calling smoke probe.
	PurposeProbe Purpose = "probe"

	// PurposeTitle is the small-model session-title generation, which already
	// has a non-model fallback (deriveTitle) for when it does not happen.
	PurposeTitle Purpose = "title"

	// PurposeSampling is an MCP server's sampling request routed back through
	// our adapter. It is somebody else's call, made on our credentials.
	PurposeSampling Purpose = "sampling"
)

// urgency classifies a Purpose by the only question a retry policy actually
// needs answered: who is blocked while this call is retrying?
type urgency int

const (
	// urgencyAttended — the call is on the critical path of a turn a human is
	// waiting for, but is not that turn. Compaction, the guard, a sub-agent and
	// a debate role are all here: failing them fast does not spare the user
	// anything, because the turn they serve fails with them. This is the
	// baseline, and it is also where PurposeUnspecified lands.
	urgencyAttended urgency = iota
	// urgencyForeground — the user's own turn. Worth more patience than the
	// baseline precisely because a human is absorbing the wait either way, and
	// the alternative to one more retry is showing them an error.
	urgencyForeground
	// urgencyBackground — nobody is waiting and the failure is invisible or
	// already has a fallback. These fail fast: retrying them during a capacity
	// or rate-limit window is load added to a backend that is already failing,
	// spent on output no one will read this minute.
	urgencyBackground
)

// urgencyOf maps a Purpose to its retry urgency. Anything absent — including a
// purpose added later and not listed here — is attended, i.e. today's policy.
// That default is the conservative one on purpose: getting the baseline when
// you meant background costs some wasted retries, while getting background when
// you meant foreground drops a user's turn.
func urgencyOf(p Purpose) urgency {
	switch p {
	case PurposeForeground:
		return urgencyForeground
	case PurposeProbe, PurposeTitle, PurposeSampling:
		return urgencyBackground
	default:
		// PurposeUnspecified, PurposeCompaction, PurposeGuard,
		// PurposeSubAgent, PurposeDebate.
		return urgencyAttended
	}
}

// purposeKey is the context key carrying a run-scoped default Purpose.
type purposeKeyType struct{}

var purposeKey purposeKeyType

// WithPurpose returns a context declaring the default Purpose for every
// provider request made under it that does not carry one of its own. It is for
// *launchers* — the code that starts a run knows what kind of run it is, and
// threading that through every intermediate signature would touch code that has
// no business knowing about retry policy at all.
//
// A request's own Purpose still wins (see EffectivePurpose): the component
// making a call knows what that call is better than the launcher does, and
// compaction inside a foreground run is still compaction.
func WithPurpose(ctx context.Context, p Purpose) context.Context {
	if p == PurposeUnspecified {
		return ctx
	}
	return context.WithValue(ctx, purposeKey, p)
}

// PurposeFrom returns the run-scoped default Purpose carried by ctx, or
// PurposeUnspecified when none was declared.
func PurposeFrom(ctx context.Context) Purpose {
	if ctx == nil {
		return PurposeUnspecified
	}
	p, _ := ctx.Value(purposeKey).(Purpose)
	return p
}

// EffectivePurpose resolves the Purpose that governs one request: the
// request's own tag when it has one, otherwise the run-scoped default from
// ctx, otherwise PurposeUnspecified.
//
// The per-call tag beating the run-scoped one is the whole reason both exist.
// A launcher says "this whole run is a sub-agent"; the summarizer inside it
// still says "this particular call is a compaction". Reversing the precedence
// would erase exactly the distinction P67.6 needs.
func EffectivePurpose(ctx context.Context, req Request) Purpose {
	if req.Purpose != PurposeUnspecified {
		return req.Purpose
	}
	return PurposeFrom(ctx)
}
