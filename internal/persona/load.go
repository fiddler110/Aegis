package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/trust"
	"go.yaml.in/yaml/v3"
)

// ProjectDir returns the project-level persona directory for projectRoot
// (".aegis/personas"), the directory whose files are gated by workspace
// trust (P27.7/FIND-09) — as opposed to the user/global directory, which is
// operator-authored and always fully trusted.
func ProjectDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".aegis", "personas")
}

// DiscoverDirs returns the standard directories searched for persona files:
// user-global first, project-local second (project overrides global).
func DiscoverDirs(dataDir, projectRoot string) []string {
	return []string{
		filepath.Join(dataDir, "personas"),
		ProjectDir(projectRoot),
	}
}

// LoadFromDirs scans directories for *.md persona files and registers each.
// Later directories override earlier ones, and file personas override
// built-ins of the same name.
//
// projectDir, when non-empty, marks which of dirs is the project-level
// persona directory (see ProjectDir/DiscoverDirs). Personas loaded from it
// have their control fields — mode, tools, rules, output_guard — honored
// only when projectTrusted is true (P27.7/FIND-09): a cloned repository can
// plant a `.aegis/personas/foo.md` with `output_guard: none` and
// `mode: auto` to silently disable guardrails, so those fields are ignored
// (falling back to persona/session defaults) until the operator explicitly
// trusts the workspace (`aegis trust`). Personas loaded from any other
// directory (user/global, or when projectDir is "") are unaffected — the
// system-prompt body still gets the untrusted-provenance wrap (P24.4)
// regardless. Returns the number of personas loaded.
func LoadFromDirs(projectDir string, projectTrusted bool, dirs ...string) int {
	count := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		honorControlFields := dir != projectDir || projectTrusted
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				continue
			}
			p, err := parsePersonaFile(filepath.Join(dir, e.Name()), honorControlFields)
			if err != nil {
				continue
			}
			register(p)
			count++
		}
	}
	return count
}

// Refresh rescans dirs and atomically replaces the file-loaded persona set, so
// edits, additions, and deletions in persona directories take effect without a
// daemon restart. A directory signature (name, size, mtime of every *.md file)
// short-circuits the rescan when nothing changed, making it cheap enough to
// call on every persona list or switch. Returns the number of file personas
// now loaded and whether the set was rebuilt.
//
// projectDir/projectTrusted gate control-field trust exactly as documented on
// LoadFromDirs (P27.7/FIND-09). Callers pass a fixed value for the lifetime
// of the process (the workspace-trust decision is only re-evaluated on
// restart, matching how config.Load()'s own workspace-trust gate behaves),
// so it plays no part in the change-detection signature below.
func Refresh(projectDir string, projectTrusted bool, dirs ...string) (int, bool) {
	sig := dirSignature(dirs)

	mu.RLock()
	unchanged := sig == refreshSig
	n := len(loaded)
	mu.RUnlock()
	if unchanged {
		return n, false
	}

	fresh := map[string]Persona{}
	var freshOrder []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		honorControlFields := dir != projectDir || projectTrusted
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				continue
			}
			p, err := parsePersonaFile(filepath.Join(dir, e.Name()), honorControlFields)
			if err != nil {
				continue
			}
			if _, exists := fresh[p.Name]; !exists {
				freshOrder = append(freshOrder, p.Name)
			}
			fresh[p.Name] = p
		}
	}

	mu.Lock()
	loaded = fresh
	loadedOrder = freshOrder
	refreshSig = sig
	mu.Unlock()
	return len(fresh), true
}

// dirSignature builds a change-detection key from every persona file's name,
// size, and mtime across dirs. Any edit, add, or delete produces a new key.
func dirSignature(dirs []string) string {
	var b strings.Builder
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			fmt.Fprintf(&b, "%s/%s|%d|%d;", dir, e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// register adds or overrides a file-loaded persona.
func register(p Persona) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := loaded[p.Name]; !exists {
		loadedOrder = append(loadedOrder, p.Name)
	}
	loaded[p.Name] = p
}

type frontmatter struct {
	Description string    `yaml:"description"`
	Model       string    `yaml:"model"`
	Mode        string    `yaml:"mode"`
	Tools       []string  `yaml:"tools"`
	Rules       []string  `yaml:"rules"`
	OutputGuard yaml.Node `yaml:"output_guard"`
}

func parsePersonaFile(path string, honorControlFields bool) (Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Persona{}, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	p, err := parsePersonaBytes(name, data, honorControlFields)
	if err != nil {
		return Persona{}, err
	}
	p.Loaded = true
	p.Path = path
	// Unlike built-ins (compiled into the binary), a project/user persona
	// file's body is arbitrary content from disk — a dependency, template
	// repo, or cloned project could plant one to inject instructions into
	// every session that loads it. Wrap it in the same untrusted-provenance
	// marker used for MCP/web tool output (FIND-05/P24.4) rather than
	// splicing it into the system prompt verbatim.
	if p.System != "" {
		// scan=false: unlike a one-off web fetch, this content is re-injected
		// into every session using the persona, and persona/skill prose
		// legitimately discusses its own role/instructions/system-prompt
		// often enough that the heuristic scan (internal/trust) would be
		// noisy here. The provenance marker itself — framing the body as
		// data, not commands — is the mitigation FIND-05 asks for; scanning
		// stays available (scan_output-style) for web/MCP content where a
		// single miss matters more than a session-wide false positive.
		p.System = trust.Wrap("persona_untrusted_content", [][2]string{{"persona", name}},
			"a persona file loaded from this project's or the user's .aegis/personas/ directory, not a built-in",
			p.System, false)
	}
	return p, nil
}

// parsePersonaBytes parses a persona's *.md content (YAML frontmatter + body)
// into a Persona. The caller sets Loaded/Path since those differ between
// disk-loaded custom personas and embedded built-ins.
//
// honorControlFields gates mode/tools/rules/output_guard (P27.7/FIND-09):
// when false, those frontmatter fields are dropped so the persona falls back
// to persona/session defaults for them — the persona's description, model
// choice, and system-prompt body (separately wrapped as untrusted, P24.4)
// are unaffected either way. Model is deliberately not gated here: it only
// selects among models the operator already permitted, not a permission or
// guardrail change.
func parsePersonaBytes(name string, data []byte, honorControlFields bool) (Persona, error) {
	fmText, body := splitFrontmatter(string(data))
	var fm frontmatter
	if fmText != "" {
		if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
			return Persona{}, err
		}
	}
	p := Persona{
		Name:        name,
		Description: fm.Description,
		System:      strings.TrimSpace(body),
		Model:       fm.Model,
		Mode:        fm.Mode,
		Tools:       fm.Tools,
		Rules:       fm.Rules,
		Guard:       parseGuard(fm.OutputGuard),
	}
	if !honorControlFields {
		p.Mode = ""
		p.Tools = nil
		p.Rules = nil
		p.Guard = nil
	}
	return p, nil
}

// parseGuard interprets the output_guard node: a scalar "none"/"false" disables
// the guard; a mapping is decoded into a GuardConfig; absence returns nil.
func parseGuard(n yaml.Node) *GuardConfig {
	switch n.Kind {
	case 0: // absent
		return nil
	case yaml.ScalarNode:
		switch strings.ToLower(strings.TrimSpace(n.Value)) {
		case "none", "false", "off", "disabled":
			return &GuardConfig{Disabled: true}
		}
		return nil
	case yaml.MappingNode:
		var g struct {
			Mode       string   `yaml:"mode"`
			Schema     []string `yaml:"schema"`
			Rubric     string   `yaml:"rubric"`
			MaxRetries int      `yaml:"max_retries"`
		}
		if err := n.Decode(&g); err != nil {
			return nil
		}
		return &GuardConfig{Mode: g.Mode, Schema: g.Schema, Rubric: g.Rubric, MaxRetries: g.MaxRetries}
	}
	return nil
}

// splitFrontmatter splits a markdown file into its YAML frontmatter and body.
// Returns ("", whole) when there is no leading --- block.
func splitFrontmatter(content string) (fm, body string) {
	if !strings.HasPrefix(content, "---") {
		return "", content
	}
	rest := content[3:]
	before, after, found := strings.Cut(rest, "\n---")
	if !found {
		return "", content
	}
	fm = strings.TrimSpace(before)
	body = after
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	}
	return fm, body
}
