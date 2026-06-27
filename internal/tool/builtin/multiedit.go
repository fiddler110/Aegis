package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/scottymacleod/aegis/internal/checkpoint"
	"github.com/scottymacleod/aegis/internal/filetracker"
	"github.com/scottymacleod/aegis/internal/tool"
)

// multieditTool applies multiple edits to one or more files in a single call,
// reducing round-trips when the model needs to make several related changes.
type multieditTool struct {
	root    string
	tracker *filetracker.Tracker
}

func (t *multieditTool) Name() string                { return "multi_edit" }
func (t *multieditTool) Capability() tool.Capability { return tool.CapWrite }
func (t *multieditTool) Description() string {
	return "Apply multiple string replacements across one or more files in a single call. " +
		"Each edit specifies a file path, old_string, and new_string. Edits are applied in order. " +
		"The call fails atomically if any edit cannot be applied."
}
func (t *multieditTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"edits":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"]},"description":"ordered list of replacements to apply"}},"required":["edits"]}`)
}

type editSpec struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
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

	// Phase 1: validate all paths and load file contents.
	type fileState struct {
		abs     string
		content string
	}
	files := make(map[string]*fileState)

	for i, e := range args.Edits {
		abs, err := resolvePath(t.root, e.Path)
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("edit %d: %v", i+1, err), IsError: true}, nil
		}
		if _, ok := files[abs]; !ok {
			if t.tracker != nil {
				if err := t.tracker.CheckWrite(abs); err != nil {
					return tool.Result{Content: fmt.Sprintf("edit %d: %v", i+1, err), IsError: true}, nil
				}
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return tool.Result{Content: fmt.Sprintf("edit %d: cannot read %s: %v", i+1, e.Path, err), IsError: true}, nil
			}
			files[abs] = &fileState{abs: abs, content: string(data)}
		}
	}

	// Phase 2: apply all edits in order (in memory).
	for i, e := range args.Edits {
		abs, _ := resolvePath(t.root, e.Path)
		fs := files[abs]
		n := strings.Count(fs.content, e.OldString)
		if n == 0 {
			return tool.Result{Content: fmt.Sprintf("edit %d: old_string not found in %s", i+1, e.Path), IsError: true}, nil
		}
		fs.content = strings.Replace(fs.content, e.OldString, e.NewString, 1)
	}

	// Phase 3: write all modified files.
	var results []string
	for _, fs := range files {
		checkpoint.SnapshotterFrom(ctx).Capture(fs.abs)
		if err := os.WriteFile(fs.abs, []byte(fs.content), 0o644); err != nil {
			return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
		}
		if t.tracker != nil {
			t.tracker.RecordWrite(fs.abs)
		}
		results = append(results, fs.abs)
	}

	return tool.Result{Content: fmt.Sprintf("applied %d edit(s) across %d file(s)", len(args.Edits), len(files))}, nil
}
