//go:build live_eval || live_workflow

package eval

import (
	"log/slog"
	"os"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
)

// liveAdapter builds the adapter a live tier talks to — through
// providerfactory.Build, from a config, exactly as production does (EXEC-6).
//
// The tier used to construct openai.New("ollama", …) directly while production
// builds ollama.New for `provider.default: ollama`, and the two differ in ways
// that decide behavior:
//
//   - Request.Format is honored only by the native adapter, so the tier
//     exercised the output guard's *unconstrained* decoding path while
//     production gets the grammar-constrained one.
//   - `think` is honored only on the native endpoint; the /v1 compatibility
//     layer accepts and ignores it.
//   - keep_alive, num_ctx and the P53.5 capability latch are native-adapter
//     concerns the tier did not touch at all.
//   - The whole decorator chain — retry, admission control, the per-model
//     harness profile and the salvage inside it — was absent.
//
// Together that is why EXEC-1 survived a green suite and a nightly eval: a
// default-on control, broken on the flagship local model, visible in the first
// sentence of output, on a code path no tier ran.
func liveAdapter(t *testing.T) (provider.Adapter, string) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Provider.Default = "ollama"
	cfg.Provider.BaseURL = liveBaseURL()
	cfg.Provider.Model = liveModel()

	adapter, err := providerfactory.Build(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("build live adapter: %v", err)
	}
	return adapter, cfg.Provider.Model
}

// liveBaseURL is the model server the live tiers talk to. Note the *native*
// endpoint, with no "/v1" suffix: the compat path is not what ships.
func liveBaseURL() string {
	if v := os.Getenv("AEGIS_EVAL_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:11434"
}

// liveModel is the model the live tiers exercise. The default is a *thinking*
// model on purpose (EXEC-6): the local profile's target population reasons
// before it answers, and the previous default (llama3.2) did not — which is the
// second reason EXEC-1, a defect that only manifests on a reasoning model, went
// unseen by a tier built to catch exactly that class of thing.
func liveModel() string {
	if v := os.Getenv("AEGIS_EVAL_MODEL"); v != "" {
		return v
	}
	return defaultLiveModel
}

// defaultLiveModel is pinned here rather than inline so the two live tiers
// cannot drift onto different models.
// It matches the model .github/workflows/nightly-eval.yml pulls, so a local
// run and the nightly exercise the same thing.
const defaultLiveModel = "qwen3:8b"
