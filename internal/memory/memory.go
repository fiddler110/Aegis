// Package memory provides file-based, persistent memory and skills that are
// injected into the system prompt, plus helpers to append new memories. This
// mirrors the file-memory model in Claude Code and the self-written skills in
// Hermes/OpenClaw.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const cacheMaxAge = 5 * time.Second

// sourcesCache is a TTL cache for the content returned by Sources.Load and
// Sources.LoadContext. A nil cache pointer means no caching (zero-value Sources).
type sourcesCache struct {
	mu sync.Mutex
	// load caches Sources.Load output
	loadVal    string
	loadExpiry time.Time
	// ctx caches Sources.LoadContext output
	ctxVal    string
	ctxExpiry time.Time
}

// Sources locates memory and skill files for a workspace and the user.
type Sources struct {
	// ProjectRoot is the workspace directory.
	ProjectRoot string
	// DataDir is the per-user data directory (global memory/skills).
	DataDir string
	// cache is an optional TTL cache. Nil means always read from disk (test default).
	cache *sourcesCache
}

// NewSources creates a Sources with a TTL cache to avoid re-reading files on
// every request. Zero-value Sources literals (used in tests) have no cache and
// always read fresh from disk.
func NewSources(projectRoot, dataDir string) Sources {
	return Sources{
		ProjectRoot: projectRoot,
		DataDir:     dataDir,
		cache:       &sourcesCache{},
	}
}

// ProjectMemoryPath returns the project-scoped memory file path.
func (s Sources) ProjectMemoryPath() string {
	return filepath.Join(s.ProjectRoot, ".aegis", "memory.md")
}

// GlobalMemoryPath returns the user-scoped memory file path.
func (s Sources) GlobalMemoryPath() string {
	return filepath.Join(s.DataDir, "memory.md")
}

func (s Sources) skillDirs() []string {
	return []string{
		filepath.Join(s.DataDir, "skills"),
		filepath.Join(s.ProjectRoot, ".aegis", "skills"),
	}
}

// Load assembles the memory/skills block for the system prompt. It returns an
// empty string when no memory or skills exist. When the Sources was created via
// NewSources, results are cached for cacheMaxAge.
func (s Sources) Load() string {
	if s.cache != nil {
		s.cache.mu.Lock()
		defer s.cache.mu.Unlock()
		if time.Now().Before(s.cache.loadExpiry) {
			return s.cache.loadVal
		}
		v := s.loadDirect()
		s.cache.loadVal = v
		s.cache.loadExpiry = time.Now().Add(cacheMaxAge)
		return v
	}
	return s.loadDirect()
}

func (s Sources) loadDirect() string {
	var sections []string

	if txt := readIfExists(s.GlobalMemoryPath()); txt != "" {
		sections = append(sections, "# User memory\n\n"+txt)
	}
	if txt := readIfExists(s.ProjectMemoryPath()); txt != "" {
		sections = append(sections, "# Project memory\n\n"+txt)
	}
	if skills := s.loadSkills(); skills != "" {
		sections = append(sections, "# Skills\n\n"+skills)
	}

	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

func (s Sources) loadSkills() string {
	var parts []string
	for _, dir := range s.skillDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if txt := readIfExists(filepath.Join(dir, name)); txt != "" {
				title := strings.TrimSuffix(name, ".md")
				parts = append(parts, fmt.Sprintf("## %s\n\n%s", title, txt))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// Append adds a timestamped entry to the given memory file, creating it (and
// its parent directory) if needed.
const maxMemoryEntry = 4096

func Append(path, entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return fmt.Errorf("empty memory entry")
	}
	if len(entry) > maxMemoryEntry {
		return fmt.Errorf("memory entry too large (%d bytes, max %d)", len(entry), maxMemoryEntry)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	stamp := time.Now().Format("2006-01-02")
	_, err = fmt.Fprintf(f, "- (%s) %s\n", stamp, entry)
	return err
}

// SaveSkill writes a named skill file under the project skills directory.
func (s Sources) SaveSkill(name, content string) (string, error) {
	name = sanitize(name)
	if name == "" {
		return "", fmt.Errorf("invalid skill name")
	}
	dir := filepath.Join(s.ProjectRoot, ".aegis", "skills")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func readIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func sanitize(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
