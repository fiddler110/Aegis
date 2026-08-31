package config

import (
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/modelcaps"
	"github.com/fiddler110/aegis/internal/profile"
	"github.com/fiddler110/aegis/internal/provider/sse"
	"github.com/fiddler110/aegis/internal/toolshim"
)

// ProviderConfig selects and configures the model provider.
type ProviderConfig struct {
	Default       string `koanf:"default"`        // adapter name: "anthropic", "openai", "ollama"
	Model         string `koanf:"model"`          // model id
	SmallModel    string `koanf:"small_model"`    // optional fast model for title gen + compaction (falls back to Model)
	MaxTokens     int    `koanf:"max_tokens"`     // response token cap
	BaseURL       string `koanf:"base_url"`       // optional API base override
	MaxRetries    int    `koanf:"max_retries"`    // transient-failure retries; 0 disables
	MaxIterations int    `koanf:"max_iterations"` // max engine turns per run; 0 = harness default (40)
	LoopThreshold int    `koanf:"loop_threshold"` // identical-turn abort count; 0 = harness default (5)
	// ZeroToolNudge (P28.3) bounds the corrective-nudge retries fired when a
	// turn that plainly reads as actionable produces zero tool calls (a model
	// dumping its reasoning as prose instead of calling a tool). 0 = harness
	// default (1 retry); negative disables the nudge entirely.
	ZeroToolNudge int `koanf:"zero_tool_nudge"`
	// Temperature overrides the backend's sampling temperature. nil (unset)
	// leaves it to the backend, which is rarely what an agentic run wants:
	// Ollama's default is 0.8 unless a Modelfile says otherwise, so two runs
	// of the same prompt against the same model take visibly different paths.
	// Measured (P38.1 re-test, 2026-08-09): one run opened by writing a file,
	// the next opened with an unprompted web search, same prompt and model.
	// A tool-dispatching run is a decision process, not a creative one — set
	// this to 0 when reproducibility matters more than variety.
	Temperature *float64 `koanf:"temperature"`
	// Seed pins the backend's RNG so a run at a fixed temperature is
	// reproducible turn for turn. nil (unset) leaves the backend to pick.
	// Only meaningful alongside Temperature: a seed at temperature 0.8 fixes
	// which of many paths is taken, not that there is only one.
	Seed    *int              `koanf:"seed"`
	Headers map[string]string `koanf:"headers"` // extra HTTP headers sent with every request (e.g. gateway auth)
	// Think controls extended thinking. Its three states are not two: true
	// enables, false disables, and nil — the default — means "the provider's
	// default", which is *not* the same as off (EXEC-5). Anthropic defaults
	// off (thinking is billed); native Ollama defaults **on** (P77.1, see
	// providerfactory); openai-compat targets stay off. Set it to false
	// explicitly to disable. Individual call sites may still override it: a
	// short classification call suppresses thinking regardless of this
	// setting (see provider.SuppressesExtendedThinking).
	Think           *bool  `koanf:"think"`
	ReasoningEffort string `koanf:"reasoning_effort"` // OpenAI o1/o3 reasoning_effort: "low", "medium", "high", or "" (omit)
	ContextWindow   int    `koanf:"context_window"`   // model context window in tokens; 0 = auto (skips compaction for local models)
	// VRAMBudgetGB is how much memory, in GiB, the operator is willing to let
	// the model server hold across *every* concurrently resident model (P69.6).
	// It is what makes a co-resident plan possible: since P69.1 each debate seat
	// resolves its own model, so a debate holds two or three models in VRAM at
	// once, and context_window alone can only size one of them at a time — each
	// as if it were alone.
	//
	// It is a figure the operator states, never one Aegis detects. No GPU/VRAM
	// introspection is attempted on any platform (P17.5, and P20.3 rejected it
	// again), and the number wanted here is not the card's capacity anyway: it is
	// capacity minus whatever the driver reserve and the desktop compositor
	// already hold, which only the operator can see. ~14.5 of a 16 GB card is the
	// measured figure on the machine P69.5/P69.6 were calibrated against.
	//
	//   0 (unset) — no resident-set planning at all. Every path keeps the
	//               behavior it had before P69.6; the feature exists only for an
	//               operator who opted in.
	//   n > 0     — plan every co-resident set against n GiB.
	//
	// Only meaningful for a local Ollama backend; a cloud provider has nothing
	// resident to budget. Read via VRAMBudgetBytes(), never this field directly.
	VRAMBudgetGB float64 `koanf:"vram_budget_gb"`
	// AutofitContext lets the daemon size the serving context window from
	// vram_budget_gb at startup and on first use of a newly-selected model,
	// rather than serving whatever context_window says (P72.1).
	//
	// The fit itself is gated on vram_budget_gb, not on this flag: with a budget
	// stated and *no* context_window configured, the daemon already has nothing
	// to contradict and fits. This flag answers the other case — a
	// context_window that IS set. That number is frequently load-bearing (the
	// debate topology's 16k pin is documented as such), and silently replacing a
	// figure an operator worked out is the one thing P72.1 was filed saying not
	// to do. So overriding a configured window is a separate, explicit "yes".
	//
	//   false (default) — fit only when context_window is unset.
	//   true            — fit always, and let the fitted window override a
	//                     configured one (announced in the log, never written
	//                     back to config.yaml).
	//
	// Like vram_budget_gb it is a property of the operator's machine, so it is
	// frozen from project config (see freeze.go).
	AutofitContext bool `koanf:"autofit_context"`
	// KVCacheType declares the element type Ollama stores K and V in — its
	// OLLAMA_KV_CACHE_TYPE, llama.cpp's --cache-type-k/v. "" (unset) means f16,
	// Ollama's default; "q8_0" roughly halves the cache and "q4_0" roughly
	// quarters it.
	//
	// This is a declaration, not a discovery: Ollama does not report the setting
	// over its API, so an operator running a quantized cache must say so, or every
	// window is planned against roughly twice the bytes it actually costs —
	// erring safe, but wasting most of the headroom the quantization bought. A
	// declaration in the wrong direction is caught empirically by Ollama's own
	// size/size_vram split (ollamainfo.Footprint.FullyOnGPU) rather than silently
	// believed. An unrecognized value is treated as f16; KVCacheTypeValid reports
	// whether it was recognized, so a typo can be named rather than left looking
	// like a working setting.
	KVCacheType string `koanf:"kv_cache_type"`
	// KeepAlive controls how long Ollama keeps the model resident after a
	// request, via the native adapter's keep_alive field (P33.10). Only the
	// native "ollama" adapter honors it; the OpenAI-compat path cannot send it.
	// Accepts a Go duration ("30m") or an integer number of seconds; "-1" pins
	// the model in memory forever, "0" unloads it immediately. "" (unset) is
	// NOT passed through as Ollama's 5m default: providerfactory substitutes
	// a bounded resident default (providerfactory.defaultOllamaKeepAlive, 30m)
	// so a multi-turn agentic run reuses its KV cache across turns instead of
	// reprocessing the whole conversation each turn (P35.4). It is still never
	// defaulted to "-1" — the model unloads once a run goes idle, so RAM is
	// held only during active work (the limited-RAM concern behind P33.10).
	KeepAlive string `koanf:"keep_alive"`
	// ResponseHeaderTimeoutSec bounds how long a streamed request waits for
	// the response headers, in seconds (P35.5). Shared by every adapter via
	// sse.NewStreamingClient. Ollama withholds the response header until
	// prompt-eval (prefill) finishes, so a legitimately slow prefill on a
	// large local context can trip the default and abort the whole turn as a
	// transport error before any content streams. 0 (unset) keeps the
	// default (sse.DefaultResponseHeaderTimeout, 30 minutes as of P38.1) so
	// behavior is unchanged unless a user opts in to raising it further. Read
	// via ResponseHeaderTimeout(), never this field directly.
	ResponseHeaderTimeoutSec int `koanf:"response_header_timeout"`
	// StreamIdleTimeoutSec bounds the gap between two streamed chunks, in
	// seconds (P59.2). ResponseHeaderTimeoutSec above stops applying the moment
	// the headers arrive, and the streaming client deliberately has no overall
	// timeout, so before this key a model runner that wedged mid-generation left
	// the turn blocked on a read indefinitely — and cost.max_wall_clock_per_run
	// could not help, because it is checked between turns, never inside one.
	// 0 (unset) keeps the default (sse.DefaultStreamIdleTimeout, 10 minutes);
	// a negative value disables the bound. Read via StreamIdleTimeout(), never
	// this field directly. Honored by every adapter: it reached only the native
	// ollama one until P61.1, which is worse than it sounds — the openai adapter
	// is a local path too (Ollama's /v1 compat endpoint), so the backend most
	// likely to wedge was half unprotected by a key users read as global.
	StreamIdleTimeoutSec int `koanf:"stream_idle_timeout"`
	// TaskRouting opts a session's user-facing turns into per-turn model
	// routing (P9.4): a local heuristic classifies each turn as "simple" or
	// "complex" and simple turns run on SmallModel instead of Model. Off by
	// default — this is speculative, opt-in-only work; a daemon that never
	// sets this sees zero behavior change. Has no effect unless SmallModel is
	// also set, mirroring the existing "no small_model configured = no
	// behavior change" precedent guardModel/generateTitle/compaction already
	// follow. Never applies when an explicit per-session /model override
	// (P14.7) is in effect — that override always wins.
	TaskRouting bool `koanf:"task_routing"`
	// Fallback lists ordered (provider, model) pairs tried in order after the
	// primary adapter exhausts its own retries (P5.9). Empty = no failover.
	Fallback []ProviderFallbackConfig `koanf:"fallback"`
	// AllowCloudFallback must be explicitly set to fail over from a local
	// provider (ollama) to a cloud provider (anthropic, openai). Cloud-to-cloud
	// and any-to-local failover never requires this flag. Guards against a
	// local-only session silently sending data off the machine on an outage.
	AllowCloudFallback bool `koanf:"allow_cloud_fallback"`
	// ToolCallProbeTrials is how many times the tool-calling smoke probe
	// (internal/toolcallprobe) is run when measuring a model's conformance
	// *rate* rather than a yes/no verdict (P53.4). Default
	// toolcallprobe.DefaultTrials (5) — the sample SmokeMaxTokens's own
	// calibration used. 1 reduces the probe to a single trial, exactly the
	// pre-P53.4 behavior, and disables the daemon's background refinement
	// entirely. Only ever applies to local Ollama-style providers, the same
	// scope the probe itself has. In the daemon the first trial is the only
	// one on the message path — the rest run in the background — so raising
	// this never adds first-message latency; in `aegis doctor` it is fully
	// blocking, which is why that command announces the trial count before
	// starting.
	ToolCallProbeTrials int `koanf:"tool_call_probe_trials"`
	// ModelCapabilities pre-declares per-model quirks, keyed by model name
	// (P53.5). Aegis normally discovers these by probing — sending `think` and
	// taking the 400, running the tool-calling smoke probe — and persists what
	// it learns under <data_dir>/model_caps.json. A declaration here outranks
	// anything discovered, which serves two purposes: telling Aegis about a
	// model it has never met so the failing request is never sent even once,
	// and overriding a persisted verdict that has gone stale without having to
	// delete the cache file.
	//
	//   provider:
	//     model_capabilities:
	//       "mythos-sec:24b":
	//         think: false          # never send the think parameter
	//       "some-model:latest":
	//         tool_calling: ok      # or "unsupported"
	//
	// Unset fields declare nothing and leave discovery in charge; `think:
	// false` is a declaration of non-support, not merely a default.
	ModelCapabilities map[string]modelcaps.Declared `koanf:"model_capabilities"`
	// ModelHarness declares per-model overrides for the P74.17 harness
	// behaviors (prose-tool-call salvage, argument-shape repair), keyed by
	// model id the same way ModelCapabilities is. Every model starts from the
	// provider-level default LocalPromptProfile() already picks — both
	// behaviors on for a local provider, both off for a cloud one — and a
	// named entry here corrects individual fields on top of that default
	// rather than replacing it, so naming a model only to flip one field
	// leaves the other at the default instead of resetting to false.
	//
	//   provider:
	//     model_harness:
	//       "qwen2.5-coder:1.5b":
	//         argument_shape_repair: true   # even if the provider default is cloud
	//       "gpt-oss:20b":
	//         prose_tool_call_salvage: false # this model's tool calls are already
	//                                         # structured; skip the text scan
	//
	// Unset fields declare nothing and leave the provider-level default in
	// charge, mirroring ModelCapabilities' pointer-field convention.
	ModelHarness map[string]profile.Override `koanf:"model_harness"`
	// ToolCallShim opts a session into the non-native tool-calling fallback
	// (P53.6, internal/toolshim): tool schemas are serialized into the system
	// prompt and the model's tagged JSON is parsed back into tool calls, for
	// models that cannot speak the provider's tool protocol at all — the
	// qwen2.5-coder:1.5b signature Aegis already detects and, without this,
	// only warns about.
	//
	//   provider:
	//     tool_call_shim: on     # "off" (default) | "on"
	//
	// Deliberately explicit-only. Auto-engaging it off a low measured
	// conformance rate (internal/modelcaps) is a follow-up that is only sound
	// once that rate is trustworthy, so "auto" is rejected rather than
	// silently accepted as a no-op. Off is also the safe direction: a shim
	// that quietly turns prose into executable tool calls is a security
	// surface, not a convenience. Parsed calls still pass through the same
	// permission gate, capability check, and workspace confinement as native
	// ones — the shim changes how a call arrives, never what it is allowed to
	// do. An unrecognized value is treated as off; ToolCallShimValid reports
	// whether it was recognized, so a caller can say so rather than leaving a
	// typo looking like a working setting.
	ToolCallShim string `koanf:"tool_call_shim"`
	// MaxConcurrentRequests bounds how many requests may be in flight against
	// the backend at once (P59.9); the rest queue in the adapter until a slot
	// frees. It is a policy the operator sets, not a capacity Aegis infers —
	// no VRAM detection is attempted (P20.3/P17.5 both rejected that).
	//
	//   0 (unset) — auto: local backends get MaxConcurrentRequestsDefaultLocal,
	//               cloud backends stay unbounded.
	//   n > 0     — at most n concurrent requests, whatever the backend.
	//   negative  — explicitly unbounded, including for a local backend.
	//
	// Auto bounds local backends because a single Ollama server is one GPU:
	// every concurrent request is built believing it owns the full detected
	// num_ctx, while Ollama splits its KV cache across OLLAMA_NUM_PARALLEL
	// slots and evicts models to fit. Queueing is honest there; oversubscribing
	// is not. Cloud endpoints fan out across a fleet, so bounding them by
	// default would only slow multi-agent work down.
	MaxConcurrentRequests int `koanf:"max_concurrent_requests"`
	// PromptProfile selects the system-prompt/tool-exposure shape (P25.6):
	// "auto" (default) infers from BaseURL — loopback/localhost gets the
	// "local" profile (trimmed prompt, web_search/web_fetch/security_scan/
	// git_pr deferred, repo map capped) tuned for small local models; any
	// other value is the unchanged "default" profile. "local" or "default"
	// force the choice regardless of BaseURL.
	PromptProfile string `koanf:"prompt_profile"`
	// APIKey is populated from the environment, never from config files.
	APIKey string `koanf:"-"`
}

// VRAMBudgetBytes returns provider.vram_budget_gb in bytes, or 0 when no budget
// is stated (P69.6). Zero is the "plan nothing" value every caller checks, and a
// negative figure reads as zero rather than as an error: a budget is an opt-in
// hint about hardware, and refusing to start the daemon over a mistyped one
// would be a worse trade than ignoring it. The daemon warns instead.
func (p ProviderConfig) VRAMBudgetBytes() int64 {
	if p.VRAMBudgetGB <= 0 {
		return 0
	}
	return int64(p.VRAMBudgetGB * float64(int64(1)<<30))
}

// KVCacheTypeValid reports whether provider.kv_cache_type names a KV cache
// element type Aegis knows how to size. The names mirror
// ollamainfo.KVCacheType and are spelled as literals here for the same reason
// provider.tool_call_probe_trials' default is — the config package stays free of
// a dependency on the package that consumes the value.
func (p ProviderConfig) KVCacheTypeValid() bool {
	switch p.KVCacheType {
	case "", "f16", "q8_0", "q4_0":
		return true
	}
	return false
}

// ResponseHeaderTimeout returns the configured
// provider.response_header_timeout as a time.Duration, substituting
// sse.DefaultResponseHeaderTimeout (30 minutes as of P38.1) when unset or
// non-positive (P35.5).
func (p ProviderConfig) ResponseHeaderTimeout() time.Duration {
	if p.ResponseHeaderTimeoutSec <= 0 {
		return sse.DefaultResponseHeaderTimeout
	}
	return time.Duration(p.ResponseHeaderTimeoutSec) * time.Second
}

// StreamIdleTimeout returns the configured provider.stream_idle_timeout as a
// time.Duration (P59.2), substituting sse.DefaultStreamIdleTimeout when unset.
// A negative configured value is passed through as a negative duration, which
// the adapter reads as "disable the bound" — distinct from unset, so a user who
// wants a legitimately unbounded stream can say so.
func (p ProviderConfig) StreamIdleTimeout() time.Duration {
	if p.StreamIdleTimeoutSec == 0 {
		return sse.DefaultStreamIdleTimeout
	}
	return time.Duration(p.StreamIdleTimeoutSec) * time.Second
}

// MaxConcurrentRequestsDefaultLocal is the in-flight request bound applied to a
// local backend when provider.max_concurrent_requests is unset (P59.9). One,
// because a local server is a single-GPU resource: two concurrent requests do
// not get two GPUs, they get one GPU time-slicing between them.
//
// Measured (2026-08-05, Ollama 0.30.10 / qwen3:14b / 16GB card) rather than
// assumed — see the admissionAdapter doc comment in internal/provider for the
// full numbers and for what the measurement *corrected*. In short: concurrency
// here is a latency cost, not the correctness hazard this constant was
// originally justified by. Four concurrent ~12k-token requests were not
// truncated, so the bound is chosen for turn latency (K=4 costs ~70% worse p50
// for ~60% more aggregate throughput), which is the wrong trade for an
// interactive agent and a defensible one for a batch of sub-agents. An operator
// running swarm work on a host with room raises it explicitly, and gives up
// nothing but latency headroom by doing so.
const MaxConcurrentRequestsDefaultLocal = 1

// AdmissionLimit resolves provider.max_concurrent_requests for one concrete
// (provider name, base URL) pair, returning the number of requests allowed in
// flight against it — 0 meaning unbounded, the value
// provider.WithAdmissionControl reads as "add no layer at all".
//
// It takes the pair rather than reading p.Default/p.BaseURL because a fallback
// target is a different backend with the same policy: a local primary with a
// cloud fallback must not carry its queue depth over to the cloud, and a
// local-to-local fallback must.
func (p ProviderConfig) AdmissionLimit(name, baseURL string) int {
	if p.MaxConcurrentRequests > 0 {
		return p.MaxConcurrentRequests
	}
	if p.MaxConcurrentRequests < 0 {
		return 0 // explicitly unbounded, local included
	}
	if LocalBackend(name, baseURL) {
		return MaxConcurrentRequestsDefaultLocal
	}
	return 0
}

// LocalBackend reports whether a (provider, base URL) pair names a model server
// running on this machine — the native Ollama adapter, or any OpenAI-compatible
// adapter pointed at a loopback address (LM Studio, llama.cpp, a local proxy).
// This is the "one GPU, not a fleet" test AdmissionLimit gates on, and it is
// deliberately broader than providerfactory's local/cloud *data-residency*
// check: an LM Studio endpoint is not a cloud provider for failover purposes
// either, but the question there is where the data goes and the question here is
// how many requests the hardware can hold.
func LocalBackend(name, baseURL string) bool {
	if strings.EqualFold(strings.TrimSpace(name), "ollama") {
		return true
	}
	return isLoopbackBaseURL(baseURL)
}

// ToolCallShimEnabled reports whether provider.tool_call_shim turns the P53.6
// non-native tool-calling fallback on.
func (p ProviderConfig) ToolCallShimEnabled() bool {
	return toolshim.Enabled(p.ToolCallShim)
}

// ToolCallShimValid reports whether provider.tool_call_shim holds a value the
// shim recognizes ("", "off", "on"). Callers surface a false as a warning: an
// unrecognized value is treated as off, and a user who typed "auto" or "true"
// deserves to be told that rather than to discover it from a run that never
// shimmed anything.
func (p ProviderConfig) ToolCallShimValid() bool {
	return toolshim.ValidMode(p.ToolCallShim)
}

// LocalPromptProfile reports whether the "local" prompt profile (P25.6)
// applies: a trimmed system prompt and deferred network/security tool
// schemas, tuned for the latency and instruction-following limits of small
// local models. An explicit PromptProfile of "local"/"default" wins;
// otherwise this auto-detects from BaseURL resolving to loopback.
func (p ProviderConfig) LocalPromptProfile() bool {
	switch strings.ToLower(strings.TrimSpace(p.PromptProfile)) {
	case "local":
		return true
	case "default":
		return false
	default:
		return isLoopbackBaseURL(p.BaseURL)
	}
}

// IsLoopbackBaseURL reports whether raw is a URL whose host resolves to
// loopback (127.0.0.0/8, ::1, or the literal "localhost"). Exported so
// providerfactory can reuse the same loopback test for the P27.2
// (FIND-03) provider.base_url validation as LocalPromptProfile's local-model
// auto-detection below.
func IsLoopbackBaseURL(raw string) bool {
	return isLoopbackBaseURL(raw)
}

// isLoopbackBaseURL reports whether raw is a URL whose host resolves to
// loopback (127.0.0.0/8, ::1, or the literal "localhost") — the signal used
// to auto-detect a local model server (e.g. Ollama's default
// http://localhost:11434).
func isLoopbackBaseURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// MeteredCloudEndpoint reports whether the resolved provider is a remote,
// per-token-billed endpoint — the one case where an unbounded run costs real
// money (P81.15/FIND-15), and therefore the only case that gets a shipped spend
// ceiling.
//
// The line is drawn with IsLoopbackBaseURL rather than a second test of its
// own, deliberately: providerfactory's validateBaseURL already treats "loopback"
// as the boundary between an endpoint the operator is hosting and one they are
// paying, and two predicates that mean the same thing drift apart. So an
// explicit base_url decides on its own — loopback (Ollama, LiteLLM, an
// OpenAI-compatible proxy on this machine) is never metered, anything else is
// treated as remote. Only with no base_url at all does the adapter name decide,
// and only "anthropic" and "openai" bill: exactly the names for which
// providerfactory's defaultProviderHost has a host to name.
//
// It errs toward *metered* for a remote host reached under a local adapter name
// (an Ollama server on another box), and that direction is free rather than
// merely acceptable: a USD cap over unpriced inference never fires, because the
// usage carries no dollar cost to accumulate against it.
func (p ProviderConfig) MeteredCloudEndpoint() bool {
	if strings.TrimSpace(p.BaseURL) != "" {
		return !isLoopbackBaseURL(p.BaseURL)
	}
	switch strings.ToLower(strings.TrimSpace(p.Default)) {
	case "anthropic", "openai":
		return true
	default:
		return false
	}
}

// ProviderFallbackConfig is one entry in ProviderConfig.Fallback.
type ProviderFallbackConfig struct {
	Provider string `koanf:"provider"` // "anthropic", "openai", or "ollama"
	Model    string `koanf:"model"`    // model id for this fallback
	BaseURL  string `koanf:"base_url"` // optional API base override
}
