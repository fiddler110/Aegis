import { useEffect, useRef, useState } from "preact/hooks";
import { api, consumeSSE, exchangeToken } from "./api";
import type {
  BGEventItem,
  Event,
  Message,
  PersonaInfo,
  PruneResponse,
  RewindResponse,
  RunInfo,
  Session,
  SessionMeta,
  StatusInfo,
} from "./types";
import { SessionList, type SidebarTool } from "./components/SessionList";
import { Transcript, type RenderBlock, type TranscriptItem } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { SettingsPanel } from "./components/SettingsPanel";
import { Checkpoints } from "./components/Checkpoints";
import { DebatePanel } from "./components/DebatePanel";
import { KnowledgePanel } from "./components/KnowledgePanel";
import { SkillsMemoryPanel } from "./components/SkillsMemoryPanel";
import { SecurityPanel } from "./components/SecurityPanel";
import { Activity } from "./components/Activity";

function itemsFromMessages(messages: Message[], sessionId: string): TranscriptItem[] {
  const items: TranscriptItem[] = [];
  let seq = 0;
  for (const m of messages) {
    if (m.role === "user") {
      const results = (m.content || []).filter((b) => b.type === "tool_result");
      for (const r of results) {
        items.push({
          id: `h${seq++}`,
          role: "assistant",
          bare: true,
          blocks: [
            {
              kind: "tool",
              label: (r.is_error ? "✗ " : "✓ ") + "tool result",
              body: r.content || "",
              open: false,
              err: !!r.is_error,
            },
          ],
        });
      }
      const rest = (m.content || []).filter((b) => b.type === "text" || b.type === "image");
      if (!rest.length) continue;
      const blocks: RenderBlock[] = rest.map((b) =>
        b.type === "image"
          ? { kind: "image", src: "data:" + (b.media_type || "image/png") + ";base64," + b.data }
          : { kind: "text", text: b.text || "" }
      );
      items.push({ id: `h${seq++}`, role: "user", blocks });
      continue;
    }
    const blocks: RenderBlock[] = [];
    for (const b of m.content || []) {
      if (b.type === "text" && b.text) blocks.push({ kind: "text", text: b.text });
      else if (b.type === "thinking" && b.text)
        blocks.push({ kind: "thinking", text: b.text, open: false, label: "💭 thinking" });
      else if (b.type === "tool_use")
        blocks.push({
          kind: "tool",
          label: "🔧 " + (b.name || ""),
          body: typeof b.input === "string" ? b.input : JSON.stringify(b.input || {}, null, 2),
          open: false,
          err: false,
        });
      else if (b.type === "image" && b.data)
        blocks.push({ kind: "image", src: "data:" + (b.media_type || "image/png") + ";base64," + b.data });
    }
    if (blocks.length) items.push({ id: `h${seq++}`, role: "assistant", blocks });
  }
  void sessionId;
  return items;
}

// fmtUSD renders a dollar amount at a precision that keeps small local-model
// spends visible without turning big ones into noise.
function fmtUSD(v: number): string {
  if (v > 0 && v < 0.01) return "$" + v.toFixed(4);
  return "$" + v.toFixed(2);
}

// fmtTokens renders a token count compactly ("950", "8.4k", "1.2M").
function fmtTokens(v: number): string {
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(1) + "M";
  if (v >= 1000) return (v / 1000).toFixed(1) + "k";
  return String(v);
}

const sleep = (ms: number) => new Promise((res) => setTimeout(res, ms));

interface Toast {
  id: number;
  text: string;
  warn: boolean;
}

type Panel =
  | "none"
  | "assistant"
  | "checkpoints"
  | "debate"
  | "knowledge"
  | "security"
  | "skillsmem"
  | "activity";

export function App() {
  const [sessions, setSessions] = useState<SessionMeta[]>([]);
  const [archivedSessions, setArchivedSessions] = useState<SessionMeta[]>([]);
  const [listView, setListView] = useState<"active" | "archived">("active");
  const [runs, setRuns] = useState<RunInfo[]>([]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [title, setTitle] = useState("No session");
  const [mode, setMode] = useState("");
  const [sessInfo, setSessInfo] = useState<Session | null>(null);
  const [status, setStatus] = useState<StatusInfo | null>(null);
  const [personas, setPersonas] = useState<PersonaInfo[]>([]);
  const [panel, setPanel] = useState<Panel>("none");
  const [panelBusy, setPanelBusy] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [items, setItems] = useState<TranscriptItem[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [watching, setWatching] = useState(false);
  const [phaseLabel, setPhaseLabel] = useState("Thinking");
  const [elapsed, setElapsed] = useState(0);
  const [authError, setAuthError] = useState<string | null>(null);

  const controllerRef = useRef<AbortController | null>(null);
  const timerRef = useRef<number | null>(null);
  const toastSeq = useRef(0);
  // watchRef tracks the active background-session watcher so switching chats
  // stops it and a second "Watch live" click can't start a duplicate poller.
  const watchRef = useRef<{ stop: boolean; sessionId: string } | null>(null);

  const addToast = (text: string, warn = false) => {
    const id = ++toastSeq.current;
    setToasts((prev) => [...prev, { id, text, warn }]);
    window.setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 12000);
  };

  const loadSessions = async () => {
    try {
      const list = (await (await api("/sessions")).json()) as SessionMeta[];
      setSessions(list || []);
      return list || [];
    } catch (e) {
      console.error(e);
      return null;
    }
  };

  // loadArchived fetches every session (GET /sessions?archived=true returns
  // active + archived together) and keeps only the archived ones.
  const loadArchived = async () => {
    try {
      const list = (await (await api("/sessions?archived=true")).json()) as SessionMeta[];
      const archived = (list || []).filter((s) => s.archived);
      setArchivedSessions(archived);
      return archived;
    } catch (e) {
      console.error(e);
      return null;
    }
  };

  const loadRuns = async () => {
    try {
      setRuns(((await (await api("/runs")).json()) as RunInfo[]) || []);
    } catch (e) {
      console.error(e);
    }
  };

  const loadStatus = async () => {
    try {
      setStatus((await (await api("/status")).json()) as StatusInfo);
    } catch (e) {
      console.error(e);
    }
  };

  const loadPersonas = async () => {
    try {
      setPersonas(((await (await api("/personas")).json()) as PersonaInfo[]) || []);
    } catch (e) {
      console.error(e);
    }
  };

  // refreshSessionInfo re-reads a session's metadata (persona, model, cost)
  // without touching the rendered transcript — safe to call mid/after stream.
  const refreshSessionInfo = async (id: string) => {
    try {
      const sess = (await (await api("/sessions/" + id)).json()) as Session;
      setSessInfo(sess);
      setTitle(sess.title || "(untitled)");
      setMode(sess.mode);
    } catch (e) {
      console.error(e);
    }
  };

  const clearSelection = () => {
    setCurrentId(null);
    setSessInfo(null);
    setTitle("No session");
    setMode("");
    setItems([]);
    setPanel("none");
  };

  const newSession = async () => {
    const meta = (await (
      await api("/sessions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode: "build" }),
      })
    ).json()) as SessionMeta;
    await loadSessions();
    setListView("active");
    openSession(meta.id);
  };

  const openSession = async (id: string) => {
    // Leaving a chat stops any live background watch on the previous one.
    if (watchRef.current && watchRef.current.sessionId !== id) watchRef.current.stop = true;
    setCurrentId(id);
    setPanel("none");
    const sess = (await (await api("/sessions/" + id)).json()) as Session;
    setSessInfo(sess);
    setTitle(sess.title || "(untitled)");
    setMode(sess.mode);
    setItems(itemsFromMessages(sess.messages || [], id));
    loadSessions();
    loadRuns();
  };

  const archiveSession = async (id: string) => {
    try {
      await api(`/sessions/${id}/archive`, { method: "POST" });
      if (currentId === id) clearSelection();
      addToast("Chat archived. Find it under “Archived” in the sidebar.");
      loadSessions();
      loadArchived();
    } catch (e) {
      addToast("Could not archive: " + (e as Error).message, true);
    }
  };

  const unarchiveSession = async (id: string) => {
    try {
      await api(`/sessions/${id}/unarchive`, { method: "POST" });
      addToast("Chat restored to your active list.");
      loadSessions();
      loadArchived();
    } catch (e) {
      addToast("Could not restore: " + (e as Error).message, true);
    }
  };

  // pruneOldSessions permanently deletes non-archived chats not used in the
  // last N days (POST /sessions/prune). Archived chats are kept.
  const pruneOldSessions = async (days: number) => {
    try {
      const r = await api(`/sessions/prune?days=${days}`, { method: "POST" });
      const body = (await r.json()) as PruneResponse;
      addToast(
        body.deleted === 0
          ? `Nothing to tidy up — no chats were unused for ${days} days.`
          : `Deleted ${body.deleted} old chat${body.deleted === 1 ? "" : "s"}.`
      );
      const [active, archived] = await Promise.all([loadSessions(), loadArchived()]);
      // The open chat may have been pruned out from under us.
      if (
        currentId &&
        active &&
        archived &&
        !active.some((s) => s.id === currentId) &&
        !archived.some((s) => s.id === currentId)
      ) {
        clearSelection();
      }
    } catch (e) {
      addToast("Could not tidy up: " + (e as Error).message, true);
    }
  };

  const switchPersona = async (name: string) => {
    if (!currentId) return;
    setPanelBusy(true);
    try {
      await api("/sessions/" + currentId, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ persona: name }),
      });
      await refreshSessionInfo(currentId);
      setPanel("none");
      addToast(`Assistant switched to “${name}”.`);
    } catch (e) {
      addToast("Could not switch: " + (e as Error).message, true);
    } finally {
      setPanelBusy(false);
    }
  };

  const setSessionModel = async (model: string) => {
    if (!currentId) return;
    setPanelBusy(true);
    try {
      await api("/sessions/" + currentId, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ model }),
      });
      await refreshSessionInfo(currentId);
      addToast(model ? `This chat now uses “${model}”.` : "This chat is back on the default model.");
    } catch (e) {
      addToast("Could not change model: " + (e as Error).message, true);
    } finally {
      setPanelBusy(false);
    }
  };

  // setBackgroundMode toggles detached execution (P3.2/P15.9): a background
  // chat's responses keep running on the daemon even if every tab closes.
  const setBackgroundMode = async (on: boolean) => {
    if (!currentId) return;
    setPanelBusy(true);
    try {
      await api(`/sessions/${currentId}/background`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ background: on }),
      });
      await refreshSessionInfo(currentId);
      loadSessions();
      addToast(
        on
          ? "This chat now keeps working even if you close the tab."
          : "This chat now pauses if you close the tab mid-response."
      );
    } catch (e) {
      addToast("Could not change background mode: " + (e as Error).message, true);
    } finally {
      setPanelBusy(false);
    }
  };

  const onRewound = async (r: RewindResponse) => {
    setPanel("none");
    if (currentId) await openSession(currentId);
    loadStatus();
    const files = r.files_restored === 1 ? "1 file" : `${r.files_restored} files`;
    addToast(`Went back: ${files} restored, conversation trimmed.`);
  };

  const startElapsed = () => {
    const start = Date.now();
    setPhaseLabel("Thinking");
    setElapsed(0);
    if (timerRef.current) window.clearInterval(timerRef.current);
    timerRef.current = window.setInterval(() => {
      setElapsed(Math.round((Date.now() - start) / 1000));
    }, 1000);
  };

  const stopElapsed = () => {
    if (timerRef.current) {
      window.clearInterval(timerRef.current);
      timerRef.current = null;
    }
  };

  // makeEventSink renders engine events into the assistant transcript item
  // asstId. Shared by live SSE streaming (send) and the background-session
  // catch-up poller (watchLive), so both paths draw text/thinking/tool/
  // approval blocks identically.
  const makeEventSink = (asstId: string, sessionId: string, onPhase: (label: string) => void) => {
    let streamMode: "idle" | "text" | "thinking" = "idle";
    let thinkStart = 0;

    const updateAsst = (fn: (blocks: RenderBlock[]) => RenderBlock[]) => {
      setItems((prev) => prev.map((it) => (it.id === asstId ? { ...it, blocks: fn(it.blocks) } : it)));
    };

    const finishThinking = () => {
      if (streamMode === "thinking") {
        const secs = Math.max(1, Math.round((Date.now() - thinkStart) / 1000));
        updateAsst((blocks) => {
          const copy = blocks.slice();
          const last = copy[copy.length - 1];
          if (last && last.kind === "thinking") {
            copy[copy.length - 1] = { ...last, open: false, label: `💭 thought for ${secs}s` };
          }
          return copy;
        });
      }
      streamMode = "idle";
    };

    const addText = (s: string) => {
      finishThinking();
      updateAsst((blocks) => {
        const copy = blocks.slice();
        const last = copy[copy.length - 1];
        if (streamMode === "text" && last && last.kind === "text") {
          copy[copy.length - 1] = { ...last, text: last.text + s };
        } else {
          copy.push({ kind: "text", text: s });
        }
        return copy;
      });
      streamMode = "text";
    };

    const addThinking = (s: string) => {
      if (streamMode !== "thinking") thinkStart = Date.now();
      updateAsst((blocks) => {
        const copy = blocks.slice();
        const last = copy[copy.length - 1];
        if (streamMode === "thinking" && last && last.kind === "thinking") {
          copy[copy.length - 1] = { ...last, text: last.text + s };
        } else {
          copy.push({ kind: "thinking", text: s, open: true, label: "💭 thinking…" });
        }
        return copy;
      });
      streamMode = "thinking";
    };

    const handleEvent = (ev: Event) => {
      switch (ev.kind) {
        case "text":
          onPhase("Writing");
          addText(ev.text || "");
          break;
        case "thinking":
          onPhase("Thinking");
          addThinking(ev.text || "");
          break;
        case "tool_call":
          finishThinking();
          streamMode = "idle";
          onPhase("Running " + (ev.tool || "tool"));
          updateAsst((blocks) => [
            ...blocks,
            {
              kind: "tool",
              label: "🔧 " + (ev.tool || "") + " …",
              body: ev.tool_input ? JSON.stringify(ev.tool_input, null, 2) : "",
              open: true,
              err: false,
            },
          ]);
          break;
        case "tool_result":
          updateAsst((blocks) => [
            ...blocks,
            {
              kind: "tool",
              label: (ev.tool_is_error ? "✗ " : "✓ ") + (ev.tool || "tool") + " result",
              body: ev.tool_result || "",
              open: false,
              err: !!ev.tool_is_error,
            },
          ]);
          onPhase("Thinking");
          break;
        case "approval_request":
          finishThinking();
          streamMode = "idle";
          onPhase("Waiting for your approval");
          updateAsst((blocks) => [
            ...blocks,
            {
              kind: "approval",
              reason: ev.approval_reason || `Run ${ev.tool}?`,
              approvalId: ev.approval_id || "",
              sessionId,
              tool: ev.tool,
              toolInput: ev.tool_input,
            },
          ]);
          break;
        case "cost_alert":
          // P15.4: spend crossed a configured alert threshold — surface it,
          // don't silently drop it like turn_done/steer/guard.
          addToast("Spending heads-up: " + (ev.text || "cost threshold crossed"), true);
          break;
        case "error":
          updateAsst((blocks) => [...blocks, { kind: "error", text: "Error: " + (ev.error || "") }]);
          break;
        default:
          break; // turn_done/done/steer/guard/notice: parity no-ops for now
      }
    };

    return { handleEvent, finish: finishThinking };
  };

  const send = async () => {
    const text = input.trim();
    if (!text || streaming || !currentId) return;
    setInput("");
    setStreaming(true);
    startElapsed();

    const sessionId = currentId;
    const userId = "u" + Date.now();
    const asstId = "a" + Date.now();
    setItems((prev) => [
      ...prev,
      { id: userId, role: "user", blocks: [{ kind: "text", text }] },
      { id: asstId, role: "assistant", blocks: [] },
    ]);

    const sink = makeEventSink(asstId, sessionId, setPhaseLabel);

    const controller = new AbortController();
    controllerRef.current = controller;
    try {
      const r = await api(`/sessions/${sessionId}/messages`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text }),
        signal: controller.signal,
      });
      await consumeSSE(r, sink.handleEvent);
    } catch (e) {
      sink.finish();
      const err = e as Error;
      setItems((prev) =>
        prev.map((it) =>
          it.id === asstId
            ? {
                ...it,
                blocks: [
                  ...it.blocks,
                  { kind: "error", text: err.name === "AbortError" ? "Stopped." : "Error: " + err.message },
                ],
              }
            : it
        )
      );
    } finally {
      sink.finish();
      controllerRef.current = null;
      stopElapsed();
      // Refresh the run list before dropping the streaming flag so the
      // "still working in the background" banner doesn't flash after our own
      // run ends (the stale list would briefly still contain it).
      await loadRuns();
      setStreaming(false);
      loadSessions();
      refreshSessionInfo(sessionId); // P15.4: pick up this turn's cost/tokens
      loadStatus();
    }
  };

  // watchLive reattaches to a background (detached) session whose response is
  // still running (P15.9). Buffered events already shown via the persisted
  // transcript are skipped (the poller starts after the newest buffered event)
  // and new ones render live; when the run finishes the canonical transcript
  // is reloaded from the store.
  const watchLive = async (sessionId: string) => {
    if (watchRef.current) return;
    const state = { stop: false, sessionId };
    watchRef.current = state;
    setWatching(true);

    const asstId = "w" + Date.now();
    setItems((prev) => [
      ...prev,
      {
        id: asstId,
        role: "assistant",
        bare: true,
        blocks: [{ kind: "text", text: "(catching up — new activity appears below)" }],
      },
      { id: asstId + "b", role: "assistant", blocks: [] },
    ]);
    const sink = makeEventSink(asstId + "b", sessionId, () => {});

    let since = 0;
    let finished = false;
    try {
      // Seed past the events buffered so far: what they describe is already
      // (or will shortly be) in the persisted transcript we just rendered, so
      // replaying them would duplicate it.
      const seed =
        ((await (await api(`/sessions/${sessionId}/events?since=0`)).json()) as BGEventItem[]) || [];
      if (seed.length) since = seed[seed.length - 1].id;

      while (!state.stop && !finished) {
        await sleep(2000);
        if (state.stop) break;
        let evs: BGEventItem[] = [];
        try {
          evs =
            ((await (await api(`/sessions/${sessionId}/events?since=${since}`)).json()) as BGEventItem[]) ||
            [];
        } catch {
          continue; // transient fetch error — keep polling
        }
        for (const item of evs) {
          since = item.id;
          try {
            const ev = JSON.parse(item.data) as Event;
            if (ev.kind === "done" || ev.kind === "error") finished = true;
            sink.handleEvent(ev);
          } catch {
            // ignore malformed buffered events
          }
        }
        if (!evs.length) {
          // Quiet — double-check the run is actually still in flight.
          try {
            const rs = ((await (await api("/runs")).json()) as RunInfo[]) || [];
            if (!rs.some((r) => r.session_id === sessionId)) finished = true;
          } catch {
            // keep waiting
          }
        }
      }
    } finally {
      sink.finish();
      watchRef.current = null;
      setWatching(false);
      loadRuns();
      if (!state.stop) {
        // Replace the live view with the canonical stored transcript.
        await openSession(sessionId);
        loadStatus();
        addToast("Caught up — this chat's response has finished.");
      }
    }
  };

  useEffect(() => {
    let runsTimer: number | undefined;
    exchangeToken()
      .then(() => {
        loadSessions();
        loadArchived();
        loadStatus();
        loadPersonas();
        loadRuns();
        // Keep the sidebar "working" indicators and the reattach banner
        // honest even when this tab isn't the one driving the run.
        runsTimer = window.setInterval(loadRuns, 5000);
      })
      .catch((e) => setAuthError((e as Error).message || "authentication failed"));
    return () => {
      if (runsTimer) window.clearInterval(runsTimer);
    };
  }, []);

  if (authError) {
    return (
      <section id="main">
        <div id="topbar">
          <span class="title">Aegis</span>
        </div>
        <p style={{ padding: "1rem" }}>Could not authenticate: {authError}. Reload the page to try again.</p>
      </section>
    );
  }

  const sessionTokens = (sessInfo?.input_tokens || 0) + (sessInfo?.output_tokens || 0);
  const costTitle = status
    ? `This chat: ${fmtUSD(sessInfo?.cost_usd || 0)} · ${fmtTokens(sessionTokens)} tokens` +
      `\nToday (all chats): ${fmtUSD(status.daily_cost_usd)} · ${fmtTokens(status.daily_tokens)} tokens` +
      (status.daily_cap_usd ? `\nDaily spending cap: ${fmtUSD(status.daily_cap_usd)}` : "") +
      (status.daily_token_cap ? `\nDaily token cap: ${fmtTokens(status.daily_token_cap)}` : "")
    : "";

  const runningIds = new Set(runs.map((r) => r.session_id));
  // A response is in flight for the open chat but this tab isn't streaming it
  // (started from another tab/the terminal, or left running in the background).
  const detachedRun = !!currentId && !streaming && runningIds.has(currentId);

  const openTool = (tool: SidebarTool) => setPanel(panel === tool ? "none" : tool);

  return (
    <>
      <SessionList
        sessions={sessions}
        archivedSessions={archivedSessions}
        view={listView}
        onViewChange={(v) => {
          setListView(v);
          if (v === "archived") loadArchived();
        }}
        currentId={currentId}
        runningIds={runningIds}
        onSelect={openSession}
        onNew={newSession}
        onArchive={archiveSession}
        onUnarchive={unarchiveSession}
        onPrune={pruneOldSessions}
        onOpenTool={openTool}
        activityCount={runs.length}
      />
      <section id="main">
        <div id="topbar">
          <span class="title" id="title">
            {title}
          </span>
          <span id="status" class={streaming ? "active" : undefined}>
            <span class="spinner" />
            <span id="statusText">
              {streaming ? `${phaseLabel}… ${elapsed}s` : ""}
            </span>
          </span>
          {currentId && (
            <span class="costs" title={costTitle}>
              {fmtUSD(sessInfo?.cost_usd || 0)} · {fmtTokens(sessionTokens)} tok
              {status ? ` · today ${fmtUSD(status.daily_cost_usd)}` : ""}
            </span>
          )}
          {currentId && (
            <button
              class="chip"
              title="Choose the assistant's role and model"
              onClick={() => {
                if (panel !== "assistant") loadPersonas(); // pick up on-disk persona edits
                setPanel(panel === "assistant" ? "none" : "assistant");
              }}
            >
              🎭 {sessInfo?.persona || "general"}
              {sessInfo?.model ? ` · ${sessInfo.model}` : ""}
            </button>
          )}
          {currentId && (
            <button
              class="chip"
              title="Go back to an earlier point in this chat"
              onClick={() => setPanel(panel === "checkpoints" ? "none" : "checkpoints")}
            >
              ⏪ Restore
            </button>
          )}
          <span class="badge" id="mode">
            {mode}
          </span>
        </div>
        <div class="panel-anchor">
          {panel === "assistant" && currentId && (
            <SettingsPanel
              personas={personas}
              currentPersona={sessInfo?.persona || "general"}
              modelOverride={sessInfo?.model || ""}
              defaultModel={status?.model || ""}
              background={!!sessInfo?.background}
              busy={panelBusy}
              onSwitchPersona={switchPersona}
              onSetModel={setSessionModel}
              onSetBackground={setBackgroundMode}
              onClose={() => setPanel("none")}
            />
          )}
          {panel === "checkpoints" && currentId && (
            <Checkpoints
              sessionId={currentId}
              streaming={streaming}
              onRewound={onRewound}
              onClose={() => setPanel("none")}
            />
          )}
          {panel === "debate" && <DebatePanel onClose={() => setPanel("none")} />}
          {panel === "knowledge" && <KnowledgePanel onClose={() => setPanel("none")} />}
          {panel === "security" && <SecurityPanel onClose={() => setPanel("none")} />}
          {panel === "skillsmem" && (
            <SkillsMemoryPanel addToast={addToast} onClose={() => setPanel("none")} />
          )}
          {panel === "activity" && (
            <Activity
              onOpenSession={(id) => {
                setPanel("none");
                setListView("active");
                openSession(id);
              }}
              onClose={() => setPanel("none")}
            />
          )}
        </div>
        {detachedRun && (
          <div class="bg-banner">
            <span class="livedot" />
            <span class="bb-text">
              The assistant is still working on this chat
              {sessInfo?.background ? " in the background" : " (started elsewhere)"}.
            </span>
            {sessInfo?.background ? (
              <button disabled={watching} onClick={() => currentId && watchLive(currentId)}>
                {watching ? "Watching…" : "Watch live"}
              </button>
            ) : (
              <button class="secondary" onClick={() => currentId && openSession(currentId)}>
                Refresh
              </button>
            )}
          </div>
        )}
        <Transcript items={items} />
        <Composer
          value={input}
          onChange={setInput}
          disabled={!currentId || watching || detachedRun}
          streaming={streaming}
          onSend={send}
          onStop={() => controllerRef.current?.abort()}
        />
      </section>
      <div id="toasts">
        {toasts.map((t) => (
          <div class={"toast" + (t.warn ? " warn" : "")} key={t.id}>
            <span>{t.text}</span>
            <button
              class="linkish"
              onClick={() => setToasts((prev) => prev.filter((x) => x.id !== t.id))}
            >
              ✕
            </button>
          </div>
        ))}
      </div>
    </>
  );
}
