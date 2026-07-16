package swarm

import (
	"context"
	"testing"
)

// TestInProcessSpawnAttachesBudgetOverride is the FIND-14 regression for the
// in-process backend. SubprocessBackend already gives each worker its own
// guaranteed floor share via WorkerSpec; an in-process teammate has no spec, so
// the share travels on the context. Without it, every teammate checks the same
// live shared total against the same full cap, and one sibling that has already
// spent the budget leaves the next spawn with nothing.
func TestInProcessSpawnAttachesBudgetOverride(t *testing.T) {
	// A sibling has already blown the whole $1.00 / 1000-token budget.
	tracker := &fakeTracker{usd: 1.0, tokens: 1000}

	var gotUSD float64
	var gotTokens int
	var gotOK bool
	run := func(ctx context.Context, _ SpawnConfig) (string, error) {
		gotUSD, gotTokens, gotOK = BudgetOverrideFromContext(ctx)
		return "ok", nil
	}

	b := NewInProcessBackend(run, NewRegistry(), MailboxRoot(t.TempDir()), 1.0, 1000)
	ctx := WithCostTracker(context.Background(), tracker)
	h, err := b.Spawn(ctx, SpawnConfig{Name: "t", Team: "team"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := h.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if !gotOK {
		t.Fatal("no budget override reached the RunFunc; this teammate would check the exhausted shared total against the full cap and get nothing")
	}
	// remainingBudget/remainingTokens floor an exhausted budget at a fair
	// share rather than zero — the point of the fix.
	if gotUSD <= 0 {
		t.Errorf("override USD = %v, want a non-zero floor share despite the budget being spent", gotUSD)
	}
	if gotTokens <= 0 {
		t.Errorf("override tokens = %d, want a non-zero floor share despite the budget being spent", gotTokens)
	}
}

// TestInProcessSpawnNoOverrideWithoutCaps: a caller with no cost caps
// configured has nothing to guarantee a share of, so no override is attached
// and the RunFunc keeps its existing shared-ledger behavior.
func TestInProcessSpawnNoOverrideWithoutCaps(t *testing.T) {
	tracker := &fakeTracker{usd: 0.5, tokens: 500}

	var gotOK bool
	run := func(ctx context.Context, _ SpawnConfig) (string, error) {
		_, _, gotOK = BudgetOverrideFromContext(ctx)
		return "ok", nil
	}

	b := NewInProcessBackend(run, NewRegistry(), MailboxRoot(t.TempDir()), 0, 0)
	ctx := WithCostTracker(context.Background(), tracker)
	h, err := b.Spawn(ctx, SpawnConfig{Name: "t", Team: "team"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := h.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if gotOK {
		t.Error("a budget override was attached despite no configured caps")
	}
}

// TestInProcessSpawnNoOverrideWithoutTracker: Spawn called with a bare context
// carrying no shared ledger (as some tests and detached paths do) has no total
// to compute a share from, so it must not fabricate one.
func TestInProcessSpawnNoOverrideWithoutTracker(t *testing.T) {
	var gotOK bool
	run := func(ctx context.Context, _ SpawnConfig) (string, error) {
		_, _, gotOK = BudgetOverrideFromContext(ctx)
		return "ok", nil
	}

	b := NewInProcessBackend(run, NewRegistry(), MailboxRoot(t.TempDir()), 1.0, 1000)
	h, err := b.Spawn(context.Background(), SpawnConfig{Name: "t", Team: "team"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := h.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if gotOK {
		t.Error("a budget override was attached with no shared tracker to compute a share from")
	}
}
