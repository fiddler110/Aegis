package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/tool"
)

const (
	maxReadBytes    = 50 << 20 // 50 MiB
	maxWriteContent = 10 << 20 // 10 MiB
	// defaultReadLines caps an unbounded read_file (no explicit limit) so a
	// single read of a large source file cannot balloon a turn's context.
	// P38.1: a local threat-model drive read a 2845-line file whole, and the
	// next prefill blew past the response-header timeout and later truncated
	// the session at the context limit. When the file is longer than this the
	// tool returns the first window plus a notice telling the model to page
	// with offset/limit (mirrors the read tool's own offset/limit contract).
	// An explicit limit is always honored verbatim — this only bounds the
	// "read the whole thing" default.
	defaultReadLines = 1500
	// newFileMode is the permission for files we create; parent dirs are made
	// 0o750 (see os.MkdirAll calls). On overwrite we preserve the existing
	// file's mode instead — see writePreservingMode.
	newFileMode = 0o644
)

// writePreservingMode writes data to abs. When abs already exists its current
// permission bits are preserved (so overwriting a 0o700 script or a
// mode-sensitive key/token file doesn't silently drop the exec bit or widen to
// world-readable); a newly created file lands at newFileMode.
func writePreservingMode(abs string, data []byte) error {
	mode := os.FileMode(newFileMode)
	if info, err := os.Stat(abs); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(abs, data, mode)
}

// --- read ---

type readTool struct {
	root    string
	tracker *filetracker.Tracker
}

func (t *readTool) Name() string                { return "read_file" }
func (t *readTool) Capability() tool.Capability { return tool.CapRead }
func (t *readTool) Description() string {
	return "Read a UTF-8 text file from the workspace. Returns the file contents with 1-based line numbers. An unbounded read is capped at the first 1500 lines to bound context; pass offset/limit to page through a longer file or read a specific range."
}
func (t *readTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"offset":{"type":"integer","description":"1-based start line (optional)"},"limit":{"type":"integer","description":"max lines to read (optional)"}},"required":["path"]}`)
}
func (t *readTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"content":{"type":"string","description":"file contents with 1-based line numbers prefixed"}},"required":["content"]}`)
}
func (t *readTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	abs, err := resolvePath(effectiveRoot(ctx, t.root), args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v", args.Path, err), IsError: true}, nil
	}
	defer f.Close()
	if t.tracker != nil {
		t.tracker.RecordRead(abs)
	}
	start := 1
	if args.Offset > 0 {
		start = args.Offset
	}
	// An explicit limit is honored verbatim; an unbounded read falls back to
	// defaultReadLines so "read the whole file" cannot dump a huge source file
	// into one turn's context (P38.1). capped records that the fallback applied
	// so we only emit the paging notice for the default case, never when the
	// caller asked for a specific window.
	limit := args.Limit
	capped := false
	if limit <= 0 {
		limit = defaultReadLines
		capped = true
	}
	// Scan line-by-line and stop once we've emitted `limit` lines from `start`,
	// so a bounded read of a large file allocates only what it returns rather
	// than splitting the whole file up front. The scanner is capped at
	// maxReadBytes of total input, matching the old io.LimitReader behavior;
	// splitLinesKeepFinal replicates strings.Split(data, "\n") semantics,
	// including the trailing empty "line" for a file ending in a newline.
	sc := bufio.NewScanner(io.LimitReader(f, maxReadBytes))
	sc.Split(splitLinesKeepFinal)
	sc.Buffer(make([]byte, 0, 64*1024), maxReadBytes)
	var b strings.Builder
	lineNo := 0
	count := 0
	truncated := false
	for sc.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if count >= limit {
			truncated = true // at least one more line exists past the window
			break
		}
		fmt.Fprintf(&b, "%d\t%s\n", lineNo, sc.Text())
		count++
	}
	if err := sc.Err(); err != nil {
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v", args.Path, err), IsError: true}, nil
	}
	if capped && truncated {
		next := start + count
		fmt.Fprintf(&b, "\n[read_file: showing lines %d-%d; the file continues past line %d. This default %d-line window bounds context — call read_file again with offset=%d (and a limit) to read more, or grep for the part you need.]\n",
			start, start+count-1, start+count-1, defaultReadLines, next)
	}
	return tool.Result{Content: b.String()}, nil
}

// splitLinesKeepFinal is a bufio.SplitFunc that splits on "\n" the same way
// strings.Split(s, "\n") does: each token is the text between newlines with the
// "\n" stripped, and a trailing "\n" yields a final empty token (so a file
// ending in a newline reports one more line than it has content lines, matching
// the prior strings.Split behavior). Unlike bufio.ScanLines it does not strip a
// trailing "\r", preserving CRLF bytes in the emitted line.
func splitLinesKeepFinal(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := strings.IndexByte(string(data), '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		// Final chunk with no trailing newline: emit it as the last line. When
		// data is empty at EOF we still must emit one token for an empty input
		// (strings.Split("", "\n") == [""]) but not loop forever afterward.
		return len(data), data, bufio.ErrFinalToken
	}
	return 0, nil, nil
}

// --- write ---

type writeTool struct {
	root    string
	tracker *filetracker.Tracker
}

func (t *writeTool) Name() string                { return "write_file" }
func (t *writeTool) Capability() tool.Capability { return tool.CapWrite }
func (t *writeTool) Description() string {
	return "Create or overwrite a file in the workspace with the given content. Creates parent directories as needed."
}
func (t *writeTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path to write, e.g. \"report.md\" or \"docs/output.md\""},"content":{"type":"string","description":"full text content to write to the file"}},"required":["path","content"]}`)
}
func (t *writeTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string"},"bytes_written":{"type":"integer"}},"required":["path","bytes_written"]}`)
}
func (t *writeTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if len(args.Content) > maxWriteContent {
		return tool.Result{Content: fmt.Sprintf("content too large (%d bytes, max %d)", len(args.Content), maxWriteContent), IsError: true}, nil
	}
	abs, err := resolvePath(effectiveRoot(ctx, t.root), args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	var oldContent string
	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return tool.Result{Content: err.Error(), IsError: true}, nil
		}
		// Capture prior content (empty if the file is new) for hunk attribution.
		if data, err := os.ReadFile(abs); err == nil {
			oldContent = string(data)
		}
	}
	// Capture pre-modification content for checkpoint/rewind, if a run is
	// snapshotting. Safe on a nil snapshotter.
	checkpoint.SnapshotterFrom(ctx).Capture(abs)
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return tool.Result{Content: fmt.Sprintf("mkdir failed: %v", err), IsError: true}, nil
	}
	if err := writePreservingMode(abs, []byte(args.Content)); err != nil {
		return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	if t.tracker != nil {
		t.tracker.RecordWrite(abs)
		t.tracker.RecordAgentWrite(abs, oldContent, args.Content)
	}
	return tool.Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path)}, nil
}

// --- edit ---

type editTool struct {
	root    string
	tracker *filetracker.Tracker
}

func (t *editTool) Name() string                { return "edit_file" }
func (t *editTool) Capability() tool.Capability { return tool.CapWrite }
func (t *editTool) Description() string {
	return "Replace an exact string in a file. old_string must occur exactly once unless replace_all is true."
}
func (t *editTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["path","old_string","new_string"]}`)
}
func (t *editTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	abs, err := resolvePath(effectiveRoot(ctx, t.root), args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return tool.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	f, err := os.Open(abs)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v", args.Path, err), IsError: true}, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxReadBytes))
	f.Close()
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v", args.Path, err), IsError: true}, nil
	}
	content := string(data)
	n := strings.Count(content, args.OldString)
	if n == 0 {
		return tool.Result{Content: "old_string not found in file", IsError: true}, nil
	}
	if n > 1 && !args.ReplaceAll {
		return tool.Result{Content: fmt.Sprintf("old_string occurs %d times; pass replace_all or provide a more specific string", n), IsError: true}, nil
	}
	var updated string
	if args.ReplaceAll {
		updated = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		updated = strings.Replace(content, args.OldString, args.NewString, 1)
	}
	checkpoint.SnapshotterFrom(ctx).Capture(abs)
	if err := writePreservingMode(abs, []byte(updated)); err != nil {
		return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	if t.tracker != nil {
		t.tracker.RecordWrite(abs)
		t.tracker.RecordAgentWrite(abs, content, updated)
	}
	return tool.Result{Content: fmt.Sprintf("edited %s (%d replacement(s))", args.Path, n)}, nil
}
