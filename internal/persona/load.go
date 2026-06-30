package persona

import (
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// DiscoverDirs returns the standard directories searched for persona files:
// user-global first, project-local second (project overrides global).
func DiscoverDirs(dataDir, projectRoot string) []string {
	return []string{
		filepath.Join(dataDir, "personas"),
		filepath.Join(projectRoot, ".aegis", "personas"),
	}
}

// LoadFromDirs scans directories for *.md persona files and registers each.
// Later directories override earlier ones, and file personas override built-ins
// of the same name. Returns the number of personas loaded.
func LoadFromDirs(dirs ...string) int {
	count := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				continue
			}
			p, err := parsePersonaFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			register(p)
			count++
		}
	}
	return count
}

// register adds or overrides a persona in the registry and Names list.
func register(p Persona) {
	if _, exists := registry[p.Name]; !exists {
		nameOrder = append(nameOrder, p.Name)
	}
	registry[p.Name] = p
}

type frontmatter struct {
	Description string    `yaml:"description"`
	Model       string    `yaml:"model"`
	Mode        string    `yaml:"mode"`
	Tools       []string  `yaml:"tools"`
	Rules       []string  `yaml:"rules"`
	OutputGuard yaml.Node `yaml:"output_guard"`
}

func parsePersonaFile(path string) (Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Persona{}, err
	}
	fmText, body := splitFrontmatter(string(data))
	var fm frontmatter
	if fmText != "" {
		if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
			return Persona{}, err
		}
	}
	p := Persona{
		Name:        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Description: fm.Description,
		System:      strings.TrimSpace(body),
		Model:       fm.Model,
		Mode:        fm.Mode,
		Tools:       fm.Tools,
		Rules:       fm.Rules,
		Guard:       parseGuard(fm.OutputGuard),
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
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content
	}
	fm = strings.TrimSpace(rest[:idx])
	body = rest[idx+len("\n---"):]
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	}
	return fm, body
}
