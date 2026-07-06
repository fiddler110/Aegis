import { useEffect, useRef, useState } from "preact/hooks";
import { api, consumeSSE } from "./api";
import type { Event, Message, SessionMeta } from "./types";
import { SessionList } from "./components/SessionList";
import { Transcript, type RenderBlock, type TranscriptItem } from "./components/Transcript";
import { Composer } from "./components/Composer";

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

export function App() {
  const [sessions, setSessions] = useState<SessionMeta[]>([]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [title, setTitle] = useState("No session");
  const [mode, setMode] = useState("");
  const [items, setItems] = useState<TranscriptItem[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [phaseLabel, setPhaseLabel] = useState("Thinking");
  const [elapsed, setElapsed] = useState(0);

  const controllerRef = useRef<AbortController | null>(null);
  const timerRef = useRef<number | null>(null);

  const loadSessions = async () => {
    try {
      const list = (await (await api("/sessions")).json()) as SessionMeta[];
      setSessions(list || []);
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
    const sess = (await (await api("/sessions/" + id)).json()) as {
      title?: string;
      mode: string;
      messages?: Message[];
    };
    setTitle(sess.title || "(untitled)");
    setMode(sess.mode);
    setItems(itemsFromMessages(sess.messages || [], id));
    loadSessions();
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
            },
          ]);
          break;
        case "error":
          updateAsst((blocks) => [...blocks, { kind: "error", text: "Error: " + (ev.error || "") }]);
          break;
        default:
          break; // turn_done/done/steer/guard/cost_alert: parity no-ops for now
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
    }
  };

  useEffect(() => {
    loadSessions();
  }, []);

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
          <span class="badge" id="mode">
            {mode}
          </span>
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
    </>
  );
}
