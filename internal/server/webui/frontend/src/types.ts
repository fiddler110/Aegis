export interface SessionMeta {
  id: string;
  title?: string;
  mode: string;
  updated_at: string;
}

export interface ContentBlock {
  type: "text" | "thinking" | "tool_use" | "tool_result" | "image";
  text?: string;
  name?: string;
  input?: unknown;
  content?: string;
  is_error?: boolean;
  data?: string;
  media_type?: string;
}

export interface Message {
  role: "user" | "assistant";
  content?: ContentBlock[];
}

export interface Session {
  id: string;
  title?: string;
  mode: string;
  messages?: Message[];
}

export interface Event {
  kind:
    | "text"
    | "thinking"
    | "tool_call"
    | "tool_result"
    | "turn_done"
    | "done"
    | "error"
    | "approval_request"
    | "steer"
    | "guard"
    | "cost_alert";
  text?: string;
  tool?: string;
  tool_input?: unknown;
  tool_result?: string;
  tool_is_error?: boolean;
  error?: string;
  approval_reason?: string;
  approval_id?: string;
}
