#!/usr/bin/env python3
"""
vram_topology_probe.py — decide, by measurement, whether a debate topology fits.

Companion to research/debate-topology-plan.md. That document's VRAM tables are
*derived* from an assumed Qwen3-family attention shape. This script replaces
every derived number in them with one read off the running Ollama server, then
checks the prediction against what actually lands in VRAM.

It answers four questions, cheapest first:

  1. What is this model's real KV-cache cost per token?
       Read n_layers / n_kv_heads / head_dim from /api/show's model_info and
       apply  2 * layers * kv_heads * head_dim * bytes_per_element.

  2. Does the topology fit on paper?
       Sum weights + KV + compute graph across every seat, against a usable-VRAM
       budget that is deliberately *below* the card's nameplate.

  3. Does it fit in practice?
       Load every model, push a real prompt through each so the compute graph is
       actually allocated, and assert /api/ps reports 100% GPU for all of them
       at once. This is the check that catches flash-attention/KV-quant settings
       that silently did not take effect.

  4. (--topology 2) What does a swap actually cost?
       Evict with keep_alive=0, reload, and time it. On a box with less system
       RAM than the model's own size there is no page cache to help, and this
       number is the difference between "swap once per debate" being fine and
       being unusable.

It does NOT run debates. Aegis's internal/debate package already owns the round
structure, evidence-grounding check, budget bound and verdict parsing; this is
the instrument that tells you which models you can afford to put in its seats.

Usage:
    python vram_topology_probe.py --topology 1
    python vram_topology_probe.py --topology 1 --stress
    python vram_topology_probe.py --topology 2 --stress
    python vram_topology_probe.py --seat proposer=aegis-qwen35-9b:16k \
                                  --seat arbiter=aegis-phi4-mini:8k --stress

Exit status is 0 when the topology fits and 1 when it does not, so it can gate
a build step.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field

# ---------------------------------------------------------------------------
# Tunables
# ---------------------------------------------------------------------------

DEFAULT_HOST = "http://localhost:11434"

# Usable VRAM, not nameplate VRAM. A 16GB card under Windows/WDDM gives back
# roughly 15.2GB, and the desktop compositor plus a browser holds 0.5-1.2GB of
# that. Budgeting the nameplate is how a topology "fits" right up until Ollama
# quietly offloads layers to system RAM and decode speed drops an order of
# magnitude. Override with --budget-gib once you have measured your own idle.
DEFAULT_BUDGET_GIB = 14.5

# Per-model compute/graph allocation that /api/show does not report and the KV
# formula does not cover: activation buffers, the logits tensor, and llama.cpp's
# own scratch. Scales with model size; these are conservative flat estimates by
# weight class rather than a formula, because the real value depends on batch
# size and the backend build.
COMPUTE_OVERHEAD_GIB = [
    (4.0, 0.35),   # weights < 4 GiB  -> 0.35 GiB
    (8.0, 0.70),   # weights < 8 GiB  -> 0.70 GiB
    (float("inf"), 1.00),
]

# Bytes per element for each KV cache type. q8_0 and q4_0 carry a per-block
# scale, so they are slightly above the naive 1.0 / 0.5.
KV_ELEMENT_BYTES = {"f16": 2.0, "q8_0": 1.0625, "q4_0": 0.5625}

GIB = 1024 ** 3

# Predicted headroom below which a topology is reported as "tight" rather than
# fitting. Ollama sizes its compute graph optimistically and a long tool result
# can push a prefill past the estimate, so a margin thinner than this is one
# unlucky round away from an offload.
TIGHT_HEADROOM_GIB = 1.5

# A prompt long enough to force a real prefill and a real compute-graph
# allocation. A one-token "hi" loads the weights and tells you nothing about
# whether the graph fits.
STRESS_PROMPT = (
    "Summarize the following requirement in exactly one sentence, then stop.\n\n"
    + ("The system shall maintain an auditable record of every permission "
       "decision, including the rule that matched and the tool it applied to. ") * 40
)

# The reference topologies from research/debate-topology-plan.md §1.2 / §1.3.
TOPOLOGIES = {
    1: {
        "name": "Concurrent (all seats resident)",
        "seats": {
            "proposer": "aegis-qwen35-9b:16k",
            "critic": "aegis-qwen35-9b:16k",   # same runner as proposer, by design
            "arbiter": "aegis-phi4-mini:8k",
        },
    },
    2: {
        "name": "Sequential (one swap per debate, at the arbitration boundary)",
        "seats": {
            "proposer": "aegis-qwen35-9b:32k",
            "critic": "aegis-qwen35-9b:32k",
            "arbiter": "aegis-phi4:16k",
        },
    },
}


# ---------------------------------------------------------------------------
# Ollama HTTP
# ---------------------------------------------------------------------------

class OllamaError(Exception):
    """An error the server reported, carrying its own message.

    urllib raises HTTPError, whose str() is just "HTTP Error 404: Not Found" —
    which for /api/show means "that model is not installed" and says so nowhere.
    Ollama puts the real reason in a JSON body on the error response; this
    preserves it so a missing model reads as a missing model rather than as a
    traceback.
    """

    def __init__(self, status: int, message: str):
        super().__init__(message)
        self.status = status
        self.message = message


def _request(req: urllib.request.Request, timeout: int) -> dict:
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read())
    except urllib.error.HTTPError as e:
        detail = ""
        try:
            detail = (json.loads(e.read()) or {}).get("error", "")
        except Exception:
            pass
        raise OllamaError(e.code, detail or f"HTTP {e.code} {e.reason}") from None
    except urllib.error.URLError as e:
        raise OllamaError(0, f"cannot reach {req.full_url}: {e.reason}") from None


def _post(host: str, path: str, body: dict, timeout: int = 900) -> dict:
    return _request(urllib.request.Request(
        host.rstrip("/") + path,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
    ), timeout)


def _get(host: str, path: str, timeout: int = 30) -> dict:
    return _request(urllib.request.Request(host.rstrip("/") + path), timeout)


def installed(host: str) -> list[str]:
    """Every model tag the server has locally."""
    return sorted(m["name"] for m in _get(host, "/api/tags").get("models", []))


def require_models(host: str, models: list[str]) -> None:
    """Fail before any measurement if a seat names a model that isn't here.

    This runs as a preflight rather than surfacing at the first /api/show
    because the failure is a setup step the operator has not done yet (the
    Modelfile variants in the plan's P69.2), and the useful response is the list
    of what they *do* have — not a stack trace from four frames down.
    """
    have = installed(host)
    missing = [m for m in dict.fromkeys(models) if m not in have]
    if not missing:
        return
    lines = [f"not installed: {', '.join(missing)}", "", "this server has:"]
    lines += [f"  {m}" for m in have] or ["  (nothing)"]
    lines += ["", "Build the missing variants (research/debate-topology-plan.md P69.2), or",
              "probe what you have:  --seat proposer=<model> --seat arbiter=<model>"]
    raise SystemExit("\n".join(lines))


def show(host: str, model: str) -> dict:
    """Full /api/show payload: model_info, parameters, template, details."""
    return _post(host, "/api/show", {"model": model}, timeout=60)


def ps(host: str) -> list[dict]:
    """Currently-resident models, with size (total) and size_vram (on GPU)."""
    return _get(host, "/api/ps").get("models", [])


def generate(host: str, model: str, prompt: str, keep_alive: str | None = None) -> dict:
    body = {"model": model, "prompt": prompt, "stream": False}
    if keep_alive is not None:
        body["keep_alive"] = keep_alive
    return _post(host, "/api/generate", body)


def unload(host: str, model: str) -> None:
    """Evict a model immediately.

    keep_alive=0 with an empty prompt is Ollama's documented unload path: it
    touches the runner's lifetime without generating. This is the *out-of-band*
    form, which is correct here (the probe owns the server) but is NOT what
    Aegis should do in production — a separate unload call races an in-flight
    request. There, the eviction rides on the last real request's own
    keep_alive field (plan §4.4).
    """
    try:
        _post(host, "/api/generate", {"model": model, "prompt": "", "keep_alive": 0}, timeout=120)
    except OllamaError:
        pass  # already gone


# ---------------------------------------------------------------------------
# Architecture introspection
# ---------------------------------------------------------------------------

@dataclass
class Arch:
    """The attention shape that determines KV-cache cost.

    Every field is read from the server's own /api/show model_info where the
    model reports it. Where it does not, the field is *inferred* and the name is
    recorded in `inferred` — because a number derived from a guess must not be
    printed next to one that was measured without saying which is which.
    """
    model: str
    family: str
    layers: int
    kv_heads: int
    key_len: int
    val_len: int
    train_ctx: int
    weights_bytes: int
    num_ctx: int | None          # pinned in the Modelfile, or None
    inferred: list[str] = field(default_factory=list)

    # Sliding-window attention. swa_window > 0 means some fraction of layers
    # cap their KV at that many tokens instead of holding the full context —
    # but model_info does not reliably report *which* fraction (Gemma's
    # sliding_window_pattern comes back null), so this class refuses to pick
    # one and returns a range instead. See kv_bytes.
    swa_window: int = 0
    swa_key_len: int = 0
    swa_val_len: int = 0

    @property
    def has_swa(self) -> bool:
        return self.swa_window > 0

    def _bytes_per_token(self, key_len: int, val_len: int, elem: float) -> float:
        return self.layers * self.kv_heads * (key_len + val_len) * elem

    @property
    def kv_bytes_per_token_f16(self) -> float:
        return self._bytes_per_token(self.key_len, self.val_len, 2.0)

    def kv_bytes(self, window: int, kv_type: str) -> tuple[float, float]:
        """(low, high) KV bytes for `window` tokens.

        Without sliding-window attention the two are equal and exact. With it,
        the bound is genuinely unknown from model_info alone: the low end
        assumes every layer is a sliding one (KV capped at the window size, at
        the SWA head dims), the high end assumes every layer is global. The real
        value sits between, and --stress is what resolves it. Returning a range
        is deliberate — a single confident number here would be wrong by a
        factor of 8 on a Gemma-class model, and wrong in the unsafe direction.
        """
        elem = KV_ELEMENT_BYTES[kv_type]
        high = self._bytes_per_token(self.key_len, self.val_len, elem) * window
        if not self.has_swa:
            return high, high
        swa_tokens = min(window, self.swa_window)
        low = self._bytes_per_token(
            self.swa_key_len or self.key_len, self.swa_val_len or self.val_len, elem) * swa_tokens
        return low, high

    @property
    def compute_gib(self) -> float:
        w = self.weights_bytes / GIB
        for ceiling, overhead in COMPUTE_OVERHEAD_GIB:
            if w < ceiling:
                return overhead
        return COMPUTE_OVERHEAD_GIB[-1][1]


def _mi(info: dict, family: str, *suffixes: str) -> int | None:
    """Read one namespaced model_info value, e.g. 'qwen3.block_count'.

    Keys are namespaced by architecture, so try the family prefix first and then
    any key with the same leaf — new families change the prefix, not the leaf.

    A key present with a **null** value counts as absent. Ollama emits nulls for
    fields a model does not define (gemma4.attention.head_count_kv is one), and
    an `in info` test would take the null as an answer.
    """
    for s in suffixes:
        if isinstance(v := info.get(f"{family}.{s}"), (int, float)):
            return int(v)
    for s in suffixes:
        for k, v in info.items():
            if k.endswith("." + s) and isinstance(v, (int, float)):
                return int(v)
    return None


def read_arch(host: str, model: str, weights_bytes: int = 0) -> Arch:
    data = show(host, model)
    info = data.get("model_info", {}) or {}
    family = (data.get("details", {}) or {}).get("family", "")
    inferred: list[str] = []

    layers = _mi(info, family, "block_count")

    # kv_heads: an absent head_count_kv means multi-head attention, where every
    # query head carries its own KV — so falling back to head_count is the
    # correct reading, not a guess. It is still recorded, because on a model
    # that simply failed to report a GQA ratio the fallback overestimates the
    # cache by the GQA factor (4x on the Qwen 9B), and a silent 4x is exactly
    # the error this script exists to prevent.
    kv_heads = _mi(info, family, "attention.head_count_kv")
    if kv_heads is None:
        kv_heads = _mi(info, family, "attention.head_count")
        if kv_heads is not None:
            inferred.append("kv_heads (no head_count_kv; assuming MHA)")

    # K and V head dims are reported separately and are not always equal.
    key_len = _mi(info, family, "attention.key_length")
    val_len = _mi(info, family, "attention.value_length") or key_len
    if key_len is None:
        emb = _mi(info, family, "embedding_length")
        heads = _mi(info, family, "attention.head_count")
        key_len = val_len = (emb // heads) if emb and heads else None
        if key_len:
            inferred.append("head_dim (embedding_length / head_count)")

    train_ctx = _mi(info, family, "context_length") or 0
    swa_window = _mi(info, family, "attention.sliding_window") or 0
    swa_key = _mi(info, family, "attention.key_length_swa") or 0
    swa_val = _mi(info, family, "attention.value_length_swa") or swa_key

    missing = [n for n, v in
               (("block_count", layers), ("head_count", kv_heads), ("key_length", key_len))
               if not v]
    if missing:
        raise SystemExit(
            f"{model}: /api/show did not report {', '.join(missing)}.\n"
            f"Cannot compute KV cost without it. Inspect the raw payload with:\n"
            f"  curl -s {host}/api/show -d '{{\"model\":\"{model}\"}}' | jq .model_info"
        )

    # Pinned num_ctx, if the Modelfile set one. Its absence is itself a finding:
    # docs/local-model-tuning.md §1 — unpinned, Aegis plans against Ollama's
    # 4096 default no matter what OLLAMA_CONTEXT_LENGTH says.
    num_ctx = None
    for line in (data.get("parameters") or "").splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[0] == "num_ctx":
            num_ctx = int(parts[1])

    return Arch(
        model=model, family=family or "?", layers=layers, kv_heads=kv_heads,
        key_len=key_len, val_len=val_len, train_ctx=train_ctx,
        weights_bytes=weights_bytes, num_ctx=num_ctx, inferred=inferred,
        swa_window=swa_window, swa_key_len=swa_key, swa_val_len=swa_val,
    )


def weights_bytes(host: str, model: str) -> int:
    """On-disk size of a model's weights.

    /api/show does **not** carry a size field — reading one off it silently
    yields 0 and a budget table where every model weighs nothing. /api/tags is
    the endpoint that reports it, for every installed model whether resident or
    not; /api/ps only covers what happens to be loaded right now.
    """
    for m in _get(host, "/api/tags").get("models", []):
        if m.get("name") == model or m.get("model") == model:
            return int(m.get("size", 0))
    return 0


# ---------------------------------------------------------------------------
# Budgeting
# ---------------------------------------------------------------------------

@dataclass
class Seat:
    role: str
    model: str
    arch: Arch | None = None
    window: int = 0
    predicted_gib: float = 0.0      # high end; what the fit decision uses
    predicted_low_gib: float = 0.0  # low end, differs only under SWA
    notes: list[str] = field(default_factory=list)


def plan_budget(host: str, seats: list[Seat], kv_type: str, budget_gib: float) -> tuple[float, bool]:
    """Fill in each seat's predicted footprint and report the total.

    Seats sharing one model name share one Ollama runner — Ollama keys a
    resident model by *name*, not by weight digest — so the second seat on the
    same name costs nothing. The corollary is the trap: two Modelfile variants
    of the same weights are two runners and two full copies in VRAM.
    """
    counted: set[str] = set()
    total = 0.0
    for s in seats:
        s.arch = read_arch(host, s.model, weights_bytes(host, s.model))
        if not s.arch.weights_bytes:
            s.notes.append("weight size unknown — the total below is missing this model's weights")
        s.window = s.arch.num_ctx or 4096
        if s.arch.num_ctx is None:
            s.notes.append("num_ctx NOT pinned — assuming 4096; Aegis will plan against 4096 too")
        if s.arch.train_ctx and s.window > s.arch.train_ctx:
            s.notes.append(f"num_ctx {s.window} exceeds training context {s.arch.train_ctx}")
        for what in s.arch.inferred:
            s.notes.append(f"INFERRED {what} — not reported by the model")
        if s.arch.has_swa:
            s.notes.append(
                f"sliding-window attention ({s.arch.swa_window} tokens): the KV column is a "
                f"range, not a number — --stress resolves it")

        w = s.arch.weights_bytes / GIB
        lo, hi = s.arch.kv_bytes(s.window, kv_type)
        # Budget against the high end. The point of the exercise is to not get
        # evicted, and a topology that only fits at the optimistic end of an
        # unresolved range has not been shown to fit.
        s.predicted_gib = w + hi / GIB + s.arch.compute_gib
        s.predicted_low_gib = w + lo / GIB + s.arch.compute_gib

        if s.model in counted:
            s.notes.append("shares a runner with an earlier seat — not counted twice")
        else:
            counted.add(s.model)
            total += s.predicted_gib
    return total, total <= budget_gib


def print_plan(seats: list[Seat], kv_type: str, total: float, budget: float) -> None:
    print(f"\n=== Predicted footprint (KV cache: {kv_type}) ===\n")
    hdr = f"{'seat':<10} {'model':<26} {'ctx':>7} {'weights':>9} {'KV':>8} {'graph':>7} {'total':>8}"
    print(hdr)
    print("-" * len(hdr))
    seen: set[str] = set()
    for s in seats:
        a = s.arch
        dup = s.model in seen
        seen.add(s.model)
        w = a.weights_bytes / GIB
        lo, hi = (v / GIB for v in a.kv_bytes(s.window, kv_type))
        kv_col = f"{hi:.2f}G" if not a.has_swa else f"{lo:.2f}-{hi:.2f}G"
        mark = "  (shared)" if dup else ""
        print(f"{s.role:<10} {s.model:<26} {s.window:>7} {w:>8.2f}G {kv_col:>8} "
              f"{a.compute_gib:>6.2f}G {s.predicted_gib:>7.2f}G{mark}")
        for n in s.notes:
            print(f"{'':<10} ! {n}")
    print("-" * len(hdr))
    print(f"{'TOTAL':<10} {'':<26} {'':>7} {'':>9} {'':>8} {'':>7} {total:>7.2f}G "
          f"of {budget:.2f}G budget  ({budget - total:+.2f}G headroom)")

    print("\n=== KV cost per token (read from /api/show) ===\n")
    for s in {x.model: x for x in seats}.values():
        a = s.arch
        per_tok = a.kv_bytes_per_token_f16 / 1024
        dims = f"{a.key_len}" if a.key_len == a.val_len else f"{a.key_len}k/{a.val_len}v"
        print(f"{a.model:<26} {a.layers} layers x {a.kv_heads} kv_heads x {dims} dim "
              f"= {per_tok:.0f} KiB/token @f16")
        cells = []
        for win in (8192, 16384, 32768):
            if a.train_ctx and win > a.train_ctx:
                continue
            lo, hi = (v / GIB for v in a.kv_bytes(win, kv_type))
            cells.append(f"{win // 1024}k:{hi:5.2f}G" if not a.has_swa
                         else f"{win // 1024}k:{lo:.2f}-{hi:.2f}G")
        print(f"{'':<26} {kv_type} -> {'  '.join(cells)}")
        if a.inferred:
            print(f"{'':<26} NOT measured: {'; '.join(a.inferred)}")
        if a.has_swa:
            print(f"{'':<26} sliding window {a.swa_window} tok; model_info does not say how many")
            print(f"{'':<26} layers are global, so the true cost is inside this range, not at an end")


# ---------------------------------------------------------------------------
# Empirical checks
# ---------------------------------------------------------------------------

def stress(host: str, seats: list[Seat], predicted_gib: float) -> bool:
    """Load every seat's model and push a real prompt through it, then assert
    they are all still 100% on GPU *simultaneously*.

    The simultaneity is the whole point. Loading them one at a time and
    checking each in turn passes even when the third load evicted the first.
    """
    models = list(dict.fromkeys(s.model for s in seats))
    print(f"\n=== Stress: loading {len(models)} model(s) and forcing a prefill ===\n")
    prefilled = 0
    for m in models:
        t0 = time.monotonic()
        try:
            r = generate(host, m, STRESS_PROMPT, keep_alive="30m")
        except OllamaError as e:
            print(f"  {m:<26} FAILED to load: {e.message}")
            return False
        dt = time.monotonic() - t0
        load_s = r.get("load_duration", 0) / 1e9
        prefilled = max(prefilled, int(r.get("prompt_eval_count", 0)))
        print(f"  {m:<26} ok  wall {dt:6.2f}s  load {load_s:6.2f}s  "
              f"prompt_eval {r.get('prompt_eval_count', 0)} tok")

    print("\n=== /api/ps with everything resident ===\n")
    ok = True
    resident = ps(host)
    if not resident:
        print("  nothing resident — the server evicted between load and check")
        return False
    total_vram = 0.0
    for m in resident:
        size, vram = int(m.get("size", 0)), int(m.get("size_vram", 0))
        pct = (100.0 * vram / size) if size else 0.0
        total_vram += vram / GIB
        flag = "" if pct >= 99.5 else "   <-- OFFLOADED TO SYSTEM RAM"
        print(f"  {m.get('name', '?'):<26} {size / GIB:6.2f}G total  "
              f"{vram / GIB:6.2f}G vram  {pct:5.1f}% GPU{flag}")
        if pct < 99.5:
            ok = False
    print(f"\n  measured VRAM in use: {total_vram:.2f} GiB "
          f"(predicted {predicted_gib:.2f} GiB, delta {total_vram - predicted_gib:+.2f} GiB)")

    # A large negative delta is the expected result, not a bug, and saying so
    # matters more than the number: /api/ps reports the runner's allocation at
    # the moment it is asked, and llama.cpp grows the KV cache as tokens arrive
    # rather than reserving the whole window up front. A prefill of `prefilled`
    # tokens against a `window`-token window has therefore exercised only a
    # fraction of the cache the plan budgets for.
    #
    # So this check proves residency (nothing offloaded, nothing evicted) and
    # does NOT prove the topology survives a full context. Treating a
    # comfortable delta here as headroom is how a topology passes the probe and
    # OOMs three turns into a real debate, once compaction has the window at
    # 85-96% by design.
    # Residency at low occupancy is not the question the operator is asking.
    # Report the growth still to come: whatever KV the topology has not yet
    # allocated is headroom that is already spoken for.
    widest = max((s.window for s in seats), default=0)
    unallocated = max(0.0, predicted_gib - total_vram)
    if unallocated > 0.1:
        print(f"  KV still to allocate as contexts fill: ~{unallocated:.2f} GiB")

    if widest and prefilled < widest * 0.5:
        print(f"\n  NOTE: the stress prompt filled {prefilled} of {widest} context tokens "
              f"({100.0 * prefilled / widest:.0f}%).")
        print("  KV cache is allocated as tokens arrive, so the measurement above is a")
        print("  residency check, not a full-window one. The predicted total is the number")
        print("  to budget against; this run only proves nothing offloaded at low occupancy.")

    if len(resident) < len(models):
        print(f"  only {len(resident)} of {len(models)} models stayed resident — "
              f"check OLLAMA_MAX_LOADED_MODELS")
        ok = False
    return ok


def measure_swap(host: str, seats: list[Seat]) -> None:
    """Time the one swap Topology 2 performs per debate: evict the debater,
    load the arbiter, and (because the daemon serves normal turns too) come
    back the other way.

    With 16GB of system RAM and an ~11GB debater there is no page cache to
    absorb this — each direction is a full disk read plus a cold prefill, since
    a reload discards the prefix cache.
    """
    debater = next((s.model for s in seats if s.role == "proposer"), None)
    arbiter = next((s.model for s in seats if s.role == "arbiter"), None)
    if not debater or not arbiter:
        print("\n(skipping swap measurement: need both a proposer and an arbiter seat)")
        return

    print("\n=== Swap cost (Topology 2, one swap per debate) ===\n")
    generate(host, debater, STRESS_PROMPT, keep_alive="30m")

    t0 = time.monotonic()
    unload(host, debater)
    r = generate(host, arbiter, STRESS_PROMPT, keep_alive="30m")
    fwd = time.monotonic() - t0
    print(f"  debater -> arbiter   {fwd:6.2f}s  (load {r.get('load_duration', 0) / 1e9:.2f}s)")

    t0 = time.monotonic()
    unload(host, arbiter)
    r = generate(host, debater, STRESS_PROMPT, keep_alive="30m")
    back = time.monotonic() - t0
    print(f"  arbiter -> debater   {back:6.2f}s  (load {r.get('load_duration', 0) / 1e9:.2f}s)")

    print(f"\n  round trip: {fwd + back:.2f}s of pure overhead per debate.")
    print("  Compare against MaxTurnStall (900s, per role) — this is a latency")
    print("  cost, not a correctness one. Above ~60s, prefer Topology 1.")


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main() -> int:
    # Windows consoles default to a legacy code page, which turns every
    # non-ASCII character in this script's own output into a replacement mark.
    # The report is meant to be read, so ask for UTF-8 and carry on if the
    # stream does not support reconfiguration (a pipe, a captured buffer).
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.reconfigure(encoding="utf-8")
        except (AttributeError, ValueError):
            pass

    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--host", default=DEFAULT_HOST)
    ap.add_argument("--topology", type=int, choices=[1, 2],
                    help="use a reference topology from debate-topology-plan.md")
    ap.add_argument("--seat", action="append", default=[], metavar="ROLE=MODEL",
                    help="override/define a seat, repeatable (proposer|critic|arbiter)")
    ap.add_argument("--kv-type", default="q8_0", choices=sorted(KV_ELEMENT_BYTES),
                    help="must match OLLAMA_KV_CACHE_TYPE (default q8_0)")
    ap.add_argument("--budget-gib", type=float, default=DEFAULT_BUDGET_GIB,
                    help=f"usable VRAM, below nameplate (default {DEFAULT_BUDGET_GIB})")
    ap.add_argument("--stress", action="store_true",
                    help="actually load the models and verify 100%% GPU residency")
    args = ap.parse_args()

    seats: list[Seat] = []
    if args.topology:
        t = TOPOLOGIES[args.topology]
        print(f"Topology {args.topology}: {t['name']}")
        seats = [Seat(role=r, model=m) for r, m in t["seats"].items()]
    for spec in args.seat:
        role, _, model = spec.partition("=")
        if not model:
            ap.error(f"--seat expects ROLE=MODEL, got {spec!r}")
        for s in seats:
            if s.role == role:
                s.model = model
                break
        else:
            seats.append(Seat(role=role, model=model))
    if not seats:
        ap.error("nothing to probe: pass --topology and/or --seat")

    try:
        ps(args.host)
        require_models(args.host, [s.model for s in seats])
    except OllamaError as e:
        print(f"cannot reach Ollama at {args.host}: {e.message}", file=sys.stderr)
        return 2

    if args.kv_type != "f16":
        print(f"\nNOTE: predicting with {args.kv_type} KV. That requires\n"
              f"  OLLAMA_FLASH_ATTENTION=1  OLLAMA_KV_CACHE_TYPE={args.kv_type}\n"
              f"on the *server*. If flash attention is unavailable on this GPU the\n"
              f"server falls back to f16 silently and every number below is half\n"
              f"the truth — which is exactly what --stress is for.")

    total, fits = plan_budget(args.host, seats, args.kv_type, args.budget_gib)
    print_plan(seats, args.kv_type, total, args.budget_gib)

    if not fits:
        print(f"\nVERDICT: does NOT fit on paper "
              f"({total:.2f}G > {args.budget_gib:.2f}G). Reduce a window or a seat.")
        return 1

    if not args.stress:
        print("\nVERDICT: fits on paper. Re-run with --stress to verify it fits in fact.")
        return 0

    ok = stress(args.host, seats, total)
    if args.topology == 2:
        measure_swap(args.host, seats)

    # A bare "FITS" overstates what a residency check establishes, and the
    # nuance printed above it does not survive being read as a headline. The
    # verdict is therefore qualified by the margin the *prediction* leaves:
    # residency proves nothing offloaded at the occupancy tested, while the
    # predicted total is what the topology costs once contexts fill. Below
    # TIGHT_HEADROOM_GIB there is not enough room for the compute graph to grow
    # into, and a topology that only clears by a few hundred MB has not been
    # shown to survive a real debate.
    headroom = args.budget_gib - total
    if not ok:
        verdict = "DOES NOT FIT — see offload flags above"
    elif headroom < TIGHT_HEADROOM_GIB:
        verdict = (f"RESIDENT, BUT TIGHT — {headroom:.2f} GiB predicted headroom "
                   f"(want > {TIGHT_HEADROOM_GIB:.1f}). Pin a smaller num_ctx on a seat.")
        ok = False
    else:
        verdict = f"FITS — all seats 100% on GPU, {headroom:.2f} GiB predicted headroom"
    print(f"\nVERDICT: {verdict}")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
