package memory

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/trust"
)

// contextFiles are well-known context files loaded into the system prompt.
var contextFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	".aegis/context.md",
}

// LoadContext reads well-known context files (AGENTS.md, CLAUDE.md,
// .aegis/context.md) from the project root and returns their combined
// content. Files that don't exist are silently skipped. When the Sources was
// created via NewSources, results are cached for cacheMaxAge.
func (s Sources) LoadContext() string {
	if s.cache != nil {
		s.cache.mu.Lock()
		defer s.cache.mu.Unlock()
		if time.Now().Before(s.cache.ctxExpiry) {
			return s.cache.ctxVal
		}
		v := s.loadContextDirect()
		s.cache.ctxVal = v
		s.cache.ctxExpiry = time.Now().Add(cacheMaxAge)
		return v
	}
	return s.loadContextDirect()
}

func (s Sources) loadContextDirect() string {
	var sections []string
	for _, name := range contextFiles {
		path := filepath.Join(s.ProjectRoot, name)
		txt := readIfExists(path)
		if txt != "" {
			sections = append(sections, "# "+name+"\n\n"+wrapContextFile(name, txt))
		}
	}
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

// wrapContextFile tags a project context file's (AGENTS.md/CLAUDE.md/
// .aegis/context.md) content with the same untrusted-provenance marker used
// for file-loaded personas and skills (FIND-05/P24.4). These files live at
// the project root, not compiled into the binary — a cloned repo or
// dependency could plant one to inject instructions into every session
// opened against it, the same risk P24.4 addressed for persona/skill bodies.
// scan=false for the same reason as the persona/skill wrap: this content is
// re-injected every session and legitimately discusses its own
// instructions/role often enough that the heuristic scan would be noisy
// here; the provenance framing itself is the mitigation.
func wrapContextFile(name, content string) string {
	return trust.Wrap("context_untrusted_content", [][2]string{{"file", name}},
		"a "+name+" file loaded from the project root, not authored by Aegis",
		content, false)
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
