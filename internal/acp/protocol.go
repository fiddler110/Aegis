package acp

import "encoding/json"

// protocolVersion is the ACP version this server implements.
const protocolVersion = 1

// ACP method names.
const (
	methodInitialize        = "initialize"
	methodAuthenticate      = "authenticate"
	methodNewSession        = "session/new"
	methodLoadSession       = "session/load"
	methodPrompt            = "session/prompt"
	methodCancel            = "session/cancel"
	methodSessionUpdate     = "session/update"
	methodRequestPermission = "session/request_permission"
)

// ACP stop reasons returned from session/prompt. The daemon event stream does
// not surface token/refusal stops distinctly, so only these two are emitted.
const (
	stopEndTurn   = "end_turn"
	stopCancelled = "cancelled"
)

// --- initialize ---

type initializeParams struct {
	ProtocolVersion    int             `json:"protocolVersion"`
	ClientCapabilities json.RawMessage `json:"clientCapabilities,omitempty"`
}

type initializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities agentCapabilities `json:"agentCapabilities"`
	AuthMethods       []authMethod      `json:"authMethods"`
}

type agentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities promptCapabilities `json:"promptCapabilities"`
}

type promptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type authMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// --- session/new ---

type newSessionParams struct {
	Cwd        string          `json:"cwd"`
	MCPServers json.RawMessage `json:"mcpServers,omitempty"`
}

type newSessionResult struct {
	SessionID string `json:"sessionId"`
}

// --- session/prompt ---

type promptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []contentBlock `json:"prompt"`
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}

// contentBlock is an ACP content block (text, image, audio, resource_link, or
// an embedded resource). Only the fields relevant to a given type are set.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// image / audio
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	// resource_link
	URI  string `json:"uri,omitempty"`
	Name string `json:"name,omitempty"`
	// resource (embedded)
	Resource *embeddedResource `json:"resource,omitempty"`
}

type embeddedResource struct {
	URI      string `json:"uri,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

func textBlock(s string) contentBlock { return contentBlock{Type: "text", Text: s} }

// --- session/update ---

type sessionUpdateParams struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"`
}

// sessionUpdate variant tags.
const (
	updAgentMessageChunk = "agent_message_chunk"
	updAgentThoughtChunk = "agent_thought_chunk"
	updToolCall          = "tool_call"
	updToolCallUpdate    = "tool_call_update"
)

type messageChunk struct {
	SessionUpdate string       `json:"sessionUpdate"`
	Content       contentBlock `json:"content"`
}

type toolCall struct {
	SessionUpdate string            `json:"sessionUpdate"`
	ToolCallID    string            `json:"toolCallId"`
	Title         string            `json:"title,omitempty"`
	Kind          string            `json:"kind,omitempty"`
	Status        string            `json:"status,omitempty"`
	RawInput      json.RawMessage   `json:"rawInput,omitempty"`
	Content       []toolCallContent `json:"content,omitempty"`
}

type toolCallContent struct {
	Type    string       `json:"type"` // "content"
	Content contentBlock `json:"content"`
}

// tool-call status values.
const (
	statusInProgress = "in_progress"
	statusCompleted  = "completed"
	statusFailed     = "failed"
)

// --- session/request_permission ---

type requestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  toolCall           `json:"toolCall"`
	Options   []permissionOption `json:"options"`
}

type permissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type requestPermissionResult struct {
	Outcome permissionOutcome `json:"outcome"`
}

type permissionOutcome struct {
	Outcome  string `json:"outcome"` // "selected" | "cancelled"
	OptionID string `json:"optionId,omitempty"`
}

// toolKind maps a built-in tool name to an ACP tool-call kind so editors can
// render an appropriate icon. Unknown tools fall back to "other".
func toolKind(name string) string {
	switch name {
	case "read_file", "glob", "grep", "git", "ls", "models", "lsp_diagnostics", "lsp_references":
		return "read"
	case "write_file", "edit_file", "multi_edit", "git_commit":
		return "edit"
	case "shell":
		return "execute"
	case "web_fetch", "web_search":
		return "fetch"
	default:
		return "other"
	}
}
