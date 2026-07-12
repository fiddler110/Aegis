import { useState } from "preact/hooks";
import type { SessionMeta } from "../types";

export type SidebarTool = "debate" | "knowledge" | "security" | "skillsmem" | "activity";

// SessionList is the sidebar: active/archived chat lists with archive,
// restore, and tidy-up (prune) actions (P15.9), live "working" indicators for
// chats with a response in flight, and launchers for the workspace-level
// tools (stress-test a claim, project knowledge, security check, skills &
// memory, activity — P15.6/P15.7/P15.8/P15.9).
export function SessionList({
  sessions,
  archivedSessions,
  view,
  onViewChange,
  currentId,
  runningIds,
  onSelect,
  onNew,
  onArchive,
  onUnarchive,
  onPrune,
  onOpenTool,
  activityCount,
  sandboxFallback,
  workdirSuggestions,
  defaultWorkdir,
}: {
  sessions: SessionMeta[];
  archivedSessions: SessionMeta[];
  view: "active" | "archived";
  onViewChange: (v: "active" | "archived") => void;
  currentId: string | null;
  runningIds: Set<string>;
  onSelect: (id: string) => void;
  // onNew creates a chat, optionally pinned to workdir (P15.13); it rejects
  // when the daemon refuses the workdir, so the caller (this component)
  // knows to keep the picker open rather than closing on a failed attempt.
  onNew: (workdir?: string) => Promise<void> | void;
  onArchive: (id: string) => void;
  onUnarchive: (id: string) => void;
  onPrune: (days: number) => void;
  onOpenTool: (tool: SidebarTool) => void;
  activityCount: number;
  // sandboxFallback (P25.2) surfaces GET /status's sandbox_fallback: the
  // daemon fell back to running commands unsandboxed on the host, which is
  // easy to miss as a startup log line alone — badge the button that opens
  // the panel where the detail (and the reason) lives.
  sandboxFallback?: boolean;
  // workdirSuggestions (P15.13) feeds the new-chat directory picker's
  // datalist: the configured server.session_workdir_allowlist plus recent
  // workdirs from other chats — real, known-good directories to suggest
  // instead of a placeholder guess.
  workdirSuggestions?: string[];
  // defaultWorkdir is the daemon's own default workspace (GET /status's
  // workspace, P26.1) — shown so "leave blank" has a concrete meaning.
  defaultWorkdir?: string;
}) {
  const [pruneOpen, setPruneOpen] = useState(false);
  const [pruneDays, setPruneDays] = useState("30");
  const [newChatOpen, setNewChatOpen] = useState(false);
  const [newChatWorkdir, setNewChatWorkdir] = useState("");
  const [newChatBusy, setNewChatBusy] = useState(false);

  const list = view === "active" ? sessions : archivedSessions;
  const days = parseInt(pruneDays, 10);

  // submitNewChat keeps the picker open on failure — onNew already toasts
  // the daemon's rejection reason (P15.13's "surface the error, don't
  // silently fall back" requirement), so all this needs to do is not close.
  const submitNewChat = async () => {
    setNewChatBusy(true);
    try {
      await onNew(newChatWorkdir.trim() || undefined);
      setNewChatOpen(false);
      setNewChatWorkdir("");
    } catch {
      // already surfaced as a toast by the caller
    } finally {
      setNewChatBusy(false);
    }
  };

  return (
    <aside id="sidebar">
      <header>
        <h1>Aegis</h1>
        <button
          class="secondary"
          onClick={() => {
            setNewChatOpen((v) => !v);
            setNewChatWorkdir("");
          }}
        >
          + New
        </button>
      </header>
      {newChatOpen && (
        <div class="new-chat-confirm">
          <label>
            Working directory (optional)
            <input
              type="text"
              list="workdir-suggestions"
              placeholder={defaultWorkdir || "daemon's default workspace"}
              value={newChatWorkdir}
              onInput={(e) => setNewChatWorkdir((e.target as HTMLInputElement).value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") submitNewChat();
              }}
            />
          </label>
          {workdirSuggestions && workdirSuggestions.length > 0 && (
            <datalist id="workdir-suggestions">
              {workdirSuggestions.map((w) => (
                <option value={w} key={w} />
              ))}
            </datalist>
          )}
          <span class="hint">
            Leave blank to use {defaultWorkdir ? <code>{defaultWorkdir}</code> : "the daemon's default workspace"}.
          </span>
          <div class="prune-actions">
            <button class="secondary" disabled={newChatBusy} onClick={submitNewChat}>
              {newChatBusy ? "Starting…" : "Start chat"}
            </button>
            <button
              class="secondary"
              disabled={newChatBusy}
              onClick={() => {
                setNewChatOpen(false);
                setNewChatWorkdir("");
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
      <div class="list-tabs">
        <button class={"tab" + (view === "active" ? " active" : "")} onClick={() => onViewChange("active")}>
          Chats
        </button>
        <button
          class={"tab" + (view === "archived" ? " active" : "")}
          onClick={() => onViewChange("archived")}
        >
          Archived{archivedSessions.length ? ` (${archivedSessions.length})` : ""}
        </button>
      </div>
      <div id="sessions">
        {list.length === 0 && (
          <p class="hint" style={{ padding: "10px 14px" }}>
            {view === "active" ? "No chats yet — press “+ New”." : "No archived chats."}
          </p>
        )}
        {list.map((s) => (
          <div
            class={"session" + (s.id === currentId ? " active" : "")}
            key={s.id}
            onClick={() => onSelect(s.id)}
          >
            <div class="t">{s.title || "(untitled)"}</div>
            <div class="m">
              {runningIds.has(s.id) && (
                <span class="working" title="The assistant is working on this chat">
                  <span class="livedot" /> working
                </span>
              )}
              {s.background && (
                <span
                  class="bgtag"
                  title="This chat keeps working even when no tab is watching it"
                >
                  background
                </span>
              )}
              {s.mode} · {new Date(s.updated_at).toLocaleString()}
            </div>
            <div class="row-actions">
              {view === "active" ? (
                <button
                  class="linkish"
                  title="Move this chat to Archived (it can be restored later)"
                  onClick={(e) => {
                    e.stopPropagation();
                    onArchive(s.id);
                  }}
                >
                  Archive
                </button>
              ) : (
                <button
                  class="linkish"
                  title="Move this chat back to the active list"
                  onClick={(e) => {
                    e.stopPropagation();
                    onUnarchive(s.id);
                  }}
                >
                  Restore
                </button>
              )}
            </div>
          </div>
        ))}
      </div>
      <footer class="sidebar-tools">
        <button class="tool-btn" onClick={() => onOpenTool("debate")}>
          ⚖️ Stress-test a claim
        </button>
        <button class="tool-btn" onClick={() => onOpenTool("knowledge")}>
          📚 Project knowledge
        </button>
        <button class="tool-btn" onClick={() => onOpenTool("security")}>
          🛡️ Security check
          {sandboxFallback ? (
            <span class="warn-count" title="Commands are running unsandboxed on the host — see the Sandbox tab">
              !
            </span>
          ) : null}
        </button>
        <button class="tool-btn" onClick={() => onOpenTool("skillsmem")}>
          🧠 Skills &amp; memory
        </button>
        <button class="tool-btn" onClick={() => onOpenTool("activity")}>
          📡 Activity{activityCount > 0 ? <span class="act-count">{activityCount}</span> : null}
        </button>
        {!pruneOpen ? (
          <button class="tool-btn" onClick={() => setPruneOpen(true)}>
            🧹 Tidy up old chats…
          </button>
        ) : (
          <div class="prune-confirm">
            <label>
              Permanently delete chats not used in the last{" "}
              <input
                type="number"
                min="1"
                value={pruneDays}
                onInput={(e) => setPruneDays((e.target as HTMLInputElement).value)}
              />{" "}
              days?
            </label>
            <span class="hint">Archived chats are kept. Deleted chats cannot be recovered.</span>
            <div class="prune-actions">
              <button
                class="danger"
                disabled={!Number.isFinite(days) || days <= 0}
                onClick={() => {
                  setPruneOpen(false);
                  onPrune(days);
                }}
              >
                Delete old chats
              </button>
              <button class="secondary" onClick={() => setPruneOpen(false)}>
                Cancel
              </button>
            </div>
          </div>
        )}
      </footer>
    </aside>
  );
}
