export interface SessionMeta {
  id: string;
  title?: string;
  mode: string;
  model?: string;
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
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
  persona?: string;
  model?: string;
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
  messages?: Message[];
}

export interface PersonaInfo {
  name: string;
  description: string;
}

// StatusInfo is the GET /status response (daily totals + daemon defaults).
export interface StatusInfo {
  provider: string;
  model: string;
  daily_cost_usd: number;
  daily_cap_usd?: number;
  daily_tokens: number;
  daily_token_cap?: number;
}

// CheckpointInfo is one per-turn restore point (GET /sessions/{id}/checkpoints).
export interface CheckpointInfo {
  id: string;
  seq: number;
  label: string;
  git_sha?: string;
  file_count: number;
  created_at: string;
}

export interface RewindResponse {
  scope: string;
  files_restored: number;
  messages_kept: number;
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
    | "cost_alert"
    | "notice";
  text?: string;
  tool?: string;
  tool_input?: unknown;
  tool_result?: string;
  tool_is_error?: boolean;
  error?: string;
  approval_reason?: string;
  approval_id?: string;
}
