import { useState } from "preact/hooks";
import type { SessionMeta } from "../types";

export type SidebarTool = "debate" | "knowledge" | "skillsmem" | "activity";

// SessionList is the sidebar: active/archived chat lists with archive,
// restore, and tidy-up (prune) actions (P15.9), live "working" indicators for
// chats with a response in flight, and launchers for the workspace-level
// tools (stress-test a claim, project knowledge, skills & memory, activity —
// P15.7/P15.8/P15.9).
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
}: {
  sessions: SessionMeta[];
  archivedSessions: SessionMeta[];
  view: "active" | "archived";
  onViewChange: (v: "active" | "archived") => void;
  currentId: string | null;
  runningIds: Set<string>;
  onSelect: (id: string) => void;
  onNew: () => void;
  onArchive: (id: string) => void;
  onUnarchive: (id: string) => void;
  onPrune: (days: number) => void;
  onOpenTool: (tool: SidebarTool) => void;
  activityCount: number;
}) {
  const [pruneOpen, setPruneOpen] = useState(false);
  const [pruneDays, setPruneDays] = useState("30");

  const list = view === "active" ? sessions : archivedSessions;
  const days = parseInt(pruneDays, 10);

  return (
    <aside id="sidebar">
      <header>
        <h1>Aegis</h1>
        <button class="secondary" onClick={onNew}>
          + New
        </button>
      </header>
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
