package provider

import (
	"context"
	"testing"
	"time"
)

// TestEffectivePurpose_RequestBeatsContext pins the precedence P67.3 depends
// on: a launcher's run-scoped declaration is a default, and the component
// making a particular call overrides it. Reversing this would make every
// compaction inside a foreground run indistinguishable from the turn it is
// compacting, which is exactly the distinction P67.6 is built on.
func TestEffectivePurpose_RequestBeatsContext(t *testing.T) {
	ctx := WithPurpose(context.Background(), PurposeSubAgent)

	if got := EffectivePurpose(ctx, Request{Purpose: PurposeCompaction}); got != PurposeCompaction {
		t.Errorf("request tag must win: got %q want %q", got, PurposeCompaction)
	}
	if got := EffectivePurpose(ctx, Request{}); got != PurposeSubAgent {
		t.Errorf("untagged request must fall back to the context: got %q want %q", got, PurposeSubAgent)
	}
	if got := EffectivePurpose(context.Background(), Request{}); got != PurposeUnspecified {
		t.Errorf("neither declared: got %q want %q", got, PurposeUnspecified)
	}
	//nolint:staticcheck // a nil context is what a zero-value struct field hands us
	if got := EffectivePurpose(nil, Request{}); got != PurposeUnspecified {
		t.Errorf("nil context: got %q want %q", got, PurposeUnspecified)
	}
	// WithPurpose(Unspecified) must not install a value that shadows an outer
	// declaration — "I don't know" is not a declaration.
	outer := WithPurpose(context.Background(), PurposeForeground)
	if got := PurposeFrom(WithPurpose(outer, PurposeUnspecified)); got != PurposeForeground {
		t.Errorf("unspecified must not shadow an outer purpose: got %q", got)
	}
}

// TestUrgencyOf_ClassifiesEveryPurpose is the classification's written record.
// Every constant is listed, so adding one and forgetting to think about it
// leaves a visible hole here rather than silently inheriting a policy.
func TestUrgencyOf_ClassifiesEveryPurpose(t *testing.T) {
	cases := []struct {
		p    Purpose
		want urgency
	}{
		{PurposeUnspecified, urgencyAttended}, // the conservative default
		{PurposeForeground, urgencyForeground},
		{PurposeCompaction, urgencyAttended}, // the turn it serves fails with it
		{PurposeGuard, urgencyAttended},
		{PurposeSubAgent, urgencyAttended},
		{PurposeDebate, urgencyAttended},
		{PurposeProbe, urgencyBackground},
		{PurposeTitle, urgencyBackground},
		{PurposeSampling, urgencyBackground},
		{Purpose("invented-later"), urgencyAttended}, // unknown -> baseline
	}
	for _, c := range cases {
		if got := urgencyOf(c.p); got != c.want {
			t.Errorf("urgencyOf(%q) = %d, want %d", c.p, got, c.want)
		}
	}
}

// TestPolicyFor_DerivesFromTheConfiguredBaseline checks the derivation itself,
// including the direction that matters most: an operator's configured numbers
// stay the reference point, and background never *raises* a baseline that was
// already lower than the fail-fast setting.
func TestPolicyFor_DerivesFromTheConfiguredBaseline(t *testing.T) {
	base := RetryPolicy{MaxRetries: 4, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}

	if got := policyFor(base, PurposeCompaction); got != base {
		t.Errorf("attended must be the baseline unchanged: got %+v want %+v", got, base)
	}
	if got := policyFor(base, PurposeUnspecified); got != base {
		t.Errorf("untagged must be the baseline unchanged: got %+v want %+v", got, base)
	}

	fg := policyFor(base, PurposeForeground)
	if fg.MaxRetries != base.MaxRetries+foregroundExtraRetries {
		t.Errorf("foreground retries = %d, want %d", fg.MaxRetries, base.MaxRetries+foregroundExtraRetries)
	}
	if fg.MaxDelay != base.MaxDelay || fg.BaseDelay != base.BaseDelay {
		t.Errorf("foreground must not touch the delays: got %+v", fg)
	}

	bg := policyFor(base, PurposeTitle)
	if bg.MaxRetries != backgroundMaxRetries {
		t.Errorf("background retries = %d, want %d", bg.MaxRetries, backgroundMaxRetries)
	}
	if bg.MaxDelay != backgroundMaxDelay {
		t.Errorf("background max delay = %v, want %v", bg.MaxDelay, backgroundMaxDelay)
	}

	// A baseline already stricter than the background setting keeps its own
	// numbers: these are floors on how fast background fails, not a mandate.
	strict := RetryPolicy{MaxRetries: 0, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	if got := policyFor(strict, PurposeProbe); got != strict {
		t.Errorf("a stricter baseline must survive: got %+v want %+v", got, strict)
	}
}

// TestRetry_PurposePicksThePolicy is the end-to-end assertion: the same failing
// backend, the same configured policy, three purposes, three attempt counts.
// It counts attempts rather than asserting a policy struct, because the struct
// being right is not the claim — the number of times a struggling backend gets
// hit is.
func TestRetry_PurposePicksThePolicy(t *testing.T) {
	base := RetryPolicy{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	alwaysDown := func() []error {
		errs := make([]error, 10)
		for i := range errs {
			errs[i] = NewHTTPError("fake", 503, "", "capacity")
		}
		return errs
	}

	cases := []struct {
		name  string
		req   Request
		ctx   context.Context
		calls int
	}{
		{"foreground retries harder", Request{Purpose: PurposeForeground}, context.Background(), 1 + 2 + foregroundExtraRetries},
		{"attended keeps the baseline", Request{Purpose: PurposeGuard}, context.Background(), 3},
		{"untagged keeps the baseline", Request{}, context.Background(), 3},
		{"background fails fast", Request{Purpose: PurposeTitle}, context.Background(), 1 + backgroundMaxRetries},
		{"context default applies to an untagged request", Request{}, WithPurpose(context.Background(), PurposeProbe), 1 + backgroundMaxRetries},
		{"request tag overrides the context", Request{Purpose: PurposeForeground}, WithPurpose(context.Background(), PurposeProbe), 1 + 2 + foregroundExtraRetries},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeAdapter{errs: alwaysDown()}
			r := newTestRetry(f, base, nil)
			if _, err := r.Stream(c.ctx, c.req); err == nil {
				t.Fatal("expected the backend's error to surface")
			}
			if f.calls != c.calls {
				t.Fatalf("got %d attempts, want %d", f.calls, c.calls)
			}
		})
	}
}

// TestRetry_BackgroundClampsRetryAfterHarder covers the one interaction P67.3
// calls out by name. The Retry-After clamp is unchanged in mechanism — it still
// clamps at MaxDelay, which is what keeps provider backoff inside the 900s
// MaxTurnStall bound — but a background call's MaxDelay is lower, so a server
// asking for a long wait holds a background slot for less time. The clamp is
// never loosened.
func TestRetry_BackgroundClampsRetryAfterHarder(t *testing.T) {
	base := RetryPolicy{MaxRetries: 4, BaseDelay: time.Millisecond, MaxDelay: time.Hour}

	var fgDelays []time.Duration
	fg := newTestRetry(&fakeAdapter{errs: []error{NewHTTPError("fake", 429, "120", "slow down"), nil}}, base, &fgDelays)
	if _, err := fg.Stream(context.Background(), Request{Purpose: PurposeForeground}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fgDelays) != 1 || fgDelays[0] != 120*time.Second {
		t.Fatalf("foreground should honor Retry-After under its MaxDelay: %v", fgDelays)
	}

	var bgDelays []time.Duration
	bg := newTestRetry(&fakeAdapter{errs: []error{NewHTTPError("fake", 429, "120", "slow down"), nil}}, base, &bgDelays)
	if _, err := bg.Stream(context.Background(), Request{Purpose: PurposeTitle}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bgDelays) != 1 || bgDelays[0] != backgroundMaxDelay {
		t.Fatalf("background should clamp Retry-After to %v, got %v", backgroundMaxDelay, bgDelays)
	}
}

// TestRetry_DisabledStaysDisabledForForeground guards the delta: foreground's
// bonus attempts are added to a configured baseline, so a deployment that
// turned retries off must not acquire two of them.
func TestRetry_DisabledStaysDisabledForForeground(t *testing.T) {
	inner := &fakeAdapter{errs: []error{NewHTTPError("fake", 503, "", "x")}}
	got := WithRetry(inner, RetryPolicy{MaxRetries: 0}, nil)
	if got != Adapter(inner) {
		t.Fatal("retries disabled must return the inner adapter undecorated")
	}
}
