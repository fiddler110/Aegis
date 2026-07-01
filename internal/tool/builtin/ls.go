package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/scottymacleod/aegis/internal/tool"
)

type lsTool struct{ root string }

func (t *lsTool) Name() string                { return "ls" }
func (t *lsTool) Capability() tool.Capability { return tool.CapRead }
func (t *lsTool) Description() string {
	return "List workspace directory contents as an indented tree. Skips .git, node_modules, vendor, and other generated directories."
}
func (t *lsTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"directory to list relative to workspace root (default '.')"},"depth":{"type":"integer","description":"max directory depth 1-5 (default 2)"}},"required":[]}`)
}

func (t *lsTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path  string `json:"path"`
		Depth int    `json:"depth"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.Path == "" {
		args.Path = "."
	}
	if args.Depth <= 0 {
		args.Depth = 2
	}
	if args.Depth > 5 {
		args.Depth = 5
	}

	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("invalid path: %v", err), IsError: true}, nil
	}

	var lines []string
	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(abs, path)
		if rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator))
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			if depth >= args.Depth {
				return filepath.SkipDir
			}
		} else if depth >= args.Depth {
			return nil
		}
		indent := strings.Repeat("  ", depth)
		name := d.Name()
		if d.IsDir() {
			name += "/"
		}
		lines = append(lines, indent+name)
		return nil
	})
	if walkErr != nil {
		return tool.Result{Content: fmt.Sprintf("ls failed: %v", walkErr), IsError: true}, nil
	}
	if len(lines) == 0 {
		return tool.Result{Content: "(empty)"}, nil
	}
	return tool.Result{Content: strings.Join(lines, "\n")}, nil
}
