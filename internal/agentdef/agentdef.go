// Package agentdef holds named sub-agent definitions. Each definition carries a
// system prompt, a permission mode, and an optional allow-list of tools, and is
// referenced by the `agent` tool's subagent_type. Built-ins ship here; file- and
// plugin-based definitions are layered on in a later phase.
package agentdef

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Definition describes a named sub-agent type.
type Definition struct {
	Name         string
	Description  string
	SystemPrompt string   // child system prompt; empty -> inherit caller's
	Mode         string   // permission mode: "plan" (read-only) or "build"
	Tools        []string // allowed tool names; empty -> all exposed tools
}

const explorePrompt = `You are a focused, read-only exploration sub-agent. Investigate the task using read and search tools only. Do not attempt to modify files or run commands. Report a concise, well-organized answer with concrete file paths and findings — the conclusion the caller needs, not a raw file dump.`

const generalPrompt = `You are a capable general-purpose sub-agent handling a delegated task. Work in small verifiable steps, prefer reading before writing, ground claims in tool output, and return a concise summary of what you did and what you found.`

// builtins are the always-available definitions.
var builtins = map[string]Definition{
	"general": {
		Name:         "general",
		Description:  "General-purpose agent for multi-step delegated tasks.",
		SystemPrompt: generalPrompt,
		Mode:         "build",
	},
	"explore": {
		Name:         "explore",
		Description:  "Read-only search/exploration agent; returns findings, not file dumps.",
		SystemPrompt: explorePrompt,
		Mode:         "plan",
	},
	"plan": {
		Name:        "plan",
		Description: "Read-only planning/analysis agent.",
		Mode:        "plan",
	},
	"build": {
		Name:        "build",
		Description: "Full-access agent for development work.",
		Mode:        "build",
	},
}

// DefaultType is used when the caller omits subagent_type.
const DefaultType = "general"

// custom holds user-defined agent definitions loaded from markdown files.
var (
	customMu sync.RWMutex
	custom   = map[string]Definition{}
)

// Resolve returns the definition for a subagent type. It checks custom
// definitions first (user-defined override builtins), then builtins. Unknown
// names fall back to the default definition (with ok=false).
func Resolve(name string) (Definition, bool) {
	if name == "" {
		name = DefaultType
	}
	customMu.RLock()
	d, ok := custom[name]
	customMu.RUnlock()
	if ok {
		return d, true
	}
	d, ok = builtins[name]
	if !ok {
		return builtins[DefaultType], false
	}
	return d, true
}

// Register adds a custom agent definition. It overrides builtins of the same
// name.
func Register(def Definition) {
	customMu.Lock()
	custom[def.Name] = def
	customMu.Unlock()
}

// Names returns all available definition names (builtins + custom), sorted.
func Names() []string {
	customMu.RLock()
	defer customMu.RUnlock()
	seen := make(map[string]bool)
	for n := range builtins {
		seen[n] = true
	}
	for n := range custom {
		seen[n] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// DiscoverDirs returns the standard directories to search for agent definitions.
func DiscoverDirs(dataDir, projectRoot string) []string {
	return []string{
		filepath.Join(dataDir, "agents"),
		filepath.Join(projectRoot, ".agentharness", "agents"),
	}
}

// LoadFromDirs scans directories for agent definition markdown files and
// registers them. Later directories override earlier ones. Each file has YAML
// frontmatter (name, description, mode, tools) and a body that becomes the
// system prompt.
func LoadFromDirs(dirs ...string) int {
	count := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			def, err := parseDefFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			Register(def)
			count++
		}
	}
	return count
}

// parseDefFile reads an agent definition from a markdown file with YAML
// frontmatter.
func parseDefFile(path string) (Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, err
	}
	content := string(data)

	if !strings.HasPrefix(content, "---") {
		return Definition{}, fmt.Errorf("missing frontmatter in %s", path)
	}
	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) < 2 {
		return Definition{}, fmt.Errorf("unclosed frontmatter in %s", path)
	}
	fm := strings.TrimSpace(parts[0])
	body := strings.TrimSpace(parts[1])

	def := Definition{SystemPrompt: body}

	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "name":
			def.Name = val
		case "description":
			def.Description = val
		case "mode":
			def.Mode = val
		case "tools":
			def.Tools = parseToolList(val)
		}
	}

	if def.Name == "" {
		def.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if def.Mode == "" {
		def.Mode = "build"
	}
	return def, nil
}

func parseToolList(s string) []string {
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// ClearCustom removes all custom definitions (for testing).
func ClearCustom() {
	customMu.Lock()
	custom = map[string]Definition{}
	customMu.Unlock()
}
