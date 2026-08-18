package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// qwen35KVPerToken is the calibration figure P69.5 measured: 33 blocks x 4 KV
// heads x (256+256) dims at f16 = 132 KiB per token. The stub below reports that
// geometry so the windows these tests plan are the ones the real arithmetic
// would produce, not a fixture-shaped approximation of them.
const qwen35KVPerToken = 33 * 4 * (256 + 256) * 2

// fakeOllama serves the two endpoints resident-set planning reads: /api/show for
// each model's KV geometry and /api/ps for what is currently loaded (which is
// where the resident weight figure comes from — /api/tags' on-disk size is wrong
// by the size of a never-resident vision projector).
func fakeOllama(t *testing.T, loadedWindow int, weights map[string]int64) *httptest.Server {
	t.Helper()
	modelInfo := map[string]any{
		"general.architecture":           "qwen3",
		"qwen3.block_count":              33,
		"qwen3.attention.head_count_kv":  4,
		"qwen3.attention.key_length":     256,
		"qwen3.attention.value_length":   256,
		"qwen3.context_length":           262144,
		"qwen3.attention.sliding_window": 0,
		"qwen3.attention.head_count":     32,
		"qwen3.embedding_length":         8192,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
			Name  string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		name := req.Model
		if name == "" {
			name = req.Name
		}
		if _, ok := weights[name]; !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model_info": modelInfo})
	})
	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, r *http.Request) {
		var models []map[string]any
		for name, wt := range weights {
			size := wt + int64(loadedWindow)*qwen35KVPerToken
			models = append(models, map[string]any{
				"name":           name,
				"model":          name,
				"context_length": loadedWindow,
				"size":           size,
				"size_vram":      size,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func residentSetServer(t *testing.T, budgetGB float64, weights map[string]int64) *Server {
	t.Helper()
	ts := fakeOllama(t, 16000, weights)
	s := &Server{
		cfg: &config.Config{Provider: config.ProviderConfig{
			Default:      "ollama",
			Model:        "primary",
			BaseURL:      ts.URL,
			VRAMBudgetGB: budgetGB,
			MaxTokens:    4096,
		}},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ollamaBase: ts.URL,
	}
	return s
}

func gib(f float64) int64 { return int64(f * float64(int64(1)<<30)) }

// The claim's whole contract: windows move for the duration of the set and come
// back afterwards. Both transitions leave the entry non-final, because the plan
// is a prediction (the next run's /api/ps reading is the verdict) and so is the
// restored solo window (the runner is still loaded at the planned size until
// something reloads it).
func TestResidentSetClaimInstallsAndRestores(t *testing.T) {
	s := residentSetServer(t, 14.5, map[string]int64{"primary": gib(4), "arbiter": gib(3)})
	s.ctxWinMu.Lock()
	s.setWindowLocked("primary", ctxWinEntry{win: 51200, src: "config", final: true, max: 262144})
	s.setWindowLocked("arbiter", ctxWinEntry{win: 40000, src: "ollama:loaded", final: true, max: 131072})
	s.ctxWinMu.Unlock()

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	s.ctxWinMu.Lock()
	e, _ := s.windowLocked("primary")
	arb, hadArb := s.windowLocked("arbiter")
	s.ctxWinMu.Unlock()
	if e.win >= 51200 {
		t.Errorf("primary window = %d during the claim, want it shrunk from 51200 to make room for the arbiter", e.win)
	}
	if e.src != residentSetSrc {
		t.Errorf("src = %q during the claim, want %q so the logs say why the window moved", e.src, residentSetSrc)
	}
	if e.final {
		t.Error("a planned window is a prediction and must stay non-final so /api/ps re-measures it")
	}
	// The training maximum is the ceiling the phased drive's escalation climbs
	// toward, and it is a property of the model, not of the claim — losing it
	// here would silently disable that escalation for the debate's duration.
	// Asserted on the arbiter because the *global* model's entry has never
	// carried a max: windowLocked synthesizes it from the ctxWin/Src/Final
	// fields, which predate the field (P52.1).
	if arb.max != 131072 {
		t.Errorf("arbiter max = %d, want the detected 131072 carried through the claim", arb.max)
	}
	if !hadArb || arb.win <= 0 {
		t.Fatalf("arbiter got no planned window (%+v)", arb)
	}
	if arb.win != e.win {
		t.Errorf("windows differ: primary %d, arbiter %d — the split is by equal tokens", e.win, arb.win)
	}

	release()

	s.ctxWinMu.Lock()
	after, _ := s.windowLocked("primary")
	afterArb, _ := s.windowLocked("arbiter")
	s.ctxWinMu.Unlock()
	if after.win != 51200 || after.src != "config" {
		t.Errorf("after release: %d/%q, want the pre-claim 51200/config back", after.win, after.src)
	}
	if after.final {
		t.Error("the restored solo window is not authoritative until something reloads the runner")
	}
	if afterArb.win != 40000 || afterArb.src != "ollama:loaded" || afterArb.max != 131072 {
		t.Errorf("arbiter after release: %+v, want the pre-claim 40000/ollama:loaded/max 131072", afterArb)
	}
}

// A model with no entry before the claim had never been detected, and that is a
// real state. Restoring a fabricated entry over it would pin a planned window as
// that model's permanent answer, so release drops it back into the
// detect-on-first-use path instead.
func TestResidentSetClaimDropsEntriesItInvented(t *testing.T) {
	s := residentSetServer(t, 14.5, map[string]int64{"primary": gib(4), "arbiter": gib(3)})

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	s.ctxWinMu.Lock()
	_, during := s.ctxWinByModel["arbiter"]
	s.ctxWinMu.Unlock()
	if !during {
		t.Fatal("the arbiter got no planned entry")
	}

	release()
	s.ctxWinMu.Lock()
	_, after := s.ctxWinByModel["arbiter"]
	s.ctxWinMu.Unlock()
	if after {
		t.Error("a claim-invented entry outlived the claim")
	}
}

// The anti-thrash property, named so the reason survives a refactor. Ollama holds
// one runner per model name, so a session turn that resolves the debate's model
// mid-debate must be served the *planned* window — resolving the solo window
// would force an unload/reload, and the debate's next seat would force it back.
// It falls out of writing real cache entries rather than adding a lookup layer,
// which is exactly why that decision is worth a test of its own.
func TestSessionTurnDuringADebateSeesThePlannedWindow(t *testing.T) {
	s := residentSetServer(t, 14.5, map[string]int64{"primary": gib(4), "arbiter": gib(3)})
	s.ctxWinMu.Lock()
	s.setWindowLocked("primary", ctxWinEntry{win: 51200, src: "config", final: true})
	s.ctxWinMu.Unlock()

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	win, src := s.effectiveContextWindowFor(context.Background(), "primary")
	if src != residentSetSrc {
		t.Errorf("a turn mid-claim resolved %q, want the planned entry — anything else flips the runner", src)
	}
	if win >= 51200 {
		t.Errorf("a turn mid-claim resolved %d, want the smaller planned window", win)
	}
}

// The reason a plan is installed through setWindowLocked rather than consulted
// beside the cache: setWindowLocked is what retunes the daemon-wide summarizer.
// A parallel lookup layer would leave the compactor budgeting against the solo
// window while the runner serves the planned one — P66.14's disagreement, one
// layer down.
func TestResidentSetClaimRetunesAndRestoresTheSummarizer(t *testing.T) {
	s := residentSetServer(t, 14.5, map[string]int64{"primary": gib(4), "arbiter": gib(3)})
	s.compModel = "primary"
	s.ctxWinMu.Lock()
	s.setWindowLocked("primary", ctxWinEntry{win: 51200, src: "config", final: true})
	s.ctxWinMu.Unlock()
	s.summarizer = compaction.New(compaction.Options{ContextWindow: 51200, MaxBudget: 1})

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	s.ctxWinMu.Lock()
	planned, _ := s.windowLocked("primary")
	s.ctxWinMu.Unlock()
	if got := s.summarizer.ContextWindow(); got != planned.win {
		t.Errorf("summarizer window = %d during the claim, want the planned %d", got, planned.win)
	}

	release()
	if got := s.summarizer.ContextWindow(); got != 51200 {
		t.Errorf("summarizer window = %d after release, want the restored 51200", got)
	}
}

// Two debates planning against one GPU would each install windows out from under
// the other, so the second waits. It waits *interruptibly*: a queued claim that
// is cancelled must return rather than block for the length of the first debate.
func TestConcurrentResidentSetClaimsSerialize(t *testing.T) {
	s := residentSetServer(t, 14.5, map[string]int64{"primary": gib(4), "arbiter": gib(3)})

	release, err := s.claimResidentSet(context.Background(), []string{"primary"})
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	blocked := make(chan error, 1)
	go func() {
		r2, err := s.claimResidentSet(ctx, []string{"arbiter"})
		if err == nil {
			r2()
		}
		blocked <- err
	}()

	select {
	case err := <-blocked:
		t.Fatalf("second claim did not queue behind the first (err %v)", err)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-blocked:
		if err == nil {
			t.Error("a cancelled queued claim returned success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled queued claim did not return; the wait is not honoring its context")
	}

	release()
	r3, err := s.claimResidentSet(context.Background(), []string{"arbiter"})
	if err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	r3()
}

// The whole feature is invisible until an operator states a budget. This is what
// makes it safe to land ahead of the wiring: no budget, no plan, no entry
// touched, and a release that does nothing.
func TestResidentSetClaimIsInertWithoutABudget(t *testing.T) {
	s := residentSetServer(t, 0, map[string]int64{"primary": gib(4), "arbiter": gib(3)})
	s.ctxWinMu.Lock()
	s.setWindowLocked("primary", ctxWinEntry{win: 51200, src: "config", final: true})
	s.ctxWinMu.Unlock()

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	s.ctxWinMu.Lock()
	e, _ := s.windowLocked("primary")
	_, hasArb := s.ctxWinByModel["arbiter"]
	s.ctxWinMu.Unlock()
	if e.win != 51200 || e.src != "config" || !e.final {
		t.Errorf("entry changed with no budget configured: %+v", e)
	}
	if hasArb {
		t.Error("planning ran with no budget configured")
	}
}

// A set that cannot fit is refused before any model turn is spent, with a reason
// the operator can act on. Refusing at debate start is the earliest honest point:
// a resident set is a property of the workload, so it is not knowable at daemon
// start.
func TestResidentSetClaimRefusesWhatCannotFit(t *testing.T) {
	s := residentSetServer(t, 1, map[string]int64{"primary": gib(4), "arbiter": gib(3)})

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err == nil {
		release()
		t.Fatal("planned 7 GiB of weights into a 1 GiB budget")
	}
	if !strings.Contains(err.Error(), "vram_budget_gb") {
		t.Errorf("refusal %q does not name the key the operator would change", err)
	}

	// A refused claim must not hold the semaphore: the next debate has to be
	// able to try.
	done := make(chan struct{})
	go func() {
		r, _ := s.claimResidentSet(context.Background(), []string{"primary"})
		r()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a refused claim left the semaphore held")
	}
}

// A claim never raises a window. Growing one would force an unload/reload for no
// gain — and on the common case where every debate seat shares one model, the
// planned window is larger than the detected one, so honoring it would make
// every debate pay a cold reload to gain context nobody asked for.
func TestResidentSetClaimNeverRaisesAWindow(t *testing.T) {
	s := residentSetServer(t, 14.5, map[string]int64{"primary": gib(4)})
	s.ctxWinMu.Lock()
	s.setWindowLocked("primary", ctxWinEntry{win: 8192, src: "ollama:loaded", final: true})
	s.ctxWinMu.Unlock()

	release, err := s.claimResidentSet(context.Background(), []string{"primary"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	s.ctxWinMu.Lock()
	e, _ := s.windowLocked("primary")
	s.ctxWinMu.Unlock()
	if e.win != 8192 || e.src != "ollama:loaded" || !e.final {
		t.Errorf("entry = %+v, want the smaller measured 8192 left alone", e)
	}
}

// Plumbing check for the empirical half of the design: the stub's footprints are
// built from the same arithmetic ollamainfo derives weights with, so a drift in
// either would surface here rather than as a silently wrong plan.
func TestFakeOllamaWeightsRoundTrip(t *testing.T) {
	ts := fakeOllama(t, 16000, map[string]int64{"primary": gib(4)})
	g, ok := ollamainfo.Geometry(context.Background(), ts.URL, "primary")
	if !ok {
		t.Fatal("no geometry from the stub")
	}
	if bpt, _ := g.BytesPerToken(ollamainfo.KVTypeF16); bpt != qwen35KVPerToken {
		t.Fatalf("stub geometry gives %d bytes/token, want %d", bpt, qwen35KVPerToken)
	}
	f, loaded := ollamainfo.Loaded(context.Background(), ts.URL, "primary")
	if !loaded {
		t.Fatal("stub reports nothing loaded")
	}
	w, ok := ollamainfo.WeightsBytes(f, g, ollamainfo.KVTypeF16)
	if !ok || w != gib(4) {
		t.Fatalf("derived weights = %d (ok=%v), want %d", w, ok, gib(4))
	}
}

// The daemon must plan for the trio the debate actually runs, resolved through
// the same precedence the runner uses. Duplicates are deliberately kept: two
// seats on one model share a runner, and the planner has to see that to avoid
// counting the weights twice.
func TestDebateSeatModelsResolvesTheRunningTrio(t *testing.T) {
	s := &Server{
		cfg:    &config.Config{Provider: config.ProviderConfig{Model: "global-model"}},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	models := s.debateSeatModels(debate.WithDefaults(debate.Config{}))
	if len(models) != 3 {
		t.Fatalf("got %d seat models (%v), want 3", len(models), models)
	}
	for i, m := range models {
		if m == "" {
			t.Errorf("seat %d resolved to no model; an unresolvable persona must still fall back to the global one", i)
		}
	}
}

// "Nothing to plan" is not a failure. A caller whose seat-model resolver is
// unwired hands over blanks, meaning every seat runs on the daemon default —
// one already-sized model, nothing co-resident. Reading that as a set that could
// not be fitted would refuse debates that are fine.
func TestResidentSetClaimTreatsAnUnnamedSetAsNothingToPlan(t *testing.T) {
	s := residentSetServer(t, 14.5, map[string]int64{"primary": gib(4)})
	release, err := s.claimResidentSet(context.Background(), []string{"", "", ""})
	if err != nil {
		t.Fatalf("an all-blank set was refused rather than skipped: %v", err)
	}
	release()
}
