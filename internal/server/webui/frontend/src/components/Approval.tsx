import { useState } from "preact/hooks";
import { api } from "../api";

export function Approval({
  sessionId,
  approvalId,
  reason,
}: {
  sessionId: string;
  approvalId: string;
  reason: string;
}) {
  const [answered, setAnswered] = useState<"allowed" | "rejected" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const answer = async (ok: boolean) => {
    setBusy(true);
    try {
      await api(`/sessions/${sessionId}/approve`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ approved: ok, id: approvalId }),
      });
      setAnswered(ok ? "allowed" : "rejected");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div class="approval">
      <div class="q">⚠️ {reason}</div>
      {!answered && (
        <>
          <button disabled={busy} onClick={() => answer(true)}>
            Allow
          </button>{" "}
          <button class="secondary" disabled={busy} onClick={() => answer(false)}>
            Reject
          </button>
        </>
      )}
      {error && <p class="err">{error}</p>}
      {answered && <div class="m">{answered === "allowed" ? "Allowed." : "Rejected."}</div>}
    </div>
  );
}
