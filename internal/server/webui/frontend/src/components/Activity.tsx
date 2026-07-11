import { useEffect, useState } from "preact/hooks";
import { api } from "../api";
import type { RunInfo, Teammate } from "../types";

// Activity is the daemon-wide activity popover (P15.9): every response
// currently being worked on across all chats (GET /runs) and any helper
// agents the assistant has spawned (GET /teammates), refreshed every few
// seconds while the panel is open.

function ago(iso: string): string {
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.round(secs / 60)}m ago`;
  return `${Math.round(secs / 3600)}h ago`;
}

// phase turns the run's most recent event kind into a plain-language label.
function phase(lastKind: string): string {
  switch (lastKind) {
    case "text":
      return "writing";
    case "thinking":
      return "thinking";
    case "tool_call":
    case "tool_result":
      return "using tools";
    case "approval_request":
      return "waiting for approval";
    default:
      return "working";
  }
}

function helperStatus(status: string): { label: string; cls: string } {
  switch (status) {
    case "running":
      return { label: "working", cls: "run" };
    case "done":
      return { label: "finished", cls: "ok" };
    case "failed":
      return { label: "failed", cls: "fail" };
    default:
      return { label: status, cls: "" };
  }
}

export function Activity({
  onOpenSession,
  onClose,
}: {
  onOpenSession: (id: string) => void;
  onClose: () => void;
}) {
  const [runs, setRuns] = useState<RunInfo[] | null>(null);
  const [teammates, setTeammates] = useState<Teammate[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let stop = false;
    const load = async () => {
      try {
        const [r, t] = await Promise.all([
          (await api("/runs")).json() as Promise<RunInfo[]>,
          (await api("/teammates")).json() as Promise<Teammate[]>,
        ]);
        if (stop) return;
        setRuns(r || []);
        setTeammates(t || []);
        setError(null);
      } catch (e) {
        if (!stop) setError((e as Error).message);
      }
    };
    load();
    const timer = window.setInterval(load, 3000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, []);

  return (
    <>
      <div class="backdrop" onClick={onClose} />
      <div class="panel" role="dialog" aria-label="Activity">
        <div class="panel-head">
          <strong>Activity</strong>
          <button class="linkish" onClick={onClose}>
            Close
          </button>
        </div>
        <p class="hint">What the assistant is working on right now, across all your chats.</p>
        {error && <p class="err">{error}</p>}

        <div class="act-section">Responses in progress</div>
        {runs === null && !error && <p class="hint">Loading…</p>}
        {runs !== null && runs.length === 0 && <p class="hint">Nothing is running right now.</p>}
        {(runs || []).map((r) => (
          <div class="act-row" key={r.run_id}>
            <span class="livedot" />
            <div class="act-info">
              <div class="act-title">{r.title || "(untitled chat)"}</div>
              <div class="act-meta">
                {phase(r.last_kind)} · started {ago(r.started_at)}
                {r.tools > 0 ? ` · ${r.tools} tool step${r.tools === 1 ? "" : "s"}` : ""}
              </div>
            </div>
            <button class="secondary" onClick={() => onOpenSession(r.session_id)}>
              Open
            </button>
          </div>
        ))}

        <div class="act-section">Helper agents</div>
        {teammates === null && !error && <p class="hint">Loading…</p>}
        {teammates !== null && teammates.length === 0 && (
          <p class="hint">No helper agents have been used yet. The assistant spawns them for bigger jobs.</p>
        )}
        {(teammates || []).map((t) => {
          const st = helperStatus(t.status);
          return (
            <div class="act-row" key={t.agent_id}>
              <span class={"act-status " + st.cls}>{st.label}</span>
              <div class="act-info">
                <div class="act-title">
                  {t.name}
                  {t.team ? <span class="act-team"> · team {t.team}</span> : null}
                </div>
                <div class="act-meta">
                  started {ago(t.started_at)}
                  {t.summary ? ` · ${t.summary}` : ""}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
}
