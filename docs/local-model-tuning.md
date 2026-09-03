# Tuning a local model for Aegis

Ollama's stock model definitions are tuned for **chat**. Aegis drives a model through a long
multi-turn **agent loop**, which exercises three things a chat setup never does:

1. whether the chat template survives an assistant turn that *both* narrates and calls a tool,
2. whether the sampling parameters keep tool arguments faithful over dozens of turns,
3. whether the served context window is large enough to hold the task.

All three are properties of the **model** as registered in Ollama, not of Aegis's config, and all
three are fixed the same way: a small `Modelfile` that derives a tuned variant from the base model.
This page is the procedure, and the measurements behind each recommendation.

> The naming convention used throughout: `aegis-<base>:<window>`, e.g. `aegis-qwen3-14b:32k`. Keeping
> the tuned variant under its own name means the stock model stays available for comparison, which is
> what makes a regression visible.

---

## The procedure

```bash
# 1. Capture what the base model currently does
ollama show <base-model>                      # params, context length, capabilities
ollama show --modelfile <base-model>          # the template as Ollama will render it

# 2. Write a Modelfile (see the sections below for each PARAMETER and the TEMPLATE)
# 3. Build the tuned variant
ollama create aegis-<base>:<window> -f Modelfile

# 4. Verify (see "Verifying the result")
aegis doctor
```

---

## 1. Pin the context window

**Always pin `num_ctx` in the Modelfile.** `OLLAMA_CONTEXT_LENGTH` is a server-wide environment
variable that Aegis **cannot see before a model loads**, so the daemon plans against Ollama's 4096
default, decides the task will not fit, and compacts (or skips) accordingly. A modelfile-pinned
`num_ctx` is readable from `/api/show` at any time, which is why it is second in the daemon's
detection order — see [Context Window](providers.md#context-window).

```
PARAMETER num_ctx 32768
```

For agent workloads 16k–32k is a realistic minimum; at 4096 a handful of file reads fills the window.
Size it against VRAM: the KV cache at 32k is a substantial fraction of the model's own footprint, and
a model that spills to system RAM decodes at a fraction of the speed.

Do not set `num_ctx` beyond the model's **training** context (`ollama show` reports it as "context
length"). Running near or past that limit degrades long-context recall precisely when the agent loop
needs it most — every turn of a compacted conversation sits at 85–96% of the window by design.

---

## 2. Fix the chat template if it drops tool calls

**This is the highest-value change on this page, and it is invisible until you look for it.**

Ollama renders conversation history server-side from the model's own chat template. Several stock
templates — including Qwen3's — are **legacy Go `text/template`** and write the assistant turn as:

```
{{ if .Content }}{{ .Content }}{{ else if .ToolCalls }}<tool_call>…{{ end }}
```

Content and tool calls are **mutually exclusive branches**. A thinking model narrates before it calls
a tool most turns, so on those turns the **tool call is silently deleted from the rendered history**.
The model then sees a `<tool_response>` for a call it has no record of making, and the arguments —
the path it read, the edit it made, the command it ran — are gone.

### Detecting it

Aegis detects this automatically and warns:

```
WARN ollama: model's chat template drops tool calls from an assistant turn that also has text;
     withholding that text so the call survives in history  model=qwen3:14b-32k
```

To check by hand, look for `else if` reaching `.ToolCalls`:

```bash
ollama show --modelfile <model> | grep -n 'else if.*ToolCalls'
```

A **Jinja** template (one containing `{%` … `%}`, Ollama's newer renderer) is not affected — it
renders prose and the call in sequence.

### Fixing it

Split the `else if` into two independent `if`s, so both render:

```
{{ if .Content }}{{ .Content }}
{{- end }}
{{- if .ToolCalls }}<tool_call>
{{ range .ToolCalls }}{"name": "{{ .Function.Name }}", "arguments": {{ .Function.Arguments }}}
{{ end }}</tool_call>
{{- end }}
```

Extract the full template, patch that one branch, and rebuild. The template is long and whitespace-
sensitive, so edit it programmatically rather than retyping it:

```bash
# Extract the template exactly as Ollama holds it
curl -s http://localhost:11434/api/show -d '{"model":"qwen3:14b"}' \
  | python -c "import json,sys,io; io.open('tmpl.txt','w',encoding='utf-8',newline='').write(json.load(sys.stdin)['template'])"

# Patch the assistant branch, then wrap it in a Modelfile
python - <<'PY'
import io
t = io.open('tmpl.txt', encoding='utf-8', newline='').read()
old = '{{ if .Content }}{{ .Content }}\n{{- else if .ToolCalls }}<tool_call>\n'
new = '{{ if .Content }}{{ .Content }}\n{{- end }}\n{{- if .ToolCalls }}<tool_call>\n'
assert old in t, "anchor not found — inspect tmpl.txt and adjust"
io.open('Modelfile', 'w', encoding='utf-8', newline='\n').write(
    'FROM qwen3:14b\n\nPARAMETER num_ctx 32768\n\nTEMPLATE """' + t.replace(old, new, 1) + '"""\n')
PY

ollama create aegis-qwen3-14b:32k -f Modelfile
```

**Aegis mitigates this even if you do nothing**, by withholding the prose so the call survives — but
that discards the model's narration, which the template fix keeps. Fixing the template is strictly
better; the mitigation exists for models you did not build.

**What does *not* work:** sending the prose as its own assistant message followed by a second
message carrying the call. Ollama coalesces adjacent same-role messages before templating, so the
pair arrives at the template as the same content-plus-calls message and is dropped identically. This
was measured, not assumed.

---

## 3. Set sampling for tool use, not for chat

Stock parameters are tuned for conversational variety. An agent loop wants argument fidelity: a file
path, a JSON edit, a shell command are all cases where "creative" is "wrong".

> These values are **reasoned defaults, not measured ones** — see
> [What is not measured here](#what-is-not-measured-here). Unlike the template fix, they rest on
> judgement rather than an experiment.

```
PARAMETER temperature 0.2
PARAMETER top_p 0.8
PARAMETER top_k 20
PARAMETER repeat_penalty 1
```

- **`temperature`** is the one that matters most. Qwen3's stock 0.6 is a chat default; 0.2 is a
  reasonable agent default. Do not use 0 — some models degenerate into repetition loops, which the
  engine's loop detector will (correctly) abort.
- **`top_p` / `top_k`** tighten the tail. 0.8/20 pairs with the lower temperature.
- **`repeat_penalty 1`** (i.e. off) is deliberate. A penalty above 1 punishes legitimately repeated
  tokens, and agent traffic is full of them — repeated file paths, repeated JSON keys, repeated
  indentation. Penalising those corrupts tool arguments.
- **`num_predict -1`** lets Aegis's own `max_tokens` bound the completion instead of a second,
  invisible cap. Aegis already sizes the completion against the window (see
  [Context Window](providers.md#context-window)).

---

## 4. Keep the model resident

Not a Modelfile setting, but it belongs to the same tuning pass — see
[Keep-alive and pre-warm](providers.md#keep-alive-and-pre-warm). The native adapter's default
(`30m`) is usually right; the thing to avoid is Ollama's own 5-minute default, under which a
multi-turn run reloads the model *and* reprocesses the whole conversation between turns.

---

## 5. Know the risk if you turn on the tool-call shim

`provider.tool_call_shim` (`internal/toolshim`) and the always-on prose-salvage decorator
(`provider.WithProseToolCallSalvage`, `internal/provider/prosetoolcall.go`) both exist to recover a
tool call a local model wrote as free-form text instead of a structured `tool_calls` entry — the
common failure mode this whole document is tuning against. Neither can yet distinguish a call the
model *intended* from a call it merely *quoted* — a web page, a file, or a tool result the model read
and echoed back verbatim in a summary or explanation, if that text happens to look like a tool call.

This is why the shim is opt-in (`tool_call_shim: off` by default) and why a call either mechanism
recovers is now labeled **"recovered from prose"** in the approval prompt: the label is provenance,
not containment. Read it as "this call did not arrive as a native structured call — check that the
model actually meant to make it" before approving a write, execute, or network call carrying that
label, especially right after a turn that read content from outside the workspace (a fetched URL, an
MCP tool result, a file the model didn't author). The real containment — never promoting a call parsed
out of a span of output that reproduces untrusted content — needs turn-scoped taint tracking that does
not exist yet ([P81.28](../research/roadmap.md), sequenced behind
[P81.1](../research/roadmap.md)'s tracking mechanism). Until then, the label is what you have.

---

## Verifying the result

Three checks, cheapest first.

**1. `aegis doctor`** reports a tool-calling conformance rate over several trials
(`provider.tool_call_probe_trials`, default 5). This catches "the model cannot speak the protocol at
all" — see [Tool-calling reliability](providers.md#tool-calling-reliability-for-local-models). It
does **not** catch the template defect, because that only appears in multi-turn history.

**2. A history-fidelity check.** This is the one that catches a dropped tool call. Send a history
where the assistant narrated *and* called a tool, then ask the model what it did:

```bash
python - <<'PY'
import json, urllib.request
TOOLS = [{"type":"function","function":{"name":"read_file","description":"Read a file.",
  "parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]
HIST = [
  {"role":"user","content":"What is the token in config.txt? Read it, then tell me the token and the exact path."},
  {"role":"assistant","content":"I'll read the configuration file to find the token.",
   "tool_calls":[{"function":{"name":"read_file","arguments":{"path":"srv/etc/config.txt"}}}]},
  {"role":"tool","name":"read_file","content":"token = ZX-4417-QQ"},
  {"role":"user","content":"Which file path did you read, and what was the token?"},
]
body = {"model":"aegis-qwen3-14b:32k","messages":HIST,"tools":TOOLS,"stream":False,
        "options":{"temperature":0}}
r = urllib.request.urlopen(urllib.request.Request("http://localhost:11434/api/chat",
      data=json.dumps(body).encode(), headers={"Content-Type":"application/json"}), timeout=600)
out = json.loads(r.read())["message"]["content"]
print(out)
print("PASS" if "srv/etc/config.txt" in out else "FAIL — the tool call is not reaching the model")
PY
```

A model that answers `/etc/config.txt`, or invents a path, is not seeing its own tool call. Run it
three times; this failure is highly reproducible rather than occasional.

**3. The live workflow tier**, which is the end-to-end check:

```bash
AEGIS_EVAL_MODEL=aegis-qwen3-14b:32k \
  go test -tags live_workflow -count=1 -timeout 25m ./internal/eval/ -run TestLiveWorkflow -v
```

`-count=1` is mandatory — Go's test cache cannot see that the model server changed, so a cached pass
looks exactly like a reproduced one. Raise `-timeout` above the 10-minute default; the tier's
subtests together exceed it on a local model.

Two subtests answer different questions, and it is worth reading them differently:

- **`FixSeededBug`** is the **control**: a three-tool-call run/fix/verify loop. A model that fails it
  cannot drive an agent loop at all. It is pass/fail and says nothing finer.
- **`SecurityTriage`** is the **discriminator**: a graded audit-and-fix task scored out of 12, which
  is what to use when comparing two models or two Modelfile settings. Run it alone with
  `-run 'TestLiveWorkflow$/SecurityTriage'`.

The score table is what to read, not the total:

```
score 11/12
    found_hardcoded_credential   1/1
    found_unsafe_deserialization 1/1
    precision                    1/2  1 finding(s) name a file that does not have that problem: ...
    fixed_sql_injection          1/1
    no_regression                1/1
```

Two models can reach the same total for opposite reasons — one that audits well and cannot edit, one
that edits well and never reports — and the per-criterion rows are the only place that distinction
survives. **Three runs is enough to compare two models** (measured: complete separation at n=3), which
is the practical reason to prefer this subtest over the control for tuning decisions.

---

## Worked example

A complete `Modelfile` for a tuned Qwen3-14B, combining every section above (the `TEMPLATE` is
elided — generate it with the script in section 2 rather than transcribing it):

```
FROM qwen3:14b

PARAMETER num_ctx 32768
PARAMETER temperature 0.2
PARAMETER top_p 0.8
PARAMETER top_k 20
PARAMETER repeat_penalty 1
PARAMETER num_predict -1

TEMPLATE """<the stock template, with the assistant branch's `else if .ToolCalls` split in two>"""
```

```bash
ollama create aegis-qwen3-14b:32k -f Modelfile
```

Then point Aegis at it:

```yaml
provider:
  default: ollama
  model: aegis-qwen3-14b:32k
  keep_alive: "30m"
```

---

## Measured results

Two measurements, taken 2026-08-17 on `qwen3:14b-32k` (Q4_K_M, `num_ctx 32768`, RX 7900 GRE 16GB).
They differ in strength, and it is worth being clear which is which.

### The template defect — deterministic, and the reason to act

The history-fidelity probe from ["Verifying the result"](#verifying-the-result), three trials per arm
at `temperature 0`:

| arm | named the path it actually read |
|---|---|
| stock `qwen3:14b-32k` (prose + call sent together) | **0/3** — answered `/etc/config.txt` |
| stock, prose withheld (Aegis's mitigation) | **3/3** |
| template-corrected, prose kept | **3/3** |
| `aegis-qwen35-9b:32k` (ships a Jinja template) | **3/3** |

This is a mechanism-level result: reproducible, deterministic, and it isolates the cause by flipping
one variable. **This — not the end-to-end numbers below — is the evidence the template fix rests on.**

### End-to-end — directionally consistent, but underpowered

`TestLiveWorkflow/FixSeededBug`, n=6 per arm, same fixture, same day:

| arm | passed | tool calls per run (median) |
|---|---|---|
| unmitigated (pre-fix Aegis, stock template) | **0/6** | 1, 1, 4, 1, 1, 1 (**1**) |
| Aegis's mitigation (prose withheld) | **1/6** | 2, 1, 3, 1, 2, 2 (**2**) |
| template-corrected model | **2/6** | 9, 3, 39, 1, 1, 4 (**3.5**) |

**0/6 versus 2/6 is not a significant difference** (Fisher's exact, p ≈ 0.45), and no claim of one is
made here. What the table does show is a change in *failure shape*: the unmitigated arm gave up after
a single `shell` call in five of six runs, having run the script, seen the traceback, and stopped.
Both corrected arms engage further, and both produced passes where the unmitigated arm produced none
across 6 runs here and 2 more on 2026-08-16 (**0/8** in total).

The seeded-bug task is known to be too weak to settle differences of this size — that is
[P62.9](../research/roadmap.md)'s own standing conclusion, and this run reinforces rather than
overturns it. Treat the end-to-end table as consistent with the probe, not as independent
confirmation of it. **It has since been replaced as the tier's discriminating task** (P68.3); the
graded results below are the ones to use for comparisons.

### The two models on the graded task

`TestLiveWorkflow/SecurityTriage`, n=3 per model, same day:

| model | scores | mean |
|---|---|---|
| `aegis-qwen35-9b:32k` | 9, 11, 12 | **10.7 / 12** |
| `qwen3:14b-32k` (with Aegis's template mitigation) | 3, 2, 3 | **2.7 / 12** |

Complete separation at n=3. The 14b's failure is specific and repeatable: it **never wrote
`findings.json` in any of the three runs**, despite greping the codebase extensively — it does the
searching and never produces the artifact. That is a reporting-step failure rather than a
tool-reachability one, and it is the sort of thing a pass/fail task reports as the same red as
"gave up immediately".

### What is *not* measured here

The sampling parameters in [section 3](#3-set-sampling-for-tool-use-not-for-chat) are **reasoned
defaults, not experimental results.** They are drawn from what `aegis-qwen35-9b:32k` — the
best-performing local model on this tier — already ships with, plus the general principle that agent
traffic wants fidelity over variety. No A/B has been run isolating `temperature`, `top_p` or
`repeat_penalty` on this workload, and the seeded-bug task above is demonstrably too weak to run one.
They are a sensible starting point; they are not a finding.

**Two A/Bs were attempted on 2026-08-17 and both were void**, which is worth stating so this section
is not later read as "tested and found not to matter". Temperature 0.2 against 0.6, n=3–5 per arm,
single-variable Modelfiles verified to differ only in that parameter: the 9b scored 12/12 on every
run of both arms (rubric exhausted) and the corrected 14b scored 3/12 on every run of both arms
(pinned low). A saturated instrument returns a flat result whether or not the variable matters, so
neither outcome is evidence either way. Widening the rubric's measuring band is
[P68.4](../research/roadmap.md).

---

## What to check when adopting a new model

A short checklist, in the order the failures actually bite:

| Check | How | Why it matters |
|---|---|---|
| Template drops tool calls | `ollama show --modelfile <m> \| grep 'else if.*ToolCalls'`, or the history probe above | Silently corrupts multi-turn history; invisible to single-call probes |
| Context window pinned | `ollama show <m>` reports `num_ctx` | Unpinned, Aegis plans against 4096 |
| `num_ctx` ≤ training context | `ollama show <m>` "context length" | Long-context recall degrades exactly where the agent loop lives |
| Tool-calling conformance | `aegis doctor` | A manifest claiming `tools` is not evidence the weights deliver |
| Sampling tuned for tools | `ollama show <m>` Parameters | Chat defaults trade argument fidelity for variety |
| Fits in VRAM at that window | `ollama ps` reports 100% GPU | Spilling to system RAM costs more than the larger model buys |

See also: [Providers & Models](providers.md), [Configuration Reference](configuration.md).
