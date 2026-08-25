package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/lsp"
	"github.com/fiddler110/aegis/internal/tool"
)

// multieditTool applies multiple edits to one or more files in a single call,
// reducing round-trips when the model needs to make several related changes.
//
// Every edit goes through the same applyReplacement used by edit_file, so the
// two tools agree on what a safe edit is. They did not always: multi_edit used
// to count occurrences and then replace only the first, reporting success for
// an ambiguous match that edit_file rejects — an agent batching edits got a
// plausible confirmation and the wrong file.
type multieditTool struct {
	root    string
	tracker *filetracker.Tracker
	lsp     *lsp.Manager
}

func (t *multieditTool) Name() string                { return "multi_edit" }
func (t *multieditTool) Capability() tool.Capability { return tool.CapWrite }
func (t *multieditTool) Description() string {
	return "Apply multiple string replacements across one or more files in a single call. " +
		"Each edit specifies a file path, old_string and new_string, and optionally replace_all. " +
		"Edits are applied in order, and later edits see earlier ones. Like edit_file, old_string " +
		"must match exactly once in the file's current state unless replace_all is set. " +
		"To create a new file, pass an empty old_string for a path that does not exist. " +
		"The call is all-or-nothing: nothing is written unless every edit applies."
}
func (t *multieditTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"edits":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"old_string":{"type":"string","description":"exact text to replace; empty only when creating a new file"},"new_string":{"type":"string","description":"replacement text"},"replace_all":{"type":"boolean","description":"replace every occurrence instead of requiring a unique match"}},"required":["path","old_string","new_string"]},"description":"ordered list of replacements to apply"}},"required":["edits"]}`)
}
func (t *multieditTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"edits_applied":{"type":"integer"},"files":{"type":"array","items":{"type":"string"}}},"required":["edits_applied","files"]}`)
}

type editSpec struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

// fileState is the in-memory working copy of one file across a multi_edit call.
type fileState struct {
	abs     string
	rel     string // the path as the caller wrote it, for messages
	orig    string // content before any edits, for hunk attribution and rollback
	content string // content as edits are applied
	created bool   // did not exist on disk before this call
}

func (t *multieditTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Edits []editSpec `json:"edits"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if len(args.Edits) == 0 {
		return tool.Result{Content: "edits array is required and must not be empty", IsError: true}, nil
	}
	const maxEdits = 100
	if len(args.Edits) > maxEdits {
		return tool.Result{Content: fmt.Sprintf("too many edits (%d, max %d)", len(args.Edits), maxEdits), IsError: true}, nil
	}

	// Phase 1: resolve every path once and load each distinct file's content.
	// The resolved path is cached per edit because resolveWrite walks and
	// resolves symlinks on every call; a 100-edit batch over a handful of files
	// previously paid that cost twice per edit rather than once per file.
	files := make(map[string]*fileState)
	resolved := make([]*fileState, len(args.Edits))

	for i, e := range args.Edits {
		abs, err := resolveWrite(ctx, t.root, e.Path)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("edit %d (%s): %v", i+1, e.Path, err), IsError: true}, nil
		}
		st, ok := files[abs]
		if !ok {
			var err error
			st, err = t.loadFile(abs, e)
			if err != nil {
				return tool.Result{Content: fmt.Sprintf("edit %d: %v", i+1, err), IsError: true}, nil
			}
			files[abs] = st
		}
		resolved[i] = st
	}

	// Phase 2: apply every edit in order, in memory. Any failure returns before
	// a single byte has been written.
	for i, e := range args.Edits {
		st := resolved[i]
		if st.created && st.content == "" && e.OldString == "" {
			// Seeding a newly created file: there is nothing to anchor against,
			// so new_string simply becomes the initial content. Subsequent edits
			// to the same path in this batch are ordinary anchored edits.
			st.content = e.NewString
			continue
		}
		updated, _, errMsg := applyReplacement(st.content, e.OldString, e.NewString, e.ReplaceAll, st.rel)
		if errMsg != "" {
			return tool.Result{Content: fmt.Sprintf("edit %d: %s (no files were modified)", i+1, errMsg), IsError: true}, nil
		}
		st.content = updated
	}

	// Phase 3: write. Order the writes so a failure message and any retry are
	// deterministic rather than dependent on map iteration order.
	pending := make([]*fileState, 0, len(files))
	for _, st := range files {
		if st.content == st.orig && !st.created {
			continue // edits cancelled out; nothing to write
		}
		pending = append(pending, st)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].abs < pending[j].abs })

	written := make([]*fileState, 0, len(pending))
	for _, st := range pending {
		checkpoint.SnapshotterFrom(ctx).Capture(st.abs)
		if st.created {
			if err := os.MkdirAll(filepath.Dir(st.abs), 0o750); err != nil {
				return t.rollback(written, fmt.Sprintf("mkdir for %s failed: %v", st.rel, err)), nil
			}
		}
		// writePreservingMode, not a hardcoded 0o644: multi_edit used to widen a
		// 0o600 file to world-readable and drop the exec bit off a 0o700 script,
		// while write_file and edit_file preserved the mode.
		if err := writePreservingMode(st.abs, []byte(st.content)); err != nil {
			return t.rollback(written, fmt.Sprintf("writing %s failed: %v", st.rel, err)), nil
		}
		written = append(written, st)
		if t.tracker != nil {
			t.tracker.RecordWrite(st.abs)
			t.tracker.RecordAgentWrite(st.abs, st.orig, st.content)
		}
	}

	names := make([]string, 0, len(written))
	for _, st := range written {
		names = append(names, st.rel)
	}
	if len(names) == 0 {
		return tool.Result{Content: "no files changed (every edit was a no-op)"}, nil
	}
	result := fmt.Sprintf("applied %d edit(s) across %d file(s): %s",
		len(args.Edits), len(written), strings.Join(names, ", "))
	for _, st := range written {
		result = appendLSPFeedback(ctx, t.lsp, t.root, st.abs, st.rel, result)
	}
	return tool.Result{Content: result}, nil
}

// loadFile prepares the working copy for abs. A missing file is an error unless
// the first edit naming it has an empty old_string, which requests creation.
func (t *multieditTool) loadFile(abs string, first editSpec) (*fileState, error) {
	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(abs)
	switch {
	case err == nil:
		if isBinary(data) {
			return nil, fmt.Errorf("%s appears to be a binary file; string replacement works on UTF-8 text only", first.Path)
		}
		if int64(len(data)) > maxReadBytes {
			return nil, fmt.Errorf("%s is %d bytes, over the %d-byte edit limit; editing it in place would truncate the file", first.Path, len(data), maxReadBytes)
		}
		return &fileState{abs: abs, rel: first.Path, orig: string(data), content: string(data)}, nil
	case errors.Is(err, fs.ErrNotExist) && first.OldString == "":
		return &fileState{abs: abs, rel: first.Path, created: true}, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%s does not exist; pass an empty old_string to create it, or use write_file", first.Path)
	default:
		return nil, fmt.Errorf("cannot read %s: %v", first.Path, err)
	}
}

// rollback restores the files already written by a failed batch, so the
// all-or-nothing promise in the tool's description holds for write errors too —
// not just for edits that fail to apply. A file created by this call is removed
// rather than restored to an empty state.
//
// Restoration is itself best-effort: if the filesystem is refusing writes, the
// undo can fail as well. The result says exactly which files are in which state
// rather than claiming a clean abort it cannot verify.
func (t *multieditTool) rollback(written []*fileState, cause string) tool.Result {
	var stuck []string
	for _, st := range written {
		var err error
		if st.created {
			err = os.Remove(st.abs)
		} else {
			err = writePreservingMode(st.abs, []byte(st.orig))
		}
		if err != nil {
			stuck = append(stuck, st.rel)
			continue
		}
		if t.tracker != nil {
			t.tracker.RecordWrite(st.abs)
			t.tracker.RecordAgentWrite(st.abs, st.content, st.orig)
		}
	}
	msg := cause
	if len(stuck) == 0 {
		msg += "; all earlier edits in this call were rolled back, so no file was changed"
	} else {
		msg += fmt.Sprintf("; rollback failed for %s — re-read %s before editing further",
			strings.Join(stuck, ", "), pluralThem(len(stuck)))
	}
	return tool.Result{Content: msg, IsError: true}
}

func pluralThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}
