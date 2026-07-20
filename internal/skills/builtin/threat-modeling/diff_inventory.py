#!/usr/bin/env python3
"""Deterministic diff between two ``inventory.yaml`` sidecars (baseline vs current).

Companion script for the ``threat-modeling`` skill's *incremental update* flow
(SKILL.md §6). It replaces the model eyeballing two YAML sidecars with a single
deterministic pass that classifies every threat as ``new`` / ``resolved`` /
``still-present`` / ``changed`` by matching on stable identity, and reports the
per-STRIDE-category and per-tier count deltas — the raw material for an update
run's "Changes Since Baseline" section.

Why a script: like recon.py, the skill's discipline is *determinism* (same two
inventories → byte-identical diff) and *bounded model context*. Matching by id
(with a fingerprint fallback for renumbered ids) is mechanical; leaving it to
the model invites spurious "resolved + new" churn every time ids shift.

Schema it reads (authority: references/skeletons/skeleton-inventory.md):
  * top-level ``metadata`` / ``baseline`` maps (framework, target, date, commit,
    directory, deployment_classification, ...);
  * top-level ``components`` / ``flows`` / ``threats`` lists whose items are
    ``- key: value`` maps;
  * each ``threats`` item: id, component, category, tier, prerequisite,
    severity, cwe, owasp, status, finding (+ incremental ``change_status``).
Unknown keys round-trip harmlessly.

Matching rule (see match_threats):
  1. **id match** — a current threat whose ``id`` equals an unconsumed baseline
     ``id`` is that threat. Primary, because ids are the intended stable key.
  2. **fingerprint fallback** — ids renumber between runs, so any still-unmatched
     current threat is matched to an unmatched baseline threat sharing the
     fingerprint ``(component, category, title-ish)`` (normalized, lowercased;
     "title-ish" uses an optional ``title``/``name``/``finding`` field when
     present, else empty). Same-fingerprint groups are paired in sorted order so
     the pairing is deterministic. This keeps a renumbered-but-otherwise-identical
     threat as ``still-present`` / ``changed`` instead of a spurious
     ``resolved`` + ``new`` pair.
  Anything unmatched: baseline-only → ``resolved``; current-only → ``new``.

Classification of a matched pair: ``still-present`` when ``tier``,
``prerequisite``, ``severity`` and ``status`` are all equal; otherwise
``changed``, reporting old→new for each differing field.

Python 3.7+, standard library only — no pyyaml, same dependency posture as the
skill's other bundled scripts. Deterministic: every list is sorted.

Usage:
  python diff_inventory.py <baseline-inventory.yaml> <current-inventory.yaml>

Exit status: 0 on success; non-zero only on unreadable / malformed / empty
input.
"""

import argparse
import os
import sys

# Fields whose change flips a matched threat from still-present to changed.
COMPARED_FIELDS = ("tier", "prerequisite", "severity", "status")

# STRIDE-A category-word → letter, used only to annotate the per-category delta
# table for STRIDE runs. The table itself keys on the raw ``category`` strings so
# it stays correct for any framework (LINDDUN/PASTA/etc.); unmapped categories
# simply have no letter.
STRIDE_LETTER = {
    "spoofing": "S",
    "tampering": "T",
    "repudiation": "R",
    "information-disclosure": "I",
    "information disclosure": "I",
    "denial-of-service": "D",
    "denial of service": "D",
    "elevation-of-privilege": "E",
    "elevation of privilege": "E",
    "abuse": "A",  # STRIDE-A's added 7th category (Abuse); authz failures stay under E
}


# --------------------------------------------------------------------------- #
# Minimal YAML reader — tuned to the exact shape skeleton-inventory.md emits.
# --------------------------------------------------------------------------- #
# Handles precisely: top-level scalar keys; top-level map keys (metadata,
# baseline) with indented ``key: value`` children; top-level list keys
# (components, flows, threats) whose items are ``- key: value`` maps with scalar
# values. NOT a general YAML parser (no nested lists, block scalars, anchors) —
# the sidecar is deliberately one-line-per-entry so this stays a few dozen lines.

class MalformedInventory(Exception):
    """Raised when a file cannot be parsed as an inventory sidecar."""


def _strip_scalar(value):
    """Trim a scalar value and drop a single layer of surrounding quotes."""
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
        return value[1:-1]
    return value


def _split_kv(text):
    """Split ``key: value`` on the first colon. Returns (key, value_or_None)."""
    idx = text.find(":")
    if idx < 0:
        raise MalformedInventory("expected 'key: value', got: %r" % text)
    key = text[:idx].strip()
    rest = text[idx + 1:].strip()
    return key, (rest if rest != "" else None)


# --- Flow-mapping list items (inventory.py's emitted shape) ----------------- #
# inventory.py writes each list entry as a single-line YAML flow mapping,
# `- {id: "T1", component: "Engine", tier: 2}`, per skeleton-inventory.md's
# "one line per entry" directive. We parse that shape here (and keep the block
# `- key: value` style below as a fallback) so this reader stays compatible with
# whatever produced the sidecar — the script or a hand-written block-style file.

def _split_top_commas(s):
    """Split on commas that are not inside a double-quoted string."""
    parts, buf, inq, i = [], [], False, 0
    while i < len(s):
        c = s[i]
        if c == '"':
            inq = not inq
            buf.append(c)
        elif c == "\\" and inq and i + 1 < len(s):
            buf.append(c)
            buf.append(s[i + 1])
            i += 2
            continue
        elif c == "," and not inq:
            parts.append("".join(buf))
            buf = []
        else:
            buf.append(c)
        i += 1
    if buf:
        parts.append("".join(buf))
    return parts


def _unescape(s):
    out, i = [], 0
    while i < len(s):
        if s[i] == "\\" and i + 1 < len(s):
            out.append(s[i + 1])
            i += 2
        else:
            out.append(s[i])
            i += 1
    return "".join(out)


def _flow_scalar(v):
    v = v.strip()
    if v.startswith('"'):
        return _unescape(v[1:].rsplit('"', 1)[0])
    if len(v) >= 2 and v[0] == v[-1] and v[0] == "'":
        return v[1:-1]
    if v.lstrip("-").isdigit():
        return v  # keep numbers as strings; comparisons normalize anyway
    return v


def _parse_flow_map(s):
    """Parse a single-line flow mapping ``{k: v, k2: "v2"}`` into a dict."""
    s = s.strip()
    if s.startswith("{"):
        s = s[1:]
    if s.endswith("}"):
        s = s[:-1]
    d = {}
    for kv in _split_top_commas(s):
        if ":" not in kv:
            continue
        key, _, val = kv.partition(":")  # first colon separates (keys have none)
        key = key.strip()
        if key:
            d[key] = _flow_scalar(val)
    return d


def parse_inventory(path):
    """Parse an inventory.yaml sidecar into a dict. Raises MalformedInventory."""
    try:
        with open(path, "rb") as fh:
            raw = fh.read()
    except (OSError, IOError) as exc:
        raise MalformedInventory("cannot read %s: %s" % (path, exc))
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError:
        text = raw.decode("latin-1")

    result = {}
    pending_key = None      # top-level key whose block we're currently filling
    mode = None             # None | 'map' | 'list'
    current_item = None     # dict for the list item being filled

    for lineno, rawline in enumerate(text.splitlines(), 1):
        # Drop a full-line comment; ignore blank lines. (We deliberately do not
        # strip inline '#' comments — a value may legitimately contain '#'.)
        stripped = rawline.strip()
        if not stripped or stripped.startswith("#"):
            continue
        indent = len(rawline) - len(rawline.lstrip(" "))

        if indent == 0:
            key, value = _split_kv(stripped)
            if value is not None:
                result[key] = _strip_scalar(value)
                pending_key, mode, current_item = None, None, None
            else:
                # Opens a nested block; list vs map decided by the next line.
                pending_key = key
                mode = None
                current_item = None
                if key not in result:
                    result[key] = None
            continue

        # indent > 0 — must belong to the current top-level block.
        if pending_key is None:
            raise MalformedInventory(
                "line %d: indented content with no parent key" % lineno)

        if stripped.startswith("- "):
            if not isinstance(result.get(pending_key), list):
                result[pending_key] = []
            mode = "list"
            item_text = stripped[2:].lstrip()
            if item_text.startswith("{"):
                # Flow-mapping entry (inventory.py's emitted shape) — the whole
                # entry is on this one line; no indented children follow.
                current_item = _parse_flow_map(item_text)
                result[pending_key].append(current_item)
            else:
                key, value = _split_kv(stripped[2:])
                current_item = {key: _strip_scalar(value) if value is not None else ""}
                result[pending_key].append(current_item)
        elif stripped == "-":
            # Bare list marker with fields on following lines.
            if not isinstance(result.get(pending_key), list):
                result[pending_key] = []
            mode = "list"
            current_item = {}
            result[pending_key].append(current_item)
        else:
            key, value = _split_kv(stripped)
            scalar = _strip_scalar(value) if value is not None else ""
            if mode == "list" and current_item is not None:
                current_item[key] = scalar
            else:
                # Map entry under the pending top-level key (metadata/baseline).
                if not isinstance(result.get(pending_key), dict):
                    result[pending_key] = {}
                    mode = "map"
                result[pending_key][key] = scalar

    if not result:
        raise MalformedInventory("%s: empty or no recognizable content" % path)
    threats = result.get("threats")
    if threats is not None and not isinstance(threats, list):
        raise MalformedInventory("%s: 'threats' is not a list" % path)
    return result


# --------------------------------------------------------------------------- #
# Matching + classification
# --------------------------------------------------------------------------- #

def _norm(value):
    return str(value or "").strip().lower()


def _title_ish(threat):
    """A best-effort stable identity token for the fingerprint fallback.

    The skeleton defines no title field, so we use whichever descriptive field
    is present (``title``/``name``/``summary``), falling back to ``finding``.
    Absent all of them this is "" and the fingerprint reduces to
    (component, category) — the documented minimum."""
    for field in ("title", "name", "summary", "finding"):
        if threat.get(field):
            return _norm(threat[field])
    return ""


def _fingerprint(threat):
    return (_norm(threat.get("component")), _norm(threat.get("category")),
            _title_ish(threat))


def _sort_key(threat):
    # Deterministic ordering for pairing same-fingerprint groups and for output.
    return (_norm(threat.get("component")), _norm(threat.get("category")),
            str(threat.get("id") or ""))


def match_threats(base, cur):
    """Return (pairs, resolved, added) where pairs is a list of
    (baseline_threat, current_threat) matched by the rule in the module
    docstring, resolved is baseline-only threats, added is current-only."""
    base_used = [False] * len(base)
    cur_used = [False] * len(cur)
    pairs = []

    # Pass 1: id match.
    base_by_id = {}
    for i, t in enumerate(base):
        tid = t.get("id")
        if tid:
            base_by_id.setdefault(tid, []).append(i)
    for j in sorted(range(len(cur)), key=lambda j: _sort_key(cur[j])):
        cid = cur[j].get("id")
        if not cid:
            continue
        for i in base_by_id.get(cid, []):
            if not base_used[i]:
                base_used[i] = cur_used[j] = True
                pairs.append((base[i], cur[j]))
                break

    # Pass 2: fingerprint fallback for renumbered ids.
    base_by_fp = {}
    for i, t in enumerate(base):
        if not base_used[i]:
            base_by_fp.setdefault(_fingerprint(t), []).append(i)
    for group in base_by_fp.values():
        group.sort(key=lambda i: _sort_key(base[i]))
    for j in sorted(range(len(cur)), key=lambda j: _sort_key(cur[j])):
        if cur_used[j]:
            continue
        group = base_by_fp.get(_fingerprint(cur[j]))
        if not group:
            continue
        while group and base_used[group[0]]:
            group.pop(0)
        if group:
            i = group.pop(0)
            base_used[i] = cur_used[j] = True
            pairs.append((base[i], cur[j]))

    resolved = [base[i] for i in range(len(base)) if not base_used[i]]
    added = [cur[j] for j in range(len(cur)) if not cur_used[j]]
    return pairs, resolved, added


def classify_pair(base_t, cur_t):
    """Return (change_status, detail_parts) for a matched pair."""
    detail = []
    # Surface a renumbered id (fingerprint-matched) so the fallback is visible.
    bid, cid = str(base_t.get("id") or ""), str(cur_t.get("id") or "")
    if bid != cid:
        detail.append("id: %s->%s" % (bid or "-", cid or "-"))
    changed_fields = []
    for field in COMPARED_FIELDS:
        old, new = _norm(base_t.get(field)), _norm(cur_t.get(field))
        if old != new:
            changed_fields.append(
                "%s: %s->%s" % (field, base_t.get(field) or "-",
                                cur_t.get(field) or "-"))
    if changed_fields:
        detail.extend(changed_fields)
        return "changed", detail
    return "still-present", detail


# --------------------------------------------------------------------------- #
# Delta tables
# --------------------------------------------------------------------------- #

def _counts_by(threats, field):
    counts = {}
    for t in threats:
        key = str(t.get(field) or "(unset)").strip() or "(unset)"
        counts[key] = counts.get(key, 0) + 1
    return counts


def _delta_rows(base, cur, field):
    b = _counts_by(base, field)
    c = _counts_by(cur, field)
    rows = []
    for key in sorted(set(b) | set(c)):
        bn, cn = b.get(key, 0), c.get(key, 0)
        rows.append((key, bn, cn, cn - bn))
    return rows


# --------------------------------------------------------------------------- #
# Rendering (token-efficient, sorted, deterministic)
# --------------------------------------------------------------------------- #

def _md_table(header, rows):
    out = ["| " + " | ".join(header) + " |",
           "| " + " | ".join("---" for _ in header) + " |"]
    for row in rows:
        out.append("| " + " | ".join(str(c) for c in row) + " |")
    return out


def render(base_inv, cur_inv):
    base = base_inv.get("threats") or []
    cur = cur_inv.get("threats") or []
    pairs, resolved, added = match_threats(base, cur)

    classified = []  # (status, threat_id_for_display, component, category, detail)
    n_still = n_changed = 0
    for base_t, cur_t in pairs:
        status, detail = classify_pair(base_t, cur_t)
        if status == "still-present":
            n_still += 1
        else:
            n_changed += 1
        classified.append((status, str(cur_t.get("id") or base_t.get("id") or "-"),
                           cur_t.get("component") or "-",
                           cur_t.get("category") or "-",
                           "; ".join(detail) if detail else "-"))
    for t in resolved:
        classified.append(("resolved", str(t.get("id") or "-"),
                           t.get("component") or "-", t.get("category") or "-", "-"))
    for t in added:
        classified.append(("new", str(t.get("id") or "-"),
                           t.get("component") or "-", t.get("category") or "-", "-"))

    order = {"changed": 0, "new": 1, "resolved": 2, "still-present": 3}
    classified.sort(key=lambda r: (order[r[0]], _norm(r[2]), _norm(r[3]), r[1]))

    out = []
    w = out.append

    w("# Threat Inventory Diff")
    bmeta = base_inv.get("metadata") or {}
    cmeta = cur_inv.get("metadata") or {}
    w("Baseline: `%s` (commit `%s`, %s)  ->  Current: `%s` (commit `%s`, %s)" % (
        bmeta.get("directory", "?"), bmeta.get("commit", "?"), bmeta.get("date", "?"),
        cmeta.get("directory", "?"), cmeta.get("commit", "?"), cmeta.get("date", "?")))
    w("")
    w("**Summary:** %d new · %d resolved · %d still-present · %d changed "
      "(baseline %d threats -> current %d threats)"
      % (len(added), len(resolved), n_still, n_changed, len(base), len(cur)))
    w("")

    w("## Changes")
    w("")
    out.extend(_md_table(
        ["Threat ID", "Component", "Category", "Change", "Detail"],
        [(r[1], r[2], r[3], r[0], r[4]) for r in classified]))
    if not classified:
        w("_(no threats in either inventory)_")
    w("")

    w("## Category deltas (STRIDE-A)")
    w("")
    cat_rows = []
    for cat, bn, cn, delta in _delta_rows(base, cur, "category"):
        letter = STRIDE_LETTER.get(_norm(cat), "")
        label = "%s (%s)" % (cat, letter) if letter else cat
        cat_rows.append((label, bn, cn, "%+d" % delta))
    out.extend(_md_table(["Category", "Baseline", "Current", "Delta"], cat_rows))
    w("")

    w("## Tier deltas")
    w("")
    tier_rows = [(tier, bn, cn, "%+d" % delta)
                 for tier, bn, cn, delta in _delta_rows(base, cur, "tier")]
    out.extend(_md_table(["Tier", "Baseline", "Current", "Delta"], tier_rows))
    w("")

    return "\n".join(out)


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #

def main(argv=None):
    ap = argparse.ArgumentParser(
        description="Deterministic diff between two inventory.yaml threat sidecars.")
    ap.add_argument("baseline", help="baseline run's inventory.yaml")
    ap.add_argument("current", help="current run's inventory.yaml")
    args = ap.parse_args(argv)

    # The digest uses arrow/middot glyphs; Windows cp1252 can't encode them, so
    # force UTF-8 for byte-identical output on every platform (same as recon.py).
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

    for path in (args.baseline, args.current):
        if not os.path.isfile(path):
            sys.stderr.write("diff_inventory.py: not a file: %s\n" % path)
            return 2

    try:
        base_inv = parse_inventory(args.baseline)
        cur_inv = parse_inventory(args.current)
    except MalformedInventory as exc:
        sys.stderr.write("diff_inventory.py: %s\n" % exc)
        return 1

    sys.stdout.write(render(base_inv, cur_inv) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
