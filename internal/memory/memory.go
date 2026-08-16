// Package memory provides file-based, persistent memory that is injected
// into the system prompt, plus helpers to append new memories and save
// skill files (loaded and injected by internal/skills, which handles
// progressive disclosure). This mirrors the file-memory model in Claude
// Code and the self-written skills in Hermes/OpenClaw.
package memory

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/trust"
)

const cacheMaxAge = 5 * time.Second

// sourcesCache is a TTL cache for the content returned by Sources.Load and
// Sources.LoadContext. A nil cache pointer means no caching (zero-value Sources).
type sourcesCache struct {
	mu sync.Mutex
	// load caches Sources.Load output
	loadVal    string
	loadExpiry time.Time
	// ctx caches Sources.LoadContextCapped output. ctxCap records the byte
	// budget it was produced under, so a caller asking for a different cap
	// than the cached one recomputes instead of being served the other
	// caller's size (the daemon and `aegis chat` pass different budgets).
	ctxVal    string
	ctxCap    int
	ctxExpiry time.Time
	// relevance caches LoadRelevant's entries + document-frequency table
	// (P8.5), invalidated on the underlying files' mtime/size rather than a
	// fixed TTL since a stale relevance score is worse than a stale skip.
	relevance relevanceSnapshot
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

	// P24.17 (FIND-30): unlike other readIfExists callers in this package
	// (skills, AGENTS.md/CLAUDE.md context files), memory.md is the one file
	// Aegis itself writes to via Append — so it's the one file where an
	// integrity sidecar makes sense. readMemoryFileChecked prepends a warning
	// if the file was edited outside that write path.
	//
	// P27.6 (FIND-07): memory.md content (global and project alike) can also
	// be hand-edited or planted by anything with filesystem access — same
	// untrusted-provenance risk P24.4 addressed for persona/skill bodies —
	// so wrap it in the same marker before it re-enters the system prompt.
	// The persona/skill precedent doesn't distinguish project from user/
	// global provenance (only compiled-in built-ins are exempt), so both
	// sections are wrapped here too.
	if txt := readMemoryFileChecked(s.GlobalMemoryPath()); txt != "" {
		sections = append(sections, "# User memory\n\n"+wrapMemoryFile("user", txt))
	}
	if txt := readMemoryFileChecked(s.ProjectMemoryPath()); txt != "" {
		sections = append(sections, "# Project memory\n\n"+wrapMemoryFile("project", txt))
	}

	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

// Append adds a timestamped entry to the given memory file, creating it (and
// its parent directory) if needed.
const maxMemoryEntry = 4096

// maxMemoryFileSize bounds a memory.md file's total size (P32.8). Without
// this, a long-running project/user memory accumulates entries forever,
// growing system-prompt injection cost every session (Load() injects the
// whole file, unfiltered) and slowing LoadRelevant's per-entry TF-IDF scan
// linearly. Append enforces it by dropping the oldest entries (FIFO) once
// the file would grow past it — see pruneToCap.
const maxMemoryFileSize = 64 * 1024

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
	stamp := time.Now().Format("2006-01-02")
	_, writeErr := fmt.Fprintf(f, "- (%s) %s\n", stamp, entry)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := pruneToCap(path); err != nil {
		slog.Warn("memory: failed to prune file to size cap", "path", path, "error", err)
	}
	// P24.17 (FIND-30): refresh the integrity sidecar to match the file's
	// full post-append (and, when pruneToCap trimmed it, post-prune) content.
	// This is Aegis's own write path, so the new baseline is trusted; a
	// future load whose content diverges from this baseline without going
	// through Append again gets flagged.
	updateSidecarAfterWrite(path)
	return nil
}

// pruneToCap drops the oldest entries (FIFO — Append is pure append-only, so
// line order is already chronological, oldest first) once path exceeds
// maxMemoryFileSize, so the file stays a bounded, most-recent window instead
// of growing forever (P32.8). No-op when the file is already under the cap.
// The just-appended last line is never dropped, even in the pathological
// case where it alone exceeds the cap (maxMemoryEntry is well under
// maxMemoryFileSize, so that only happens if the cap itself is misconfigured
// very small).
//
// This operates on whole lines, so it assumes the Append-written "- (date)
// entry" format; a hand-edited memory.md using multi-line markdown
// structures (headers, code blocks) can have a structure cut through the
// middle when pruning triggers. That's an accepted tradeoff for keeping this
// a plain size/FIFO policy rather than a markdown-aware one — see P32.8 in
// roadmap.md.
func pruneToCap(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) <= maxMemoryFileSize {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	size := len(data)
	drop := 0
	for size > maxMemoryFileSize && drop < len(lines)-1 {
		size -= len(lines[drop]) + 1 // +1 for the line's trailing newline
		drop++
	}
	kept := strings.Join(lines[drop:], "\n") + "\n"
	return os.WriteFile(path, []byte(kept), 0o600)
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

// wrapMemoryFile tags a memory.md file's content with the same
// untrusted-provenance marker used for file-loaded personas, skills, and
// context files (FIND-05/P24.4, P27.6). scope is "user" or "project" and
// is surfaced both as a tag attribute and in the source description so a
// reviewer can tell which memory file a given block came from.
func wrapMemoryFile(scope, content string) string {
	return trust.Wrap("memory_untrusted_content", [][2]string{{"scope", scope}},
		"the "+scope+" memory.md file", content, false)
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
