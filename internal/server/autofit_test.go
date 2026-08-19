package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// autofitOllama is fakeOllama with residency that actually moves: /api/generate
// loads a model at the num_ctx it was asked for, and /api/ps reports only what
// has been loaded. That distinction is the whole subject of P72.1 — the fit
// cannot run until something has loaded the model, and the weights it measures
// are only reported for a model that is resident — so a fixture where every
// model is permanently loaded at a fixed window would pass a test the real
// chicken-and-egg fails.
type autofitOllama struct {
	*httptest.Server
	mu sync.Mutex
	// weights is every model this server knows about, and its resident weight
	// bytes. A model absent from it 404s from /api/show, as an unpulled tag does.
	weights map[string]int64
	// loadedAt maps a resident model to the window its runner was created with.
	loadedAt map[string]int
	// loads counts /api/generate load calls, so a test can assert that a fit
	// which changes nothing costs no reload.
	loads map[string]int
	// unloads counts keep_alive:0 evictions, which is how a resident-set claim
	// says it is finished with a model it brought in.
	unloads map[string]int
	// spill makes Ollama report that it placed a model partly in system RAM,
	// which is its own verdict that the requested window did not fit.
	spill bool
}

func newAutofitOllama(t *testing.T, weights map[string]int64, loadedAt map[string]int) *autofitOllama {
	t.Helper()
	f := &autofitOllama{
		weights:  weights,
		loadedAt: map[string]int{},
		loads:    map[string]int{},
		unloads:  map[string]int{},
	}
	for k, v := range loadedAt {
		f.loadedAt[k] = v
	}
	modelInfo := map[string]any{
		"general.architecture":          "qwen3",
		"qwen3.block_count":             33,
		"qwen3.attention.head_count_kv": 4,
		"qwen3.attention.key_length":    256,
		"qwen3.attention.value_length":  256,
		"qwen3.context_length":          262144,
		"qwen3.attention.head_count":    32,
		"qwen3.embedding_length":        8192,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model, Name string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		name := req.Model
		if name == "" {
			name = req.Name
		}
		f.mu.Lock()
		_, known := f.weights[name]
		f.mu.Unlock()
		if !known {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model_info": modelInfo})
	})
	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		models := []map[string]any{}
		for name, win := range f.loadedAt {
			size := f.weights[name] + int64(win)*qwen35KVPerToken
			vram := size
			if f.spill {
				vram = size / 2
			}
			models = append(models, map[string]any{
				"name": name, "model": name, "context_length": win,
				"size": size, "size_vram": vram,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
	})
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model     string `json:"model"`
			KeepAlive *int   `json:"keep_alive"`
			Options   struct {
				NumCtx int `json:"num_ctx"`
			} `json:"options"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, known := f.weights[req.Model]; !known {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if req.KeepAlive != nil && *req.KeepAlive == 0 {
			delete(f.loadedAt, req.Model)
			f.unloads[req.Model]++
			_ = json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "done": true})
			return
		}
		win := req.Options.NumCtx
		if win <= 0 {
			win = 4096 // Ollama's own default, as a bare load would get
		}
		f.loadedAt[req.Model] = win
		f.loads[req.Model]++
		_ = json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "done": true})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	f.Server = ts
	return f
}

func (f *autofitOllama) loadedWindow(model string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loadedAt[model]
}

func (f *autofitOllama) loadCount(model string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loads[model]
}

func (f *autofitOllama) isLoaded(model string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.loadedAt[model]
	return ok
}

func (f *autofitOllama) unloadCount(model string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unloads[model]
}

// claimServer is autofitServer with the Ollama base already resolved, for tests
// that exercise a claim directly rather than through daemon startup.
func claimServer(t *testing.T, f *autofitOllama, mutate func(*config.ProviderConfig)) *Server {
	t.Helper()
	s := autofitServer(t, f, mutate)
	s.ollamaBase = f.URL
	return s
}

// autofitServer builds a daemon pointed at f, with cfg applied on top of the
// local-Ollama defaults these tests share.
func autofitServer(t *testing.T, f *autofitOllama, mutate func(*config.ProviderConfig)) *Server {
	t.Helper()
	p := config.ProviderConfig{
		Default:      "ollama",
		Model:        "primary",
		BaseURL:      f.URL,
		VRAMBudgetGB: 14.5,
		MaxTokens:    4096,
	}
	if mutate != nil {
		mutate(&p)
	}
	return &Server{
		cfg:    &config.Config{Provider: p},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func (s *Server) windowOf(model string) (int, string) {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	e, _ := s.windowLocked(model)
	return e.win, e.src
}

// The item's closure condition, in a test: with a budget stated and no
// context_window configured, a fresh daemon boot serves a window solved from the
// hardware rather than a default. 82944 is the exact arithmetic for 14.5 GiB
// against 4 GiB of weights at 132 KiB/token, which is the same figure `aegis
// models --fit` prints — there is one solver, not a daemon-flavored second one.
func TestAutofitSizesTheWindowFromTheBudget(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, nil)
	s := autofitServer(t, f, nil)
	s.initContextWindow(context.Background())

	win, src := s.windowOf("primary")
	if win != 82944 || src != autofitSrc {
		t.Fatalf("got %d/%q, want 82944/%s", win, src, autofitSrc)
	}
	// The runner must actually be at the fitted window: a fit the daemon merely
	// believes in is one the next detection reconciles away.
	if got := f.loadedWindow("primary"); got != 82944 {
		t.Errorf("ollama runner is at window %d, want the fitted 82944", got)
	}
}

// The chicken-and-egg is resolved by loading, not by guessing: a model that has
// never been loaded has no measurable weights, and /api/tags' on-disk size is
// wrong by the size of a never-resident vision projector. So the boot pass must
// issue the load itself rather than fall back to an estimate.
func TestAutofitLoadsAnUnloadedModelToMeasureIt(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, nil)
	s := autofitServer(t, f, nil)
	s.initContextWindow(context.Background())

	if f.loadCount("primary") == 0 {
		t.Fatal("nothing was loaded, so nothing could have been measured")
	}
	if win, _ := s.windowOf("primary"); win == 0 {
		t.Fatal("no window was resolved at all")
	}
}

// A configured context_window is frequently load-bearing — this repo's debate
// topology pins one for documented reasons — so the budget alone must not
// replace it. The fit stays inert and says which flag would enable it.
func TestAutofitLeavesAConfiguredWindowAloneUnlessAsked(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, map[string]int{"primary": 16000})
	s := autofitServer(t, f, func(p *config.ProviderConfig) { p.ContextWindow = 16000 })
	s.initContextWindow(context.Background())

	if win, src := s.windowOf("primary"); win != 16000 || src == autofitSrc {
		t.Fatalf("got %d/%q, want the configured 16000 left alone", win, src)
	}
	if f.loadCount("primary") != 0 {
		t.Error("an inert fit must not reload the model")
	}
}

// ...and with the explicit permission, it does replace it. This is the machine
// P72.1 was filed from: context_window 16000 against a card that fits 82944.
func TestAutofitOverridesAConfiguredWindowWhenEnabled(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, map[string]int{"primary": 16000})
	s := autofitServer(t, f, func(p *config.ProviderConfig) {
		p.ContextWindow = 16000
		p.AutofitContext = true
	})
	s.initContextWindow(context.Background())

	if win, src := s.windowOf("primary"); win != 82944 || src != autofitSrc {
		t.Fatalf("got %d/%q, want 82944/%s", win, src, autofitSrc)
	}
}

// provider.small_model is co-resident with the primary with no debate in sight:
// compaction runs on it while keep_alive still holds the primary. Sizing each
// against the whole budget is the P69.6 bug one layer out, so the pair goes
// through the resident-set solver and both windows come back smaller than either
// would get alone.
func TestAutofitPlansTheSmallModelCoResident(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4), "small": gib(3)}, nil)
	s := autofitServer(t, f, func(p *config.ProviderConfig) { p.SmallModel = "small" })
	s.initContextWindow(context.Background())

	pw, psrc := s.windowOf("primary")
	sw, _ := s.windowOf("small")
	if pw != 29696 || sw != 29696 || psrc != autofitSrc {
		t.Fatalf("got primary %d/%q, small %d; want both 29696 fitted as a set", pw, psrc, sw)
	}
	if pw >= 82944 {
		t.Error("the primary was sized as if it were alone — the set was not planned")
	}
	kv := int64(pw+sw) * qwen35KVPerToken
	if total := gib(7) + kv; total > ollamainfo.BudgetBytes(14.5) {
		t.Errorf("the plan does not fit its own budget: %s of %s",
			ollamainfo.FormatGiB(total), ollamainfo.FormatGiB(ollamainfo.BudgetBytes(14.5)))
	}
}

// The fit is a prediction and /api/ps is the verdict, so the entry stays
// non-final and the post-run refresh re-measures. What it must not do is
// reconcile the fitted window back down to provider.context_window — that would
// undo the fit on the first turn that finished, silently.
func TestAutofitSurvivesTheNextRefresh(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, nil)
	s := autofitServer(t, f, func(p *config.ProviderConfig) {
		p.ContextWindow = 16000
		p.AutofitContext = true
	})
	s.initContextWindow(context.Background())
	if win, _ := s.windowOf("primary"); win != 82944 {
		t.Fatalf("fit did not install: %d", win)
	}

	s.maybeRefreshContextWindowFor(context.Background(), "primary")
	if win, src := s.windowOf("primary"); win != 82944 || src != autofitSrc {
		t.Fatalf("after refresh got %d/%q, want the fitted 82944 to stand", win, src)
	}
}

// Ollama serving less than the fit asked for is the arithmetic being refuted,
// and reality wins: the daemon budgets against what is actually served, exactly
// as it does for an over-optimistic configured window.
func TestAutofitYieldsToWhatOllamaActuallyServes(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, nil)
	s := autofitServer(t, f, nil)
	s.initContextWindow(context.Background())

	// The card turns out to hold less than stated: Ollama re-creates the runner
	// at a smaller window than was asked for.
	f.mu.Lock()
	f.loadedAt["primary"] = 20480
	f.mu.Unlock()

	s.maybeRefreshContextWindowFor(context.Background(), "primary")
	if win, src := s.windowOf("primary"); win != 20480 || src != "ollama:loaded" {
		t.Fatalf("got %d/%q, want the served 20480 to win", win, src)
	}
}

// P72.1 step 3: a model the session switched to mid-run joins the resident set
// rather than being fitted as if it were alone — keep_alive is still holding the
// model it switched away from. Admission runs after the turn, which is the first
// moment the new model's weights are measurable.
func TestAutofitAdmitsAModelSwitchedToMidSession(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4), "switched": gib(3)}, nil)
	s := autofitServer(t, f, nil)
	s.initContextWindow(context.Background())
	if win, _ := s.windowOf("primary"); win != 82944 {
		t.Fatalf("boot fit did not install: %d", win)
	}

	// /model switched the session; the turn ran and loaded it.
	f.mu.Lock()
	f.loadedAt["switched"] = 4096
	f.mu.Unlock()
	s.maybeRefreshContextWindowFor(context.Background(), "switched")

	pw, _ := s.windowOf("primary")
	sw, src := s.windowOf("switched")
	if pw != 29696 || sw != 29696 || src != autofitSrc {
		t.Fatalf("got primary %d, switched %d/%q; want both re-planned to 29696", pw, sw, src)
	}
}

// A resident-set claim (P69.6) outranks the fit while it is installed: the plan
// sized this model against everything resident right now, and the fit sized it
// alone. Without this the mid-debate refresh — which the claim deliberately
// leaves enabled — would reconcile the window back up to the solo figure and
// hand the debate's next seat a window the set cannot afford.
func TestResidentSetPlanOutranksTheFittedWindow(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, nil)
	s := autofitServer(t, f, nil)
	s.initContextWindow(context.Background())

	s.ctxWinMu.Lock()
	s.setWindowLocked("primary", ctxWinEntry{win: 8192, src: residentSetSrc})
	s.applyDetectedWindowFor("primary", ollamainfo.Result{
		ContextWindow: 8192, Source: ollamainfo.SourceLoaded,
	})
	s.ctxWinMu.Unlock()

	if win, _ := s.windowOf("primary"); win != 8192 {
		t.Fatalf("got %d, want the planned 8192 to stand against the %d fit", win, s.autofitWin["primary"])
	}
}

// No budget stated means nothing is planned and nothing is loaded — the whole
// feature is invisible to an install that did not opt in, which is the posture
// P69.6 established for the same config key.
func TestAutofitIsInertWithoutABudget(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, map[string]int{"primary": 16000})
	s := autofitServer(t, f, func(p *config.ProviderConfig) { p.VRAMBudgetGB = 0 })
	s.initContextWindow(context.Background())

	if win, src := s.windowOf("primary"); src == autofitSrc {
		t.Fatalf("fitted %d/%q with no budget configured", win, src)
	}
	if f.loadCount("primary") != 0 {
		t.Error("an install with no budget must not be reloaded at boot")
	}
}

// The /v1 compat path cannot carry num_ctx, so a fitted window is a number the
// server would never receive. Installing it would have the daemon budget its
// conversation against a window nothing serves — the silent front-truncation
// this subsystem exists to prevent, arrived at by the fix for it.
func TestAutofitDoesNotRunOnTheCompatPath(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, map[string]int{"primary": 16000})
	s := autofitServer(t, f, func(p *config.ProviderConfig) {
		p.Default = "openai"
		p.BaseURL = f.URL + "/v1"
	})
	s.initContextWindow(context.Background())

	if win, src := s.windowOf("primary"); src == autofitSrc {
		t.Fatalf("fitted %d/%q on a path that cannot send num_ctx", win, src)
	}
}

// A budget too small for the weights alone is a refusal, not a window: the
// daemon says so and keeps serving what it had, rather than installing
// something under the viable floor.
func TestAutofitRefusesABudgetThatCannotHoldTheWeights(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(12)}, nil)
	s := autofitServer(t, f, func(p *config.ProviderConfig) {
		p.VRAMBudgetGB = 8
		p.ContextWindow = 16000
		p.AutofitContext = true
	})
	s.initContextWindow(context.Background())

	if win, src := s.windowOf("primary"); src == autofitSrc {
		t.Fatalf("installed %d/%q from a budget that cannot hold the weights", win, src)
	}
}

// The fit can destroy the evidence it was computed from. WeightsBytes derives
// weights by subtracting the KV cache a loaded window accounts for, and
// BytesPerToken is a deliberate upper bound for sliding-window models — so after
// the fit resizes a model upward, that subtraction can leave nothing and the
// model stops being measurable. This was found live on 2026-08-19: a debate
// started after the boot fit was refused for want of a weight figure the daemon
// had measured half a minute earlier. Weights do not change with the window, so
// the remembered figure is the right one to plan against.
func TestAutofitRemembersWeightsAfterResizingPastMeasurability(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4)}, nil)
	s := autofitServer(t, f, nil)
	s.initContextWindow(context.Background())
	if win, _ := s.windowOf("primary"); win != 82944 {
		t.Fatalf("boot fit did not install: %d", win)
	}

	// Sliding-window attention: the real cache at the fitted window is a
	// fraction of the linear estimate, so the footprint no longer covers it.
	f.mu.Lock()
	f.weights["primary"] = gib(4) - int64(82944)*qwen35KVPerToken/2
	f.mu.Unlock()
	if _, ok, _ := ollamainfo.PlanFor(context.Background(), f.URL,
		[]string{"primary"}, ollamainfo.BudgetBytes(14.5), "", nil); ok {
		t.Fatal("fixture does not reproduce the unmeasurable footprint")
	}

	// A debate claiming the same model must still be plannable.
	release, err := s.claimResidentSet(context.Background(), []string{"primary", "primary"})
	if err != nil {
		t.Fatalf("claim refused after the fit made the model unmeasurable: %v", err)
	}
	release()
}

// A seat model that has never been loaded has no measurable weights, and P69.6
// answered that by refusing the whole debate. On any machine whose arbiter runs
// a different model from its proposer that is *every cold start* — the refusal
// says "cannot fit" when the truth is "nobody asked". Verified live 2026-08-19
// against the configured trio on this machine: `model "..." is not loaded, so
// its resident weights cannot be measured`. The claim now loads the set first.
func TestClaimLoadsAnUnmeasuredSeatModelInsteadOfRefusing(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4), "arbiter": gib(3)}, map[string]int{"primary": 16000})
	s := claimServer(t, f, nil)

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim refused a set it could have measured: %v", err)
	}
	defer release()

	if f.loadCount("arbiter") == 0 {
		t.Error("the unmeasured seat model was never loaded")
	}
	if win, src := s.windowOf("arbiter"); win == 0 || src != residentSetSrc {
		t.Errorf("arbiter got %d/%q, want a planned window", win, src)
	}
}

// The plan is not just a belief about windows: the runner has to be the size the
// plan says, or the first seat pays the reload inside its own turn and until
// then Ollama is serving a window nothing agrees with.
func TestClaimCommitsPlannedWindowsToTheRunners(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4), "arbiter": gib(3)}, map[string]int{"primary": 82944})
	s := claimServer(t, f, nil)
	s.ctxWinMu.Lock()
	s.setWindowLocked("primary", ctxWinEntry{win: 82944, src: autofitSrc})
	s.ctxWinMu.Unlock()

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	want, _ := s.windowOf("primary")
	if got := f.loadedWindow("primary"); got != want {
		t.Errorf("runner is at %d, plan installed %d — the plan was never committed", got, want)
	}
	if want >= 82944 {
		t.Errorf("the co-resident window (%d) is not below the solo one; the fixture proves nothing", want)
	}
}

// The window the daemon returns to assumes nothing else is on the card, so the
// members the claim brought in have to go when the workload does. Otherwise the
// restored solo window contends with an arbiter that keep_alive holds for
// another half hour, and Ollama answers a load it cannot place by spilling.
func TestClaimUnloadsWhatItBroughtInOnRelease(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4), "arbiter": gib(3)}, map[string]int{"primary": 16000})
	s := claimServer(t, f, nil)

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	release()

	if f.isLoaded("arbiter") {
		t.Error("the arbiter is still resident after the debate that needed it ended")
	}
	// The daemon's own model is what every ordinary turn runs on. Evicting it to
	// tidy up would make the next message pay a cold load for nothing.
	if !f.isLoaded("primary") {
		t.Error("the daemon's own model was unloaded")
	}
}

// A model that was already resident is not this claim's to evict — it was
// serving somebody before the debate started and may be serving them after.
func TestClaimDoesNotUnloadAModelItDidNotLoad(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4), "arbiter": gib(3)},
		map[string]int{"primary": 16000, "arbiter": 16000})
	s := claimServer(t, f, nil)

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	release()

	if !f.isLoaded("arbiter") {
		t.Error("a model the claim found already resident was evicted by it")
	}
}

// provider.small_model is the daemon's own too — compaction and title generation
// run on it — so a seat that happens to name it must not have it evicted
// underneath them when the debate ends.
func TestClaimNeverUnloadsTheDaemonsSmallModel(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4), "small": gib(2)}, map[string]int{"primary": 16000})
	s := claimServer(t, f, func(p *config.ProviderConfig) { p.SmallModel = "small" })

	release, err := s.claimResidentSet(context.Background(), []string{"primary", "small"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	release()

	if f.unloadCount("small") != 0 {
		t.Error("provider.small_model was evicted by a debate that borrowed it")
	}
}

// A refused plan must not leave the models it loaded to find that out sitting in
// VRAM: the claim failed, so nothing it did should outlive it.
func TestClaimReleasesResidencyWhenThePlanIsRefused(t *testing.T) {
	f := newAutofitOllama(t, map[string]int64{"primary": gib(4), "arbiter": gib(12)}, map[string]int{"primary": 16000})
	s := claimServer(t, f, nil)

	if _, err := s.claimResidentSet(context.Background(), []string{"primary", "arbiter"}); err == nil {
		t.Fatal("a set that cannot fit was accepted")
	}
	if f.isLoaded("arbiter") {
		t.Error("the refused claim left the model it loaded resident")
	}
}
