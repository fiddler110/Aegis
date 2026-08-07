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
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DefaultMaxBytes caps the rendered map at roughly 2000 tokens (~4 chars/token).
const DefaultMaxBytes = 8000

// schemaVersion is mixed into the repository fingerprint so a change to what
// Build extracts or how Render formats it invalidates on-disk caches from an
// older binary. Bump it whenever the cached shape changes. v2 (P49.1) adds
// per-file import edges — a v1 cache has none, so it must rebuild rather than
// load edge-less.
const schemaVersion = 2

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
	Path    string   `json:"path"`              // repo-relative, slash-separated
	Symbols []string `json:"symbols"`           // top-level declaration signatures
	Imports []string `json:"imports,omitempty"` // dependency edges: repo-relative paths where resolvable, else the raw import token (P49.1)
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

// maxImportsPerFile caps the edges recorded for one file so a machine-generated
// file with hundreds of imports can't dominate the rendered map's byte budget.
const maxImportsPerFile = 40

// Import-extraction regexps. These pull the specifier (module path / package)
// out of an import statement; resolution to a repo-relative path (or leaving it
// as a bare token) happens in the per-language helpers below.
var (
	goImportSingleRe = regexp.MustCompile(`^import\s+(?:[\w.]+\s+|\.\s+|_\s+)?"([^"]+)"`)
	goImportSpecRe   = regexp.MustCompile(`^(?:[\w.]+\s+|\.\s+|_\s+)?"([^"]+)"`)
	pyImportRe       = regexp.MustCompile(`^import\s+([\w.]+)`)
	pyFromRe         = regexp.MustCompile(`^from\s+(\.*[\w.]*)\s+import\s`)
	jsFromRe         = regexp.MustCompile(`(?:^|\s)(?:import|export)\b[^;]*?\bfrom\s+["']([^"']+)["']`)
	jsBareImportRe   = regexp.MustCompile(`^import\s+["']([^"']+)["']`)
	jsRequireRe      = regexp.MustCompile(`(?:require|import)\s*\(\s*["']([^"']+)["']\s*\)`)
	rustUseRe        = regexp.MustCompile(`^\s*(?:pub\s+)?use\s+([^;{]+)`)
	rubyRequireRe    = regexp.MustCompile(`^\s*(require|require_relative)\s+["']([^"']+)["']`)
)

// extractImports returns the dependency edges for one file: repo-relative paths
// where a specifier resolves inside the repo (relative imports, Go module-local
// packages, require_relative), else the raw specifier token (third-party /
// stdlib — still useful signal and cheap to filter). Order follows source order
// with duplicates removed; the list is capped at maxImportsPerFile.
func extractImports(src, ext, rel, goModule string) []string {
	var raw []func() []string
	switch ext {
	case ".go":
		raw = []func() []string{func() []string { return goImports(src, goModule) }}
	case ".py":
		raw = []func() []string{func() []string { return pyImports(src, rel) }}
	case ".js", ".jsx", ".ts", ".tsx", ".mjs":
		raw = []func() []string{func() []string { return jsImports(src, rel) }}
	case ".rs":
		raw = []func() []string{func() []string { return rustImports(src) }}
	case ".rb":
		raw = []func() []string{func() []string { return rubyImports(src, rel) }}
	default:
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, fn := range raw {
		for _, imp := range fn() {
			if imp == "" || seen[imp] {
				continue
			}
			seen[imp] = true
			out = append(out, imp)
			if len(out) >= maxImportsPerFile {
				return out
			}
		}
	}
	return out
}

// goImports handles both single-line `import "x"` and block `import ( ... )`
// forms. A path under the repo's own module resolves to its repo-relative
// package directory; everything else (stdlib, third-party) stays a bare token.
func goImports(src, goModule string) []string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if inBlock {
			if trimmed == ")" || strings.HasPrefix(trimmed, ")") {
				inBlock = false
				continue
			}
			if m := goImportSpecRe.FindStringSubmatch(trimmed); m != nil {
				out = append(out, resolveGoImport(m[1], goModule))
			}
			continue
		}
		if trimmed == "import (" || (strings.HasPrefix(trimmed, "import (") && !strings.Contains(trimmed, `"`)) {
			inBlock = true
			continue
		}
		if m := goImportSingleRe.FindStringSubmatch(trimmed); m != nil {
			out = append(out, resolveGoImport(m[1], goModule))
		}
	}
	return out
}

// resolveGoImport maps a module-local import path to its repo-relative package
// directory (dropping the module prefix); anything else is returned unchanged.
func resolveGoImport(imp, goModule string) string {
	if goModule == "" {
		return imp
	}
	if imp == goModule {
		return "."
	}
	if sub := strings.TrimPrefix(imp, goModule+"/"); sub != imp {
		return sub
	}
	return imp
}

// pyImports handles `import a.b`, `from a.b import c`, and relative
// (`from . import x`, `from ..pkg import y`) forms. Relative specifiers resolve
// against the importing file's package directory; absolute ones stay bare
// dotted tokens.
func pyImports(src, rel string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := pyFromRe.FindStringSubmatch(trimmed); m != nil {
			spec := m[1]
			if strings.HasPrefix(spec, ".") {
				out = append(out, resolvePyRelative(rel, spec))
			} else if spec != "" {
				out = append(out, spec)
			}
			continue
		}
		if m := pyImportRe.FindStringSubmatch(trimmed); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// resolvePyRelative resolves a dotted relative import (leading dots select the
// starting package: one = current, each extra = one level up) to a
// repo-relative path, or leaves it as the raw token if it escapes the repo root.
func resolvePyRelative(rel, spec string) string {
	dots := 0
	for dots < len(spec) && spec[dots] == '.' {
		dots++
	}
	// The first dot selects the file's own package directory; each extra dot
	// climbs one level. Build the path with explicit ".." segments so that
	// climbing above the repo root surfaces as a leading "../" after cleaning.
	segments := []string{path.Dir(rel)}
	for i := 1; i < dots; i++ {
		segments = append(segments, "..")
	}
	if remainder := spec[dots:]; remainder != "" {
		segments = append(segments, strings.ReplaceAll(remainder, ".", "/"))
	}
	joined := path.Clean(path.Join(segments...))
	if joined == "." || joined == ".." || strings.HasPrefix(joined, "../") {
		return spec // escapes repo root; keep the raw token
	}
	return joined
}

// jsImports handles `import … from "x"`, bare `import "x"`, `export … from "x"`,
// and `require("x")` / dynamic `import("x")`. Relative specifiers resolve
// against the importing file's directory; bare package names stay tokens.
func jsImports(src, rel string) []string {
	var out []string
	add := func(spec string) {
		if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
			out = append(out, resolveRelPath(rel, spec))
		} else {
			out = append(out, spec)
		}
	}
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := jsFromRe.FindStringSubmatch(trimmed); m != nil {
			add(m[1])
			continue
		}
		if m := jsBareImportRe.FindStringSubmatch(trimmed); m != nil {
			add(m[1])
			continue
		}
		if m := jsRequireRe.FindStringSubmatch(trimmed); m != nil {
			add(m[1])
		}
	}
	return out
}

// rustImports records `use` paths verbatim (trimmed of any brace group). Rust's
// module system doesn't map cleanly to files without full resolution, so these
// stay as tokens — still useful "what does this pull in" signal.
func rustImports(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if m := rustUseRe.FindStringSubmatch(line); m != nil {
			spec := strings.TrimSpace(m[1])
			if i := strings.IndexByte(spec, '{'); i >= 0 {
				spec = strings.TrimSpace(strings.TrimRight(spec[:i], ": "))
			}
			if spec != "" {
				out = append(out, spec)
			}
		}
	}
	return out
}

// rubyImports resolves `require_relative "x"` against the file's directory and
// keeps `require "x"` (gems / stdlib) as bare tokens.
func rubyImports(src, rel string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if m := rubyRequireRe.FindStringSubmatch(line); m != nil {
			if m[1] == "require_relative" {
				out = append(out, resolveRelPath(rel, m[2]))
			} else {
				out = append(out, m[2])
			}
		}
	}
	return out
}

// resolveRelPath joins a `./`- or `../`-relative specifier against the importing
// file's directory and cleans it to a repo-relative path, or returns the raw
// specifier when it escapes the repo root.
func resolveRelPath(rel, spec string) string {
	joined := path.Clean(path.Join(path.Dir(rel), spec))
	if joined == "." || joined == ".." || strings.HasPrefix(joined, "../") {
		return spec
	}
	return joined
}

// goModulePath reads the module path from root/go.mod, or "" if absent.
func goModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
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
	goModule := goModulePath(root)

	fpLines := []string{fmt.Sprintf("schema:%d", schemaVersion)}
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
			imports := extractImports(string(data), strings.ToLower(filepath.Ext(name)), rel, goModule)
			m.Files = append(m.Files, FileEntry{Path: rel, Symbols: symbols, Imports: imports})
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
// appended naming how many files were omitted.
//
// The count is load-bearing, not cosmetic. On a repository large enough to
// truncate, the surviving prefix can be a tiny fraction of the tree (on Aegis
// itself: 10 of 672 files), and a bare "truncated" notice leaves the model
// believing it has seen a whole small repo rather than the first inch of a
// large one. Naming the omitted count — and the `repomap` tool that can answer
// for the rest — is what lets it know to ask.
func (m *Map) Render() string {
	budget := m.maxBytes
	if budget <= 0 {
		budget = DefaultMaxBytes
	}
	out, shown := m.renderPrefix(budget)
	if shown >= len(m.Files) {
		return out
	}
	// Truncating: re-render with room reserved for the notice so the byte cap
	// holds *including* it. Reserve against the widest possible count so the
	// second pass's (larger) omitted count can never overflow the reservation.
	reserve := len(truncationNotice(len(m.Files)))
	out, shown = m.renderPrefix(budget - reserve)
	return out + truncationNotice(len(m.Files)-shown)
}

// renderPrefix writes as many whole file entries as fit in budget, returning the
// text and how many entries were written.
func (m *Map) renderPrefix(budget int) (string, int) {
	var b strings.Builder
	b.WriteString("# Repository map\n")
	shown := 0
	for _, f := range m.Files {
		// Symbols are the load-bearing content and must never be dropped while
		// this file's entry is kept; render them first and let the whole entry
		// truncate at the file boundary if they don't fit.
		var fb strings.Builder
		fb.WriteString(f.Path)
		fb.WriteString("\n")
		for _, s := range f.Symbols {
			fb.WriteString("  ")
			fb.WriteString(s)
			fb.WriteString("\n")
		}
		if b.Len()+fb.Len() > budget {
			break
		}
		b.WriteString(fb.String())
		shown++
		// Edges render after symbols so a tight budget drops them first: the
		// import line is only written when it still fits, otherwise skipped
		// without truncating the map.
		if edges := renderEdges(f.Imports); edges != "" && b.Len()+len(edges) <= budget {
			b.WriteString(edges)
		}
	}
	return b.String(), shown
}

// truncationNotice formats the closing line for a truncated map. Its length is
// monotonic in omitted, which Render relies on when reserving budget for it.
func truncationNotice(omitted int) string {
	return fmt.Sprintf("… [repo map truncated to fit context budget: %d more file(s) not shown — use the `repomap` tool (action \"map\", \"skeleton\" or \"importers\") to query them]\n", omitted)
}

// renderEdges formats a file's import edges as a single compact "  → a, b, c"
// line (trailing newline), or "" when the file has no imports.
func renderEdges(imports []string) string {
	if len(imports) == 0 {
		return ""
	}
	return "  → " + strings.Join(imports, ", ") + "\n"
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

// LoadOrBuild returns the parsed repository map for root. It reuses the on-disk
// cache at cachePath when the cache is present and its fingerprint still matches
// the repository; otherwise it rebuilds via Build and best-effort refreshes the
// cache (a save failure is ignored so a read-only workspace still yields a map).
// This is the parsed-Map analog of Load — which returns only the rendered text
// used for prompt injection — and backs the on-demand `repomap` query tool
// (P49.2), so map/skeleton/importers read the same cache the injector does.
func LoadOrBuild(root, cachePath string, opts Options) (*Map, error) {
	if data, readErr := os.ReadFile(cachePath); readErr == nil {
		var cached Map
		if json.Unmarshal(data, &cached) == nil && cached.Fingerprint != "" {
			if current, fpErr := fingerprint(root, opts); fpErr == nil && current == cached.Fingerprint {
				cached.maxBytes = opts.maxBytes()
				return &cached, nil
			}
		}
	}
	m, err := Build(root, opts)
	if err != nil {
		return nil, err
	}
	_ = m.Save(cachePath) // best-effort refresh; a read-only workspace still returns the map
	return m, nil
}

// fingerprint computes the repository fingerprint via a stat-only walk, matching
// the scheme used during Build.
func fingerprint(root string, opts Options) (string, error) {
	extraIgnore := make(map[string]bool, len(opts.Ignore))
	for _, d := range opts.Ignore {
		extraIgnore[d] = true
	}
	lines := []string{fmt.Sprintf("schema:%d", schemaVersion)}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (ignoredDirs[name] || extraIgnore[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if _, ok := langPatterns[strings.ToLower(filepath.Ext(name))]; !ok {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
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
