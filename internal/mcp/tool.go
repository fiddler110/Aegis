package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/scottymacleod/agentharness/internal/tool"
)

// mcpTool adapts an MCP server tool to the harness tool.Tool interface.
type mcpTool struct {
	client      *Client
	info        ToolInfo
	exposedName string
}

func (t *mcpTool) Name() string                { return t.exposedName }
func (t *mcpTool) Description() string          { return t.info.Description }
func (t *mcpTool) Capability() tool.Capability  { return tool.CapNetwork }
func (t *mcpTool) InputSchema() json.RawMessage {
	if len(t.info.InputSchema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return t.info.InputSchema
}
func (t *mcpTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	text, isErr, err := t.client.CallTool(ctx, t.info.Name, input)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("mcp call failed: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: text, IsError: isErr}, nil
}

// ServerConfig configures one MCP server.
type ServerConfig struct {
	Name    string            `koanf:"name"`
	Command string            `koanf:"command"`
	Args    []string          `koanf:"args"`
	Env     map[string]string `koanf:"env"`
}

// RegisterServers connects each configured MCP server, registers its tools
// (namespaced as mcp__<server>__<tool>), and returns the live clients for
// later cleanup. A server that fails to connect is logged and skipped.
func RegisterServers(ctx context.Context, reg *tool.Registry, servers []ServerConfig, logger *slog.Logger) []*Client {
	var clients []*Client
	for _, sc := range servers {
		if sc.Command == "" || sc.Name == "" {
			continue
		}
		client, err := NewHTTPOrStdio(ctx, sc)
		if err != nil {
			logger.Warn("mcp server connect failed", "server", sc.Name, "err", err)
			continue
		}
		tools, err := client.ListTools(ctx)
		if err != nil {
			logger.Warn("mcp list tools failed", "server", sc.Name, "err", err)
			_ = client.Close()
			continue
		}
		for _, info := range tools {
			name := fmt.Sprintf("mcp__%s__%s", sc.Name, info.Name)
			if err := reg.Register(&mcpTool{client: client, info: info, exposedName: name}); err != nil {
				logger.Warn("mcp tool register failed", "tool", name, "err", err)
			}
		}
		logger.Info("mcp server connected", "server", sc.Name, "tools", len(tools))
		clients = append(clients, client)
	}
	return clients
}

func flattenEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
