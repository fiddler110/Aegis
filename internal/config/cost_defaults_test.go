package config

import (
	"os"
	"testing"
)

// costEnvKeys are every AEGIS_* variable that can reach a spend cap or the
// provider classification the cap keys off. Scrubbed in each test below because
// a real one in the developer's own environment would otherwise decide the
// answer instead of the fixture.
var costEnvKeys = []string{
	"AEGIS_PROVIDER_DEFAULT", "AEGIS_PROVIDER_BASE_URL",
	"AEGIS_COST_SESSION_CAP_USD", "AEGIS_COST_DAILY_CAP_USD",
}

// loadWithGlobalConfig writes body as the operator's own ~/.config/aegis/
// config.yaml and loads. The *global* layer, not the project one, because these
// caps are the operator's answer to "how much may this spend" — and a project
// file would additionally be exercising the trust freeze, which is not what any
// of these tests is about.
func loadWithGlobalConfig(t *testing.T, body string) *Config {
	t.Helper()
	if body != "" {
		if err := os.MkdirAll(defaultDataDir(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(GlobalConfigPath(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// TestCloudSpendCapsDefaultOn is P81.15/FIND-15's headline case. Before it,
// every cost bound but max_turn_stall shipped at 0 = unlimited, and
// max_turn_stall measures *silence* — a model looping, or steered by an injected
// instruction, produces tokens the whole time and never trips it. Against a
// metered cloud endpoint that meant the only real ceiling on a runaway was the
// operator reading their bill.
func TestCloudSpendCapsDefaultOn(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)
	clearEnv(t, costEnvKeys...)

	// No cost: block anywhere, provider.default at its shipped "anthropic".
	cfg := loadWithGlobalConfig(t, "provider:\n  model: claude-opus-4-8\n")

	if !cfg.Provider.MeteredCloudEndpoint() {
		t.Fatal("the shipped provider.default should classify as a metered cloud endpoint")
	}
	if cfg.Cost.SessionCapUSD != DefaultCloudSessionCapUSD {
		t.Errorf("cost.session_cap_usd = %v, want the cloud default %v", cfg.Cost.SessionCapUSD, DefaultCloudSessionCapUSD)
	}
	if cfg.Cost.DailyCapUSD != DefaultCloudDailyCapUSD {
		t.Errorf("cost.daily_cap_usd = %v, want the cloud default %v", cfg.Cost.DailyCapUSD, DefaultCloudDailyCapUSD)
	}
	// Only the two USD caps are defaulted. The token caps stay at zero on
	// purpose: a token count is not a spend, the right value for one is entirely
	// workload-shaped, and a wrong guess would stop legitimate long work with no
	// bill to justify it. `aegis harden` is where an operator opts into those.
	if cfg.Cost.SessionTokenCap != 0 || cfg.Cost.DailyTokenCap != 0 {
		t.Errorf("token caps = %d/%d, want 0/0 — only the USD caps carry a shipped default",
			cfg.Cost.SessionTokenCap, cfg.Cost.DailyTokenCap)
	}
}

// TestLoopbackProviderStaysUnbounded is the other half, and the one that decides
// whether the default above is shippable at all. This project's normal mode is a
// local Ollama: inference there is unpriced, so a USD ceiling over it can only
// ever be a stop with no spend behind it. The classification must be the
// provider's, not the config's — which is why MeteredCloudEndpoint reuses
// IsLoopbackBaseURL rather than testing for loopback a second way.
func TestLoopbackProviderStaysUnbounded(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)
	clearEnv(t, costEnvKeys...)

	for _, tc := range []struct{ name, yaml string }{
		{"ollama at its default port", "provider:\n  default: ollama\n  base_url: http://localhost:11434\n"},
		{"an OpenAI-compatible proxy on 127.0.0.1", "provider:\n  default: openai\n  base_url: http://127.0.0.1:4000/v1\n"},
		{"ollama with no base_url at all", "provider:\n  default: ollama\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectConfigDir(t)
			cfg := loadWithGlobalConfig(t, tc.yaml)
			if cfg.Provider.MeteredCloudEndpoint() {
				t.Fatalf("%s classified as a metered cloud endpoint", tc.name)
			}
			if cfg.Cost.SessionCapUSD != 0 || cfg.Cost.DailyCapUSD != 0 {
				t.Errorf("caps = $%v/$%v, want unbounded — a spend ceiling over unpriced local "+
					"inference is a stop with no spend behind it", cfg.Cost.SessionCapUSD, cfg.Cost.DailyCapUSD)
			}
		})
	}
}

// TestExplicitSpendCapsWin covers both directions of "the operator answered".
// The zero case is the load-bearing one: 0 is the documented way to say
// *unlimited*, so an operator who wrote it must keep getting it — which is only
// possible because the two keys are absent from defaults() and koanf's Exists
// can therefore tell "unset" from "set to zero".
func TestExplicitSpendCapsWin(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)
	clearEnv(t, costEnvKeys...)

	t.Run("a stated value replaces the default", func(t *testing.T) {
		redirectConfigDir(t)
		cfg := loadWithGlobalConfig(t, "cost:\n  session_cap_usd: 2.5\n  daily_cap_usd: 7.5\n")
		if cfg.Cost.SessionCapUSD != 2.5 || cfg.Cost.DailyCapUSD != 7.5 {
			t.Errorf("caps = $%v/$%v, want the operator's $2.50/$7.50", cfg.Cost.SessionCapUSD, cfg.Cost.DailyCapUSD)
		}
	})

	t.Run("a stated zero still means unlimited", func(t *testing.T) {
		redirectConfigDir(t)
		cfg := loadWithGlobalConfig(t, "cost:\n  session_cap_usd: 0\n  daily_cap_usd: 0\n")
		if cfg.Cost.SessionCapUSD != 0 || cfg.Cost.DailyCapUSD != 0 {
			t.Errorf("caps = $%v/$%v, want 0/0 — an explicit zero is the documented way to say "+
				"unlimited and must not be overwritten by the cloud default",
				cfg.Cost.SessionCapUSD, cfg.Cost.DailyCapUSD)
		}
	})

	t.Run("one cap stated leaves the other defaulted", func(t *testing.T) {
		redirectConfigDir(t)
		cfg := loadWithGlobalConfig(t, "cost:\n  daily_cap_usd: 0\n")
		if cfg.Cost.SessionCapUSD != DefaultCloudSessionCapUSD {
			t.Errorf("cost.session_cap_usd = %v, want the cloud default %v — the two keys are "+
				"answered independently", cfg.Cost.SessionCapUSD, DefaultCloudSessionCapUSD)
		}
		if cfg.Cost.DailyCapUSD != 0 {
			t.Errorf("cost.daily_cap_usd = %v, want the stated 0", cfg.Cost.DailyCapUSD)
		}
	})

	t.Run("the environment layer counts as stated", func(t *testing.T) {
		redirectConfigDir(t)
		t.Setenv("AEGIS_COST_DAILY_CAP_USD", "0")
		cfg := loadWithGlobalConfig(t, "")
		if cfg.Cost.DailyCapUSD != 0 {
			t.Errorf("cost.daily_cap_usd = %v, want the AEGIS_COST_DAILY_CAP_USD zero", cfg.Cost.DailyCapUSD)
		}
	})
}

// TestMeteredCloudEndpointClassification pins the predicate itself, including
// the two directions its doc comment argues for: a base_url decides on its own
// when it is set, and the adapter name decides only when it is not.
func TestMeteredCloudEndpointClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		p       ProviderConfig
		metered bool
	}{
		{"anthropic, no base_url", ProviderConfig{Default: "anthropic"}, true},
		{"openai, no base_url", ProviderConfig{Default: "openai"}, true},
		{"ollama, no base_url", ProviderConfig{Default: "ollama"}, false},
		{"an unknown adapter name", ProviderConfig{Default: "something-else"}, false},
		{"case and spacing are not a bypass", ProviderConfig{Default: " Anthropic "}, true},
		{"a cloud name pointed at loopback", ProviderConfig{Default: "openai", BaseURL: "http://127.0.0.1:4000/v1"}, false},
		{"::1 is loopback too", ProviderConfig{Default: "openai", BaseURL: "http://[::1]:4000/v1"}, false},
		// The err-toward-metered direction: a local adapter name reaching a
		// remote host. Free rather than merely acceptable — a USD cap over
		// unpriced inference never fires, because nothing accumulates against it.
		{"ollama on another machine", ProviderConfig{Default: "ollama", BaseURL: "http://192.168.1.20:11434"}, true},
		{"a gateway in front of anthropic", ProviderConfig{Default: "anthropic", BaseURL: "https://gateway.corp.example/v1"}, true},
	} {
		if got := tc.p.MeteredCloudEndpoint(); got != tc.metered {
			t.Errorf("%s: MeteredCloudEndpoint() = %v, want %v", tc.name, got, tc.metered)
		}
	}
}
