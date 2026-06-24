// Package builtin provides the harness's built-in tools: file access, search,
// shell execution, and web access. File and shell tools are confined to a
// workspace root so the agent cannot reach outside the project by default.
package builtin

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/scottymacleod/agentharness/internal/memory"
	"github.com/scottymacleod/agentharness/internal/tool"
)

// Options configures the built-in tool set.
type Options struct {
	// Root is the workspace directory file/search/shell tools are confined to.
	Root string
	// DataDir is the per-user data directory; when set, memory/skill tools are
	// registered.
	DataDir string
	// ShellTimeoutSec bounds how long a shell command may run (0 -> default).
	ShellTimeoutSec int
	// HTTPUserAgent is sent by the web tools.
	HTTPUserAgent string
}

// Register adds all built-in tools to the registry.
func Register(reg *tool.Registry, opts Options) error {
	if opts.Root == "" {
		opts.Root = "."
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	if opts.HTTPUserAgent == "" {
		opts.HTTPUserAgent = "agentharness/0.1"
	}

	tools := []tool.Tool{
		&readTool{root: root},
		&writeTool{root: root},
		&editTool{root: root},
		&globTool{root: root},
		&grepTool{root: root},
		newShellTool(root, opts.ShellTimeoutSec),
		&fetchTool{userAgent: opts.HTTPUserAgent},
		&searchTool{userAgent: opts.HTTPUserAgent},
	}
	if opts.DataDir != "" {
		src := memory.Sources{ProjectRoot: root, DataDir: opts.DataDir}
		tools = append(tools, &rememberTool{src: src}, &saveSkillTool{src: src})
	}
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// resolvePath joins p against root and rejects paths that escape it.
func resolvePath(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", p)
	}
	return abs, nil
}

// parseArgs unmarshals tool input into v, returning a friendly error.
func parseArgs(input json.RawMessage, v any) error {
	if len(input) == 0 {
		return nil
	}
	if err := json.Unmarshal(input, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// schema is a tiny helper to declare a tool's JSON Schema inline.
func schema(s string) json.RawMessage { return json.RawMessage(s) }
