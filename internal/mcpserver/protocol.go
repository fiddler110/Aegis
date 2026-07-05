// Package mcpserver exposes the Aegis daemon as a Model Context Protocol
// (MCP) server (P6.3): sessions become MCP tools callable from any
// MCP-speaking harness (Claude Code, Codex, editors), the reverse direction
// of internal/mcp's client (which lets Aegis call out to external MCP
// servers). Transport is JSON-RPC 2.0, newline-delimited, over stdio — the
// same shape internal/mcp's client and internal/acp's server already use,
// rolled separately here (as those two packages each do for their own
// protocol) rather than introduced as a new shared abstraction.
//
// v1 scope, deliberately: three tools (aegis_prompt, aegis_new_session,
// aegis_list_sessions) that drive real Aegis sessions through the existing
// daemon HTTP API — no direct passthrough of individual built-in tools
// (security_scan, read_file, etc.) bypassing the agent loop, and no
// notifications/cancelled propagation. Both are undone follow-ups, not
// oversights.
package mcpserver

import "encoding/json"

const protocolVersion = "2024-11-05"

// --- initialize ---

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// --- tools/list ---

type toolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []toolInfo `json:"tools"`
}

var (
	promptInputSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"text": {"type": "string", "description": "The task or message to send to the agent."},
			"session_id": {"type": "string", "description": "Continue an existing session instead of starting a new one. Omit to start a new one."},
			"mode": {"type": "string", "enum": ["plan", "build", "auto"], "description": "Permission mode for a new session (ignored if session_id is set). Defaults to the server's configured default (plan unless changed)."},
			"persona": {"type": "string", "description": "Named persona for a new session (ignored if session_id is set)."}
		},
		"required": ["text"]
	}`)

	newSessionInputSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"mode": {"type": "string", "enum": ["plan", "build", "auto"], "description": "Permission mode. Defaults to the server's configured default (plan unless changed)."},
			"persona": {"type": "string", "description": "Named persona to apply."}
		}
	}`)

	emptyInputSchema = json.RawMessage(`{"type": "object", "properties": {}}`)
)

func listedTools() []toolInfo {
	return []toolInfo{
		{
			Name: "aegis_prompt",
			Description: "Delegate a task to an Aegis agent session and wait for it to finish. Starts a " +
				"new session unless session_id is given, in which case the task continues that " +
				"session's conversation. Returns the agent's final text reply; the reply's trailing " +
				"\"[session: <id>]\" marker can be passed back as session_id to continue the conversation. " +
				"By default runs in plan mode (read-only); pass mode: \"build\" for a session allowed to " +
				"write files and run commands.",
			InputSchema: promptInputSchema,
		},
		{
			Name:        "aegis_new_session",
			Description: "Create a new Aegis session without sending it a message yet. Returns the session id.",
			InputSchema: newSessionInputSchema,
		},
		{
			Name:        "aegis_list_sessions",
			Description: "List existing Aegis sessions (id, title, mode, last updated).",
			InputSchema: emptyInputSchema,
		},
	}
}

// --- tools/call ---

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

func textResult(text string) toolCallResult {
	return toolCallResult{Content: []contentBlock{{Type: "text", Text: text}}}
}

func errResult(err error) toolCallResult {
	return toolCallResult{Content: []contentBlock{{Type: "text", Text: err.Error()}}, IsError: true}
}

type promptArgs struct {
	SessionID string `json:"session_id,omitempty"`
	Text      string `json:"text"`
	Mode      string `json:"mode,omitempty"`
	Persona   string `json:"persona,omitempty"`
}

type newSessionArgs struct {
	Mode    string `json:"mode,omitempty"`
	Persona string `json:"persona,omitempty"`
}
