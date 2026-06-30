package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProviderPatch holds the provider fields to write into the global config file.
type ProviderPatch struct {
	Adapter    string // "anthropic" | "openai"
	BaseURL    string // empty = omit from YAML
	Model      string
	MaxTokens  int
	MaxRetries int   // 0 falls back to 4
	Think      *bool // nil = "~" (provider default)
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
func PatchProjectSandbox(p SandboxPatch) error {
	return patchSandbox(ProjectConfigPath(), p)
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
