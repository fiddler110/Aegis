import type { SessionMeta } from "../types";

export function SessionList({
  sessions,
  currentId,
  onSelect,
  onNew,
}: {
  sessions: SessionMeta[];
  currentId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
}) {
  return (
    <aside id="sidebar">
      <header>
        <h1>Aegis</h1>
        <button class="secondary" onClick={onNew}>
          + New
        </button>
      </header>
      <div id="sessions">
        {sessions.map((s) => (
          <div
            class={"session" + (s.id === currentId ? " active" : "")}
            key={s.id}
            onClick={() => onSelect(s.id)}
          >
            <div class="t">{s.title || "(untitled)"}</div>
            <div class="m">
              {s.mode} · {new Date(s.updated_at).toLocaleString()}
            </div>
          </div>
        ))}
      </div>
    </aside>
  );
}
