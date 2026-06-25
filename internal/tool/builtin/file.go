package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/scottymacleod/agentharness/internal/filetracker"
	"github.com/scottymacleod/agentharness/internal/tool"
)

const (
	maxReadBytes    = 50 << 20 // 50 MiB
	maxWriteContent = 10 << 20 // 10 MiB
)

// --- read ---

type readTool struct {
	root    string
	tracker *filetracker.Tracker
}

func (t *readTool) Name() string               { return "read_file" }
func (t *readTool) Capability() tool.Capability { return tool.CapRead }
func (t *readTool) Description() string {
	return "Read a UTF-8 text file from the workspace. Returns the file contents with 1-based line numbers."
}
func (t *readTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"offset":{"type":"integer","description":"1-based start line (optional)"},"limit":{"type":"integer","description":"max lines to read (optional)"}},"required":["path"]}`)
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
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
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
	if t.tracker != nil {
		t.tracker.RecordRead(abs)
	}
	lines := strings.Split(string(data), "\n")
	start := 1
	if args.Offset > 0 {
		start = args.Offset
	}
	var b strings.Builder
	count := 0
	for i := start - 1; i < len(lines); i++ {
		if args.Limit > 0 && count >= args.Limit {
			break
		}
		fmt.Fprintf(&b, "%d\t%s\n", i+1, lines[i])
		count++
	}
	return tool.Result{Content: b.String()}, nil
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
	return schema(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
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
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return tool.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return tool.Result{Content: fmt.Sprintf("mkdir failed: %v", err), IsError: true}, nil
	}
	if err := os.WriteFile(abs, []byte(args.Content), 0o644); err != nil {
		return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	if t.tracker != nil {
		t.tracker.RecordWrite(abs)
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
	abs, err := resolvePath(t.root, args.Path)
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
	if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
		return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	if t.tracker != nil {
		t.tracker.RecordWrite(abs)
	}
	return tool.Result{Content: fmt.Sprintf("edited %s (%d replacement(s))", args.Path, n)}, nil
}
