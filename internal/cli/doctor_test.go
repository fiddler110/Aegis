package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/ollamainfo"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
)

func runDoctor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newDoctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// TestDoctorCleanSetupExitsZero exercises the acceptance criterion directly:
// a config with nothing misconfigured (cloud provider + API key present,
// sandbox left at its "local" default, every scanner disabled) reports no
// FAIL rows and a nil error (main.go maps that to exit 0). The sandbox row
// itself is expected to be a WARN, not a PASS, since P27.14/FIND-04: local
// gives no isolation and doctor now says so rather than passing silently.
//
// Unlike TestDoctorNamesPodmanMisconfig this needs no selectSandbox seam
// (P34.7): the "local" default takes SelectSandbox's `case "", "local"`,
// which constructs the local backend without probing for a runtime. Every
// other row is deterministic here too — the provider/tool-calling rows reach
// the host only behind ollamaNativeBase, a pure-config predicate that is ""
// for this cloud config, and disableAllScanners keeps the scanners row from
// calling security.Resolve. The daemon row is the one that still reads the
// host (it probes cfg.Server.Addr, so a maintainer running `aegis serve`
// gets PASS instead of WARN, plus possible extra rows), but it cannot flip
// either assertion below: it emits only PASS/WARN and never FAIL by design,
// and the "no isolation" WARN comes from the sandbox row regardless.
func TestDoctorCleanSetupExitsZero(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-fake")
	disableAllScanners(t)

	out, err := runDoctor(t)
	if err != nil {
		t.Fatalf("doctor: %v\noutput:\n%s", err, out)
	}
	if strings.Contains(out, "FAIL") {
		t.Errorf("expected no FAIL rows in clean setup, got:\n%s", out)
	}
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "no isolation") {
		t.Errorf("expected the local-backend sandbox row to WARN about no isolation, got:\n%s", out)
	}
}

// fakeContainerBackend stands in for the *sandbox.ContainerBackend
// SelectSandbox returns once a container runtime is present. doctorSandboxCheck
// only ever reads Name() (and Close()s it), so the exec methods are never
// called.
type fakeContainerBackend struct{ name string }

func (f fakeContainerBackend) Name() string { return f.name }
func (f fakeContainerBackend) Exec(context.Context, string, sandbox.ExecOpts) (string, error) {
	return "", nil
}
func (f fakeContainerBackend) ExecStreaming(context.Context, string, sandbox.ExecOpts, func(string)) error {
	return nil
}
func (f fakeContainerBackend) Close() error { return nil }

// withSelectSandbox injects a deterministic sandbox-selection result for the
// duration of one test, the same shape as internal/security's
// withDetectRuntime.
func withSelectSandbox(t *testing.T, fn func(cfg config.SandboxConfig, cwd string, logger *slog.Logger) (sandbox.Backend, bool, string, error)) {
	t.Helper()
	orig := selectSandbox
	selectSandbox = fn
	t.Cleanup(func() { selectSandbox = orig })
}

// doctorRow returns the rendered report line for the named check plus its
// "-> fix" continuation line, so an assertion can be scoped to one row
// instead of matching anywhere in the report.
func doctorRow(t *testing.T, out, name string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != name {
			continue
		}
		if i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "->") {
			return line + "\n" + lines[i+1]
		}
		return line
	}
	t.Fatalf("no %q row in doctor output:\n%s", name, out)
	return ""
}

// TestDoctorNamesPodmanMisconfig reproduces P25.2's live-eval misconfig
// (sandbox.backend: podman with no podman runtime present) and checks doctor
// names both the problem and the correcting config key, per P26.1's
// acceptance criteria.
//
// Both branches are asserted through the selectSandbox seam rather than
// whatever the host happens to have installed (P34.7). Before the seam this
// test reached the real runtime probe, so it only passed on machines without
// podman — i.e. precisely when it was not exercising the misconfig it claims
// to cover, and starting a podman machine turned it red. The seam also lets
// the runtime-present branch be asserted at all, which no host-dependent
// version could do in the same run.
func TestDoctorNamesPodmanMisconfig(t *testing.T) {
	cases := []struct {
		name         string
		sb           sandbox.Backend
		fallback     bool
		reason       string
		wantSeverity string
		wantContains []string
	}{
		{
			// No podman on the host: SelectSandbox falls back to the real
			// local backend and reports why, exactly as it does when
			// NewContainerBackend returns ErrNoContainerRuntime.
			name:     "runtime absent",
			sb:       sandbox.NewLocalBackendWithEnv(nil),
			fallback: true,
			reason: fmt.Sprintf("configured sandbox backend %q unavailable (%v) — running unsandboxed on the host",
				"container", sandbox.ErrNoContainerRuntime),
			wantSeverity: "WARN",
			// The problem and the correcting config key, per P26.1.
			wantContains: []string{"is not active", "sandbox.backend"},
		},
		{
			// Podman present: the configured backend is the active one, so
			// there is no misconfig left to name.
			name:         "runtime present",
			sb:           fakeContainerBackend{name: "container:podman"},
			fallback:     false,
			wantSeverity: "PASS",
			wantContains: []string{`configured "container", active "container:podman"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			redirectConfigDir(t)
			t.Setenv("ANTHROPIC_API_KEY", "sk-test-fake")
			disableAllScanners(t)

			if err := config.PatchGlobalSandbox(config.SandboxPatch{Backend: "podman"}); err != nil {
				t.Fatalf("patch sandbox: %v", err)
			}

			withSelectSandbox(t, func(cfg config.SandboxConfig, _ string, _ *slog.Logger) (sandbox.Backend, bool, string, error) {
				// config.Normalize rewrites the runtime name typed into
				// backend ("podman") into the backend/runtime pair
				// SelectSandbox actually switches on — assert that still
				// happens, since it is what makes this a live misconfig
				// rather than an unknown-backend error.
				if cfg.Backend != "container" || cfg.Runtime != "podman" {
					t.Errorf(`sandbox.backend "podman" normalized to backend=%q runtime=%q, want "container"/"podman"`, cfg.Backend, cfg.Runtime)
				}
				return tc.sb, tc.fallback, tc.reason, nil
			})

			out, err := runDoctor(t)
			if err != nil {
				t.Fatalf("doctor: %v\noutput:\n%s", err, out)
			}
			row := doctorRow(t, out, "sandbox")
			if !strings.HasPrefix(row, tc.wantSeverity) {
				t.Errorf("sandbox row: want %s, got:\n%s", tc.wantSeverity, row)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(row, want) {
					t.Errorf("sandbox row missing %q, got:\n%s", want, row)
				}
			}
		})
	}
}

// disableAllScanners writes an explicit "enabled: false" for every built-in
// scanner descriptor, so doctorScannerCheck reports "no scanners enabled"
// regardless of what's actually installed on the machine running the test.
func disableAllScanners(t *testing.T) {
	t.Helper()
	falseVal := false
	tools := make(map[string]config.SecurityToolConfig)
	for _, d := range security.Descriptors() {
		tools[d.Name] = config.SecurityToolConfig{Enabled: &falseVal}
	}
	if err := config.PatchGlobalSecurity(config.SecurityPatch{DefaultMethod: "auto", Tools: tools}); err != nil {
		t.Fatalf("disable scanners: %v", err)
	}
}

func TestLooksLikeThinkingModel(t *testing.T) {
	trueVal := true
	cases := []struct {
		name string
		cfg  config.ProviderConfig
		want bool
	}{
		{"explicit think", config.ProviderConfig{Think: &trueVal}, true},
		{"reasoning effort", config.ProviderConfig{ReasoningEffort: "high"}, true},
		{"deep model name", config.ProviderConfig{Model: "qwen3.6:35b-a3b-deep"}, true},
		{"deepseek model name", config.ProviderConfig{Model: "deepseek-r1:70b"}, true},
		{"plain model", config.ProviderConfig{Model: "llama3.2"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeThinkingModel(tc.cfg); got != tc.want {
				t.Errorf("looksLikeThinkingModel(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}

func TestDoctorWorkspaceTrustCheck(t *testing.T) {
	pass := doctorWorkspaceTrustCheck(&config.Config{})
	if pass.Severity != doctorPass {
		t.Errorf("no frozen settings: got %v, want pass", pass.Severity)
	}

	warn := doctorWorkspaceTrustCheck(&config.Config{
		WorkspaceTrust: config.WorkspaceTrustStatus{Frozen: true, Changes: []string{"permission: mode build -> auto"}},
	})
	if warn.Severity != doctorWarn {
		t.Errorf("frozen settings: got %v, want warn", warn.Severity)
	}
	if !strings.Contains(warn.Fix, "aegis trust") {
		t.Errorf("fix hint should point at `aegis trust`, got %q", warn.Fix)
	}
}

func TestDoctorGuardCheck(t *testing.T) {
	base := config.ProviderConfig{Model: "qwen3.6:35b-a3b-deep"}

	warn := doctorGuardCheck(&config.Config{
		OutputGuard: config.OutputGuardConfig{Enabled: true, Mode: "llm"},
		Provider:    base,
	})
	if warn.Severity != doctorWarn {
		t.Errorf("thinking model + no small_model: got %v, want warn", warn.Severity)
	}

	withSmall := base
	withSmall.SmallModel = "llama3.2"
	pass := doctorGuardCheck(&config.Config{
		OutputGuard: config.OutputGuardConfig{Enabled: true, Mode: "llm"},
		Provider:    withSmall,
	})
	if pass.Severity != doctorPass {
		t.Errorf("thinking model + small_model set: got %v, want pass", pass.Severity)
	}

	disabled := doctorGuardCheck(&config.Config{
		OutputGuard: config.OutputGuardConfig{Enabled: false, Mode: "llm"},
		Provider:    base,
	})
	if disabled.Severity != doctorPass {
		t.Errorf("guard disabled: got %v, want pass", disabled.Severity)
	}
}

func TestDoctorWorkdirCheck(t *testing.T) {
	loopback := doctorWorkdirCheck(&config.Config{})
	if loopback.Severity != doctorPass {
		t.Errorf("loopback default: got %v, want pass", loopback.Severity)
	}

	remoteNoAllowlist := doctorWorkdirCheck(&config.Config{
		Server: config.ServerConfig{AllowRemote: true},
	})
	if remoteNoAllowlist.Severity != doctorWarn {
		t.Errorf("allow_remote with empty allowlist: got %v, want warn", remoteNoAllowlist.Severity)
	}

	remoteWithAllowlist := doctorWorkdirCheck(&config.Config{
		Server: config.ServerConfig{AllowRemote: true, SessionWorkdirAllowlist: []string{"/srv/projects"}},
	})
	if remoteWithAllowlist.Severity != doctorPass {
		t.Errorf("allow_remote with allowlist entries: got %v, want pass", remoteWithAllowlist.Severity)
	}
}

func TestSamePath(t *testing.T) {
	a := filepath.Join(os.TempDir(), "aegis-doctor-samepath")
	b := a + string(filepath.Separator)
	if !samePath(a, b) {
		t.Errorf("samePath(%q, %q) = false, want true", a, b)
	}
	if samePath(a, a+"-other") {
		t.Errorf("samePath(%q, %q) = true, want false", a, a+"-other")
	}
}

// TestDoctorProviderCheckMissingAPIKey exercises the cloud-provider fail
// path without any network dependency.
func TestDoctorProviderCheckMissingAPIKey(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "anthropic", APIKey: ""}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := doctorProviderCheck(ctx, cfg)
	if got.Severity != doctorFail {
		t.Errorf("missing API key: got %v, want fail", got.Severity)
	}
	if !strings.Contains(got.Fix, "ANTHROPIC_API_KEY") {
		t.Errorf("fix hint missing env var name: %q", got.Fix)
	}
}

// TestDoctorToolCallCheckSkipsCloudProvider confirms the P28.2 smoke test
// never fires a live network call for a cloud provider — it's scoped to
// local (Ollama-style) providers only, where the tool-calling reliability
// variance was actually observed. This is also what keeps
// TestDoctorCleanSetupExitsZero (which configures a fake ANTHROPIC_API_KEY,
// no reachable model) free of any live-network dependency in CI.
func TestDoctorToolCallCheckSkipsCloudProvider(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "anthropic", APIKey: "sk-test-fake", Model: "claude-x"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := doctorToolCallCheck(ctx, cfg)
	if got.Severity != doctorPass {
		t.Errorf("cloud provider: got %v, want pass (skipped)", got.Severity)
	}
	if !strings.Contains(got.Detail, "skipped") {
		t.Errorf("expected a skipped detail message, got %q", got.Detail)
	}
}

// TestDoctorToolCallCheckSkipsUnresolvedModel confirms the check skips
// rather than making a live call when model is still "auto"/unresolved.
func TestDoctorToolCallCheckSkipsUnresolvedModel(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "ollama", Model: "auto"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := doctorToolCallCheck(ctx, cfg)
	if got.Severity != doctorPass {
		t.Errorf("unresolved model: got %v, want pass (skipped)", got.Severity)
	}
}

// TestDoctorDeepFlagAddsRow confirms `--deep` appends the P39.4 structured
// multi-turn fill row, and that omitting it leaves `aegis doctor`'s output
// byte-for-byte unchanged — this tier must be strictly opt-in, since (unlike
// doctor's other checks) it needs a live, reachable model and takes
// meaningfully longer.
func TestDoctorDeepFlagAddsRow(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-fake")
	disableAllScanners(t)

	without, err := runDoctor(t)
	if err != nil {
		t.Fatalf("doctor: %v\noutput:\n%s", err, without)
	}
	if strings.Contains(without, "structured multi-turn fill") {
		t.Errorf("expected no structured-multi-turn-fill row without --deep, got:\n%s", without)
	}

	with, err := runDoctor(t, "--deep")
	if err != nil {
		t.Fatalf("doctor --deep: %v\noutput:\n%s", err, with)
	}
	if !strings.Contains(with, "structured multi-turn fill") {
		t.Errorf("expected a structured-multi-turn-fill row with --deep, got:\n%s", with)
	}
	// A cloud provider config means the check must skip (no live network
	// call), same gating as doctorToolCallCheck.
	if !strings.Contains(with, "skipped") {
		t.Errorf("expected the deep-fill row to skip for a cloud provider, got:\n%s", with)
	}
}

// TestDoctorDeepFillCheckSkipsCloudProvider mirrors
// TestDoctorToolCallCheckSkipsCloudProvider: the P39.4 probe is scoped to
// local (Ollama-style) providers only.
func TestDoctorDeepFillCheckSkipsCloudProvider(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "anthropic", APIKey: "sk-test-fake", Model: "claude-x"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := doctorDeepFillCheck(ctx, cfg)
	if got.Severity != doctorPass {
		t.Errorf("cloud provider: got %v, want pass (skipped)", got.Severity)
	}
	if !strings.Contains(got.Detail, "skipped") {
		t.Errorf("expected a skipped detail message, got %q", got.Detail)
	}
}

// TestDoctorDeepFillCheckDetectsFabrication drives the P39.4 probe against a
// scripted local model that claims the fill task is complete on its first
// turn with zero tool calls — P38.6's shape — and confirms it WARNs (never
// FAILs) and names the failure shape.
func TestDoctorDeepFillCheckDetectsFabrication(t *testing.T) {
	const body = `{"message":{"role":"assistant","content":"The document is now complete — all sections filled."},"done":false}
{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop"}
`
	srv := sseServer(t, body)
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: srv.URL, Model: "qwen3:14b"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := doctorDeepFillCheck(ctx, cfg)
	if got.Severity != doctorWarn {
		t.Fatalf("fabricated completion: got %v, want warn; detail=%q", got.Severity, got.Detail)
	}
	if !strings.Contains(got.Detail, "claimed completion without executing the work") {
		t.Errorf("expected detail to name the fabrication shape, got %q", got.Detail)
	}
}

// TestDoctorDeepFillCheckWarnsOnTransportFailure confirms an unreachable
// local model server degrades to WARN, not FAIL, matching
// doctorToolCallCheck's contract.
func TestDoctorDeepFillCheckWarnsOnTransportFailure(t *testing.T) {
	srv := sseServer(t, "")
	unreachable := srv.URL
	srv.Close()

	cfg := &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: unreachable, Model: "qwen3:14b"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := doctorDeepFillCheck(ctx, cfg)
	if got.Severity != doctorWarn {
		t.Fatalf("unreachable server: got %v, want warn (never fail); detail=%q", got.Severity, got.Detail)
	}
}

// sseServer starts an httptest server that answers any POST with the given
// newline-delimited-JSON body, mimicking Ollama's native /api/chat streaming
// response (the shape internal/provider/ollama consumes; provider "ollama"
// is native as of P33.9, not OpenAI-compat SSE).
func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDoctorToolCallCheckDetectsZeroToolCalls reproduces the P28.2 live-eval
// failure mode directly: a model that answers an obviously-actionable
// prompt in prose, with no tool_calls in the response, must WARN (never
// FAIL — this must stay non-fatal for offline/CI use) and name the doc
// pointer in Fix.
func TestDoctorToolCallCheckDetectsZeroToolCalls(t *testing.T) {
	const body = `{"message":{"role":"assistant","content":"I would list the files by running ls."},"done":false}
{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop"}
`
	srv := sseServer(t, body)
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: srv.URL, Model: "deepseek-r1:8b"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := doctorToolCallCheck(ctx, cfg)
	if got.Severity != doctorWarn {
		t.Fatalf("zero tool calls: got %v, want warn; detail=%q", got.Severity, got.Detail)
	}
	if !strings.Contains(got.Detail, "zero tool calls") {
		t.Errorf("expected detail to call out zero tool calls, got %q", got.Detail)
	}
	if !strings.Contains(got.Fix, "docs/providers.md") {
		t.Errorf("expected fix to point at docs/providers.md, got %q", got.Fix)
	}
}

// TestDoctorToolCallCheckPassesOnToolCall confirms a model that does call
// the smoke-test tool reports PASS with the observed call count.
func TestDoctorToolCallCheckPassesOnToolCall(t *testing.T) {
	const body = `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"list_files","arguments":{"path":"."}}}]},"done":false}
{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":3}
`
	srv := sseServer(t, body)
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: srv.URL, Model: "gpt-oss:20b"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := doctorToolCallCheck(ctx, cfg)
	if got.Severity != doctorPass {
		t.Fatalf("one tool call: got %v, want pass; detail=%q", got.Severity, got.Detail)
	}
	if !strings.Contains(got.Detail, "1 tool call") {
		t.Errorf("expected detail to report the call count, got %q", got.Detail)
	}
}

// TestDoctorToolCallCheckWarnsOnTransportFailure confirms an unreachable
// local model server degrades to WARN, not FAIL — this check must never be
// able to make `aegis doctor` exit non-zero on its own.
func TestDoctorToolCallCheckWarnsOnTransportFailure(t *testing.T) {
	srv := sseServer(t, "")
	unreachable := srv.URL
	srv.Close() // close immediately so the port is refused

	cfg := &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: unreachable, Model: "qwythos:latest"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := doctorToolCallCheck(ctx, cfg)
	if got.Severity != doctorWarn {
		t.Fatalf("unreachable server: got %v, want warn (never fail); detail=%q", got.Severity, got.Detail)
	}
}

// withDetectOllamaInfo injects a deterministic ollamainfo.Detect result for one
// test, so the P35.3 context_window calibration can be asserted without a live
// Ollama server — the same seam shape as withSelectSandbox.
func withDetectOllamaInfo(t *testing.T, fn func(context.Context, string, string) (ollamainfo.Result, bool)) {
	t.Helper()
	orig := detectOllamaInfo
	detectOllamaInfo = fn
	t.Cleanup(func() { detectOllamaInfo = orig })
}

// TestDoctorProviderAdapterCalibratesContextWindow (P35.3): a legacy-compat
// config whose model reports a large real max gets a calibrated
// context_window recommendation, not the fixed 16GB-VRAM baseline a
// skill-driven session overruns.
func TestDoctorProviderAdapterCalibratesContextWindow(t *testing.T) {
	withDetectOllamaInfo(t, func(context.Context, string, string) (ollamainfo.Result, bool) {
		return ollamainfo.Result{ModelMax: 262144}, true
	})
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default: "openai", BaseURL: "http://localhost:11434/v1", Model: "qwen3.6:35b-a3b-fast",
	}}

	got := doctorProviderAdapterCheck(context.Background(), cfg)
	if got.Severity != doctorWarn {
		t.Fatalf("severity = %v, want WARN; detail=%q", got.Severity, got.Detail)
	}
	if !strings.Contains(got.Fix, "provider.context_window: 131072") {
		t.Errorf("fix should recommend the calibrated 131072, got: %q", got.Fix)
	}
	if strings.Contains(got.Fix, "context_window: 32768") {
		t.Errorf("fix should not fall back to the fixed 32768 when the real max is known, got: %q", got.Fix)
	}
}

// TestDoctorProviderAdapterFallsBackWhenUndetectable: when the model's max
// can't be probed (Ollama unreachable at doctor time), the recommendation
// falls back to the baseline rather than erroring.
func TestDoctorProviderAdapterFallsBackWhenUndetectable(t *testing.T) {
	withDetectOllamaInfo(t, func(context.Context, string, string) (ollamainfo.Result, bool) {
		return ollamainfo.Result{}, false
	})
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default: "openai", BaseURL: "http://localhost:11434/v1", Model: "somemodel",
	}}

	got := doctorProviderAdapterCheck(context.Background(), cfg)
	if got.Severity != doctorWarn {
		t.Fatalf("severity = %v, want WARN; detail=%q", got.Severity, got.Detail)
	}
	if !strings.Contains(got.Fix, "provider.context_window: 32768") {
		t.Errorf("fix should fall back to baseline 32768 when the max is undetectable, got: %q", got.Fix)
	}
}

// TestDoctorProviderAdapterPassesOnNativeAdapter: a native "ollama" provider is
// not on the legacy compat path, so the check passes and never probes.
func TestDoctorProviderAdapterPassesOnNativeAdapter(t *testing.T) {
	withDetectOllamaInfo(t, func(context.Context, string, string) (ollamainfo.Result, bool) {
		t.Fatal("Detect must not be called for a native-adapter config")
		return ollamainfo.Result{}, false
	})
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default: "ollama", BaseURL: "http://localhost:11434", Model: "llama3.2",
	}}
	if got := doctorProviderAdapterCheck(context.Background(), cfg); got.Severity != doctorPass {
		t.Fatalf("severity = %v, want PASS; detail=%q", got.Severity, got.Detail)
	}
}
