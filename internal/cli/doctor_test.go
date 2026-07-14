package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
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

// TestDoctorNamesPodmanMisconfig reproduces P25.2's live-eval misconfig
// (sandbox.backend: podman with no podman runtime present) and checks
// doctor names both the problem and the correcting config key, per P26.1's
// acceptance criteria.
func TestDoctorNamesPodmanMisconfig(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-fake")
	disableAllScanners(t)

	if err := config.PatchGlobalSandbox(config.SandboxPatch{Backend: "podman"}); err != nil {
		t.Fatalf("patch sandbox: %v", err)
	}

	out, err := runDoctor(t)
	if err != nil {
		t.Fatalf("doctor: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "sandbox") {
		t.Errorf("expected a sandbox WARN row, got:\n%s", out)
	}
	if !strings.Contains(out, "sandbox.backend") {
		t.Errorf("expected the correcting config key sandbox.backend to be named, got:\n%s", out)
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

// sseServer starts an httptest server that answers any POST with the given
// SSE-formatted body, mimicking an OpenAI-compatible /chat/completions
// streaming response (the shape internal/provider/openai consumes).
func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
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
	const body = `data: {"choices":[{"delta":{"content":"I would list the files by running ls."},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

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
	const body = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"list_files","arguments":"{\"path\":\".\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

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
