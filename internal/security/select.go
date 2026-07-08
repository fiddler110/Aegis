package security

import (
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"sort"
	"strings"
)

// categoryAliases groups scanner names under short, memorable keywords a
// user would actually type (`/scan secrets`, `aegis scan --scanner sast`),
// independent of each ScannerDescriptor's free-text Category field (which
// exists for human display in `aegis security status`, not for selection
// matching — "SCA / IaC / secrets" isn't a clean keyword to type or match).
var categoryAliases = map[string][]string{
	"secrets":   {"gitleaks", "trufflehog"},
	"sast":      {"opengrep", "semgrep", "gosec", "bandit", "brakeman", "njsscan"},
	"sca":       {"osv-scanner", "grype"},
	"deps":      {"osv-scanner", "grype"},
	"iac":       {"kubescape", "hadolint"},
	"misconfig": {"kubescape", "hadolint", "trivy"},
}

// ResolveSelector resolves one user-typed selector token (`/scan trufflehog`,
// `/scan secrets`) to the scanner name(s) it refers to: an exact scanner name
// (case-insensitive) resolves to itself; otherwise a category alias expands
// to its group. ok is false when selector matches neither, so a caller like
// the TUI's /scan dispatcher can fall back to treating the token as a path
// instead of erroring outright on an unrecognized word.
func ResolveSelector(selector string) ([]string, bool) {
	key := strings.ToLower(strings.TrimSpace(selector))
	if key == "" {
		return nil, false
	}
	if _, ok := descriptors[key]; ok {
		return []string{key}, true
	}
	if names, ok := categoryAliases[key]; ok {
		out := make([]string, len(names))
		copy(out, names)
		return out, true
	}
	return nil, false
}

// CategoryAlias describes one category selector token and the scanner names
// it expands to (sorted) — for listing purposes (`aegis scan --list`, `/scan
// list`), separate from ResolveSelector's own internal lookup use.
type CategoryAlias struct {
	Name     string
	Scanners []string
}

// CategoryAliases returns every recognized category alias, sorted by name,
// each with its member scanner names sorted.
func CategoryAliases() []CategoryAlias {
	names := make([]string, 0, len(categoryAliases))
	for n := range categoryAliases {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]CategoryAlias, 0, len(names))
	for _, n := range names {
		members := make([]string, len(categoryAliases[n]))
		copy(members, categoryAliases[n])
		sort.Strings(members)
		out = append(out, CategoryAlias{Name: n, Scanners: members})
	}
	return out
}

// SelectorNames lists every recognized selector token (every scanner name
// plus every category alias), sorted — used to build a helpful error message
// when a selector doesn't resolve.
func SelectorNames() []string {
	names := make([]string, 0, len(descriptors)+len(categoryAliases))
	for n := range descriptors {
		names = append(names, n)
	}
	for a := range categoryAliases {
		names = append(names, a)
	}
	sort.Strings(names)
	return names
}

// SelectScanners filters all down to just the scanners named or grouped by
// selectors (resolved via ResolveSelector, unioned across multiple tokens)
// and force-enables each selected tool in the returned Options — an explicit
// `/scan trufflehog` request overrides both DefaultEnabled and any
// config-level disable, the same way `/scan image <ref>` already runs a
// distinct, explicitly-requested scanner set. Returns an error naming the bad
// token and every valid one (SelectorNames) if any selector doesn't resolve.
// selectors == nil is a no-op: (all, opts, nil).
func SelectScanners(all []Scanner, opts Options, selectors []string) ([]Scanner, Options, error) {
	if len(selectors) == 0 {
		return all, opts, nil
	}
	want := map[string]bool{}
	for _, s := range selectors {
		names, ok := ResolveSelector(s)
		if !ok {
			return nil, opts, fmt.Errorf("unrecognized scanner/category %q — valid: %s", s, strings.Join(SelectorNames(), ", "))
		}
		for _, n := range names {
			want[n] = true
		}
	}
	filtered := make([]Scanner, 0, len(want))
	for _, sc := range all {
		if want[sc.Name()] {
			filtered = append(filtered, sc)
		}
	}
	tools := make(map[string]ToolPolicy, len(opts.Tools))
	maps.Copy(tools, opts.Tools)
	for name := range want {
		p := tools[name]
		p.Enabled = true
		p.EnabledExplicit = true
		tools[name] = p
	}
	opts.Tools = tools
	return filtered, opts, nil
}

// languageScanner maps a detected project language to its dedicated opt-in
// SAST engine (P11.3) — these tools only make sense for a project actually
// written in that language, which is exactly why they default to off rather
// than running (and reporting irrelevant errors) on every project regardless
// of language.
var languageScanner = map[string]string{
	"go":     "gosec",
	"python": "bandit",
	"ruby":   "brakeman",
	"node":   "njsscan",
}

// languageMarkers are the file names/extensions DetectLanguages looks for.
// Deliberately simple — first marker found wins per language, no attempt at
// a full language-identification library — since this only needs to be
// right enough to auto-suggest one of the opt-in language-targeted scanners
// (languageScanner) or, for a language with no dedicated engine wired in yet,
// to name it in a scan preview so the operator knows opengrep's general SAST
// coverage (30+ languages) is what's actually covering it — not to classify
// a repository precisely.
var languageMarkers = map[string][]string{
	"go":     {"go.mod", ".go"},
	"python": {"requirements.txt", "pyproject.toml", "Pipfile", "setup.py", ".py"},
	"ruby":   {"Gemfile", ".rb"},
	"node":   {"package.json", ".ts", ".tsx", ".js", ".jsx"},
	"rust":   {"Cargo.toml", ".rs"},
	"java":   {"pom.xml", "build.gradle", "build.gradle.kts", ".java"},
	"csharp": {".csproj", ".sln", ".cs"},
	"cpp":    {"CMakeLists.txt", ".cpp", ".cc", ".cxx", ".hpp"},
	"c":      {".c", ".h"},
	"php":    {"composer.json", ".php"},
	"kotlin": {".kt", ".kts"},
	"swift":  {"Package.swift", ".swift"},
}

// detectLanguagesMaxFiles bounds DetectLanguages' walk on a huge tree — a
// marker file/extension usually turns up within the first few hundred
// entries; this is a safety cap, not a tuned budget.
const detectLanguagesMaxFiles = 5000

// detectLanguagesSkipDirs are directories DetectLanguages never descends
// into: dependency/build/VCS directories that would otherwise dominate the
// file budget above and can carry another language's marker files (e.g. a
// vendored Go module inside a Node project) that say nothing about what the
// project itself is written in.
var detectLanguagesSkipDirs = map[string]bool{
	".git": true, ".aegis": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	".venv": true, "venv": true, "__pycache__": true,
}

// DetectLanguages walks dir (bounded, skipping common dependency/build/VCS
// directories) and returns every language with at least one marker file or
// extension present, sorted. Best-effort and approximate by design — see
// languageMarkers — good enough to pick a dedicated opt-in SAST engine, not
// a precise repository classification.
func DetectLanguages(dir string) []string {
	found := map[string]bool{}
	seen := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if seen > detectLanguagesMaxFiles || len(found) == len(languageMarkers) {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if path != dir && detectLanguagesSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		name := d.Name()
		ext := filepath.Ext(name)
		for lang, markers := range languageMarkers {
			if found[lang] {
				continue
			}
			for _, m := range markers {
				if name == m || (strings.HasPrefix(m, ".") && ext == m) {
					found[lang] = true
					break
				}
			}
		}
		return nil
	})
	langs := make([]string, 0, len(found))
	for l := range found {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

// DetectedLanguage pairs one language DetectLanguages found with the
// dedicated opt-in SAST engine available for it, if any (languageScanner) —
// Scanner is "" when no dedicated engine is wired in yet, meaning opengrep's
// general multi-language SAST coverage is what actually covers that
// language today. Used to build a human-readable scan preview (`aegis scan`,
// `/scan`) rather than silently leaving an unmapped language undiscoverable.
type DetectedLanguage struct {
	Language string
	Scanner  string
}

// DetectLanguageSummary is DetectLanguages plus, per language, its dedicated
// scanner name if one exists — the shape the interactive scan picker prints
// (e.g. "Detected: go (gosec), rust").
func DetectLanguageSummary(dir string) []DetectedLanguage {
	langs := DetectLanguages(dir)
	out := make([]DetectedLanguage, 0, len(langs))
	for _, l := range langs {
		out = append(out, DetectedLanguage{Language: l, Scanner: languageScanner[l]})
	}
	return out
}

// AutoEnableLanguageScanners returns opts with each detected language's
// dedicated scanner (languageScanner) enabled for this run, unless the
// operator already made an explicit enabled/disabled choice for that tool
// (ToolPolicy.EnabledExplicit) or it's already enabled some other way — an
// explicit config choice always wins over auto-detection, never the other
// way around. This is what lets a plain `/scan <path>` / `aegis scan .`
// "identify the project's language and select the correct scanner" instead
// of requiring security.tools.gosec.enabled: true by hand for a Go project.
func AutoEnableLanguageScanners(dir string, opts Options) Options {
	langs := DetectLanguages(dir)
	if len(langs) == 0 {
		return opts
	}
	tools := make(map[string]ToolPolicy, len(opts.Tools))
	maps.Copy(tools, opts.Tools)
	changed := false
	for _, lang := range langs {
		name, ok := languageScanner[lang]
		if !ok {
			continue
		}
		if p, exists := tools[name]; exists && (p.EnabledExplicit || p.Enabled) {
			continue
		}
		p := tools[name]
		p.Enabled = true
		if p.Method == "" {
			p.Method = opts.DefaultMethod
		}
		tools[name] = p
		changed = true
	}
	if !changed {
		return opts
	}
	opts.Tools = tools
	return opts
}
