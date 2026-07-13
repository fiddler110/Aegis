package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
	// P21.5: unlimited/off by default so existing single-user deployments see
	// no behavior change; sse_buffer_size falls back to a sane built-in cap.
	if cfg.Server.MaxConcurrentRuns != 0 {
		t.Errorf("server.max_concurrent_runs = %d, want 0 (unlimited)", cfg.Server.MaxConcurrentRuns)
	}
	if cfg.Server.MaxRunDurationSec != 0 {
		t.Errorf("server.max_run_duration_sec = %d, want 0 (unlimited)", cfg.Server.MaxRunDurationSec)
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
}

// TestEnvOverrideServerLimits is the P21.5 counterpart to TestEnvOverride:
// the daemon resource-ceiling keys must be settable via AEGIS_SERVER_* env
// vars the same way server.addr already is.
func TestEnvOverrideServerLimits(t *testing.T) {
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

// TestLoadDotEnvAppliesPermissionHardening exercises the FIND-29/P24.16
// fsguard.RestrictToOwner call loadDotEnv makes on a successful read. On
// POSIX, fsguard.RestrictToOwner is a no-op, so this mainly guards against a
// regression where the hardening call errors out and (incorrectly) fails
// the whole load; on Windows it also confirms the file remains readable
// and its variables still get applied after the ACL is tightened.
func TestLoadDotEnvAppliesPermissionHardening(t *testing.T) {
	clearEnv(t, "AEGIS_TEST_DOTENV_VAR")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("AEGIS_TEST_DOTENV_VAR=hardened\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := loadDotEnv(envPath); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if got := os.Getenv("AEGIS_TEST_DOTENV_VAR"); got != "hardened" {
		t.Errorf("AEGIS_TEST_DOTENV_VAR = %q, want %q", got, "hardened")
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
