// Package skills discovers and loads skill definition files from well-known
// locations, injecting them into the system prompt as named XML blocks.
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
	Name    string // derived from filename without extension
	Content string // raw markdown body
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
		name := strings.TrimSuffix(base, filepath.Ext(base))
		dst = append(dst, Skill{Name: name, Content: string(data)})
	}
	return dst
}

// BuildBlock returns a <skills>…</skills> XML block for all discovered skills,
// or an empty string when none are found. Callers can append this to the system
// prompt without needing to know whether any skills exist.
func BuildBlock(workDir string) string {
	loaded := Discover(workDir)
	if len(loaded) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<skills>\n")
	for _, sk := range loaded {
		fmt.Fprintf(&sb, "<skill name=%q>\n%s\n</skill>\n", sk.Name, strings.TrimSpace(sk.Content))
	}
	sb.WriteString("</skills>")
	return sb.String()
}

// InjectIntoSystem appends discovered skills to a system prompt as XML blocks.
// Returns base unchanged when no skills are found.
func InjectIntoSystem(base, workDir string) string {
	block := BuildBlock(workDir)
	if block == "" {
		return base
	}
	if base == "" {
		return block
	}
	return base + "\n\n" + block
}
