// Package plugins provides a loader for external process tools. A process tool
// is an external command declared in config with a JSON Schema for its input.
// When invoked, the harness pipes the tool input as JSON to the command's
// stdin and captures stdout as the result. This is simpler than MCP — no
// protocol negotiation, just stdin/stdout JSON.
package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/tool"
)

// ProcessToolConfig declares one external process tool.
type ProcessToolConfig struct {
	Name        string          `koanf:"name"`
	Description string          `koanf:"description"`
	Command     string          `koanf:"command"`
	Args        []string        `koanf:"args"`
	InputSchema json.RawMessage `koanf:"input_schema"` // JSON Schema for the tool's input
	// Capability is informational only ("read", "write", "execute",
	// "network") — shown in tool listings/docs, but never fed to the
	// permission gate (P42.2). It's config data, potentially from an
	// untrusted project's .aegis/config.yaml; trusting it verbatim would let
	// a plugin declare "read" or "write" to dodge the plan-mode/build-mode
	// friction that Execute's actual arbitrary-command-execution deserves.
	// Capability() below always reports CapExecute regardless of this value.
	Capability string `koanf:"capability"`
	TimeoutSec int    `koanf:"timeout_sec"` // per-invocation timeout; default 30
}

// processTool adapts an external command to the tool.Tool interface.
type processTool struct {
	cfg ProcessToolConfig
}

func (t *processTool) Name() string        { return t.cfg.Name }
func (t *processTool) Description() string { return t.cfg.Description }
func (t *processTool) InputSchema() json.RawMessage {
	if len(t.cfg.InputSchema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return t.cfg.InputSchema
}

// Capability always reports CapExecute: Execute below runs an arbitrary host
// command no matter what cfg.Capability claims, and cfg is untrusted project
// config the permission gate must not let narrow its own friction (P42.2).
func (t *processTool) Capability() tool.Capability {
	return tool.CapExecute
}

func (t *processTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	timeout := time.Duration(t.cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, t.cfg.Command, t.cfg.Args...)
	cmd.Stdin = strings.NewReader(string(input))

	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))

	if runCtx.Err() == context.DeadlineExceeded {
		return tool.Result{Content: fmt.Sprintf("plugin %s timed out after %s\n%s", t.cfg.Name, timeout, text), IsError: true}, nil
	}
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("plugin %s failed: %v\n%s", t.cfg.Name, err, text), IsError: true}, nil
	}
	if text == "" {
		text = "(no output)"
	}
	return tool.Result{Content: text}, nil
}

// RegisterProcessTools registers all configured process tools into the
// registry. Tools with missing name or command are skipped.
func RegisterProcessTools(reg *tool.Registry, configs []ProcessToolConfig, logger *slog.Logger) {
	for _, cfg := range configs {
		if cfg.Name == "" || cfg.Command == "" {
			continue
		}
		if cfg.TimeoutSec <= 0 {
			cfg.TimeoutSec = 30
		}
		t := &processTool{cfg: cfg}
		if err := reg.Register(t); err != nil {
			if logger != nil {
				logger.Warn("plugin tool register failed", "name", cfg.Name, "err", err)
			}
			continue
		}
		if logger != nil {
			logger.Info("registered process tool", "name", cfg.Name, "command", cfg.Command)
		}
	}
}
