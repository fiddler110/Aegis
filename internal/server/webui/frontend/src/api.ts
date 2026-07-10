import type { Event } from "./types";

// authToken holds the real daemon auth token, set once by exchangeToken().
// It never comes from the page itself: the HTML shell only carries a
// short-lived, single-use page token (data-page-token), traded in for this
// one via POST /auth/exchange so a leaked page source can't be replayed as
// a standing credential (P15.12).
let authToken = "";

function pageToken(): string {
  return document.getElementById("root")?.dataset.pageToken ?? "";
}

// exchangeToken redeems the page token for the real auth token. Must
// resolve before any other api() call — the app's mount effect awaits it
// before doing anything else.
export async function exchangeToken(): Promise<void> {
  const r = await fetch("/auth/exchange", {
    method: "POST",
    headers: { Authorization: "Bearer " + pageToken() },
  });
  if (!r.ok) throw new Error((await r.text()) || String(r.status));
  const body = (await r.json()) as { token: string };
  authToken = body.token;
}

export async function api(path: string, opts: RequestInit = {}): Promise<Response> {
  const r = await fetch(path, {
    ...opts,
    headers: {
      Authorization: "Bearer " + authToken,
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
