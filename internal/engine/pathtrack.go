package engine

// P78.2: split out of engine.go — the per-run written/read file tracking used
// by the output guard (quarantine-on-fail validation) and by compaction's
// FileContextCompactor carry-across (P65.2). Purely a file move; no behavior
// changed.

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/fiddler110/aegis/internal/guard"
)

// writtenPathsFromInput extracts workspace-relative file paths from a
// write-capability tool call's input, recognizing the "path"/"file_path"
// fields used by write_file/edit_file/diagram, and the "edits[].path" shape
// used by multi_edit. Unrecognized shapes (e.g. an MCP or custom write tool
// with different field names) yield no paths — the guard simply won't see
// that tool's output, matching the existing subjectFor limitation in
// internal/permission/rules.go rather than guessing.
func writtenPathsFromInput(input json.RawMessage) []string {
	var args struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
		Edits    []struct {
			Path string `json:"path"`
		} `json:"edits"`
	}
	if json.Unmarshal(input, &args) != nil {
		return nil
	}
	var paths []string
	if args.Path != "" {
		paths = append(paths, args.Path)
	}
	if args.FilePath != "" {
		paths = append(paths, args.FilePath)
	}
	for _, e := range args.Edits {
		if e.Path != "" {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// recordWrittenPaths adds paths to the current run's written-files set.
func (e *Engine) recordWrittenPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	e.writtenFilesMu.Lock()
	defer e.writtenFilesMu.Unlock()
	if e.writtenFiles == nil {
		// Run resets this per run; the lazy init keeps a tool call outside a
		// Run (or before its reset) from panicking on a nil map write.
		e.writtenFiles = make(map[string]struct{})
	}
	for _, p := range paths {
		e.writtenFiles[p] = struct{}{}
	}
}

// recordReadPaths adds paths to the current run's read-files list (P65.2),
// preserving first-seen order and dropping duplicates.
func (e *Engine) recordReadPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	e.readFilesMu.Lock()
	defer e.readFilesMu.Unlock()
	if e.readFileSet == nil {
		e.readFileSet = make(map[string]struct{})
	}
	for _, p := range paths {
		if _, dup := e.readFileSet[p]; dup {
			continue
		}
		e.readFileSet[p] = struct{}{}
		e.readFiles = append(e.readFiles, p)
	}
}

// touchedFiles reports what this run has read and modified, for a
// FileContextCompactor to carry across a compaction (P65.2).
//
// The written half is sorted while the read half keeps insertion order, and the
// asymmetry is not an oversight: writtenFiles is a set owned by the output guard
// and has no recency information to preserve, so a stable order is the best
// available; readFiles was built for this caller and does have one, which is
// what the carried list's cap depends on.
func (e *Engine) touchedFiles() (read, modified []string) {
	e.readFilesMu.Lock()
	read = append(read, e.readFiles...)
	e.readFilesMu.Unlock()

	e.writtenFilesMu.Lock()
	for p := range e.writtenFiles {
		modified = append(modified, p)
	}
	e.writtenFilesMu.Unlock()
	sort.Strings(modified)
	return read, modified
}

// markToolStarted records that a tool call reached its Execute (P65.1). Called
// from both the sequential and the concurrent tool paths, hence the mutex.
func (e *Engine) markToolStarted(id string) {
	if id == "" {
		return
	}
	e.startedToolsMu.Lock()
	defer e.startedToolsMu.Unlock()
	if e.startedTools == nil {
		// Run resets this per run; the lazy init keeps a tool call outside a
		// Run (or before its reset) from panicking on a nil map write, matching
		// recordWrittenPaths above.
		e.startedTools = make(map[string]struct{})
	}
	e.startedTools[id] = struct{}{}
}

// startedToolSet snapshots the started-call IDs for repairOrphanedToolUses. A
// copy rather than the live map: the repair is a pure function of the message
// list and this set, and handing it the map under no lock would race a tool
// round still finishing from an earlier cancelled Run.
func (e *Engine) startedToolSet() map[string]struct{} {
	e.startedToolsMu.Lock()
	defer e.startedToolsMu.Unlock()
	out := make(map[string]struct{}, len(e.startedTools))
	for id := range e.startedTools {
		out[id] = struct{}{}
	}
	return out
}

// maxGuardFiles caps how many written files are read back for guard
// validation, so a task that touches dozens of files doesn't balloon the
// validator prompt or issue that many extra reads.
const maxGuardFiles = 5

// collectWrittenFiles reads back the current content of every file written
// or edited so far this run via the registered read_file tool (so path
// resolution/sandboxing matches whatever wrote it), for the output guard to
// validate against the actual deliverable rather than only the assistant's
// chat summary. Best-effort: a tool registry without read_file, or a read
// failure for a given path, silently yields no content for that path rather
// than failing the run — the guard still gets the chat text either way.
func (e *Engine) collectWrittenFiles(ctx context.Context) []guard.FileContent {
	e.writtenFilesMu.Lock()
	paths := make([]string, 0, len(e.writtenFiles))
	for p := range e.writtenFiles {
		paths = append(paths, p)
	}
	e.writtenFilesMu.Unlock()
	if len(paths) == 0 || e.tools == nil {
		return nil
	}
	reader, ok := e.tools.Get("read_file")
	if !ok {
		return nil
	}
	// Same decoration executeTool applies: without it read_file resolves
	// against its construction-time root, not this session's workdir (ARCH-03).
	ctx = e.toolCtx(ctx)
	sort.Strings(paths) // deterministic order for reproducible prompts/tests
	if len(paths) > maxGuardFiles {
		paths = paths[:maxGuardFiles]
	}
	var out []guard.FileContent
	for _, p := range paths {
		input, err := json.Marshal(map[string]string{"path": p})
		if err != nil {
			continue
		}
		res, err := reader.Execute(ctx, input)
		if err != nil || res.IsError {
			continue
		}
		out = append(out, guard.FileContent{Path: p, Content: res.Content})
	}
	return out
}
