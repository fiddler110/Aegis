// Package config loads layered Aegis configuration.
//
// Precedence (lowest to highest): built-in defaults -> global config file ->
// per-project config file (./.aegis/config.yaml) -> environment
// variables (AEGIS_*). API keys are always read from the environment.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config is the fully resolved harness configuration.
type Config struct {
	DataDir    string              `koanf:"data_dir"`
	LogLevel   string              `koanf:"log_level"`
	Provider   ProviderConfig      `koanf:"provider"`
	Server     ServerConfig        `koanf:"server"`
	Permission PermissionConfig    `koanf:"permission"`
	Diagram    DiagramConfig       `koanf:"diagram"`
	Cost       CostConfig          `koanf:"cost"`
	Swarm      SwarmConfig         `koanf:"swarm"`
	Sandbox    SandboxConfig       `koanf:"sandbox"`
	Security   SecurityConfig      `koanf:"security"`
	LSP        []LSPServerConfig   `koanf:"lsp"`
	Plugins    []ProcessToolConfig `koanf:"plugins"`
	MCP        []MCPServerConfig   `koanf:"mcp"`
}

// SwarmConfig configures multi-agent sub-agent execution.
type SwarmConfig struct {
	Backend string `koanf:"backend"` // "in_process" (default) or "subprocess"
}

// SandboxConfig configures command execution isolation.
type SandboxConfig struct {
	Backend string `koanf:"backend"` // "local" (default) or "container"
	Runtime string `koanf:"runtime"` // container runtime preference: "docker", "podman", "container" (Apple); empty = auto-detect
	Image   string `koanf:"image"`   // container image (default "ubuntu:22.04")
	Network bool   `koanf:"network"` // allow network access inside containers (default false)
}

// CostConfig configures spend tracking.
type CostConfig struct {
	BudgetUSD float64 `koanf:"budget_usd"` // abort a run past this estimated cost; 0 = unlimited
}

// LSPServerConfig configures one LSP language server.
type LSPServerConfig struct {
	Name       string   `koanf:"name"`       // e.g. "gopls"
	Command    string   `koanf:"command"`    // executable
	Args       []string `koanf:"args"`       // CLI arguments
	Extensions []string `koanf:"extensions"` // file extensions (e.g. [".go"])
}

// ProcessToolConfig declares one external process tool (plugin).
type ProcessToolConfig struct {
	Name        string   `koanf:"name"`
	Description string   `koanf:"description"`
	Command     string   `koanf:"command"`
	Args        []string `koanf:"args"`
	InputSchema string   `koanf:"input_schema"` // JSON Schema as a string
	Capability  string   `koanf:"capability"`   // "read", "write", "execute", "network"
	TimeoutSec  int      `koanf:"timeout_sec"`
}

// MCPServerConfig configures one external MCP server to connect over stdio or HTTP.
type MCPServerConfig struct {
	Name    string            `koanf:"name"`
	Command string            `koanf:"command"`
	Args    []string          `koanf:"args"`
	Env     map[string]string `koanf:"env"`
	Auth    string            `koanf:"auth"` // Bearer token for HTTP servers
}

// ProviderConfig selects and configures the model provider.
type ProviderConfig struct {
	Default         string            `koanf:"default"`          // adapter name: "anthropic", "openai", "ollama"
	Model           string            `koanf:"model"`            // model id
	SmallModel      string            `koanf:"small_model"`      // optional fast model for title gen + compaction (falls back to Model)
	MaxTokens       int               `koanf:"max_tokens"`       // response token cap
	BaseURL         string            `koanf:"base_url"`         // optional API base override
	MaxRetries      int               `koanf:"max_retries"`      // transient-failure retries; 0 disables
	MaxIterations   int               `koanf:"max_iterations"`   // max engine turns per run; 0 = harness default (40)
	LoopThreshold   int               `koanf:"loop_threshold"`   // identical-turn abort count; 0 = harness default (5)
	Headers         map[string]string `koanf:"headers"`          // extra HTTP headers sent with every request (e.g. gateway auth)
	Think           *bool             `koanf:"think"`            // controls extended thinking for Ollama reasoning models (nil/false = disable; true = enable)
	ReasoningEffort string            `koanf:"reasoning_effort"` // OpenAI o1/o3 reasoning_effort: "low", "medium", "high", or "" (omit)
	ContextWindow   int               `koanf:"context_window"`   // model context window in tokens; 0 = auto (skips compaction for local models)
	// APIKey is populated from the environment, never from config files.
	APIKey string `koanf:"-"`
}

// ServerConfig configures the local daemon.
type ServerConfig struct {
	Addr string `koanf:"addr"` // host:port the daemon listens on
}

// PermissionConfig sets the default agent permission posture.
type PermissionConfig struct {
	Mode            string `koanf:"mode"`              // "plan" or "build"
	AutoApproveExec bool   `koanf:"auto_approve_exec"` // auto-approve shell/execute tool calls
}

// SecurityConfig configures contextual security policies.
type SecurityConfig struct {
	EgressThenWrite  bool     `koanf:"egress_then_write"` // require approval for writes after network egress
	NetworkAllowList []string `koanf:"network_allowlist"` // restrict network calls to these domains (empty = no restriction)
}

// DiagramConfig configures diagram rendering.
type DiagramConfig struct {
	KrokiURL string `koanf:"kroki_url"` // Kroki endpoint for multi-format rendering
}

const (
	// EnvPrefix is the environment-variable prefix for overrides.
	EnvPrefix = "AEGIS_"
	appDir    = "aegis"
)

func defaults() map[string]any {
	return map[string]any{
		"data_dir":                     defaultDataDir(),
		"log_level":                    "info",
		"provider.default":             "anthropic",
		"provider.model":               "claude-opus-4-8",
		"provider.max_tokens":          32768,
		"provider.max_retries":         4,
		"server.addr":                  "127.0.0.1:4127",
		// "build" is the intentional default: the agent can read and write
		// files freely, but shell/execute calls still prompt for approval
		// (or are denied non-interactively). Use "plan" for read-only
		// exploration and "auto" (with auto_approve_exec: true) only in
		// fully trusted, sandboxed environments.
		"permission.mode":              "build",
		"permission.auto_approve_exec": false,
		"diagram.kroki_url":            "https://kroki.io",
		"cost.budget_usd":              0.0,
		"swarm.backend":                "in_process",
		"sandbox.backend":              "local",
		"sandbox.image":                "ubuntu:22.04",
		"sandbox.network":              false,
		"security.egress_then_write":   false,
	}
}

// defaultDataDir returns the per-user data directory for the harness.
// Windows : %AppData%\aegis   (e.g. C:\Users\scott\AppData\Roaming\aegis)
// macOS   : ~/.config/aegis   (XDG-compatible; avoids ~/Library/Application Support)
// Linux   : ~/.config/aegis   (XDG default)
func defaultDataDir() string {
	if runtime.GOOS != "windows" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".config", appDir)
		}
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appDir)
}

// GlobalConfigPath returns the path to the user-level config file.
func GlobalConfigPath() string {
	return filepath.Join(defaultDataDir(), "config.yaml")
}

// ProjectConfigPath returns the path to the project-level config file.
func ProjectConfigPath() string {
	return filepath.Join(".aegis", "config.yaml")
}

// loadDotEnv reads a .env-style file and sets any variables it contains into
// the process environment, skipping variables already present so that real
// environment variables always take precedence. The file format is KEY=VALUE
// per line; lines beginning with # and blank lines are ignored. Values may be
// surrounded by single or double quotes (stripped on read).
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if k == "" {
			continue
		}
		// Real env vars take precedence; only inject missing keys.
		if _, exists := os.LookupEnv(k); !exists {
			if err := os.Setenv(k, v); err != nil {
				return fmt.Errorf("setenv %s: %w", k, err)
			}
		}
	}
	return nil
}

// Load resolves configuration from all layers and returns the result.
func Load() (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(defaults(), "."), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	// Load .aegis/.env before other layers so secrets (MCP tokens, API keys)
	// can be set there without living in version-controlled config files.
	// Real environment variables always override values in the .env file.
	if err := loadDotEnv(filepath.Join(".aegis", ".env")); err != nil {
		return nil, fmt.Errorf("load .aegis/.env: %w", err)
	}

	for _, path := range []string{GlobalConfigPath(), ProjectConfigPath()} {
		if _, err := os.Stat(path); err != nil {
			continue // missing config files are fine
		}
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load config %s: %w", path, err)
		}
	}

	// Env: AEGIS_PROVIDER_MODEL -> provider.model, AEGIS_PROVIDER_BASE_URL -> provider.base_url
	// Only the first underscore after a known section prefix becomes a dot;
	// remaining underscores stay as-is so compound field names (base_url,
	// max_tokens, etc.) are preserved.
	sections := map[string]bool{
		"provider": true, "server": true, "permission": true,
		"diagram": true, "cost": true, "swarm": true,
		"sandbox": true, "security": true,
	}
	envCb := func(s string) string {
		s = strings.ToLower(strings.TrimPrefix(s, EnvPrefix))
		if idx := strings.Index(s, "_"); idx > 0 {
			if sections[s[:idx]] {
				return s[:idx] + "." + s[idx+1:]
			}
		}
		return s
	}
	if err := k.Load(env.Provider(EnvPrefix, ".", envCb), nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.Provider.APIKey = providerAPIKey(cfg.Provider.Default)

	// Expand $VAR / ${VAR} references in MCP auth tokens so secrets can be
	// kept in environment variables or .aegis/.env rather than in the YAML.
	for i := range cfg.MCP {
		if cfg.MCP[i].Auth != "" {
			cfg.MCP[i].Auth = os.ExpandEnv(cfg.MCP[i].Auth)
		}
	}

	return &cfg, nil
}

// providerAPIKey reads the API key for the given provider from the environment.
func providerAPIKey(provider string) string {
	switch provider {
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "ollama":
		if k := os.Getenv("OPENAI_API_KEY"); k != "" {
			return k
		}
		return "ollama"
	default:
		return ""
	}
}

// EnsureDataDir creates the configured data directory if it does not exist.
func (c *Config) EnsureDataDir() error {
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir %s: %w", c.DataDir, err)
	}
	return nil
}

// SessionDBPath returns the path to the session database.
func (c *Config) SessionDBPath() string {
	return filepath.Join(c.DataDir, "sessions.db")
}

// LogPath returns the path to the harness log file.
func (c *Config) LogPath() string {
	return filepath.Join(c.DataDir, "aegis.log")
}

// AuthTokenPath returns the path to the daemon auth token file.
func (c *Config) AuthTokenPath() string {
	return filepath.Join(c.DataDir, "daemon.token")
}
