package persona

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

// builtinFS holds the persona files Aegis ships embedded in the binary, so
// go build/go run need no external files. Each is the same *.md +
// YAML-frontmatter format used by custom personas (internal/persona/load.go)
// — only the source (embedded vs. on-disk) and trust level (Loaded stays
// false) differ.
//
//go:embed builtin/*.md
var builtinFS embed.FS

// builtinOrder preserves the display order of built-in personas. It is a
// plain name list (not content), so it stays a Go literal rather than moving
// into the embedded files.
var builtinOrder = []string{
	"general", "critic", "arbiter", "security", "platform-architect", "security-architect",
	"security-engineer", "appsec-engineer", "developer", "security-researcher",
	"red-team", "risk-assessor", "business-analyst", "data-analyst",
	"network-security-architect", "report-writer", "sre",
	"infrastructure-architect", "cloud-architect", "cloud-security-engineer",
	"security-critic", "security-arbiter",
}

// builtins holds the personas compiled into this binary (parsed from
// builtin/*.md at package init). It is never mutated after init; file-loaded
// personas live in the separate loaded map (load.go) so a hot reload can
// rebuild them without touching built-ins.
var builtins = mustLoadBuiltins()

func mustLoadBuiltins() map[string]Persona {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		panic(fmt.Sprintf("persona: reading embedded builtin dir: %v", err))
	}
	out := make(map[string]Persona, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(pathExt(e.Name()), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), pathExt(e.Name()))
		data, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			panic(fmt.Sprintf("persona: reading embedded builtin %q: %v", e.Name(), err))
		}
		p, err := parsePersonaBytes(name, data)
		if err != nil {
			panic(fmt.Sprintf("persona: parsing embedded builtin %q: %v", e.Name(), err))
		}
		out[name] = p
	}
	missing := make([]string, 0)
	for _, name := range builtinOrder {
		if _, ok := out[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		panic(fmt.Sprintf("persona: builtinOrder references missing embedded persona file(s): %v", missing))
	}
	return out
}

func pathExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}
