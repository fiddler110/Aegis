package toolcallprobe

import (
	"context"
	"sync"
	"testing"
)

// fakeStore is an in-memory Store, standing in for *modelcaps.Store so this
// package's tests stay free of the store's file I/O.
type fakeStore struct {
	mu     sync.Mutex
	rec    map[string][4]int // verdict-index, trials, calls, noVerdict
	writes int
}

func newFakeStore() *fakeStore { return &fakeStore{rec: map[string][4]int{}} }

func verdictIndex(v string) int {
	if v == "ok" {
		return 1
	}
	return 2
}

func indexVerdict(i int) string {
	if i == 1 {
		return "ok"
	}
	return "unsupported"
}

func (f *fakeStore) ToolCalling(model string) (string, int, int, int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rec[model]
	if !ok {
		return "", 0, 0, 0, false
	}
	return indexVerdict(r[0]), r[1], r[2], r[3], true
}

func (f *fakeStore) SetToolCalling(model, verdict string, trials, calls, noVerdict int) {
	if verdict != "ok" && verdict != "unsupported" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec[model] = [4]int{verdictIndex(verdict), trials, calls, noVerdict}
	f.writes++
}

func (f *fakeStore) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

// TestGatePersistsAndReusesTheSample is the P53.5 headline: the sample a gate
// measures must survive process exit, so the next daemon reuses it instead of
// re-running five generations on a model that generates at single-digit
// tokens/sec.
func TestGatePersistsAndReusesTheSample(t *testing.T) {
	store := newFakeStore()

	a := &sequenceAdapter{trials: []trial{called(1), refused(), called(1), refused(), called(1)}}
	g := NewGate(WithTrials(5), WithStore(store))
	if v := g.Verdict(context.Background(), a, "m"); v != OK {
		t.Fatalf("verdict = %v, want OK", v)
	}
	g.wait()
	g.Close()

	verdict, trials, calls, noVerdict, ok := store.ToolCalling("m")
	if !ok {
		t.Fatal("nothing persisted")
	}
	if verdict != "ok" || trials != 5 || calls != 3 || noVerdict != 0 {
		t.Fatalf("persisted (%q, %d, %d, %d), want (ok, 5, 3, 0)", verdict, trials, calls, noVerdict)
	}

	// A fresh gate over a fresh adapter: the persisted sample must answer
	// without a single probe reaching the model.
	next := &sequenceAdapter{} // any call at all fails the script
	g2 := NewGate(WithTrials(5), WithStore(store))
	defer g2.Close()
	if v := g2.Verdict(context.Background(), next, "m"); v != OK {
		t.Fatalf("restarted gate verdict = %v, want OK from the persisted record", v)
	}
	g2.wait()
	if n := next.calls.Load(); n != 0 {
		t.Fatalf("restarted gate probed %d times, want 0 — the whole point is not re-paying it", n)
	}
	c, ok := g2.Conformance("m")
	if !ok || c.Trials != 5 || c.ToolCallTrials != 3 {
		t.Fatalf("restored conformance = %+v, want 5 trials / 3 calls", c)
	}
	if rate, ok := c.Rate(); !ok || rate != 0.6 {
		t.Errorf("restored rate = (%v, %v), want (0.6, true)", rate, ok)
	}
}

// TestGateNeverPersistsAnUnreachedVerdict is the P34.2 contract, one layer
// down. A model truncated before it could answer is Unknown — not a failure —
// and writing that to disk would carry the false accusation across restarts
// instead of merely across sessions.
func TestGateNeverPersistsAnUnreachedVerdict(t *testing.T) {
	store := newFakeStore()
	a := &sequenceAdapter{trials: []trial{truncated(), truncated()}}
	g := NewGate(WithTrials(2), WithStore(store))
	defer g.Close()

	if v := g.Verdict(context.Background(), a, "m"); v != Unknown {
		t.Fatalf("verdict = %v, want Unknown", v)
	}
	g.wait()
	if _, _, _, _, ok := store.ToolCalling("m"); ok {
		t.Fatal("an all-truncated sample was persisted as a verdict")
	}
	if store.writeCount() != 0 {
		t.Fatalf("store written %d times for a no-verdict sample", store.writeCount())
	}
}

// TestGatePersistsTrialOneBeforeRefinementCompletes guards the shutdown case: a
// daemon that exits mid-refinement should still have saved the verdict it
// actually reached, rather than losing the probe entirely.
func TestGatePersistsTrialOneBeforeRefinementCompletes(t *testing.T) {
	store := newFakeStore()
	release := make(chan struct{})
	a := &blockingAdapter{release: release}
	g := NewGate(WithTrials(3), WithStore(store))

	if v := g.Verdict(context.Background(), a, "m"); v != OK {
		t.Fatalf("verdict = %v, want OK", v)
	}
	// Refinement is still blocked on the channel, so this can only be trial 1.
	verdict, trials, _, _, ok := store.ToolCalling("m")
	if !ok || verdict != "ok" || trials != 1 {
		t.Fatalf("persisted (%q, %d, ok=%v), want (ok, 1, true) from the blocking trial alone", verdict, trials, ok)
	}
	close(release)
	g.Close()
}

// TestGateWithoutStoreIsUnchanged pins that the pre-P53.5 behavior is exactly
// what a gate with no store still does: probe every model once per process.
func TestGateWithoutStoreIsUnchanged(t *testing.T) {
	a := &sequenceAdapter{trials: []trial{called(1)}}
	g := NewGate()
	defer g.Close()
	if v := g.Verdict(context.Background(), a, "m"); v != OK {
		t.Fatalf("verdict = %v, want OK", v)
	}
	if n := a.calls.Load(); n != 1 {
		t.Fatalf("probed %d times, want 1", n)
	}
}
