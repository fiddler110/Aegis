#!/usr/bin/env python3
"""Drive an Aegis daemon session over its HTTP API and summarize the run.

Usage: drive.py <base_url> <token_file> <model-or-"default"> <guard:on|off> <task text>
Prints a timeline of SSE events with timestamps, then a summary line.
"""
import json, sys, time, urllib.request

base, token_file, model, guard, task = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]
token = open(token_file).read().strip()

def req(method, path, body=None, stream=False):
    r = urllib.request.Request(base + path, method=method,
        data=json.dumps(body).encode() if body is not None else None,
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
    return urllib.request.urlopen(r, timeout=1200)

sess = json.load(req("POST", "/sessions", {"title": "eval-run", "mode": "build"}))
sid = sess["id"]
if model != "default":
    req("PATCH", f"/sessions/{sid}", {"model": model}).read()

body = {"text": task}
if guard == "off":
    body["guard_enabled"] = False

t0 = time.time()
resp = req("POST", f"/sessions/{sid}/messages", body, stream=True)

first_ev = first_text = None
tool_calls = []; tool_errors = 0; text_len = 0; final_tokens = (0, 0)
last_text_chunks = []
for raw in resp:
    line = raw.decode(errors="replace").strip()
    if not line.startswith("data:"):
        continue
    try:
        ev = json.loads(line[5:])
    except json.JSONDecodeError:
        continue
    t = time.time() - t0
    kind = ev.get("kind", "")
    if first_ev is None:
        first_ev = t
    if kind == "text" and ev.get("text"):
        if first_text is None:
            first_text = t
            print(f"[{t:7.1f}s] first text delta")
        text_len += len(ev["text"])
        last_text_chunks.append(ev["text"])
        last_text_chunks = last_text_chunks[-400:]
    elif kind == "tool_call":
        inp = (ev.get("tool_input") or "")
        s = json.dumps(inp) if not isinstance(inp, str) else inp
        tool_calls.append(ev.get("tool", "?"))
        print(f"[{t:7.1f}s] tool_call {ev.get('tool')} {s[:160]}")
    elif kind == "tool_result":
        ok = "ERR" if ev.get("tool_is_error") else "ok"
        if ev.get("tool_is_error"):
            tool_errors += 1
            print(f"[{t:7.1f}s] tool_result ERR: {(ev.get('tool_result') or '')[:200]}")
        else:
            print(f"[{t:7.1f}s] tool_result ok ({len(ev.get('tool_result') or '')} chars)")
    elif kind in ("approval_request",):
        print(f"[{t:7.1f}s] APPROVAL_REQUEST {ev.get('tool')} — auto-approve should have handled this!")
    elif kind == "error":
        print(f"[{t:7.1f}s] ERROR {ev.get('error')}")
    elif kind == "done":
        final_tokens = (ev.get("input_tokens", 0), ev.get("output_tokens", 0))
        print(f"[{t:7.1f}s] done in={final_tokens[0]} out={final_tokens[1]}")
    elif kind not in ("text", "thinking"):
        print(f"[{t:7.1f}s] {kind} {ev.get('tool','')} {(ev.get('text') or '')[:80]}")

total = time.time() - t0
print("---FINAL ANSWER (tail)---")
print("".join(last_text_chunks)[-700:])
print("---SUMMARY---")
print(json.dumps({"model": model, "guard": guard, "total_s": round(total, 1),
    "first_text_s": round(first_text, 1) if first_text else None,
    "tool_calls": tool_calls, "tool_errors": tool_errors, "text_chars": text_len,
    "in_tokens": final_tokens[0], "out_tokens": final_tokens[1], "session": sid}))
