import { Fragment } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { api } from "../api";
import type {
  ScanFinding,
  ScanResponse,
  SecurityBaselineResponse,
  SecurityInstallResponse,
  SecurityStatusResponse,
  SecurityToolStatus,
} from "../types";

// SecurityPanel is the "Security check" popover (P15.6): run the built-in
// security scanners over the project (POST /security/scan) and read the
// outcome as a findings table, see which scanners are available (GET
// /security/status) with a guided install for missing ones (POST
// /security/install), and review the project's accepted risks (GET
// /security/baseline). Everything runs directly against the daemon — no
// model turn is spent. Installing a scanner modifies this computer, so the
// install flow is always two-phase: show the exact command first, run it
// only after an explicit confirmation click — never auto-confirmed.

type Tab = "scan" | "scanners" | "risks";

// Severity display order and colors — security.Severity's exact value set,
// highest first (the same order security.Report sorts findings in).
const SEVERITIES = ["CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO"];

function sevRank(s: string): number {
  const i = SEVERITIES.indexOf(s);
  return i === -1 ? SEVERITIES.length : i;
}

function sevClass(s: string): string {
  switch (s) {
    case "CRITICAL":
      return "sev crit";
    case "HIGH":
      return "sev high";
    case "MEDIUM":
      return "sev med";
    case "LOW":
      return "sev low";
    default:
      return "sev info";
  }
}

export function SecurityPanel({ onClose }: { onClose: () => void }) {
  const [tab, setTab] = useState<Tab>("scan");
  const [scanBusy, setScanBusy] = useState(false);

  return (
    <>
      <div class="backdrop" onClick={scanBusy ? undefined : onClose} />
      <div class="panel wide" role="dialog" aria-label="Security check">
        <div class="panel-head">
          <strong>Security check</strong>
          <button class="linkish" onClick={onClose}>
            Close
          </button>
        </div>
        <div class="list-tabs sec-tabs">
          <button class={"tab" + (tab === "scan" ? " active" : "")} onClick={() => setTab("scan")}>
            Check this project
          </button>
          <button
            class={"tab" + (tab === "scanners" ? " active" : "")}
            onClick={() => setTab("scanners")}
          >
            Scanners
          </button>
          <button class={"tab" + (tab === "risks" ? " active" : "")} onClick={() => setTab("risks")}>
            Accepted risks
          </button>
        </div>
        {/* All three tabs stay mounted so a running scan isn't cancelled (and
            its result isn't lost) by peeking at the other tabs. */}
        <div style={{ display: tab === "scan" ? "" : "none" }}>
          <ScanSection onBusy={setScanBusy} />
        </div>
        <div style={{ display: tab === "scanners" ? "" : "none" }}>
          <ScannersSection />
        </div>
        <div style={{ display: tab === "risks" ? "" : "none" }}>
          <RisksSection />
        </div>
      </div>
    </>
  );
}

// ─── Check this project (POST /security/scan) ──────────────────────────────

function ScanSection({ onBusy }: { onBusy: (b: boolean) => void }) {
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<ScanResponse | null>(null);
  const [openRow, setOpenRow] = useState<number | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!busy) return;
    const start = Date.now();
    const t = window.setInterval(() => setElapsed(Math.round((Date.now() - start) / 1000)), 1000);
    return () => window.clearInterval(t);
  }, [busy]);

  // Abort an in-flight scan if the panel unmounts, so the daemon stops
  // running scanners nobody is waiting for.
  useEffect(() => () => abortRef.current?.abort(), []);

  const run = async () => {
    if (busy) return;
    setBusy(true);
    onBusy(true);
    setError(null);
    setResult(null);
    setOpenRow(null);
    setElapsed(0);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      const p = path.trim();
      const r = await api("/security/scan", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(p ? { path: p } : {}),
        signal: controller.signal,
      });
      setResult((await r.json()) as ScanResponse);
    } catch (e) {
      const err = e as Error;
      setError(err.name === "AbortError" ? "Check cancelled." : err.message);
    } finally {
      setBusy(false);
      onBusy(false);
      abortRef.current = null;
    }
  };

  const findings = (result?.findings || [])
    .slice()
    .sort((a, b) => sevRank(a.severity) - sevRank(b.severity));
  const ranCount = result?.ran?.length ?? 0;
  const skipped = result?.skipped || {};
  const skippedCount = Object.keys(skipped).length;
  const suppressedCount = result?.suppressed?.length ?? 0;

  return (
    <>
      <p class="hint">
        Looks through this project for security problems — leaked passwords and keys, risky code,
        and known-vulnerable dependencies. Nothing in your files is changed.
      </p>
      <div class="sec-run">
        <input
          type="text"
          placeholder="Only check this folder (optional), e.g. src/"
          value={path}
          disabled={busy}
          onInput={(e) => setPath((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") run();
          }}
        />
        <button disabled={busy} onClick={run}>
          {busy ? `Checking… ${elapsed}s` : "Check now"}
        </button>
        {busy && (
          <button class="secondary" onClick={() => abortRef.current?.abort()}>
            Cancel
          </button>
        )}
      </div>
      {busy ? (
        <p class="hint">
          <span class="livedot" /> Several scanners are looking through the project — this can take
          a few minutes on a large one. Leave this panel open.
        </p>
      ) : (
        !result && <p class="hint">This can take a few minutes.</p>
      )}
      {error && <p class="err">{error}</p>}
      {result && (
        <>
          <p class="sec-summary">
            <strong>
              {ranCount === 0
                ? "No scanners could run"
                : findings.length === 0
                  ? "No problems found"
                  : `${findings.length} potential problem${findings.length === 1 ? "" : "s"} found`}
            </strong>
            {" · "}
            {ranCount} scanner{ranCount === 1 ? "" : "s"} ran · {skippedCount} skipped
          </p>
          {ranCount === 0 && (
            <p class="hint warn">
              None of the scanners were available — see the “Scanners” tab to install one.
            </p>
          )}
          {skippedCount > 0 && (
            <details class="advanced">
              <summary>
                Why {skippedCount === 1 ? "was 1 scanner" : `were ${skippedCount} scanners`} skipped?
              </summary>
              <ul class="sec-skips">
                {Object.entries(skipped).map(([name, reason]) => (
                  <li key={name}>
                    <strong>{name}</strong> — {reason}
                  </li>
                ))}
              </ul>
            </details>
          )}
          {suppressedCount > 0 && (
            <details class="advanced">
              <summary>
                {suppressedCount} known, accepted risk{suppressedCount === 1 ? "" : "s"} hidden
              </summary>
              <p class="hint">
                These matched an entry in this project's accepted-risk list (see the “Accepted
                risks” tab), so they aren't counted as new problems.
              </p>
              <ul class="sec-skips">
                {(result.suppressed || []).map((f, i) => (
                  <li key={i}>
                    <strong>{f.rule_id}</strong> — {f.title} ({f.location})
                  </li>
                ))}
              </ul>
            </details>
          )}
          {result.baseline_error && (
            <p class="hint warn">
              The accepted-risk list could not be read ({result.baseline_error}) — nothing was
              hidden.
            </p>
          )}
          {findings.length > 0 && (
            <table class="sec-table">
              <thead>
                <tr>
                  <th>How serious</th>
                  <th>Found by</th>
                  <th>Where</th>
                  <th>Problem</th>
                </tr>
              </thead>
              <tbody>
                {findings.map((f, i) => (
                  <Fragment key={i}>
                    <tr
                      class="sec-row"
                      title="Click for details and how to fix it"
                      onClick={() => setOpenRow(openRow === i ? null : i)}
                    >
                      <td>
                        <span class={sevClass(f.severity)}>{f.severity.toLowerCase()}</span>
                      </td>
                      <td>{f.tool}</td>
                      <td class="sec-loc">{f.location}</td>
                      <td>{f.title}</td>
                    </tr>
                    {openRow === i && (
                      <tr class="sec-detail">
                        <td colSpan={4}>
                          <FindingDetail f={f} />
                        </td>
                      </tr>
                    )}
                  </Fragment>
                ))}
              </tbody>
            </table>
          )}
          <details class="advanced">
            <summary>Technical report (raw text)</summary>
            <pre>{result.report}</pre>
          </details>
        </>
      )}
    </>
  );
}

function FindingDetail({ f }: { f: ScanFinding }) {
  const refs = [
    f.rule_id,
    f.cwe ? `CWE-${f.cwe}` : "",
    f.asvs ? `ASVS ${f.asvs}` : "",
    f.reachability ? `code is ${f.reachability}` : "",
    f.verification === "verified"
      ? "secret confirmed still active"
      : f.verification === "unverified"
        ? "secret checked — appears inactive"
        : "",
    f.seen_by?.length ? `also flagged by ${f.seen_by.join(", ")}` : "",
  ].filter(Boolean);
  return (
    <>
      {f.description && <p>{f.description}</p>}
      {f.remediation && (
        <p>
          <strong>How to fix:</strong> {f.remediation}
        </p>
      )}
      {!f.description && !f.remediation && (
        <p class="hint">The scanner gave no further detail for this finding.</p>
      )}
      {refs.length > 0 && <p class="hint">{refs.join(" · ")}</p>}
    </>
  );
}

// ─── Scanners (GET /security/status + POST /security/install) ──────────────

// InstallState tracks one tool's guided-install flow. "confirm" means the
// first (confirm:false) call came back with the exact host command and we're
// waiting for the user to explicitly approve running it — the state machine
// never goes to "running" without that click.
interface InstallState {
  phase: "loading" | "confirm" | "running" | "done";
  command?: string;
  output?: string;
  ok?: boolean;
  error?: string;
}

function ScannersSection() {
  const [tools, setTools] = useState<SecurityToolStatus[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [installs, setInstalls] = useState<Record<string, InstallState>>({});

  const load = async () => {
    setError(null);
    try {
      const r = await api("/security/status");
      const body = (await r.json()) as SecurityStatusResponse;
      setTools(body.tools || []);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const setInstall = (name: string, st: InstallState | null) =>
    setInstalls((prev) => {
      const next = { ...prev };
      if (st) next[name] = st;
      else delete next[name];
      return next;
    });

  // Phase 1: ask what installing would run — nothing executes without
  // confirm:true, so this is safe to fire on the button click.
  const askInstall = async (name: string) => {
    setInstall(name, { phase: "loading" });
    try {
      const r = await api("/security/install", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tool: name }),
      });
      const body = (await r.json()) as SecurityInstallResponse;
      if (!body.command) {
        // No guided install for this tool — surface why instead of a dead end.
        setInstall(name, { phase: "done", error: body.error || "no guided install available" });
        return;
      }
      setInstall(name, { phase: "confirm", command: body.command });
    } catch (e) {
      setInstall(name, { phase: "done", error: (e as Error).message });
    }
  };

  // Phase 2: the user explicitly approved the shown command — run it.
  const confirmInstall = async (name: string, command: string) => {
    setInstall(name, { phase: "running", command });
    try {
      const r = await api("/security/install", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tool: name, confirm: true }),
      });
      const body = (await r.json()) as SecurityInstallResponse;
      setInstall(name, {
        phase: "done",
        command,
        output: body.output,
        ok: body.ok,
        error: body.error,
      });
      if (body.ok) load(); // the tool should now resolve — refresh the badges
    } catch (e) {
      setInstall(name, { phase: "done", command, error: (e as Error).message });
    }
  };

  return (
    <>
      <p class="hint">
        Security checks are done by separate scanner programs. Each one below is either ready to
        use or missing from this computer — missing ones can usually be installed from here.
      </p>
      {error && <p class="err">{error}</p>}
      {tools === null && !error && <p class="hint">Checking which scanners are available…</p>}
      {tools !== null &&
        tools.map((t) => {
          const inst = installs[t.name];
          const unavailable = t.resolved === "unavailable";
          return (
            <div class="sec-tool" key={t.name}>
              <div class="sec-tool-head">
                <span class="sec-tool-name">
                  {t.name}
                  {t.category ? <span class="sec-cat"> · {t.category}</span> : null}
                </span>
                <span class={"avail" + (unavailable ? " off" : " ok")} title={t.status}>
                  {unavailable ? "not available" : t.status}
                </span>
                {unavailable && !inst && (
                  <button class="secondary" onClick={() => askInstall(t.name)}>
                    Install…
                  </button>
                )}
              </div>
              {unavailable && <p class="hint">{t.status.replace(/^unavailable: /, "")}</p>}
              {inst && (
                <div class="sec-install">
                  {inst.phase === "loading" && <span class="hint">Looking up the install command…</span>}
                  {inst.phase === "confirm" && inst.command && (
                    <>
                      <span>
                        Installing <strong>{t.name}</strong> will run this command on your
                        computer:
                      </span>
                      <pre>{inst.command}</pre>
                      <div class="actions">
                        <button onClick={() => confirmInstall(t.name, inst.command!)}>
                          Yes, run it
                        </button>
                        <button class="secondary" onClick={() => setInstall(t.name, null)}>
                          Cancel
                        </button>
                      </div>
                    </>
                  )}
                  {inst.phase === "running" && (
                    <span class="hint">
                      <span class="livedot" /> Installing — this can take a few minutes…
                    </span>
                  )}
                  {inst.phase === "done" && (
                    <>
                      {inst.ok ? (
                        <span class="sec-ok">Installed. This scanner is ready to use.</span>
                      ) : (
                        <span class="err">{inst.error || "The install did not finish."}</span>
                      )}
                      {inst.output && (
                        <details class="advanced">
                          <summary>Install output</summary>
                          <pre>{inst.output}</pre>
                        </details>
                      )}
                      <div class="actions">
                        <button class="secondary" onClick={() => setInstall(t.name, null)}>
                          Dismiss
                        </button>
                      </div>
                    </>
                  )}
                </div>
              )}
            </div>
          );
        })}
    </>
  );
}

// ─── Accepted risks (GET /security/baseline) — read-only ───────────────────

function RisksSection() {
  const [data, setData] = useState<SecurityBaselineResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    (async () => {
      try {
        const r = await api("/security/baseline");
        setData((await r.json()) as SecurityBaselineResponse);
      } catch (e) {
        setError((e as Error).message);
      }
    })();
  }, []);

  const entries = data?.suppressions || [];

  return (
    <>
      <p class="hint">
        Findings this project has decided to live with, each with a reason and an expiry date.
        Accepted risks are hidden from scan results until they expire.
      </p>
      {error && <p class="err">{error}</p>}
      {data && entries.length === 0 && !error && (
        <p class="hint">No accepted risks recorded for this project.</p>
      )}
      {entries.length > 0 && (
        <table class="sec-table">
          <thead>
            <tr>
              <th>Status</th>
              <th>Rule</th>
              <th>Where</th>
              <th>Why it's accepted</th>
              <th>Until</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e, i) => (
              <tr key={i}>
                <td>
                  <span
                    class={
                      "avail" +
                      (e.status === "active" ? " ok" : e.status === "expired" ? " off" : " bad")
                    }
                  >
                    {e.status}
                  </span>
                </td>
                <td class="sec-loc">{e.rule_id}</td>
                <td class="sec-loc">{e.location || "anywhere"}</td>
                <td>
                  {e.reason}
                  {e.added_by ? <span class="hint"> — {e.added_by}</span> : null}
                </td>
                <td>{e.expires}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {data?.path && <p class="hint">Kept in {data.path}.</p>}
    </>
  );
}
