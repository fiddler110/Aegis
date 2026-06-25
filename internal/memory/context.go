package memory

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// contextFiles are well-known context files loaded into the system prompt.
var contextFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	".aegis/context.md",
}

// LoadContext reads well-known context files (AGENTS.md, CLAUDE.md,
// .aegis/context.md) from the project root and returns their combined
// content. Files that don't exist are silently skipped.
func (s Sources) LoadContext() string {
	var sections []string
	for _, name := range contextFiles {
		path := filepath.Join(s.ProjectRoot, name)
		txt := readIfExists(path)
		if txt != "" {
			sections = append(sections, "# "+name+"\n\n"+txt)
		}
	}
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

// LoadIgnorePatterns reads a .aegisignore file from the project root
// and returns the patterns (one per line, # comments stripped). These patterns
// can be used to exclude files from agent operations.
func (s Sources) LoadIgnorePatterns() []string {
	path := filepath.Join(s.ProjectRoot, ".aegisignore")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// ShouldIgnore checks whether path matches any of the ignore patterns.
// Patterns use filepath.Match semantics (glob).
func ShouldIgnore(path string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, filepath.Base(path)); matched {
			return true
		}
		if matched, _ := filepath.Match(p, path); matched {
			return true
		}
	}
	return false
}
