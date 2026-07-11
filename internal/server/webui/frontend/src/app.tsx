import { useEffect, useRef, useState } from "preact/hooks";
import { api, consumeSSE, exchangeToken } from "./api";
import type { Event, Message, PersonaInfo, RewindResponse, Session, SessionMeta, StatusInfo } from "./types";
import { SessionList } from "./components/SessionList";
import { Transcript, type RenderBlock, type TranscriptItem } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { SettingsPanel } from "./components/SettingsPanel";
import { Checkpoints } from "./components/Checkpoints";

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

interface Toast {
  id: number;
  text: string;
  warn: boolean;
}

export function App() {
  const [sessions, setSessions] = useState<SessionMeta[]>([]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [title, setTitle] = useState("No session");
  const [mode, setMode] = useState("");
  const [sessInfo, setSessInfo] = useState<Session | null>(null);
  const [status, setStatus] = useState<StatusInfo | null>(null);
  const [personas, setPersonas] = useState<PersonaInfo[]>([]);
  const [panel, setPanel] = useState<"none" | "assistant" | "checkpoints">("none");
  const [panelBusy, setPanelBusy] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [items, setItems] = useState<TranscriptItem[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [phaseLabel, setPhaseLabel] = useState("Thinking");
  const [elapsed, setElapsed] = useState(0);
  const [authError, setAuthError] = useState<string | null>(null);

  const controllerRef = useRef<AbortController | null>(null);
  const timerRef = useRef<number | null>(null);
  const toastSeq = useRef(0);

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

  const newSession = async () => {
    const meta = (await (
      await api("/sessions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode: "build" }),
      })
    ).json()) as SessionMeta;
    await loadSessions();
    openSession(meta.id);
  };

  const openSession = async (id: string) => {
    setCurrentId(id);
    setPanel("none");
    const sess = (await (await api("/sessions/" + id)).json()) as Session;
    setSessInfo(sess);
    setTitle(sess.title || "(untitled)");
    setMode(sess.mode);
    setItems(itemsFromMessages(sess.messages || [], id));
    loadSessions();
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
          setPhaseLabel("Writing");
          addText(ev.text || "");
          break;
        case "thinking":
          setPhaseLabel("Thinking");
          addThinking(ev.text || "");
          break;
        case "tool_call":
          finishThinking();
          streamMode = "idle";
          setPhaseLabel("Running " + (ev.tool || "tool"));
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
          setPhaseLabel("Thinking");
          break;
        case "approval_request":
          finishThinking();
          streamMode = "idle";
          setPhaseLabel("Waiting for your approval");
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

    const controller = new AbortController();
    controllerRef.current = controller;
    try {
      const r = await api(`/sessions/${sessionId}/messages`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text }),
        signal: controller.signal,
      });
      await consumeSSE(r, handleEvent);
    } catch (e) {
      finishThinking();
      const err = e as Error;
      updateAsst((blocks) => [
        ...blocks,
        { kind: "error", text: err.name === "AbortError" ? "Stopped." : "Error: " + err.message },
      ]);
    } finally {
      finishThinking();
      setStreaming(false);
      controllerRef.current = null;
      stopElapsed();
      loadSessions();
      refreshSessionInfo(sessionId); // P15.4: pick up this turn's cost/tokens
      loadStatus();
    }
  };

  useEffect(() => {
    exchangeToken()
      .then(() => {
        loadSessions();
        loadStatus();
        loadPersonas();
      })
      .catch((e) => setAuthError((e as Error).message || "authentication failed"));
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

  return (
    <>
      <SessionList sessions={sessions} currentId={currentId} onSelect={openSession} onNew={newSession} />
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
              busy={panelBusy}
              onSwitchPersona={switchPersona}
              onSetModel={setSessionModel}
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
        </div>
        <Transcript items={items} />
        <Composer
          value={input}
          onChange={setInput}
          disabled={!currentId}
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
