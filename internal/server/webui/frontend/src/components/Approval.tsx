import { useState } from "preact/hooks";
import { api } from "../api";

// suggestPattern derives a sensible permission-rule pattern from the pending
// tool call's input, mirroring the TUI's suggestRulePattern
// (internal/tui/approval.go): shell commands are scoped to their first two
// words ("npm test …" → "npm test*"), file tools to the file's directory,
// network tools to the host.
export function suggestPattern(input: unknown): string {
  const a = (typeof input === "object" && input !== null ? input : {}) as Record<string, unknown>;
  const command = typeof a.command === "string" ? a.command : "";
  const path =
    typeof a.path === "string" && a.path
      ? a.path
      : typeof a.file_path === "string"
        ? a.file_path
        : "";
  const url = typeof a.url === "string" ? a.url : "";
  if (command.trim()) {
    const f = command.trim().split(/\s+/);
    if (f.length === 1) return f[0] + "*";
    return f[0] + " " + f[1] + "*";
  }
  if (path) {
    // Widen a file path to its containing directory, keeping the path's own
    // separator style; a bare filename is used as-is.
    const i = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
    return i < 0 ? path : path.slice(0, i + 1) + "*";
  }
  if (url) {
    try {
      const u = new URL(url);
      if (u.host) return u.protocol + "//" + u.host + "/*";
    } catch {
      // fall through to the raw URL
    }
    return url;
  }
  return "*";
}

// Approval renders an in-transcript permission prompt. Beyond one-off
// allow/reject it offers "always allow" (P15.10): a checkbox plus an editable
// pattern that saves a persistent project rule, so similar future requests
// won't ask again.
export function Approval({
  sessionId,
  approvalId,
  reason,
  tool,
  toolInput,
}: {
  sessionId: string;
  approvalId: string;
  reason: string;
  tool?: string;
  toolInput?: unknown;
}) {
  const [answered, setAnswered] = useState<"allowed" | "always" | "rejected" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [always, setAlways] = useState(false);
  const [pattern, setPattern] = useState(() => suggestPattern(toolInput));

  const answer = async (ok: boolean) => {
    setBusy(true);
    setError(null);
    const persist = ok && always && pattern.trim() !== "";
    try {
      await api(`/sessions/${sessionId}/approve`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          approved: ok,
          id: approvalId,
          ...(persist ? { allow_always: true, pattern: pattern.trim() } : {}),
        }),
      });
      setAnswered(ok ? (persist ? "always" : "allowed") : "rejected");
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
          <div class="approval-actions">
            <button disabled={busy} onClick={() => answer(true)}>
              Allow
            </button>{" "}
            <button class="secondary" disabled={busy} onClick={() => answer(false)}>
              Reject
            </button>
          </div>
          {tool && (
            <div class="always">
              <label>
                <input
                  type="checkbox"
                  checked={always}
                  disabled={busy}
                  onChange={(e) => setAlways((e.target as HTMLInputElement).checked)}
                />{" "}
                Don’t ask again for requests like this
              </label>
              {always && (
                <div class="always-detail">
                  <span>Allow “{tool}” when it matches:</span>
                  <input
                    type="text"
                    value={pattern}
                    disabled={busy}
                    onInput={(e) => setPattern((e.target as HTMLInputElement).value)}
                  />
                  <span class="hint">
                    Saved as a rule for this project — future matches run without asking. Use * as
                    a wildcard.
                  </span>
                </div>
              )}
            </div>
          )}
        </>
      )}
      {error && <p class="err">{error}</p>}
      {answered && (
        <div class="m">
          {answered === "rejected"
            ? "Rejected."
            : answered === "always"
              ? "Allowed — a rule was saved so this won’t ask again."
              : "Allowed."}
        </div>
      )}
    </div>
  );
}
