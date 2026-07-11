import { useEffect, useRef } from "preact/hooks";
import { Approval } from "./Approval";

export type RenderBlock =
  | { kind: "text"; text: string }
  | { kind: "thinking"; text: string; open: boolean; label: string }
  | { kind: "tool"; label: string; body: string; open: boolean; err: boolean }
  | { kind: "image"; src: string }
  | { kind: "error"; text: string }
  | {
      kind: "approval";
      reason: string;
      approvalId: string;
      sessionId: string;
      tool?: string;
      toolInput?: unknown;
    };

export interface TranscriptItem {
  id: string;
  role: "user" | "assistant";
  blocks: RenderBlock[];
  // bare suppresses the role header — used for standalone tool-result boxes
  // (tool results live in "user"-role API messages but render as unlabeled
  // assistant-styled boxes, matching the tool_result's originating turn).
  bare?: boolean;
}

function renderBlock(b: RenderBlock, key: number) {
  switch (b.kind) {
    case "text":
      return <p key={key}>{b.text}</p>;
    case "thinking":
      return (
        <details class="think" key={key} open={b.open}>
          <summary>{b.label}</summary>
          <p>{b.text}</p>
        </details>
      );
    case "tool":
      return (
        <details class="tool" key={key} open={b.open}>
          <summary class={b.err ? "err" : undefined}>{b.label}</summary>
          <pre>{b.body}</pre>
        </details>
      );
    case "image":
      return <img key={key} src={b.src} />;
    case "error":
      return (
        <p class="err" key={key}>
          {b.text}
        </p>
      );
    case "approval":
      return (
        <Approval
          key={key}
          sessionId={b.sessionId}
          approvalId={b.approvalId}
          reason={b.reason}
          tool={b.tool}
          toolInput={b.toolInput}
        />
      );
  }
}

export function Transcript({ items }: { items: TranscriptItem[] }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [items]);

  return (
    <div id="transcript" ref={ref}>
      {items.length === 0 && <div class="empty">Select or create a session to begin.</div>}
      {items.map((it) => (
        <div class={"msg " + it.role} key={it.id}>
          {!it.bare && (
            <div class="role">{it.role === "user" ? "🧑 User" : "🤖 Assistant"}</div>
          )}
          <div class="body">{it.blocks.map((b, i) => renderBlock(b, i))}</div>
        </div>
      ))}
    </div>
  );
}
