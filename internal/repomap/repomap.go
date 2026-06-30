// Package repomap builds a compact structural overview of a repository — a list
// of source files and their top-level symbols (functions, types, classes) — for
// injection into the model's system prompt. It gives the agent a map of a large
// codebase without reading every file.
//
// Symbol extraction is regex-based and language-aware for common languages,
// favouring breadth and robustness over perfect parsing. The rendered map is
// capped at a byte budget so it never dominates the context window, and a
// content fingerprint enables mtime-based cache invalidation.
package repomap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DefaultMaxBytes caps the rendered map at roughly 2000 tokens (~4 chars/token).
const DefaultMaxBytes = 8000

// Map is a structural overview of a repository.
type Map struct {
	Root        string      `json:"root"`
	Files       []FileEntry `json:"files"`
	Fingerprint string      `json:"fingerprint"`
	GeneratedAt time.Time   `json:"generated_at"`
	maxBytes    int
}

// FileEntry is one source file and the symbols extracted from it.
type FileEntry struct {
	Path    string   `json:"path"`    // repo-relative, slash-separated
	Symbols []string `json:"symbols"` // top-level declaration signatures
}

// Options configures a build.
type Options struct {
	MaxBytes int      // rendered-output cap; 0 = DefaultMaxBytes
	Ignore   []string // extra directory names to skip (in addition to defaults)
}

func (o Options) maxBytes() int {
	if o.MaxBytes <= 0 {
		return DefaultMaxBytes
	}
	return o.MaxBytes
}

// ignoredDirs are directory names never descended into.
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".aegis": true,
	"dist": true, "build": true, "target": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".vscode": true, ".next": true,
}

// langPatterns maps a file extension to the regexps that match a top-level
// declaration line (no leading whitespace). The full trimmed line up to an
// opening brace is used as the symbol signature.
var langPatterns = map[string][]*regexp.Regexp{
	".go": {
		regexp.MustCompile(`^func\s`),
		regexp.MustCompile(`^type\s`),
	},
	".py": {
		regexp.MustCompile(`^(class|def|async def)\s`),
	},
	".js":  jsPatterns,
	".jsx": jsPatterns,
	".ts":  jsPatterns,
	".tsx": jsPatterns,
	".mjs": jsPatterns,
	".rs": {
		regexp.MustCompile(`^(pub\s+)?(fn|struct|enum|trait|impl|mod)\s`),
	},
	".rb": {
		regexp.MustCompile(`^(class|module|def)\s`),
	},
	".java": {
		regexp.MustCompile(`^\s*(public|private|protected)?\s*(static\s+)?(final\s+)?(class|interface|enum|record)\s`),
	},
}

var jsPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(export\s+)?(default\s+)?(async\s+)?function[\s*]`),
	regexp.MustCompile(`^(export\s+)?(default\s+)?(abstract\s+)?class\s`),
	regexp.MustCompile(`^export\s+(const|let|var|interface|type|enum)\s`),
}

// Build walks root, extracts symbols from recognized source files, and returns a
// Map. Files in ignored directories are skipped. Symbol order follows source
// order; file order is sorted for deterministic output and fingerprinting.
func Build(root string, opts Options) (*Map, error) {
	m := &Map{Root: root, GeneratedAt: time.Now(), maxBytes: opts.maxBytes()}
	extraIgnore := make(map[string]bool, len(opts.Ignore))
	for _, d := range opts.Ignore {
		extraIgnore[d] = true
	}

	var fpLines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting the walk
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (ignoredDirs[name] || extraIgnore[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		patterns, ok := langPatterns[strings.ToLower(filepath.Ext(name))]
		if !ok {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		info, infoErr := d.Info()
		if infoErr == nil {
			fpLines = append(fpLines, fmt.Sprintf("%s:%d:%d", rel, info.Size(), info.ModTime().UnixNano()))
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		symbols := extractSymbols(string(data), patterns)
		if len(symbols) > 0 {
			m.Files = append(m.Files, FileEntry{Path: rel, Symbols: symbols})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	sort.Strings(fpLines)
	sum := sha256.Sum256([]byte(strings.Join(fpLines, "\n")))
	m.Fingerprint = hex.EncodeToString(sum[:])
	return m, nil
}

// extractSymbols returns the trimmed declaration lines in src matching any of
// the patterns, with trailing "{" stripped from the signature.
func extractSymbols(src string, patterns []*regexp.Regexp) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, re := range patterns {
			if re.MatchString(trimmed) {
				out = append(out, signature(trimmed))
				break
			}
		}
	}
	return out
}

// signature trims a declaration line down to its signature: everything before
// an opening brace, with trailing whitespace and any trailing colon removed.
func signature(line string) string {
	if i := strings.IndexByte(line, '{'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimRight(strings.TrimSpace(line), " :")
	return line
}

// Render produces the compact text map, capped at the configured byte budget.
// When the cap is hit, output is truncated at a file boundary and a notice is
// appended.
func (m *Map) Render() string {
	budget := m.maxBytes
	if budget <= 0 {
		budget = DefaultMaxBytes
	}
	var b strings.Builder
	b.WriteString("# Repository map\n")
	truncated := false
	for _, f := range m.Files {
		var fb strings.Builder
		fb.WriteString(f.Path)
		fb.WriteString("\n")
		for _, s := range f.Symbols {
			fb.WriteString("  ")
			fb.WriteString(s)
			fb.WriteString("\n")
		}
		if b.Len()+fb.Len() > budget {
			truncated = true
			break
		}
		b.WriteString(fb.String())
	}
	if truncated {
		b.WriteString("… [repo map truncated to fit context budget]\n")
	}
	return b.String()
}

// Save writes the map to a JSON cache file, creating parent directories.
func (m *Map) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		*Map
		Rendered string `json:"rendered"`
	}{Map: m, Rendered: m.Render()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Block wraps a rendered map in <repo_map> tags for the system prompt, or
// returns an empty string when the map is empty.
func Block(rendered string) string {
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return ""
	}
	return "<repo_map>\n" + rendered + "\n</repo_map>"
}

// Load reads a cached map from path and reports whether it is still fresh by
// recomputing the repository fingerprint (a stat-only walk, no file reads). A
// missing cache returns ("", false, nil). The cached rendered text is returned
// regardless of freshness so callers may choose to use a stale map or rebuild.
func Load(root, path string, opts Options) (rendered string, fresh bool, err error) {
	data, readErr := os.ReadFile(path)
	if os.IsNotExist(readErr) {
		return "", false, nil
	}
	if readErr != nil {
		return "", false, readErr
	}
	var cached struct {
		Fingerprint string `json:"fingerprint"`
		Rendered    string `json:"rendered"`
	}
	if jsonErr := json.Unmarshal(data, &cached); jsonErr != nil {
		return "", false, nil // treat a corrupt cache as missing
	}
	current, fpErr := fingerprint(root, opts)
	if fpErr != nil {
		return cached.Rendered, false, nil
	}
	return cached.Rendered, current == cached.Fingerprint, nil
}

// fingerprint computes the repository fingerprint via a stat-only walk, matching
// the scheme used during Build.
func fingerprint(root string, opts Options) (string, error) {
	extraIgnore := make(map[string]bool, len(opts.Ignore))
	for _, d := range opts.Ignore {
		extraIgnore[d] = true
	}
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (ignoredDirs[name] || extraIgnore[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if _, ok := langPatterns[strings.ToLower(filepath.Ext(name))]; !ok {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if info, infoErr := d.Info(); infoErr == nil {
			lines = append(lines, fmt.Sprintf("%s:%d:%d", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), nil
}
