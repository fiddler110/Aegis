// Package agentdef holds named sub-agent definitions. Each definition carries a
// system prompt, a permission mode, and an optional allow-list of tools, and is
// referenced by the `agent` tool's subagent_type. Built-ins ship here; file- and
// plugin-based definitions are layered on in a later phase.
package agentdef

import "sort"

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

// Resolve returns the definition for a subagent type. Unknown names fall back to
// the default definition (with ok=false so callers can warn if they wish).
func Resolve(name string) (Definition, bool) {
	if name == "" {
		name = DefaultType
	}
	d, ok := builtins[name]
	if !ok {
		return builtins[DefaultType], false
	}
	return d, true
}

// Names returns the available definition names, sorted.
func Names() []string {
	out := make([]string, 0, len(builtins))
	for n := range builtins {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
