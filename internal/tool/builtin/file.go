package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

// binarySniffBytes is how much of a file's head is inspected for NUL bytes
// before deciding it is not text. Matches the window grep already uses
// (isBinary in search.go), so the two tools agree on what "binary" means.
const binarySniffBytes = 8000

// looksBinary reports whether the file at abs appears to be binary, by sniffing
// its first binarySniffBytes bytes for a NUL. It is best-effort: an unreadable
// file reports false and the caller's own open/read surfaces the real error.
//
// This exists because a numbered dump of PNG or ELF bytes is worse than an
// error — the model reasons over mojibake instead of stopping, and raw control
// bytes reach the transcript.
func looksBinary(abs string) (bool, int64) {
	f, err := os.Open(abs)
	if err != nil {
		return false, 0
	}
	defer f.Close()
	var size int64
	if info, err := f.Stat(); err == nil {
		size = info.Size()
	}
	buf := make([]byte, binarySniffBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false, size
	}
	return isBinary(buf[:n]), size
}

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
	abs, err := resolveRead(ctx, t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	// Establish existence before anything infers from size. looksBinary reports
	// (false, 0) for a file it cannot open, which the zero-size branch below
	// then reported as "is empty (0 bytes)" — telling the model a missing file
	// exists and is blank, which invites it to fill or overwrite something that
	// was never there. A missing file must read as missing.
	if st, statErr := os.Stat(abs); statErr != nil {
		hint := ""
		if notFound(statErr) {
			hint = suggestPathHint(effectiveRoot(ctx, t.root), args.Path)
		}
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v%s", args.Path, statErr, hint), IsError: true}, nil
	} else if st.IsDir() {
		return tool.Result{Content: fmt.Sprintf("%s is a directory, not a file — use ls or glob to list it", args.Path), IsError: true}, nil
	}
	binary, size := looksBinary(abs)
	if binary {
		return tool.Result{Content: fmt.Sprintf("%s appears to be a binary file (%d bytes); read_file returns UTF-8 text only. Use grep or a shell tool if you need to inspect it.", args.Path, size), IsError: true}, nil
	}
	if size == 0 {
		// A zero-byte file otherwise scans as one empty numbered line ("1\t"),
		// which is indistinguishable from a failed or truncated read. Say so.
		if t.tracker != nil {
			t.tracker.RecordRead(abs)
		}
		return tool.Result{Content: fmt.Sprintf("[read_file: %s is empty (0 bytes).]\n", args.Path)}, nil
	}
	f, err := os.Open(abs)
	if err != nil {
		hint := ""
		if notFound(err) {
			hint = suggestPathHint(effectiveRoot(ctx, t.root), args.Path)
		}
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v%s", args.Path, err, hint), IsError: true}, nil
	}
	defer f.Close()
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
	// The agent has seen the whole file only when the window started at line 1,
	// ran off the end, and the byte cap never bit. Anything else — a capped
	// default read, an explicit offset/limit range, or a file past maxReadBytes
	// where the LimitReader simply stops without tripping `truncated` — leaves
	// content unseen, which write_file must know about before it replaces the
	// lot (see filetracker.CheckFullOverwrite).
	complete := start == 1 && !truncated && size <= maxReadBytes
	if t.tracker != nil {
		if complete {
			t.tracker.RecordRead(abs)
		} else {
			t.tracker.RecordPartialRead(abs)
		}
	}
	switch {
	case size > maxReadBytes:
		fmt.Fprintf(&b, "\n[read_file: %s is %d bytes; only the first %d were read. Use grep to locate what you need in a file this size.]\n",
			args.Path, size, maxReadBytes)
	case capped && truncated:
		next := start + count
		fmt.Fprintf(&b, "\n[read_file: showing lines %d-%d; the file continues past line %d. This default %d-line window bounds context — call read_file again with offset=%d (and a limit) to read more, or grep for the part you need.]\n",
			start, start+count-1, start+count-1, defaultReadLines, next)
	case count == 0 && start > 1:
		// An empty successful result reads to a model as "there is nothing
		// here", not "you overshot the end" — say which it is, so the next call
		// corrects the offset instead of concluding the file is empty.
		fmt.Fprintf(&b, "[read_file: offset %d is past the end of %s, which has %d line(s). Re-read with a smaller offset.]\n",
			start, args.Path, lineNo)
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
	// Refuse a directory-shaped path before resolution, which would clean the
	// trailing separator away. A model reaching for "mkdir" via write_file
	// otherwise creates an empty *file* exactly where the directory belongs,
	// and every subsequent write beneath it fails with a MkdirAll error that
	// names the path but not the cause ("The system cannot find the path
	// specified" on Windows). Cheap to check, and the failure it prevents is
	// unrecoverable without out-of-band cleanup.
	if strings.HasSuffix(args.Path, "/") || strings.HasSuffix(args.Path, `\`) {
		return tool.Result{Content: fmt.Sprintf("%s is a directory path, not a file: write_file creates files (parent directories are created automatically) — give the full path including the file name", args.Path), IsError: true}, nil
	}
	if bad := invalidPathChar(args.Path); bad != "" {
		return tool.Result{Content: fmt.Sprintf("%s cannot be a file name on this platform: it contains %s. If that came from a template placeholder like `2-<framework>-analysis.md`, substitute the real value first.", args.Path, bad), IsError: true}, nil
	}
	abs, err := resolveWrite(ctx, t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return tool.Result{Content: fmt.Sprintf("%s is an existing directory, not a file", args.Path), IsError: true}, nil
	}
	var oldContent string
	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return tool.Result{Content: err.Error(), IsError: true}, nil
		}
		// write_file replaces the file wholesale, so unlike an anchored edit it
		// can destroy content the agent never read. P38.1's line cap made that
		// reachable: a capped read looks, to the mtime guard alone, exactly like
		// a complete one.
		if err := t.tracker.CheckFullOverwrite(abs); err != nil {
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
		// RecordOverwrite, not RecordWrite: every byte is now the agent's own,
		// so any earlier partial-read flag no longer describes reality.
		t.tracker.RecordOverwrite(abs)
		t.tracker.RecordAgentWrite(abs, oldContent, args.Content)
	}
	return tool.Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path)}, nil
}

// --- edit ---

// readTextForEdit reads abs in full for an in-place edit, returning the content
// or a caller-facing error message (empty when it succeeded).
//
// The size check is the point. The previous io.LimitReader(f, maxReadBytes)
// path read a prefix, applied the replacement, and wrote that prefix back as
// the whole file — silently discarding everything past the cap while reporting
// success. Refusing the edit outright is the only safe answer, since an edit
// tool has no way to write back bytes it never read. Binary content is refused
// for the same reason grep skips it: substring replacement over arbitrary bytes
// is not a meaningful operation, and any write-back would mangle the file.
// root is used only to suggest a real location when display cannot be found —
// pass "" to skip the suggestion.
func readTextForEdit(abs, display, root string) (string, string) {
	info, err := os.Stat(abs)
	if err != nil {
		hint := ""
		if notFound(err) {
			hint = suggestPathHint(root, display)
		}
		return "", fmt.Sprintf("cannot read %s: %v%s", display, err, hint)
	}
	if info.IsDir() {
		return "", fmt.Sprintf("%s is a directory, not a file", display)
	}
	if info.Size() > maxReadBytes {
		return "", fmt.Sprintf("%s is %d bytes, over the %d-byte edit limit; editing it in place would truncate the file. Use a shell tool for a file this size.",
			display, info.Size(), maxReadBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Sprintf("cannot read %s: %v", display, err)
	}
	if isBinary(data) {
		return "", fmt.Sprintf("%s appears to be a binary file; string replacement works on UTF-8 text only", display)
	}
	return string(data), ""
}

// applyReplacement performs one anchored replacement against content, returning
// the new content, how many occurrences were replaced, and a caller-facing
// error message (empty on success). It is shared by edit_file and multi_edit so
// the two tools cannot drift apart on what counts as a safe edit — they did:
// multi_edit used to replace the first of several matches and report success.
//
// The happy path scans content once (Index) and builds the result with a single
// sized allocation; the O(n) Count only runs to populate an error message.
func applyReplacement(content, oldStr, newStr string, replaceAll bool, display string) (string, int, string) {
	if oldStr == "" {
		return "", 0, fmt.Sprintf("old_string is empty; give the exact text to replace in %s (to create a file or replace it wholesale, use write_file)", display)
	}
	if oldStr == newStr {
		return "", 0, fmt.Sprintf("old_string and new_string are identical, so this edit to %s would change nothing", display)
	}
	i := strings.Index(content, oldStr)
	if i < 0 {
		return "", 0, fmt.Sprintf("old_string not found in %s", display)
	}
	if replaceAll {
		return strings.ReplaceAll(content, oldStr, newStr), strings.Count(content, oldStr), ""
	}
	if strings.Contains(content[i+len(oldStr):], oldStr) {
		return "", 0, fmt.Sprintf("old_string occurs %d times in %s; pass replace_all, or include surrounding lines to make the match unique",
			strings.Count(content, oldStr), display)
	}
	var b strings.Builder
	b.Grow(len(content) - len(oldStr) + len(newStr))
	b.WriteString(content[:i])
	b.WriteString(newStr)
	b.WriteString(content[i+len(oldStr):])
	return b.String(), 1, ""
}

type editTool struct {
	root    string
	tracker *filetracker.Tracker
}

func (t *editTool) Name() string                { return "edit_file" }
func (t *editTool) Capability() tool.Capability { return tool.CapWrite }
func (t *editTool) Description() string {
	return "Replace an exact string in an existing text file. old_string must occur exactly once unless replace_all is true; include surrounding lines to make a match unique. To create a file or replace one entirely, use write_file."
}
func (t *editTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative path to an existing file"},"old_string":{"type":"string","description":"exact text to replace, including whitespace; must be non-empty and differ from new_string"},"new_string":{"type":"string","description":"replacement text"},"replace_all":{"type":"boolean","description":"replace every occurrence instead of requiring a unique match"}},"required":["path","old_string","new_string"]}`)
}
func (t *editTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string"},"replacements":{"type":"integer"}},"required":["path","replacements"]}`)
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
	abs, err := resolveWrite(ctx, t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return tool.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	content, errMsg := readTextForEdit(abs, args.Path, effectiveRoot(ctx, t.root))
	if errMsg != "" {
		return tool.Result{Content: errMsg, IsError: true}, nil
	}
	updated, n, errMsg := applyReplacement(content, args.OldString, args.NewString, args.ReplaceAll, args.Path)
	if errMsg != "" {
		return tool.Result{Content: errMsg, IsError: true}, nil
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

// invalidPathChar returns a human description of the first character in p that
// cannot appear in a file name on this platform, or "" when p is fine.
//
// Windows rejects < > : " | ? * outright, and the OS error for it names
// neither the character nor the offending component ("The filename, directory
// name, or volume label syntax is incorrect"). Observed live: a model copied
// the literal placeholder from its own skill documentation and tried to write
// `2-<framework>-analysis.md` instead of substituting `stride`, then retried
// the identical call because nothing in the error told it what to change
// (P38.1 re-test, 2026-08-09).
//
// The drive letter's colon is why only the path's base name is checked: an
// absolute Windows path legitimately contains one.
func invalidPathChar(p string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	base := filepath.Base(filepath.FromSlash(p))
	for _, r := range base {
		switch r {
		case '<', '>', '"', '|', '?', '*', ':':
			return fmt.Sprintf("%q, which Windows does not allow in a file name", r)
		}
	}
	return ""
}
