package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/workspacetrust"
)

// clearEnv unsets the given env vars for the duration of the test.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { os.Setenv(k, v) })
		} else {
			t.Cleanup(func() { os.Unsetenv(k) })
		}
		os.Unsetenv(k)
	}
}

// redirectConfigDir makes GlobalConfigPath() resolve to an empty temp directory
// so that any real config file the current user may have on disk is not loaded
// during the test.  Cleanup restores the original env vars automatically.
func redirectConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	// Unix (Linux + macOS): defaultDataDir uses os.UserHomeDir() → $HOME
	t.Setenv("HOME", tmp)
	// Windows: defaultDataDir uses os.UserConfigDir() → $APPDATA
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))
	// Prevent XDG_CONFIG_HOME from pointing somewhere real on Linux
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
}

func TestLoadDefaults(t *testing.T) {
	redirectConfigDir(t) // prevent real ~/.config/aegis/config.yaml from loading
	clearEnv(t,
		"AEGIS_PROVIDER_DEFAULT", "AEGIS_PROVIDER_MODEL",
		"AEGIS_PROVIDER_BASE_URL", "AEGIS_PROVIDER_MAX_TOKENS",
		"AEGIS_PROVIDER_MAX_RETRIES",
		"AEGIS_PERMISSION_MODE", "AEGIS_LOG_LEVEL",
		"AEGIS_DATA_DIR", "AEGIS_SERVER_ADDR",
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY",
	)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"provider default", cfg.Provider.Default, "anthropic"},
		{"provider model", cfg.Provider.Model, "claude-opus-4-8"},
		{"server addr", cfg.Server.Addr, "127.0.0.1:4127"},
		{"permission mode", cfg.Permission.Mode, "build"},
		{"log level", cfg.LogLevel, "info"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	if cfg.Provider.MaxTokens != 32768 {
		t.Errorf("max_tokens = %d, want 32768", cfg.Provider.MaxTokens)
	}
	if cfg.MCPServer.AutoApprove {
		t.Error("mcp_server.auto_approve default = true, want false")
	}
	if cfg.MCPServer.DefaultMode != "plan" {
		t.Errorf("mcp_server.default_mode = %q, want %q", cfg.MCPServer.DefaultMode, "plan")
	}
	// P27.12/FIND-14: conservative non-zero caps by default — generous for a
	// normal single-user session, still bounding a runaway/DoS case.
	if cfg.Server.MaxConcurrentRuns != 10 {
		t.Errorf("server.max_concurrent_runs = %d, want 10 (P27.12/FIND-14)", cfg.Server.MaxConcurrentRuns)
	}
	if cfg.Server.MaxRunDurationSec != 1800 {
		t.Errorf("server.max_run_duration_sec = %d, want 1800 (P27.12/FIND-14)", cfg.Server.MaxRunDurationSec)
	}
	if cfg.Server.SSEBufferSize != DefaultSSEBufferSize {
		t.Errorf("server.sse_buffer_size = %d, want %d", cfg.Server.SSEBufferSize, DefaultSSEBufferSize)
	}
	// P27.3/FIND-05: on by default — read-tool/conversation content reaches
	// a cloud provider unredacted otherwise, with no other default control.
	if !cfg.Security.RedactSecrets {
		t.Error("security.redact_secrets default = false, want true (P27.3/FIND-05)")
	}
	// P27.5/FIND-13: pinned-cert loopback TLS on by default — plain HTTP
	// otherwise leaves the bearer token and conversation content readable to
	// another local account on a shared host with packet-capture privilege.
	if !cfg.Server.TLS.Enabled {
		t.Error("server.tls.enabled default = false, want true (P27.5/FIND-13)")
	}
	// P27.13/FIND-12: heuristic prompt-injection scan of web_fetch/web_search
	// output on by default.
	if !cfg.Search.ScanOutput {
		t.Error("search.scan_output default = false, want true (P27.13/FIND-12)")
	}
}

// TestMCPServerConfigScanOutputEnabledDefaultsTrue is the P27.13/FIND-12
// regression for the per-server MCP scan_output toggle: unlike top-level
// scalar keys, defaults() has no mechanism to apply a default to elements of
// a list, so ScanOutput is a *bool (mirroring SecurityToolConfig.Enabled)
// read via ScanOutputEnabled() rather than the field directly. An MCP server
// declared with no scan_output key must still scan by default; an explicit
// `scan_output: false` must still be honored.
func TestMCPServerConfigScanOutputEnabledDefaultsTrue(t *testing.T) {
	unset := MCPServerConfig{}
	if !unset.ScanOutputEnabled() {
		t.Error("ScanOutputEnabled() with unset ScanOutput = false, want true (default on)")
	}
	off := false
	explicitFalse := MCPServerConfig{ScanOutput: &off}
	if explicitFalse.ScanOutputEnabled() {
		t.Error("ScanOutputEnabled() with explicit scan_output: false = true, want false")
	}
	on := true
	explicitTrue := MCPServerConfig{ScanOutput: &on}
	if !explicitTrue.ScanOutputEnabled() {
		t.Error("ScanOutputEnabled() with explicit scan_output: true = false, want true")
	}
}

// TestMCPServerScanOutputFromYAML confirms the *bool default flows correctly
// through a real config.Load() round trip, not just direct struct
// construction: a server with no scan_output key in YAML defaults to
// enabled; a server with an explicit `scan_output: false` stays disabled.
func TestMCPServerScanOutputFromYAML(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	writeProjectConfig(t, `
mcp:
  - name: unspecified
    command: nc
  - name: explicitly-off
    command: nc
    scan_output: false
  - name: explicitly-on
    command: nc
    scan_output: true
`)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// mcp.* is a P27.1 trust-gated key; trust this directory so the project
	// config's mcp: block actually applies for this test.
	if err := workspacetrust.Open(WorkspaceTrustStorePath()).Trust(dir); err != nil {
		t.Fatalf("Trust: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.MCP) != 3 {
		t.Fatalf("cfg.MCP = %d servers, want 3", len(cfg.MCP))
	}
	byName := map[string]MCPServerConfig{}
	for _, m := range cfg.MCP {
		byName[m.Name] = m
	}
	if !byName["unspecified"].ScanOutputEnabled() {
		t.Error(`"unspecified" (no scan_output key) should default to scanning enabled`)
	}
	if byName["explicitly-off"].ScanOutputEnabled() {
		t.Error(`"explicitly-off" (scan_output: false) should stay disabled`)
	}
	if !byName["explicitly-on"].ScanOutputEnabled() {
		t.Error(`"explicitly-on" (scan_output: true) should be enabled`)
	}
}

// TestEnvOverrideServerLimits is the P21.5 counterpart to TestEnvOverride:
// the daemon resource-ceiling keys must be settable via AEGIS_SERVER_* env
// vars the same way server.addr already is.
func TestEnvOverrideServerLimits(t *testing.T) {
	redirectConfigDir(t) // hermetic: don't let the dev's real config leak into the asserted keys
	t.Setenv("AEGIS_SERVER_MAX_CONCURRENT_RUNS", "4")
	t.Setenv("AEGIS_SERVER_MAX_RUN_DURATION_SEC", "600")
	t.Setenv("AEGIS_SERVER_SSE_BUFFER_SIZE", "64")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.MaxConcurrentRuns != 4 {
		t.Errorf("max_concurrent_runs = %d, want 4", cfg.Server.MaxConcurrentRuns)
	}
	if cfg.Server.MaxRunDurationSec != 600 {
		t.Errorf("max_run_duration_sec = %d, want 600", cfg.Server.MaxRunDurationSec)
	}
	if cfg.Server.SSEBufferSize != 64 {
		t.Errorf("sse_buffer_size = %d, want 64", cfg.Server.SSEBufferSize)
	}
}

// TestEnvOverrideServerTLS is the P27.5 regression for the documented
// AEGIS_SERVER_TLS_ENABLED escape hatch (see docs/configuration.md's env var
// table): with server.tls.enabled now defaulting to true, an operator who
// needs plain HTTP (e.g. a container/CI environment with no config file)
// must be able to opt back out via env, not just by hand-editing YAML.
// envKeyCallback's generic single-split heuristic can't reach a field nested
// two levels deep (server.tls.enabled), so this exercises the server_tls_
// special-case explicitly rather than relying on the generic path.
func TestEnvOverrideServerTLS(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("AEGIS_SERVER_TLS_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.TLS.Enabled {
		t.Error("AEGIS_SERVER_TLS_ENABLED=false did not disable TLS")
	}
}

func TestEnvOverride(t *testing.T) {
	redirectConfigDir(t) // hermetic: assert the env override, not a leaked ~/.config value
	t.Setenv("AEGIS_PROVIDER_MODEL", "claude-sonnet-4-6")
	t.Setenv("AEGIS_PERMISSION_MODE", "build")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", cfg.Provider.Model)
	}
	if cfg.Permission.Mode != "build" {
		t.Errorf("mode = %q, want build", cfg.Permission.Mode)
	}
}

func TestEnvBaseURL(t *testing.T) {
	redirectConfigDir(t) // hermetic: assert the env override, not a leaked ~/.config value
	t.Setenv("AEGIS_PROVIDER_DEFAULT", "openai")
	t.Setenv("AEGIS_PROVIDER_BASE_URL", "http://localhost:11434/v1")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("base_url = %q, want http://localhost:11434/v1", cfg.Provider.BaseURL)
	}
}

// TestDefaultDataDir asserts that the config directory path follows the
// correct convention for each OS:
//   - Windows → %AppData%\aegis  (via os.UserConfigDir)
//   - macOS   → ~/.config/aegis  (XDG-compatible, NOT ~/Library/Application Support)
//   - Linux   → ~/.config/aegis
func TestDefaultDataDir(t *testing.T) {
	got := defaultDataDir()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}

	if runtime.GOOS == "windows" {
		// Windows: should sit under %AppData%, not under the home/.config subtree.
		dotConfig := filepath.Join(home, ".config")
		if len(got) >= len(dotConfig) && got[:len(dotConfig)] == dotConfig {
			t.Errorf("on Windows defaultDataDir() = %q; must NOT be under ~/.config", got)
		}
	} else {
		// macOS (darwin) and Linux: must always be ~/.config/aegis.
		want := filepath.Join(home, ".config", appDir)
		if got != want {
			t.Errorf("on %s defaultDataDir() = %q, want %q\n"+
				"Hint: rebuild the binary — do not run an old binary compiled before the fix.",
				runtime.GOOS, got, want)
		}
	}
}

func TestOutputGuardDefaults(t *testing.T) {
	redirectConfigDir(t) // assert the built-in default, not the dev's ~/.config/aegis/config.yaml
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OutputGuard.Enabled {
		t.Error("output_guard.enabled should default to true")
	}
	if cfg.OutputGuard.Mode != "llm" {
		t.Errorf("default mode = %q, want llm", cfg.OutputGuard.Mode)
	}
	if cfg.OutputGuard.MaxRetries != 1 {
		t.Errorf("default max_retries = %d, want 1", cfg.OutputGuard.MaxRetries)
	}
	if cfg.OutputGuard.Rubric == "" {
		t.Error("default rubric should be set")
	}
}

func TestAPIKeyFromEnv(t *testing.T) {
	redirectConfigDir(t) // prevent real config file from overriding provider.default
	clearEnv(t, "AEGIS_PROVIDER_DEFAULT", "OPENAI_API_KEY")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.APIKey != "sk-test-123" {
		t.Errorf("api key not read from env, got %q", cfg.Provider.APIKey)
	}
}

// TestProviderAPIKeyGroqOpenRouterFallback confirms the "openai" provider
// case falls back to GROQ_API_KEY then OPENROUTER_API_KEY when
// OPENAI_API_KEY is unset (P29.4) — Groq/OpenRouter are reached via
// provider.default: openai + a custom base_url, not their own provider name.
func TestProviderAPIKeyGroqOpenRouterFallback(t *testing.T) {
	clearEnv(t, "OPENAI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY")

	if got := ProviderAPIKey("openai"); got != "" {
		t.Errorf("with no env vars set, got %q, want empty", got)
	}

	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	if got := ProviderAPIKey("openai"); got != "sk-or-test" {
		t.Errorf("OPENROUTER_API_KEY fallback: got %q, want sk-or-test", got)
	}

	t.Setenv("GROQ_API_KEY", "gsk-test")
	if got := ProviderAPIKey("openai"); got != "gsk-test" {
		t.Errorf("GROQ_API_KEY should take priority over OPENROUTER_API_KEY: got %q, want gsk-test", got)
	}

	t.Setenv("OPENAI_API_KEY", "sk-test")
	if got := ProviderAPIKey("openai"); got != "sk-test" {
		t.Errorf("OPENAI_API_KEY should take priority over both: got %q, want sk-test", got)
	}
}

// TestLoadDotEnvAppliesPermissionHardening exercises the FIND-29/P24.16
// fsguard.RestrictToOwner call loadDotEnv makes on a successful read. On
// POSIX, fsguard.RestrictToOwner is a no-op, so this mainly guards against a
// regression where the hardening call errors out and (incorrectly) fails
// the whole load; on Windows it also confirms the file remains readable
// and its variables still get applied after the ACL is tightened.
//
// The sample key is deliberately un-prefixed: since P66.1, AEGIS_* keys in a
// .env file are dropped rather than applied (see dotenv_trust_test.go), so an
// AEGIS_-prefixed one would make this test pass or fail for the wrong reason.
func TestLoadDotEnvAppliesPermissionHardening(t *testing.T) {
	clearEnv(t, "TEST_DOTENV_VAR")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("TEST_DOTENV_VAR=hardened\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := loadDotEnv(envPath); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if got := os.Getenv("TEST_DOTENV_VAR"); got != "hardened" {
		t.Errorf("TEST_DOTENV_VAR = %q, want %q", got, "hardened")
	}
}

// TestLoadDotEnvMissingFileNoOp confirms a missing .env file is still a
// silent no-op (not an error) now that a hardening call has been added to
// the success path — the os.IsNotExist short-circuit above it must still
// return before fsguard is ever consulted.
func TestLoadDotEnvMissingFileNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := loadDotEnv(filepath.Join(dir, "does-not-exist.env")); err != nil {
		t.Errorf("loadDotEnv on missing file: %v, want nil", err)
	}
}

// TestProviderConfig_LocalPromptProfile covers P25.6's local-model prompt
// profile auto-detection: loopback/localhost base URLs (with or without a
// port, http or https, IPv6 loopback) select the "local" profile; a remote
// host does not; and an explicit prompt_profile always wins over detection.
// TestProviderConfig_ResponseHeaderTimeout is the P35.5/P38.1 regression: unset
// (zero-value) config substitutes the default (30m as of P38.1), and an
// explicit provider.response_header_timeout (seconds) is honored.
func TestProviderConfig_ResponseHeaderTimeout(t *testing.T) {
	tests := []struct {
		name string
		sec  int
		want time.Duration
	}{
		{"unset defaults to 30m", 0, 30 * time.Minute},
		{"negative also defaults to 30m", -1, 30 * time.Minute},
		{"explicit override wins", 900, 15 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ProviderConfig{ResponseHeaderTimeoutSec: tt.sec}
			if got := p.ResponseHeaderTimeout(); got != tt.want {
				t.Errorf("ResponseHeaderTimeout(response_header_timeout=%d) = %v, want %v", tt.sec, got, tt.want)
			}
		})
	}
}

// TestLoadDefaults_ResponseHeaderTimeout confirms a fresh Load() with no
// provider.response_header_timeout set produces the 30-minute default (P38.1),
// end to end through the config layers.
func TestLoadDefaults_ResponseHeaderTimeout(t *testing.T) {
	redirectConfigDir(t)
	clearEnv(t, "AEGIS_PROVIDER_RESPONSE_HEADER_TIMEOUT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Provider.ResponseHeaderTimeout(); got != 30*time.Minute {
		t.Errorf("default ResponseHeaderTimeout = %v, want 30m", got)
	}
}

// TestEnvOverride_ResponseHeaderTimeout is the env-var half of P35.5: setting
// AEGIS_PROVIDER_RESPONSE_HEADER_TIMEOUT must change the resolved duration.
func TestEnvOverride_ResponseHeaderTimeout(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("AEGIS_PROVIDER_RESPONSE_HEADER_TIMEOUT", "600")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Provider.ResponseHeaderTimeout(); got != 10*time.Minute {
		t.Errorf("ResponseHeaderTimeout after env override = %v, want 10m", got)
	}
}

func TestProviderConfig_LocalPromptProfile(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		profile string
		want    bool
	}{
		{"empty base_url, auto", "", "", false},
		{"ollama default, auto", "http://localhost:11434", "", true},
		{"ollama default with path, auto", "http://localhost:11434/v1", "", true},
		{"127.0.0.1, auto", "http://127.0.0.1:11434", "", true},
		{"127.0.0.1 no port, auto", "http://127.0.0.1", "", true},
		{"IPv6 loopback, auto", "http://[::1]:11434", "", true},
		{"https localhost, auto", "https://localhost:8443/v1", "", true},
		{"LOCALHOST case-insensitive, auto", "http://LOCALHOST:11434", "", true},
		{"remote host, auto", "https://api.anthropic.com", "", false},
		{"remote host with private-looking path, auto", "https://openrouter.ai/api/v1", "", false},
		{"LAN IP is not loopback, auto", "http://192.168.1.10:11434", "", false},
		{"explicit local overrides remote base_url", "https://api.anthropic.com", "local", true},
		{"explicit default overrides loopback base_url", "http://localhost:11434", "default", false},
		{"auto keyword behaves like empty", "http://localhost:11434", "auto", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ProviderConfig{BaseURL: tt.baseURL, PromptProfile: tt.profile}
			if got := p.LocalPromptProfile(); got != tt.want {
				t.Errorf("LocalPromptProfile(base_url=%q, prompt_profile=%q) = %v, want %v", tt.baseURL, tt.profile, got, tt.want)
			}
		})
	}
}

// TestProviderConfig_ToolCallShim pins the P53.6 opt-in: the default and every
// unrecognized spelling leave the shim off, and an unrecognized one is
// reportable rather than silently equivalent to the default — a user who typed
// "auto" (the reserved follow-up value) or "true" has a setting that does
// nothing, and deserves to be told.
func TestProviderConfig_ToolCallShim(t *testing.T) {
	tests := []struct {
		raw         string
		wantEnabled bool
		wantValid   bool
	}{
		{"", false, true},
		{"off", false, true},
		{"on", true, true},
		{"ON", true, true},
		{"auto", false, false},
		{"true", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			p := ProviderConfig{ToolCallShim: tt.raw}
			if got := p.ToolCallShimEnabled(); got != tt.wantEnabled {
				t.Errorf("ToolCallShimEnabled(%q) = %v, want %v", tt.raw, got, tt.wantEnabled)
			}
			if got := p.ToolCallShimValid(); got != tt.wantValid {
				t.Errorf("ToolCallShimValid(%q) = %v, want %v", tt.raw, got, tt.wantValid)
			}
		})
	}
}

// TestToolCallShimDefaultsOff guards the built-in default itself, not just the
// accessor: the whole safety argument for the shim rests on it never arriving
// unasked, and that argument is only as good as this key. Asserted against
// defaults() rather than a full Load so the machine's own config file can't
// decide whether the test passes.
func TestToolCallShimDefaultsOff(t *testing.T) {
	raw, ok := defaults()["provider.tool_call_shim"]
	if !ok {
		t.Fatal("provider.tool_call_shim has no built-in default")
	}
	s, _ := raw.(string)
	p := ProviderConfig{ToolCallShim: s}
	if p.ToolCallShimEnabled() {
		t.Errorf("provider.tool_call_shim defaults to enabled (%q) — it must be opt-in", s)
	}
	if !p.ToolCallShimValid() {
		t.Errorf("the built-in default %q is not a value the shim recognizes", s)
	}
}

// TestRepoMapDefaults pins the P62.1 budget to the numbers internal/repomap
// documents (DefaultMaxBytes 8000, DefaultMaxSymbolsPerFile 3). The two are
// spelled independently — as literals in defaults() and as constants in
// repomap, so the config package needs no dependency on the package it sizes —
// which is exactly the arrangement that can drift silently, since a mismatch
// changes the injected map's shape without failing anything.
func TestRepoMapDefaults(t *testing.T) {
	redirectConfigDir(t) // hermetic: a real ~/.config/aegis/config.yaml must not decide this
	clearEnv(t, "AEGIS_REPOMAP_MAX_BYTES", "AEGIS_REPOMAP_MAX_SYMBOLS_PER_FILE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoMap.MaxBytes != 8000 {
		t.Errorf("repomap.max_bytes = %d, want 8000 (repomap.DefaultMaxBytes)", cfg.RepoMap.MaxBytes)
	}
	if cfg.RepoMap.MaxSymbolsPerFile != 3 {
		t.Errorf("repomap.max_symbols_per_file = %d, want 3 (repomap.DefaultMaxSymbolsPerFile)", cfg.RepoMap.MaxSymbolsPerFile)
	}
}

// TestRepoMapFromYAML covers the project-config path and, with it, the one
// value that must survive untouched: a negative max_symbols_per_file is the
// documented "uncapped" sentinel, so any clamp to a non-negative range would
// silently reinstate the default instead of removing the cap.
func TestRepoMapFromYAML(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)
	writeProjectConfig(t, `
repomap:
  max_bytes: 32000
  max_symbols_per_file: -1
`)
	clearEnv(t, "AEGIS_REPOMAP_MAX_BYTES", "AEGIS_REPOMAP_MAX_SYMBOLS_PER_FILE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoMap.MaxBytes != 32000 {
		t.Errorf("repomap.max_bytes = %d, want 32000", cfg.RepoMap.MaxBytes)
	}
	if cfg.RepoMap.MaxSymbolsPerFile != -1 {
		t.Errorf("repomap.max_symbols_per_file = %d, want -1 (the uncapped sentinel must survive load)", cfg.RepoMap.MaxSymbolsPerFile)
	}
}

// TestEnvOverrideRepoMap is the env-layer counterpart: repomap is a new
// envSections entry, and without it AEGIS_REPOMAP_MAX_BYTES lands on the
// unsplit key "repomap_max_bytes" and is discarded with no error.
func TestEnvOverrideRepoMap(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("AEGIS_REPOMAP_MAX_BYTES", "24000")
	t.Setenv("AEGIS_REPOMAP_MAX_SYMBOLS_PER_FILE", "6")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoMap.MaxBytes != 24000 {
		t.Errorf("repomap.max_bytes = %d, want 24000", cfg.RepoMap.MaxBytes)
	}
	if cfg.RepoMap.MaxSymbolsPerFile != 6 {
		t.Errorf("repomap.max_symbols_per_file = %d, want 6", cfg.RepoMap.MaxSymbolsPerFile)
	}
}
