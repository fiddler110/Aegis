import { useEffect, useRef, useState } from "preact/hooks";
import { api } from "../api";
import type { DebateResponse } from "../types";

// DebatePanel is the "Stress-test a claim" popover (P15.8): POST /debate runs
// a multi-agent debate — one assistant proposes the case for the claim, a
// critic attacks it over one or more rounds, and a judge issues a verdict.
// A debate spends several real model turns, so runs are slow: the panel keeps
// an elapsed timer going, blocks accidental backdrop-dismissal while running,
// and warns that closing cancels the run (aborting the fetch cancels the
// server-side debate via the request context).

interface ReportSection {
  label: string;
  body: string;
}

// parseReport splits the plain-text transcript (debate.Transcript.Format) on
// its "=== X ===" / "--- Round N … ---" markers into collapsible sections
// with plain-language labels. Unrecognized text falls into the current
// section, and a report with no markers renders as a single section.
function parseReport(report: string): ReportSection[] {
  const sections: ReportSection[] = [];
  let current: ReportSection | null = null;
  for (const line of report.split("\n")) {
    const m = line.match(/^\s*(?:===\s*(.+?)\s*===|---\s*(.+?)\s*---)\s*$/);
    if (m) {
      if (current) sections.push(current);
      current = { label: friendlyLabel(m[1] || m[2] || ""), body: "" };
      continue;
    }
    if (!current) current = { label: "Transcript", body: "" };
    current.body += line + "\n";
  }
  if (current) sections.push(current);
  return sections.filter((s) => s.label || s.body.trim());
}

function friendlyLabel(raw: string): string {
  const r = raw.trim();
  if (/^Claim$/i.test(r)) return "The claim under review";
  if (/^Verdict$/i.test(r)) return "The judge's reasoning";
  let m = r.match(/^Round (\d+) critique\s*(\[.*\])?$/i);
  if (m) {
    const tag = m[2] === "[evidence cited]" ? " (cited evidence)" : m[2] === "[unsubstantiated]" ? " (no evidence cited)" : "";
    return `Round ${m[1]} — the critic's challenge${tag}`;
  }
  m = r.match(/^Round (\d+) rebuttal$/i);
  if (m) return `Round ${m[1]} — the response`;
  m = r.match(/^Round (\d+): skipped \(budget exhausted\)$/i);
  if (m) return `Round ${m[1]} — skipped (spending budget ran out)`;
  return r;
}

function verdictSummary(v: string): { text: string; cls: string } {
  switch (v) {
    case "UPHOLD":
      return { text: "The claim held up under scrutiny.", cls: "good" };
    case "REVISE":
      return { text: "The claim mostly holds, but needs revision.", cls: "mixed" };
    case "REJECT":
      return { text: "The claim did not hold up.", cls: "bad" };
    default:
      return { text: "The judge did not reach a clear verdict — read the reasoning below.", cls: "mixed" };
  }
}

export function DebatePanel({ onClose }: { onClose: () => void }) {
  const [claim, setClaim] = useState("");
  const [domain, setDomain] = useState<"security" | "generic">("security");
  const [files, setFiles] = useState("");
  const [busy, setBusy] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DebateResponse | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!busy) return;
    const start = Date.now();
    const t = window.setInterval(() => setElapsed(Math.round((Date.now() - start) / 1000)), 1000);
    return () => window.clearInterval(t);
  }, [busy]);

  // Abort an in-flight debate if the panel unmounts, so the daemon stops
  // spending model turns on a run nobody is waiting for.
  useEffect(() => () => abortRef.current?.abort(), []);

  const run = async () => {
    const c = claim.trim();
    if (!c || busy) return;
    setBusy(true);
    setError(null);
    setResult(null);
    setElapsed(0);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      const fileList = files
        .split(/[\n,]/)
        .map((f) => f.trim())
        .filter(Boolean);
      const r = await api("/debate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          claim: c,
          domain,
          ...(fileList.length ? { files: fileList } : {}),
        }),
        signal: controller.signal,
      });
      setResult((await r.json()) as DebateResponse);
    } catch (e) {
      const err = e as Error;
      setError(err.name === "AbortError" ? "Debate cancelled." : err.message);
    } finally {
      setBusy(false);
      abortRef.current = null;
    }
  };

  const cancel = () => abortRef.current?.abort();

  const summary = result ? verdictSummary(result.verdict) : null;

  return (
    <>
      <div class="backdrop" onClick={busy ? undefined : onClose} />
      <div class="panel wide" role="dialog" aria-label="Stress-test a claim">
        <div class="panel-head">
          <strong>Stress-test a claim</strong>
          <button class="linkish" onClick={busy ? cancel : onClose}>
            {busy ? "Cancel" : "Close"}
          </button>
        </div>
        <p class="hint">
          Three assistants debate a statement you provide: one argues for it, one attacks it, and a
          judge rules on whether it holds up. Useful for security assertions, plans, or design
          decisions you want challenged before relying on them.
        </p>
        <textarea
          class="claim-input"
          placeholder="The statement to stress-test, e.g. “Our login rate-limiting is enough to stop password-guessing attacks.”"
          value={claim}
          disabled={busy}
          onInput={(e) => setClaim((e.target as HTMLTextAreaElement).value)}
        />
        <div class="debate-opts">
          <label>
            <input
              type="radio"
              name="debate-domain"
              checked={domain === "security"}
              disabled={busy}
              onChange={() => setDomain("security")}
            />
            Security claim
          </label>
          <label>
            <input
              type="radio"
              name="debate-domain"
              checked={domain === "generic"}
              disabled={busy}
              onChange={() => setDomain("generic")}
            />
            General claim (plans, documents, decisions)
          </label>
        </div>
        <details class="advanced">
          <summary>Advanced: ground the debate in specific files</summary>
          <p class="hint">
            List project file paths (one per line or comma-separated) the debaters should read
            before arguing — e.g. the document or code the claim is about.
          </p>
          <textarea
            class="files-input"
            placeholder={"docs/design.md\ninternal/auth/login.go"}
            value={files}
            disabled={busy}
            onInput={(e) => setFiles((e.target as HTMLTextAreaElement).value)}
          />
        </details>
        <div class="debate-run">
          <button disabled={busy || !claim.trim()} onClick={run}>
            {busy ? "Debating…" : "Start the debate"}
          </button>
          {!busy && !result && (
            <span class="hint">Takes a few minutes — several model turns run behind the scenes.</span>
          )}
        </div>
        {busy && (
          <p class="hint">
            <span class="livedot" /> Debate in progress ({elapsed}s) — the assistants are arguing
            for, against, and judging the claim. This can take several minutes; leave this panel
            open. Cancel stops the debate.
          </p>
        )}
        {error && <p class="err">{error}</p>}
        {result && summary && (
          <div class="debate-result">
            <div class={"verdict " + summary.cls}>
              <strong>{summary.text}</strong>
              {result.confidence && <span class="vc">Judge's confidence: {result.confidence}</span>}
            </div>
            <div class="debate-sections">
              {parseReport(result.report).map((s, i) => (
                <details key={i} open={i === 0 || /judge/i.test(s.label)}>
                  <summary>{s.label}</summary>
                  <pre>{s.body.trim()}</pre>
                </details>
              ))}
            </div>
          </div>
        )}
      </div>
    </>
  );
}
