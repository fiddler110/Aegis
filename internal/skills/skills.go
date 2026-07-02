// Package skills discovers and loads skill definition files from well-known
// locations. Skills use progressive disclosure (P4.3): at session start only a
// compact index of skill names and descriptions is injected into the system
// prompt; the full body of a skill is loaded on demand via the `skill` tool.
//
// A skill file may carry YAML frontmatter with a `description:` (and optional
// `name:`) field. Skills that declare a description participate in progressive
// disclosure. Skills without a description fall back to eager injection so
// legacy skill files keep working unchanged.
//
// Search order (first match wins per filename):
//  1. .aegis/skills/*.md  in the current working directory (project-local)
//  2. ~/.aegis/skills/*.md  in the user's home directory (global)
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a loaded skill definition.
type Skill struct {
	Name        string // from frontmatter `name:`, else the filename without extension
	Description string // from frontmatter `description:`; empty means eager-inject
	Content     string // markdown body with frontmatter stripped
}

// Discover loads all skill files from the project and user directories.
// Any error reading a directory is silently skipped so a missing .aegis/
// folder doesn't break startup.
func Discover(workDir string) []Skill {
	seen := make(map[string]bool) // filename → already loaded
	var skills []Skill

	// Project-local skills take precedence.
	projectDir := filepath.Join(workDir, ".aegis", "skills")
	skills = appendFromDir(skills, projectDir, seen)

	// User-global skills fill in anything not overridden by the project.
	if home, err := os.UserHomeDir(); err == nil {
		userDir := filepath.Join(home, ".aegis", "skills")
		skills = appendFromDir(skills, userDir, seen)
	}

	return skills
}

func appendFromDir(dst []Skill, dir string, seen map[string]bool) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dst
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		base := e.Name()
		if seen[base] {
			continue
		}
		seen[base] = true
		data, err := os.ReadFile(filepath.Join(dir, base))
		if err != nil {
			continue
		}
		sk := parseSkill(strings.TrimSuffix(base, filepath.Ext(base)), string(data))
		dst = append(dst, sk)
	}
	return dst
}

// parseSkill extracts optional YAML frontmatter (name/description) and returns a
// Skill with the frontmatter stripped from the body. defaultName is used when
// the frontmatter carries no explicit name.
func parseSkill(defaultName, raw string) Skill {
	sk := Skill{Name: defaultName, Content: strings.TrimSpace(raw)}
	body, front, ok := splitFrontmatter(raw)
	if !ok {
		return sk
	}
	sk.Content = strings.TrimSpace(body)
	for _, line := range strings.Split(front, "\n") {
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(strings.Trim(strings.TrimSpace(val), `"'`))
		switch key {
		case "name":
			if val != "" {
				sk.Name = val
			}
		case "description":
			sk.Description = val
		}
	}
	return sk
}

// splitFrontmatter separates a leading `---`-delimited YAML block from the body.
// Returns (body, frontmatter, true) when frontmatter is present.
func splitFrontmatter(raw string) (body, front string, ok bool) {
	trimmed := strings.TrimLeft(strings.TrimPrefix(raw, "\ufeff"), " \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return raw, "", false
	}
	rest := strings.TrimPrefix(trimmed, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return raw, "", false
	}
	front = rest[:end]
	body = rest[end+len("\n---"):]
	// Drop the remainder of the closing delimiter line.
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	} else {
		body = ""
	}
	return body, front, true
}

// Load returns a single discovered skill by name (matching either the
// frontmatter name or the filename), loading its full body on demand.
func Load(workDir, name string) (Skill, bool) {
	for _, sk := range Discover(workDir) {
		if strings.EqualFold(sk.Name, name) {
			return sk, true
		}
	}
	return Skill{}, false
}

// BuildBlock returns a <skills>…</skills> XML block with the full body of every
// discovered skill. Retained for callers that want eager injection of all
// skills regardless of frontmatter.
func BuildBlock(workDir string) string {
	loaded := Discover(workDir)
	if len(loaded) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<skills>\n")
	for _, sk := range loaded {
		fmt.Fprintf(&sb, "<skill name=%q>\n%s\n</skill>\n", sk.Name, sk.Content)
	}
	sb.WriteString("</skills>")
	return sb.String()
}

// BuildIndex implements progressive disclosure. Skills that declare a
// description are listed compactly (name — description) under a
// <skills_available> block, telling the model to load the full body with the
// `skill` tool. Skills without a description are eager-injected in full for
// backward compatibility. Returns an empty string when no skills exist.
func BuildIndex(workDir string) string {
	loaded := Discover(workDir)
	if len(loaded) == 0 {
		return ""
	}
	var described, legacy []Skill
	for _, sk := range loaded {
		if sk.Description != "" {
			described = append(described, sk)
		} else {
			legacy = append(legacy, sk)
		}
	}

	var sb strings.Builder
	if len(described) > 0 {
		sb.WriteString("<skills_available>\n")
		sb.WriteString("These skills can be loaded on demand. When a task matches one, call the `skill` tool with its name to load the full instructions before proceeding.\n")
		for _, sk := range described {
			fmt.Fprintf(&sb, "- %s: %s\n", sk.Name, sk.Description)
		}
		sb.WriteString("</skills_available>")
	}
	for _, sk := range legacy {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "<skill name=%q>\n%s\n</skill>", sk.Name, sk.Content)
	}
	return sb.String()
}

// InjectIntoSystem appends the progressive-disclosure skills index to a system
// prompt. Returns base unchanged when no skills are found.
func InjectIntoSystem(base, workDir string) string {
	block := BuildIndex(workDir)
	if block == "" {
		return base
	}
	if base == "" {
		return block
	}
	return base + "\n\n" + block
}
