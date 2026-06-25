// Package builtin provides the harness's built-in tools: file access, search,
// shell execution, and web access. File and shell tools are confined to a
// workspace root so the agent cannot reach outside the project by default.
package builtin

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/scottymacleod/aegis/internal/cron"
	"github.com/scottymacleod/aegis/internal/filetracker"
	"github.com/scottymacleod/aegis/internal/lsp"
	"github.com/scottymacleod/aegis/internal/memory"
	"github.com/scottymacleod/aegis/internal/sandbox"
	"github.com/scottymacleod/aegis/internal/task"
	"github.com/scottymacleod/aegis/internal/tool"
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
	// KrokiURL is the diagram rendering endpoint.
	KrokiURL string
	// Tasks, when set, enables background jobs: the shell tool's background
	// option and the task_* management tools.
	Tasks *task.Manager
	// Cron, when set, enables recurring-job tools (cron_create, etc.).
	Cron *cron.Scheduler
	// Sandbox, when set, routes shell execution through a sandbox backend
	// (local, Docker, Podman, etc.). Nil = direct local execution.
	Sandbox sandbox.Backend
	// FileTracker, when set, enables file staleness detection. Write/edit
	// tools reject edits to files modified externally since last read.
	FileTracker *filetracker.Tracker
	// LSP, when set, enables code intelligence tools (diagnostics, references).
	LSP *lsp.Manager
	// TodoList, when set, enables planning tools (todo_add, todo_update, todo_list).
	TodoList *TodoList
	// Questioner, when set, enables the ask_user tool for structured questions.
	Questioner Questioner
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
		opts.HTTPUserAgent = "aegis/0.1"
	}
	if opts.KrokiURL == "" {
		opts.KrokiURL = "https://kroki.io"
	}

	ft := opts.FileTracker
	tools := []tool.Tool{
		&readTool{root: root, tracker: ft},
		&writeTool{root: root, tracker: ft},
		&editTool{root: root, tracker: ft},
		&multieditTool{root: root, tracker: ft},
		&globTool{root: root},
		&grepTool{root: root},
		newShellTool(root, opts.ShellTimeoutSec, opts.Tasks, opts.Sandbox),
		&fetchTool{userAgent: opts.HTTPUserAgent},
		&searchTool{userAgent: opts.HTTPUserAgent},
		&modelsTool{},
		&securityScanTool{root: root},
		&diagramTool{root: root, krokiURL: opts.KrokiURL},
		&latexBuildTool{root: root},
		&latexNewDocumentTool{root: root},
	}
	if opts.DataDir != "" {
		src := memory.Sources{ProjectRoot: root, DataDir: opts.DataDir}
		tools = append(tools, &rememberTool{src: src}, &saveSkillTool{src: src})
	}
	if opts.Tasks != nil {
		tools = append(tools, TaskTools(opts.Tasks, root, opts.ShellTimeoutSec, opts.Sandbox)...)
	}
	if opts.Cron != nil {
		tools = append(tools, CronTools(opts.Cron)...)
	}
	if opts.LSP != nil {
		tools = append(tools, LSPTools(opts.LSP, root)...)
	}
	if opts.TodoList != nil {
		tools = append(tools, TodoTools(opts.TodoList)...)
	}
	if opts.Questioner != nil {
		tools = append(tools, &askTool{questioner: opts.Questioner})
	}
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// resolvePath joins p against root and rejects paths that escape it. It
// delegates to the sandbox path validator which resolves symlinks to prevent
// symlink-based workspace escapes.
func resolvePath(root, p string) (string, error) {
	return sandbox.ValidatePath(root, p)
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
