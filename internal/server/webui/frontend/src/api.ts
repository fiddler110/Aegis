import type { Event } from "./types";

// sessionCSRF is the nonce POST /auth/exchange returns for the browser
// session it establishes (P81.4). The session id itself never reaches this
// script: it lives only in the HttpOnly aegis_session cookie the browser
// attaches automatically, so a script-execution bug here can drive API calls
// but can't exfiltrate a replayable credential. This value must accompany
// every call as X-Aegis-Session-CSRF, proving the request came from this
// same-origin page (see browserSessionCSRFHeaderName's doc comment in
// internal/server/auth.go). The page itself only ever carries the short-lived,
// single-use page token (data-page-token) traded in for this via
// POST /auth/exchange, so a leaked page source can't be replayed as a
// standing credential (P15.12).
let sessionCSRF = "";

function pageToken(): string {
  return document.getElementById("root")?.dataset.pageToken ?? "";
}

// csrfToken reads the nonce GET /ui baked into the page (also set, HttpOnly,
// as the aegis_ui_csrf cookie). Sending it back as an explicit header proves
// this request came from JS running on this same-origin page — a hostile
// cross-origin page can't read either the HttpOnly cookie or this page's own
// DOM/response body, so it can't reproduce the pair (FIND-01/P24.1; see
// uiCSRFCookieName's doc comment in internal/server/auth.go).
function csrfToken(): string {
  return document.getElementById("root")?.dataset.csrfToken ?? "";
}

// exchangeToken redeems the page token for the real auth token. Must
// resolve before any other api() call — the app's mount effect awaits it
// before doing anything else.
export async function exchangeToken(): Promise<void> {
  const r = await fetch("/auth/exchange", {
    method: "POST",
    headers: {
      Authorization: "Bearer " + pageToken(),
      "X-Aegis-CSRF": csrfToken(),
    },
  });
  if (!r.ok) throw new Error((await r.text()) || String(r.status));
  const body = (await r.json()) as { csrf: string };
  sessionCSRF = body.csrf;
}

export async function api(path: string, opts: RequestInit = {}): Promise<Response> {
  const r = await fetch(path, {
    ...opts,
    // credentials default to "same-origin" for a same-origin URL like these,
    // which is what carries the HttpOnly aegis_session cookie automatically;
    // no explicit `credentials` option is needed.
    headers: {
      "X-Aegis-Session-CSRF": sessionCSRF,
      ...(opts.headers as Record<string, string> | undefined),
    },
  });
  if (!r.ok) {
    const text = await r.text();
    let msg = text || String(r.status);
    // Every JSON error body the daemon writes (writeError) is {"error": "..."} —
    // unwrap it so e.g. a rejected session workdir (P15.13) shows the actual
    // validation message in a toast instead of the raw JSON blob.
    try {
      const body = JSON.parse(text) as { error?: string };
      if (body && typeof body.error === "string" && body.error) msg = body.error;
    } catch {
      // not JSON — keep the raw text
    }
    throw new Error(msg);
  }
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
