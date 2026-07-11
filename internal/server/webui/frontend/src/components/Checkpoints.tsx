import { useEffect, useState } from "preact/hooks";
import { api } from "../api";
import type { CheckpointInfo, RewindResponse } from "../types";

// Checkpoints is the "Restore points" popover (P15.5). Every prompt creates a
// restore point; picking one rewinds the chat (and any files the assistant
// changed that turn) back to just before that prompt. Rewinding is
// destructive, so each row asks for an inline confirmation first.
export function Checkpoints({
  sessionId,
  streaming,
  onRewound,
  onClose,
}: {
  sessionId: string;
  streaming: boolean;
  onRewound: (r: RewindResponse) => void;
  onClose: () => void;
}) {
  const [checkpoints, setCheckpoints] = useState<CheckpointInfo[] | null>(null);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api(`/sessions/${sessionId}/checkpoints`)
      .then(async (r) => {
        const list = ((await r.json()) as CheckpointInfo[]) || [];
        // Newest first: the point you most likely want to go back to is recent.
        list.sort((a, b) => b.seq - a.seq);
        setCheckpoints(list);
      })
      .catch((e) => setError((e as Error).message));
  }, [sessionId]);

  const rewind = async (id: string) => {
    setBusy(true);
    setError(null);
    try {
      const r = await api(`/sessions/${sessionId}/rewind`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ checkpoint_id: id }),
      });
      onRewound((await r.json()) as RewindResponse);
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
      setConfirmId(null);
    }
  };

  return (
    <>
      <div class="backdrop" onClick={onClose} />
      <div class="panel" role="dialog" aria-label="Restore points">
        <div class="panel-head">
          <strong>Restore points</strong>
          <button class="linkish" onClick={onClose}>
            Close
          </button>
        </div>
        <p class="hint">
          Each message you send creates a restore point. Going back rewinds the conversation and
          any files the assistant changed since then.
        </p>
        {streaming && <p class="hint warn">Wait for the current response to finish first.</p>}
        {error && <p class="err">{error}</p>}
        {checkpoints === null && !error && <p class="hint">Loading…</p>}
        {checkpoints !== null && checkpoints.length === 0 && (
          <p class="hint">No restore points yet — send a message to create one.</p>
        )}
        <div class="cp-list">
          {(checkpoints || []).map((cp) => (
            <div class="cp-row" key={cp.id}>
              <div class="cp-info">
                <div class="cp-label">{cp.label || "(no prompt)"}</div>
                <div class="cp-meta">
                  {new Date(cp.created_at).toLocaleString()}
                  {cp.file_count > 0
                    ? ` · ${cp.file_count} file${cp.file_count === 1 ? "" : "s"} changed after this`
                    : ""}
                </div>
              </div>
              {confirmId !== cp.id ? (
                <button
                  class="secondary"
                  disabled={busy || streaming}
                  onClick={() => setConfirmId(cp.id)}
                >
                  Go back here
                </button>
              ) : (
                <div class="cp-confirm">
                  <span>Rewind the chat and files to just before this prompt? This can’t be undone.</span>
                  <button class="danger" disabled={busy} onClick={() => rewind(cp.id)}>
                    {busy ? "Rewinding…" : "Yes, go back"}
                  </button>
                  <button class="secondary" disabled={busy} onClick={() => setConfirmId(null)}>
                    Cancel
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    </>
  );
}
