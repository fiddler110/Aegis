package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/workspacetrust"
)

// ProviderPatch holds the provider fields to write into the global config file.
type ProviderPatch struct {
	Adapter    string // "anthropic" | "openai" | "ollama"
	BaseURL    string // empty = omit from YAML
	Model      string
	MaxTokens  int
	MaxRetries int   // 0 falls back to 4
	Think      *bool // nil = "~" (provider default)
	// ContextWindow is the serving context window in tokens. For the ollama
	// adapter it is sent as num_ctx, overriding the model's Modelfile pin so a
	// skill-driven run's ~35k-token prompt is not truncated at Ollama's small
	// default (P35.3). 0 = omit the line (auto-detect at daemon start).
	ContextWindow int
}

// PatchGlobalProvider replaces the provider: block in the global config file
// and preserves all other sections. Creates the file if it does not exist.
func PatchGlobalProvider(p ProviderPatch) error {
	if p.MaxRetries <= 0 {
		p.MaxRetries = 4
	}
	path := GlobalConfigPath()

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}

	block := buildProviderBlock(p)
	var out []byte
	if len(existing) == 0 {
		out = []byte("# Aegis configuration\n\n" + block + "\n")
	} else {
		out = spliceSection(existing, "provider", block)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, out, 0o600)
}

func buildProviderBlock(p ProviderPatch) string {
	var b strings.Builder
	b.WriteString("provider:\n")
	fmt.Fprintf(&b, "  default: %s\n", p.Adapter)
	if p.BaseURL != "" {
		fmt.Fprintf(&b, "  base_url: %q\n", p.BaseURL)
	}
	fmt.Fprintf(&b, "  model: %q\n", p.Model)
	if p.ContextWindow > 0 {
		b.WriteString("  # Serving context window in tokens (sent to Ollama as num_ctx,\n")
		b.WriteString("  # overriding the model's Modelfile pin). Sized from the model's\n")
		b.WriteString("  # detected max; raise for more headroom, lower to reclaim VRAM.\n")
		fmt.Fprintf(&b, "  context_window: %d\n", p.ContextWindow)
	}
	fmt.Fprintf(&b, "  max_tokens: %d\n", p.MaxTokens)
	fmt.Fprintf(&b, "  max_retries: %d\n", p.MaxRetries)
	think := "~"
	if p.Think != nil {
		if *p.Think {
			think = "true"
		} else {
			think = "false"
		}
	}
	fmt.Fprintf(&b, "  think: %s\n", think)
	return b.String()
}

// SandboxPatch holds the sandbox fields to write into a config file.
type SandboxPatch struct {
	Backend  string   // "local" | "container" | "auto"
	Runtime  string   // forced runtime; empty = omit
	Priority []string // auto order; empty = omit
	Image    string   // empty = omit
	Network  bool
}

// PatchGlobalSandbox replaces the sandbox: block in the global config file,
// preserving all other sections. Creates the file if it does not exist.
func PatchGlobalSandbox(p SandboxPatch) error {
	return patchSandbox(GlobalConfigPath(), p)
}

// PatchProjectSandbox replaces the sandbox: block in the project-level
// .aegis/config.yaml, preserving all other sections.
//
// sandbox.* is one of the P27.1 workspace-trust-gated keys (FIND-02): a
// successful patch here also records the current directory as trusted,
// since this write is an explicit, local operator action (e.g. `aegis
// sandbox use --project`), not a setting silently inherited from a cloned
// repository's pre-existing config.
func PatchProjectSandbox(p SandboxPatch) error {
	if err := patchSandbox(ProjectConfigPath(), p); err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}
	return workspacetrust.Open(WorkspaceTrustStorePath()).Trust(dir)
}

func patchSandbox(path string, p SandboxPatch) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	block := buildSandboxBlock(p)
	var out []byte
	if len(existing) == 0 {
		out = []byte("# Aegis configuration\n\n" + block + "\n")
	} else {
		out = spliceSection(existing, "sandbox", block)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, out, 0o600)
}

func buildSandboxBlock(p SandboxPatch) string {
	var b strings.Builder
	b.WriteString("sandbox:\n")
	if p.Backend == "" {
		p.Backend = "local"
	}
	fmt.Fprintf(&b, "  backend: %s\n", p.Backend)
	if p.Runtime != "" {
		fmt.Fprintf(&b, "  runtime: %s\n", p.Runtime)
	}
	if len(p.Priority) > 0 {
		fmt.Fprintf(&b, "  priority: [%s]\n", strings.Join(p.Priority, ", "))
	}
	if p.Image != "" {
		fmt.Fprintf(&b, "  image: %q\n", p.Image)
	}
	fmt.Fprintf(&b, "  network: %t\n", p.Network)
	return b.String()
}

// PatchProjectDefaultPersona sets default_persona in the project-level
// .aegis/config.yaml, preserving all other sections. An empty name removes
// the override, falling back to the built-in "general" default.
func PatchProjectDefaultPersona(name string) error {
	return patchDefaultPersona(ProjectConfigPath(), name)
}

// PatchGlobalDefaultPersona sets default_persona in the global config file.
func PatchGlobalDefaultPersona(name string) error {
	return patchDefaultPersona(GlobalConfigPath(), name)
}

func patchDefaultPersona(path, name string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	block := fmt.Sprintf("default_persona: %q\n", name)
	var out []byte
	if len(existing) == 0 {
		out = []byte("# Aegis configuration\n\n" + block + "\n")
	} else {
		out = spliceSection(existing, "default_persona", block)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, out, 0o600)
}

// PatchProjectSkillsEnabled replaces the skills.builtin_enabled list in the
// project-level .aegis/config.yaml, preserving all other sections. Pass the
// full desired set of enabled names, not a delta.
func PatchProjectSkillsEnabled(names []string) error {
	return patchSkillsEnabled(ProjectConfigPath(), names)
}

// PatchGlobalSkillsEnabled replaces the skills.builtin_enabled list in the
// global config file.
func PatchGlobalSkillsEnabled(names []string) error {
	return patchSkillsEnabled(GlobalConfigPath(), names)
}

func patchSkillsEnabled(path string, names []string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	block := buildSkillsBlock(names)
	var out []byte
	if len(existing) == 0 {
		out = []byte("# Aegis configuration\n\n" + block + "\n")
	} else {
		out = spliceSection(existing, "skills", block)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, out, 0o600)
}

func buildSkillsBlock(names []string) string {
	var b strings.Builder
	b.WriteString("skills:\n")
	if len(names) == 0 {
		b.WriteString("  builtin_enabled: []\n")
		return b.String()
	}
	b.WriteString("  builtin_enabled:\n")
	for _, n := range names {
		fmt.Fprintf(&b, "    - %s\n", n)
	}
	return b.String()
}

// SecurityPatch holds the security: fields to write into a config file
// (P11.11 — `/security-config`). EgressThenWrite/NetworkAllowList/DAST are
// carried through unchanged from the caller's current config rather than
// defaulted, since patchSecurity replaces the whole security: block
// wholesale and this patch only ever originates from an editor that means
// to change DefaultMethod/Tools — losing an operator's existing contextual-
// security settings (including the P11.7 DAST target allowlist) as a side
// effect would be a real regression, not a refactor.
type SecurityPatch struct {
	EgressThenWrite  bool
	NetworkAllowList []string
	DefaultMethod    string
	Tools            map[string]SecurityToolConfig
	DAST             DASTConfig
	// WSLDistro, Debate and Multiscanner are carried through for exactly the
	// reason named above: patchSecurity rewrites the whole block, so a field
	// absent here is a field deleted from the operator's config. They were
	// missing while the type was, which meant any write (`/security-config`,
	// `aegis harden`, PATCH /config/security) silently dropped an operator's
	// security.wsl_distro and security.debate settings as a side effect.
	WSLDistro    string
	Debate       DebateIntegrationConfig
	Multiscanner MultiscannerConfig
	// Netscanner is the second built image's pin (P55.7), carried through for
	// the same reason as Multiscanner: `aegis security build-image` rewrites
	// this block, so a netscanner pin absent here is one deleted by whichever
	// of the two images was built second.
	Netscanner NetscannerConfig
}

// PatchProjectSecurity replaces the security: block in the project-level
// .aegis/config.yaml, preserving all other sections.
func PatchProjectSecurity(p SecurityPatch) error {
	return patchSecurity(ProjectConfigPath(), p)
}

// PatchGlobalSecurity replaces the security: block in the global config file.
func PatchGlobalSecurity(p SecurityPatch) error {
	return patchSecurity(GlobalConfigPath(), p)
}

func patchSecurity(path string, p SecurityPatch) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	block := buildSecurityBlock(p)
	var out []byte
	if len(existing) == 0 {
		out = []byte("# Aegis configuration\n\n" + block + "\n")
	} else {
		out = spliceSection(existing, "security", block)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, out, 0o600)
}

func buildSecurityBlock(p SecurityPatch) string {
	var b strings.Builder
	b.WriteString("security:\n")
	fmt.Fprintf(&b, "  egress_then_write: %t\n", p.EgressThenWrite)
	if len(p.NetworkAllowList) == 0 {
		b.WriteString("  network_allowlist: []\n")
	} else {
		b.WriteString("  network_allowlist:\n")
		for _, d := range p.NetworkAllowList {
			fmt.Fprintf(&b, "    - %q\n", d)
		}
	}
	defaultMethod := p.DefaultMethod
	if defaultMethod == "" {
		defaultMethod = "auto"
	}
	fmt.Fprintf(&b, "  default_method: %s\n", defaultMethod)

	if len(p.Tools) == 0 {
		b.WriteString("  tools: {}\n")
	} else {
		b.WriteString("  tools:\n")
		names := make([]string, 0, len(p.Tools))
		for name := range p.Tools {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			tc := p.Tools[name]
			fmt.Fprintf(&b, "    %s:\n", name)
			fmt.Fprintf(&b, "      enabled: %t\n", tc.ToolEnabled())
			if tc.Method != "" {
				fmt.Fprintf(&b, "      method: %s\n", tc.Method)
			}
			if tc.Install != "" {
				fmt.Fprintf(&b, "      install: %s\n", tc.Install)
			}
			if tc.Image != "" {
				fmt.Fprintf(&b, "      image: %q\n", tc.Image)
			}
			if tc.TemplatesVersion != "" {
				fmt.Fprintf(&b, "      templates_version: %q\n", tc.TemplatesVersion)
			}
			if tc.Verify {
				fmt.Fprintf(&b, "      verify: %t\n", tc.Verify)
			}
		}
	}

	b.WriteString("  dast:\n")
	if len(p.DAST.AllowedTargets) == 0 {
		b.WriteString("    allowed_targets: []\n")
	} else {
		b.WriteString("    allowed_targets:\n")
		for _, target := range p.DAST.AllowedTargets {
			fmt.Fprintf(&b, "      - %q\n", target)
		}
	}
	fmt.Fprintf(&b, "    allow_active: %t\n", p.DAST.AllowActive)

	if p.WSLDistro != "" {
		fmt.Fprintf(&b, "  wsl_distro: %s\n", p.WSLDistro)
	}
	if p.Debate.ThreatModel || p.Debate.Triage {
		b.WriteString("  debate:\n")
		fmt.Fprintf(&b, "    threat_model: %t\n", p.Debate.ThreatModel)
		fmt.Fprintf(&b, "    triage: %t\n", p.Debate.Triage)
	}
	// Only written once an image has actually been built. An empty block would
	// otherwise claim an enabled multiscanner with no image, which resolves to
	// a MethodNone reason on every scan.
	if ms := p.Multiscanner; ms.Image != "" || ms.ImageID != "" {
		b.WriteString("  multiscanner:\n")
		fmt.Fprintf(&b, "    enabled: %t\n", ms.Enabled)
		fmt.Fprintf(&b, "    image: %q\n", ms.Image)
		fmt.Fprintf(&b, "    image_id: %q\n", ms.ImageID)
		// Only written when known: an empty value would round-trip as
		// "unknown" anyway, and omitting it keeps a config written by an older
		// binary indistinguishable from one that simply has nothing to record.
		if ms.SourceFingerprint != "" {
			fmt.Fprintf(&b, "    source_fingerprint: %q\n", ms.SourceFingerprint)
		}
		if ms.Runtime != "" {
			fmt.Fprintf(&b, "    runtime: %s\n", ms.Runtime)
		}
		if ms.Concurrency > 0 {
			fmt.Fprintf(&b, "    concurrency: %d\n", ms.Concurrency)
		}
		if len(ms.Tools) == 0 {
			b.WriteString("    tools: []\n")
		} else {
			b.WriteString("    tools:\n")
			for _, t := range ms.Tools {
				fmt.Fprintf(&b, "      - %s\n", t)
			}
		}
	}

	// Same "only once something was built" rule as the multiscanner block above,
	// and the same reason: an enabled block naming no image resolves to a
	// MethodNone reason on every image scan and recon run.
	if ns := p.Netscanner; ns.Image != "" || ns.ImageID != "" {
		b.WriteString("  netscanner:\n")
		fmt.Fprintf(&b, "    enabled: %t\n", ns.Enabled)
		fmt.Fprintf(&b, "    image: %q\n", ns.Image)
		fmt.Fprintf(&b, "    image_id: %q\n", ns.ImageID)
		if ns.SourceFingerprint != "" {
			fmt.Fprintf(&b, "    source_fingerprint: %q\n", ns.SourceFingerprint)
		}
		if ns.Runtime != "" {
			fmt.Fprintf(&b, "    runtime: %s\n", ns.Runtime)
		}
		if len(ns.Tools) == 0 {
			b.WriteString("    tools: []\n")
		} else {
			b.WriteString("    tools:\n")
			for _, t := range ns.Tools {
				fmt.Fprintf(&b, "      - %s\n", t)
			}
		}
	}

	return b.String()
}

// CostPatch holds the cost: fields to write into a config file (used by
// `aegis harden`). Zero values are written as-is (0 = unlimited) — callers
// that only want to tighten caps still explicitly unset should read the
// current config first and only pass through fields they mean to change.
type CostPatch struct {
	BudgetUSD       float64
	MaxTokensPerRun int
	SessionCapUSD   float64
	DailyCapUSD     float64
	SessionTokenCap int
	DailyTokenCap   int
	AlertThreshold  float64
	// MaxWallClockPerRunSec (P52.15) is carried here purely so a caller that
	// rewrites the cost block preserves it. patchCost splices in a freshly
	// built block, so any cost key absent from this struct is silently dropped
	// from the user's file — `aegis harden` does not set a wall-clock bound,
	// but it must not erase one the user set.
	MaxWallClockPerRunSec int
}

// PatchProjectCost replaces the cost: block in the project-level
// .aegis/config.yaml, preserving all other sections.
func PatchProjectCost(p CostPatch) error {
	return patchCost(ProjectConfigPath(), p)
}

// PatchGlobalCost replaces the cost: block in the global config file.
func PatchGlobalCost(p CostPatch) error {
	return patchCost(GlobalConfigPath(), p)
}

func patchCost(path string, p CostPatch) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	block := buildCostBlock(p)
	var out []byte
	if len(existing) == 0 {
		out = []byte("# Aegis configuration\n\n" + block + "\n")
	} else {
		out = spliceSection(existing, "cost", block)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, out, 0o600)
}

func buildCostBlock(p CostPatch) string {
	var b strings.Builder
	b.WriteString("cost:\n")
	fmt.Fprintf(&b, "  budget_usd: %g\n", p.BudgetUSD)
	fmt.Fprintf(&b, "  max_tokens_per_run: %d\n", p.MaxTokensPerRun)
	fmt.Fprintf(&b, "  max_wall_clock_per_run: %d\n", p.MaxWallClockPerRunSec)
	fmt.Fprintf(&b, "  session_cap_usd: %g\n", p.SessionCapUSD)
	fmt.Fprintf(&b, "  daily_cap_usd: %g\n", p.DailyCapUSD)
	fmt.Fprintf(&b, "  session_token_cap: %d\n", p.SessionTokenCap)
	fmt.Fprintf(&b, "  daily_token_cap: %d\n", p.DailyTokenCap)
	alert := p.AlertThreshold
	if alert <= 0 {
		alert = 0.8
	}
	fmt.Fprintf(&b, "  alert_threshold: %g\n", alert)
	return b.String()
}

// spliceSection replaces the named top-level YAML section with newBlock.
// Everything from "key:" to the next top-level key is replaced. If the
// section is not found, newBlock is appended.
func spliceSection(data []byte, key, newBlock string) []byte {
	lines := strings.Split(string(data), "\n")

	start, end := -1, len(lines)
	for i, line := range lines {
		if start < 0 {
			// Unindented, non-comment line starting with "key:"
			if len(line) > 0 && line[0] != '#' && line[0] != ' ' && line[0] != '\t' &&
				(line == key+":" || strings.HasPrefix(line, key+": ") || strings.HasPrefix(line, key+":\t")) {
				start = i
			}
		} else {
			// Next top-level key: starts with [A-Za-z], not a comment
			c := line[0:]
			if len(c) > 0 {
				ch := c[0]
				if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
					end = i
					break
				}
			}
		}
	}

	newLines := strings.Split(strings.TrimRight(newBlock, "\n"), "\n")
	var result []string
	if start >= 0 {
		result = append(result, lines[:start]...)
		result = append(result, newLines...)
		result = append(result, "")
		result = append(result, lines[end:]...)
	} else {
		result = append(result, lines...)
		result = append(result, "")
		result = append(result, newLines...)
		result = append(result, "")
	}
	return []byte(strings.Join(result, "\n"))
}
