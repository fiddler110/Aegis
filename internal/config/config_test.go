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
