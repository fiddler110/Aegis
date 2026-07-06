import type { Event } from "./types";

export function authToken(): string {
  return document.getElementById("root")?.dataset.token ?? "";
}

export async function api(path: string, opts: RequestInit = {}): Promise<Response> {
  const r = await fetch(path, {
    ...opts,
    headers: {
      Authorization: "Bearer " + authToken(),
      ...(opts.headers as Record<string, string> | undefined),
    },
  });
  if (!r.ok) throw new Error((await r.text()) || String(r.status));
  return r;
}

// consumeSSE reads a text/event-stream POST response and calls cb per JSON event.
export async function consumeSSE(resp: Response, cb: (ev: Event) => void): Promise<void> {
  const reader = resp.body!.getReader();
  const dec = new TextDecoder();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let idx: number;
    while ((idx = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, idx);
      buf = buf.slice(idx + 1);
      const m = line.match(/^data:\s?(.*)$/);
      if (!m) continue;
      try {
        cb(JSON.parse(m[1]));
      } catch {
        // ignore malformed lines
      }
    }
  }
}
