package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/scottymacleod/aegis/internal/tool"
)

// mcpTool adapts an MCP server tool to the harness tool.Tool interface.
type mcpTool struct {
	client      *Client
	info        ToolInfo
	exposedName string
}

func (t *mcpTool) Name() string                { return t.exposedName }
func (t *mcpTool) Description() string         { return t.info.Description }
func (t *mcpTool) Capability() tool.Capability { return tool.CapNetwork }
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

// mcpResourceListTool lists available resources from an MCP server.
type mcpResourceListTool struct {
	client      *Client
	exposedName string
}

func (t *mcpResourceListTool) Name() string { return t.exposedName }
func (t *mcpResourceListTool) Description() string {
	return "List available resources from the " + t.client.Server() + " MCP server"
}
func (t *mcpResourceListTool) Capability() tool.Capability { return tool.CapNetwork }
func (t *mcpResourceListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *mcpResourceListTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	resources, err := t.client.ListResources(ctx)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("list resources failed: %v", err), IsError: true}, nil
	}
	if len(resources) == 0 {
		return tool.Result{Content: "no resources available"}, nil
	}
	data, _ := json.Marshal(resources)
	return tool.Result{Content: string(data)}, nil
}

// mcpResourceReadTool reads a resource by URI from an MCP server.
type mcpResourceReadTool struct {
	client      *Client
	exposedName string
}

func (t *mcpResourceReadTool) Name() string { return t.exposedName }
func (t *mcpResourceReadTool) Description() string {
	return "Read a resource by URI from the " + t.client.Server() + " MCP server"
}
func (t *mcpResourceReadTool) Capability() tool.Capability { return tool.CapNetwork }
func (t *mcpResourceReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string","description":"Resource URI to read"}},"required":["uri"]}`)
}
func (t *mcpResourceReadTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(input, &params); err != nil || params.URI == "" {
		return tool.Result{Content: "uri is required", IsError: true}, nil
	}
	text, mimeType, err := t.client.ReadResource(ctx, params.URI)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("read resource failed: %v", err), IsError: true}, nil
	}
	content := text
	if mimeType != "" && mimeType != "text/plain" && mimeType != "text/plain; charset=utf-8" {
		content = fmt.Sprintf("[%s]\n%s", mimeType, text)
	}
	return tool.Result{Content: content}, nil
}

// mcpPromptListTool lists available prompt templates from an MCP server.
type mcpPromptListTool struct {
	client      *Client
	exposedName string
}

func (t *mcpPromptListTool) Name() string { return t.exposedName }
func (t *mcpPromptListTool) Description() string {
	return "List available prompt templates from the " + t.client.Server() + " MCP server"
}
func (t *mcpPromptListTool) Capability() tool.Capability { return tool.CapNetwork }
func (t *mcpPromptListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *mcpPromptListTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	prompts, err := t.client.ListPrompts(ctx)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("list prompts failed: %v", err), IsError: true}, nil
	}
	if len(prompts) == 0 {
		return tool.Result{Content: "no prompts available"}, nil
	}
	data, _ := json.Marshal(prompts)
	return tool.Result{Content: string(data)}, nil
}

// mcpPromptGetTool retrieves a named prompt template with its rendered messages.
type mcpPromptGetTool struct {
	client      *Client
	exposedName string
}

func (t *mcpPromptGetTool) Name() string { return t.exposedName }
func (t *mcpPromptGetTool) Description() string {
	return "Get a prompt template by name from the " + t.client.Server() + " MCP server"
}
func (t *mcpPromptGetTool) Capability() tool.Capability { return tool.CapNetwork }
func (t *mcpPromptGetTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Prompt name"},"arguments":{"type":"object","description":"Prompt arguments as key-value pairs","additionalProperties":{"type":"string"}}},"required":["name"]}`)
}
func (t *mcpPromptGetTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(input, &params); err != nil || params.Name == "" {
		return tool.Result{Content: "name is required", IsError: true}, nil
	}
	desc, messages, err := t.client.GetPrompt(ctx, params.Name, params.Arguments)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("get prompt failed: %v", err), IsError: true}, nil
	}
	var sb strings.Builder
	if desc != "" {
		sb.WriteString(desc)
		sb.WriteString("\n\n")
	}
	for _, m := range messages {
		fmt.Fprintf(&sb, "[%s]: %s\n", m.Role, m.Content.Text)
	}
	return tool.Result{Content: strings.TrimSpace(sb.String())}, nil
}

// ServerConfig configures one MCP server.
type ServerConfig struct {
	Name    string            `koanf:"name"`
	Command string            `koanf:"command"`
	Args    []string          `koanf:"args"`
	Env     map[string]string `koanf:"env"`
	Auth    string            `koanf:"auth"` // Bearer token for HTTP servers
}

// RegisterServers connects each configured MCP server, registers its tools
// (namespaced as mcp__<server>__<tool>), and returns the live clients for
// later cleanup. A server that fails to connect is logged and skipped.
//
// For servers that support resources or prompts, additional tools are registered:
//   - mcp__<server>__list_resources / mcp__<server>__read_resource
//   - mcp__<server>__list_prompts  / mcp__<server>__get_prompt
//
// Dynamic refresh: if the server sends a notifications/tools/list_changed
// notification, the tool list is re-fetched and the registry is updated.
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

		// Probe resources support. Servers that don't implement resources/list
		// return an MCP error; we skip registration silently in that case.
		if _, err := client.ListResources(ctx); err == nil {
			prefix := fmt.Sprintf("mcp__%s", sc.Name)
			_ = reg.Register(&mcpResourceListTool{client: client, exposedName: prefix + "__list_resources"})
			_ = reg.Register(&mcpResourceReadTool{client: client, exposedName: prefix + "__read_resource"})
			logger.Info("mcp resources registered", "server", sc.Name)
		}

		// Probe prompts support similarly.
		if _, err := client.ListPrompts(ctx); err == nil {
			prefix := fmt.Sprintf("mcp__%s", sc.Name)
			_ = reg.Register(&mcpPromptListTool{client: client, exposedName: prefix + "__list_prompts"})
			_ = reg.Register(&mcpPromptGetTool{client: client, exposedName: prefix + "__get_prompt"})
			logger.Info("mcp prompts registered", "server", sc.Name)
		}

		// Wire dynamic tool refresh: re-list and upsert on tools/list_changed.
		if reg != nil {
			serverName := sc.Name
			cl := client
			client.onToolsChanged = func() {
				newTools, err := cl.ListTools(ctx)
				if err != nil {
					logger.Warn("mcp tool refresh failed", "server", serverName, "err", err)
					return
				}
				for _, info := range newTools {
					name := fmt.Sprintf("mcp__%s__%s", serverName, info.Name)
					reg.Upsert(&mcpTool{client: cl, info: info, exposedName: name})
				}
				logger.Info("mcp tools refreshed", "server", serverName, "tools", len(newTools))
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
