package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/tool"
)

// repomapMaxBytes is the render budget the on-demand tool uses. It is far larger
// than the injected block's local cap (4000 bytes) because the whole point of a
// pull-on-demand query is to expose the parts truncated out of the always-on
// block; a 200KB ceiling still bounds a pathological repo.
const repomapMaxBytes = 200000

// repomapTool is a deferred, read-capability query over the structural
// repository map (files, top-level symbols, import edges) built by
// internal/repomap. It lets the model ask "what's in this file / who imports it /
// show me the map" without spending a read or a repo-wide grep (P49.2).
type repomapTool struct{ root string }

func (t *repomapTool) Name() string                { return "repomap" }
func (t *repomapTool) Capability() tool.Capability { return tool.CapRead }

func (t *repomapTool) Description() string {
	return "Query the repository's structural map (files, top-level symbols, and import/dependency edges) on demand, without reading files or grepping. Actions: \"map\" (the whole map, optionally filtered by a path glob), \"skeleton\" (one file's symbols and imports), \"importers\" (which files import a given file — its blast radius)."
}

func (t *repomapTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"action":{"type":"string","enum":["map","skeleton","importers"],"description":"map: whole repo map; skeleton: one file's symbols+imports; importers: files that import the target"},"path":{"type":"string","description":"repo-relative file path; required for skeleton and importers"},"glob":{"type":"string","description":"optional path glob to filter the map action (e.g. 'internal/tool/*')"}},"required":["action"]}`)
}

func (t *repomapTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"result":{"type":"string","description":"rendered map / skeleton / importer list"}},"required":["result"]}`)
}

func (t *repomapTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Action string `json:"action"`
		Path   string `json:"path"`
		Glob   string `json:"glob"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}

	root := effectiveRoot(ctx, t.root)
	cache := filepath.Join(root, ".aegis", "repomap.json")
	// One load for the whole call: LoadOrBuild reuses the fresh on-disk cache the
	// injector writes, or rebuilds+refreshes it. The large budget only affects
	// rendering, not the fingerprint, so it never defeats cache reuse.
	m, err := repomap.LoadOrBuild(root, cache, repomap.Options{MaxBytes: repomapMaxBytes})
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("repomap unavailable: %v", err), IsError: true}, nil
	}

	switch args.Action {
	case "map":
		return t.mapAction(m, args.Glob)
	case "skeleton":
		return t.fileAction(root, m, args.Path, false)
	case "importers":
		return t.fileAction(root, m, args.Path, true)
	case "":
		return tool.Result{Content: "action is required (map, skeleton, or importers)", IsError: true}, nil
	default:
		return tool.Result{Content: fmt.Sprintf("unknown action %q (want map, skeleton, or importers)", args.Action), IsError: true}, nil
	}
}

// mapAction renders the whole map, or — when glob is set — only the entries whose
// repo-relative path matches it. path.Match is a single path-segment glob (no
// "/" crossing by "*"), which is the expected shape for a filter like
// "internal/tool/*".
func (t *repomapTool) mapAction(m *repomap.Map, glob string) (tool.Result, error) {
	if glob == "" {
		return tool.Result{Content: m.Render()}, nil
	}
	var kept []repomap.FileEntry
	for _, f := range m.Files {
		ok, err := path.Match(glob, f.Path)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("invalid glob %q: %v", glob, err), IsError: true}, nil
		}
		if ok {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return tool.Result{Content: fmt.Sprintf("no files match glob %q", glob)}, nil
	}
	return tool.Result{Content: renderEntries(kept)}, nil
}

// fileAction backs both skeleton (importers=false) and importers (importers=true)
// since they share path normalization and the not-found messaging.
func (t *repomapTool) fileAction(root string, m *repomap.Map, p string, importers bool) (tool.Result, error) {
	if strings.TrimSpace(p) == "" {
		return tool.Result{Content: "path is required for this action", IsError: true}, nil
	}
	target, err := repoRelative(root, p)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("invalid path: %v", err), IsError: true}, nil
	}

	if importers {
		var out []string
		for _, f := range m.Files {
			for _, edge := range f.Imports {
				if edgeRefersTo(edge, target) {
					out = append(out, f.Path)
					break
				}
			}
		}
		if len(out) == 0 {
			return tool.Result{Content: fmt.Sprintf("no importers found for %s", target)}, nil
		}
		sort.Strings(out)
		return tool.Result{Content: fmt.Sprintf("Files importing %s:\n%s", target, strings.Join(out, "\n"))}, nil
	}

	for _, f := range m.Files {
		if f.Path == target {
			return tool.Result{Content: renderEntry(f)}, nil
		}
	}
	return tool.Result{Content: fmt.Sprintf("no symbols indexed for %s (not a recognized source file, or it has no top-level declarations)", target)}, nil
}

// edgeRefersTo reports whether an import edge points at target. P49.1 records
// edges in three resolved shapes, so a target file can be referred to by any of:
//   - its exact repo-relative path (rare — most importers name a package, not a file);
//   - its path with the extension stripped (JS/TS/Python relative imports resolve
//     to an extension-less path, e.g. import "./lib/util" -> "src/lib/util");
//   - its containing directory (Go module-local imports resolve to the package
//     directory, e.g. import ".../internal/store" -> "internal/store").
func edgeRefersTo(edge, target string) bool {
	if edge == target {
		return true
	}
	if edge == strings.TrimSuffix(target, path.Ext(target)) {
		return true
	}
	if edge == path.Dir(target) {
		return true
	}
	return false
}

// repoRelative normalizes a user-supplied path (relative to root, absolute, or
// dot-prefixed) into the repo-relative slash form the map is keyed by, rejecting
// anything that escapes the workspace root. It mirrors how repomap.Build keys
// entries — plain filepath.Rel against root with no symlink evaluation — so the
// lookup matches (resolvePath's EvalSymlinks would diverge on e.g. macOS's
// /var -> /private/var and never match a map key).
func repoRelative(root, p string) (string, error) {
	p = filepath.FromSlash(p)
	rel := filepath.Clean(p)
	if filepath.IsAbs(p) {
		r, err := filepath.Rel(root, p)
		if err != nil {
			return "", err
		}
		rel = r
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path escapes workspace root")
	}
	return rel, nil
}

// renderEntries renders a slice of file entries in the same shape as
// repomap.Map.Render (header + per-file symbols + a "→ imports" line), used for
// the glob-filtered map where the package's own byte-budgeted renderer can't be
// reused (its budget field is unexported).
func renderEntries(files []repomap.FileEntry) string {
	var b strings.Builder
	b.WriteString("# Repository map\n")
	for _, f := range files {
		b.WriteString(renderEntry(f))
	}
	return b.String()
}

// renderEntry renders one file entry: path, indented symbols, then a compact
// "→ a, b" import line (edges last, matching the injected block's ordering).
func renderEntry(f repomap.FileEntry) string {
	var b strings.Builder
	b.WriteString(f.Path)
	b.WriteString("\n")
	for _, s := range f.Symbols {
		b.WriteString("  ")
		b.WriteString(s)
		b.WriteString("\n")
	}
	if len(f.Imports) > 0 {
		b.WriteString("  → ")
		b.WriteString(strings.Join(f.Imports, ", "))
		b.WriteString("\n")
	}
	return b.String()
}
