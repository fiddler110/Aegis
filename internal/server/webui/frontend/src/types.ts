export interface SessionMeta {
  id: string;
  title?: string;
  mode: string;
  model?: string;
  background?: boolean;
  archived?: boolean;
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
  created_at: string;
  updated_at: string;
  archived_at?: string;
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
  background?: boolean;
  archived?: boolean;
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

// RunInfo is one in-flight message run (GET /runs).
export interface RunInfo {
  run_id: string;
  session_id: string;
  title: string;
  started_at: string;
  tools: number; // tool calls so far this run
  last_kind: string; // most recent event kind
}

// Teammate is a sub-agent tracked by the swarm registry (GET /teammates).
export interface Teammate {
  agent_id: string;
  name: string;
  team: string;
  status: string;
  summary?: string;
  started_at: string;
  ended_at?: string;
}

// PruneResponse reports how many sessions POST /sessions/prune deleted.
export interface PruneResponse {
  deleted: number;
}

// BGEventItem is one buffered engine event from a background session
// (GET /sessions/{id}/events?since=N); data is a JSON-encoded Event.
export interface BGEventItem {
  id: number;
  data: string;
}

// DebateResponse is the POST /debate result: a formatted transcript plus the
// arbiter's parsed verdict (empty strings when the verdict didn't parse).
export interface DebateResponse {
  report: string;
  verdict: string; // "UPHOLD" | "REVISE" | "REJECT" | ""
  confidence: string; // "high" | "medium" | "low" | ""
}

// KnowledgeResult is one document matched by a knowledge query.
export interface KnowledgeResult {
  path: string;
  title: string;
  snippet: string;
  score: number;
}

// KnowledgeResponse carries the outcome of POST /knowledge: doc_count/db_path/
// embeddings_enabled after "index", results/count after "query".
export interface KnowledgeResponse {
  doc_count?: number;
  db_path?: string;
  embeddings_enabled?: boolean;
  results?: KnowledgeResult[];
  count?: number;
}

// MemoryResponse is the GET /memory response: what the assistant remembers
// for this project and across all projects, plus the names of the playbooks
// (skills) it can currently use.
export interface MemoryResponse {
  project_memory: string;
  user_memory: string;
  skills: string[] | null;
}

// BuiltinSkillInfo is one embedded built-in playbook in the /config/skills
// catalog (name + description), whether or not it is currently enabled.
export interface BuiltinSkillInfo {
  name: string;
  description: string;
}

// ConfigSkillsResponse is the GET/PATCH /config/skills response: which
// built-in playbooks are turned on, plus the full catalog of built-ins that
// could be (P15.7).
export interface ConfigSkillsResponse {
  scope: string;
  builtin_enabled: string[] | null;
  available: BuiltinSkillInfo[] | null;
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
