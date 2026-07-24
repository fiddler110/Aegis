#!/usr/bin/env python3
"""Threat-model reconnaissance — deterministic, language-agnostic repo digest.

Companion script for the `threat-modeling` skill's architecture phase (SKILL.md
§2, phase 1 of §4.2). It replaces the token-expensive "read every entry point,
config file, and handler into the model's context" exploration with a single
deterministic pass that emits a compact, structured digest the model curates
into components — instead of discovering them by reading raw source.

Why a script: the skill's whole discipline is *determinism* (same repo → same
component ids, boundary counts, flow counts, fingerprints) plus *bounded peak
context* on local models. A script is deterministic by construction and reads
the filesystem once outside the model's context window. It extracts only the
mechanical facts — file tree, tech stack, dependencies, bind/listen addresses,
config keys, entry points, security-infra signals, and per-file declared
symbols — and leaves every judgment call (which symbols are threat-model
components, trust boundaries, threats, severities) to the model.

Contract:
  * Facts only, never decisions. The digest *suggests* a deployment class with
    its evidence; the model confirms or overrides it (SKILL.md §2.4).
  * Python 3.7+, standard library only — no third-party imports, so it runs
    anywhere `python`/`python3` does, same dependency posture as the skill's
    other bundled scripts.
  * Deterministic output: every list is sorted; two runs on the same tree
    produce byte-identical output (git metadata aside).

Usage:
  python recon.py [ROOT] [--json PATH] [--full] [--max-files N]

  ROOT          repo/directory to scan (default: current directory)
  --json PATH   also write the complete machine-readable facts as JSON
  --full        lift the per-section truncation caps (large repos: verbose)
  --max-files N stop walking after N candidate source files (default 8000)

The digest goes to stdout; read that. `--json` is for tooling / future
inventory-fingerprint reuse, not required for the skill flow.
"""

import argparse
import json
import os
import re
import subprocess
import sys
from collections import defaultdict, OrderedDict

# --------------------------------------------------------------------------- #
# Walk configuration
# --------------------------------------------------------------------------- #

# Directories never worth walking — build output, vendored deps, VCS internals,
# and prior threat-model runs (SKILL.md excludes its own output tree).
EXCLUDED_DIRS = {
    ".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build", "out",
    "target", "bin", "obj", "__pycache__", ".venv", "venv", "env",
    ".mypy_cache", ".pytest_cache", ".tox", ".gradle", ".idea", ".vscode",
    "coverage", ".next", ".nuxt", ".cache", "site-packages", ".terraform",
    "Pods", "DerivedData", ".claude",
}

# A path containing any of these segments is a prior threat-model run or the
# skill's own materialized copy — never model our own output.
EXCLUDED_PATH_MARKERS = (
    os.path.join(".aegis", "security", "threat-model"),
    os.path.join(".aegis", "builtin-skills"),
    os.path.join(".aegis", "skills"),
    "threat-model-",
    "builtin-skills",
)

# Extension → language label. Extensions not here are still counted as "other"
# but not scanned for symbols.
LANG_BY_EXT = {
    ".go": "Go", ".py": "Python", ".js": "JavaScript", ".jsx": "JavaScript",
    ".ts": "TypeScript", ".tsx": "TypeScript", ".mjs": "JavaScript",
    ".cjs": "JavaScript", ".rb": "Ruby", ".java": "Java", ".kt": "Kotlin",
    ".cs": "C#", ".cpp": "C++", ".cc": "C++", ".cxx": "C++", ".c": "C",
    ".h": "C/C++ header", ".hpp": "C++ header", ".rs": "Rust", ".php": "PHP",
    ".swift": "Swift", ".scala": "Scala", ".m": "Objective-C", ".sh": "Shell",
    ".ps1": "PowerShell", ".pl": "Perl", ".lua": "Lua", ".ex": "Elixir",
    ".exs": "Elixir", ".dart": "Dart", ".groovy": "Groovy",
}

SOURCE_EXTS = set(LANG_BY_EXT.keys())

MAX_READ_BYTES = 262144      # 256 KB — never read more of one file than this
MAX_FILE_BYTES = 4_000_000   # skip files larger than ~4 MB outright (blobs)

# --------------------------------------------------------------------------- #
# Signal patterns — bounded regex scans over source/config text
# --------------------------------------------------------------------------- #

# Bind / listen sites → deployment classification + Component Exposure Table.
# We capture the address literal so the model can tell 127.0.0.1 from 0.0.0.0.
#
# LISTENER patterns are actual server-bind *calls* — strong deployment
# evidence. LITERAL patterns are bare address strings (config defaults,
# allow-lists, comparisons) — weaker: an incidental "0.0.0.0" literal must not
# by itself flip a loopback daemon to "internet-facing". suggest_deployment()
# weights the two differently.
LISTENER_PATTERNS = [
    re.compile(r"""ListenAndServe(?:TLS)?\s*\(\s*["'`]([^"'`]*)["'`]""", re.I),
    re.compile(r"""net\.Listen\s*\(\s*["'`]\w+["'`]\s*,\s*["'`]([^"'`]*)["'`]""", re.I),
    re.compile(r"""\.(?:listen|bind)\s*\(\s*(?:host\s*=\s*)?["'`]?([\d.:a-zA-Z_\-]+)["'`]?""", re.I),
    re.compile(r"""app\.run\([^)]*host\s*=\s*["'`]([^"'`]+)["'`]""", re.I),
]
LITERAL_PATTERNS = [
    re.compile(r"""(?:host|addr|address|bind|listen)\s*[:=]\s*["'`]([\d.]+(?::\d+)?)["'`]""", re.I),
    re.compile(r"""["'`](0\.0\.0\.0|127\.0\.0\.1|localhost|::1|\[::\])(?::\d+)?["'`]"""),
]
# Presence of a server listener *call*, whose bind address is often not a
# literal (config/flag-driven, e.g. Go's `srv.ListenAndServe()` reading
# srv.Addr). Detecting the call at all is what separates "a service that
# listens" (localhost-service / internal / internet-facing) from a CLI/desktop
# tool with no listener — even when the address itself can't be read here.
LISTENER_PRESENCE = [
    re.compile(r"\.ListenAndServe(?:TLS)?\s*\(", re.I),
    re.compile(r"\bhttp\.ListenAndServe\s*\(", re.I),
    re.compile(r"\.Serve\s*\(\s*\w"),              # grpc/http Server.Serve(listener)
    re.compile(r"\buvicorn\.run\s*\(|\bapp\.run\s*\(", re.I),
    re.compile(r"\.listen\s*\(\s*\d"),             # node server.listen(3000)
]

# Environment / config key reads.
ENV_PATTERNS = [
    re.compile(r"""os\.Getenv\s*\(\s*["'`]([A-Z0-9_]+)["'`]"""),
    re.compile(r"""os\.environ(?:\.get)?\s*[\[(]\s*["'`]([A-Z0-9_]+)["'`]"""),
    re.compile(r"""process\.env\.([A-Z0-9_]+)"""),
    re.compile(r"""process\.env\[\s*["'`]([A-Z0-9_]+)["'`]"""),
    re.compile(r"""System\.getenv\s*\(\s*["'`]([A-Z0-9_]+)["'`]"""),
    re.compile(r"""ENV\[\s*["'`]([A-Z0-9_]+)["'`]"""),
    re.compile(r"""getenv\s*\(\s*["'`]([A-Z0-9_]+)["'`]"""),
]

# Security-infrastructure keyword families → Security Infrastructure Inventory.
# Presence of one of these near a file is a *hint*, verified by the model
# (SKILL.md §3 — inventory security infra before flagging its absence).
SECURITY_SIGNALS = OrderedDict([
    ("auth/authz", re.compile(r"\b(authenticat|authoriz|oauth|oidc|jwt|bearer|rbac|casbin|permission|middleware\.auth|login|session token)\b", re.I)),
    ("tls/crypto", re.compile(r"\b(tls\.config|x509|https|mtls|ssl_context|certificate|rustls|openssl|crypto/|libsodium)\b", re.I)),
    ("secrets", re.compile(r"\b(vault|secret manager|keyring|kms|api[_-]?key|credential|\.env|dotenv|secretbox)\b", re.I)),
    ("sandbox/isolation", re.compile(r"\b(sandbox|seccomp|apparmor|namespace|chroot|firejail|gvisor|container\.run|--network none)\b", re.I)),
    ("input validation", re.compile(r"\b(validat|sanitiz|escape|parameteriz|prepared statement|bleach|zod|joi|pydantic)\b", re.I)),
    ("crypto-signing", re.compile(r"\b(hmac|signature|sign\(|verify\(|ed25519|rsa\.|ecdsa)\b", re.I)),
])

# External-dependency / network-egress hints.
EGRESS_SIGNALS = OrderedDict([
    ("http client", re.compile(r"\b(http\.Client|httpClient|requests\.(get|post)|axios|fetch\(|urllib|HttpClient|net/http|reqwest|WebClient)\b")),
    ("sql/db", re.compile(r"\b(database/sql|sqlx|gorm|sqlalchemy|psycopg|mysql|sqlite3|pg\.|mongoose|mongodb|redis|Dapper|Entity Framework)\b", re.I)),
    ("cloud sdk", re.compile(r"\b(aws-sdk|boto3|azure\.|google\.cloud|Azure\.AI|OpenAI|anthropic|@aws-sdk)\b", re.I)),
    ("message queue", re.compile(r"\b(kafka|rabbitmq|amqp|nats\.|pubsub|sqs|servicebus|celery)\b", re.I)),
    ("subprocess/exec", re.compile(r"\b(exec\.Command|subprocess\.|os/exec|Runtime\.exec|child_process|shell_exec|Process\.Start|os\.system)\b")),
])

# Entry-point markers.
ENTRY_PATTERNS = [
    re.compile(r"^\s*func\s+main\s*\(", re.M),
    re.compile(r'^\s*if\s+__name__\s*==\s*["\']__main__["\']', re.M),
    re.compile(r"static\s+(?:void|int|async\s+Task)\s+Main\s*\(", ),
    re.compile(r"public\s+static\s+void\s+main\s*\("),
    re.compile(r"^\s*fn\s+main\s*\(", re.M),
]

# --------------------------------------------------------------------------- #
# Symbol extractors — per-language top-level declaration patterns
# --------------------------------------------------------------------------- #

SYMBOL_PATTERNS = {
    "Go": [
        re.compile(r"^type\s+([A-Z]\w*)\s+(?:struct|interface)\b", re.M),
    ],
    "Python": [
        re.compile(r"^class\s+([A-Za-z_]\w*)", re.M),
    ],
    "TypeScript": [
        re.compile(r"^\s*export\s+(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_]\w*)", re.M),
        re.compile(r"^\s*export\s+interface\s+([A-Za-z_]\w*)", re.M),
    ],
    "JavaScript": [
        re.compile(r"^\s*(?:export\s+(?:default\s+)?)?class\s+([A-Za-z_]\w*)", re.M),
    ],
    "Java": [re.compile(r"^\s*(?:public\s+|final\s+|abstract\s+)*(?:class|interface|enum)\s+([A-Za-z_]\w*)", re.M)],
    "Kotlin": [re.compile(r"^\s*(?:public\s+|internal\s+|abstract\s+|open\s+|data\s+|sealed\s+)*class\s+([A-Za-z_]\w*)", re.M)],
    "C#": [re.compile(r"^\s*(?:public\s+|internal\s+|abstract\s+|sealed\s+|partial\s+|static\s+)*(?:class|interface|record)\s+([A-Za-z_]\w*)", re.M)],
    "Rust": [re.compile(r"^\s*(?:pub\s+)?(?:struct|enum|trait)\s+([A-Za-z_]\w*)", re.M)],
    "Ruby": [re.compile(r"^\s*(?:class|module)\s+([A-Z]\w*)", re.M)],
    "PHP": [re.compile(r"^\s*(?:abstract\s+|final\s+)*class\s+([A-Za-z_]\w*)", re.M)],
    "Swift": [re.compile(r"^\s*(?:public\s+|open\s+|final\s+)*(?:class|struct|protocol|actor)\s+([A-Za-z_]\w*)", re.M)],
    "Scala": [re.compile(r"^\s*(?:case\s+)?(?:class|object|trait)\s+([A-Za-z_]\w*)", re.M)],
}
# Aliases so TSX/JSX share TS/JS extractors.
SYMBOL_PATTERNS["C++"] = [re.compile(r"^\s*(?:class|struct)\s+([A-Za-z_]\w*)", re.M)]


# --------------------------------------------------------------------------- #
# Helpers
# --------------------------------------------------------------------------- #

def is_excluded_dir(name):
    return name in EXCLUDED_DIRS


def is_test_file(rel):
    """Test/spec/fixture files — real code, but never threat-model components,
    and their loopback test servers are not deployment evidence."""
    low = rel.replace("\\", "/").lower()
    base = low.rsplit("/", 1)[-1]
    if "_test." in base or ".test." in base or ".spec." in base:
        return True
    if base.startswith("test_") or base.startswith("spec_"):
        return True
    for seg in ("/test/", "/tests/", "/testdata/", "/__tests__/", "/fixtures/", "/spec/"):
        if seg in low:
            return True
    return False


def is_excluded_path(rel):
    low = rel.replace("\\", "/").lower()
    for marker in EXCLUDED_PATH_MARKERS:
        if marker.replace("\\", "/").lower() in low:
            return True
    return False


def top_level_entries(root):
    """Immediate child directories of root, each flagged excluded or walked.

    Feeds the digest's Coverage Ledger section (SKILL.md §2 step 6): every
    name here must land in 0.1-architecture.md's Coverage Ledger as either
    Covered or Excluded — including the auto-excluded ones (vendored/build/
    VCS/cache), which still need their own Excluded row rather than being
    silently dropped.
    """
    entries = []
    try:
        names = sorted(os.listdir(root))
    except OSError:
        return entries
    for name in names:
        full = os.path.join(root, name)
        if os.path.isdir(full):
            entries.append((name, is_excluded_dir(name)))
    return entries


def read_text(path):
    """Read up to MAX_READ_BYTES of a file as text, tolerant of encoding."""
    try:
        with open(path, "rb") as fh:
            raw = fh.read(MAX_READ_BYTES)
    except (OSError, IOError):
        return ""
    if b"\x00" in raw[:1024]:  # crude binary guard
        return ""
    for enc in ("utf-8", "latin-1"):
        try:
            return raw.decode(enc)
        except UnicodeDecodeError:
            continue
    return ""


def rel_posix(root, path):
    return os.path.relpath(path, root).replace("\\", "/")


def git_metadata(root):
    def run(args):
        try:
            out = subprocess.run(
                ["git", "-C", root] + args,
                capture_output=True, text=True, timeout=10,
            )
            if out.returncode == 0:
                return out.stdout.strip()
        except (OSError, subprocess.SubprocessError):
            pass
        return ""

    if not run(["rev-parse", "--is-inside-work-tree"]):
        return None
    return {
        "remote": run(["remote", "get-url", "origin"]),
        "branch": run(["rev-parse", "--abbrev-ref", "HEAD"]),
        "commit": run(["rev-parse", "--short", "HEAD"]),
        "commit_date": run(["log", "-1", "--format=%ai", "HEAD"]),
    }


# --------------------------------------------------------------------------- #
# Manifest parsers — best-effort, stdlib only
# --------------------------------------------------------------------------- #

def parse_go_mod(text):
    module = ""
    gover = ""
    deps = []
    m = re.search(r"^module\s+(\S+)", text, re.M)
    if m:
        module = m.group(1)
    m = re.search(r"^go\s+(\S+)", text, re.M)
    if m:
        gover = m.group(1)
    for m in re.finditer(r"^\s*([\w./\-]+)\s+v\S+", text, re.M):
        dep = m.group(1)
        if dep not in ("module", "go", "require", "replace", "exclude"):
            deps.append(dep)
    return {"module": module, "language_version": gover, "dependencies": sorted(set(deps))}


def parse_json_manifest(text, dep_keys):
    try:
        data = json.loads(text)
    except (ValueError, TypeError):
        return None
    deps = []
    for k in dep_keys:
        v = data.get(k)
        if isinstance(v, dict):
            deps.extend(v.keys())
    out = {"dependencies": sorted(set(deps))}
    if isinstance(data.get("name"), str):
        out["name"] = data["name"]
    if isinstance(data.get("scripts"), dict):
        out["scripts"] = sorted(data["scripts"].keys())
    return out


def parse_requirements(text):
    deps = []
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or line.startswith("-"):
            continue
        deps.append(re.split(r"[<>=!~ \[]", line)[0])
    return {"dependencies": sorted(set(d for d in deps if d))}


def parse_pyproject(text):
    deps = re.findall(r'["\']([A-Za-z0-9_.\-]+)\s*(?:[<>=!~].*?)?["\']', text)
    name = ""
    m = re.search(r'^\s*name\s*=\s*["\']([^"\']+)["\']', text, re.M)
    if m:
        name = m.group(1)
    return {"name": name, "dependencies": sorted(set(deps))[:60]}


def parse_dockerfile(text):
    exposes = re.findall(r"^\s*EXPOSE\s+(.+)$", text, re.M | re.I)
    users = re.findall(r"^\s*USER\s+(\S+)", text, re.M | re.I)
    froms = re.findall(r"^\s*FROM\s+(\S+)", text, re.M | re.I)
    return {
        "expose": sorted(set(p.strip() for e in exposes for p in e.split())),
        "user": users[-1] if users else "(root — no USER directive)",
        "base_images": froms,
    }


# --------------------------------------------------------------------------- #
# Core scan
# --------------------------------------------------------------------------- #

def scan(root, max_files):
    facts = {
        "root": os.path.abspath(root),
        "git": git_metadata(root),
        "languages": defaultdict(int),
        "manifests": OrderedDict(),
        "bind_sites": [],
        "listener_files": set(),
        "env_keys": set(),
        "entry_points": [],
        "security_signals": defaultdict(set),   # family -> {files}
        "egress_signals": defaultdict(set),
        "components": [],   # {file, lang, symbols, signals}
        "file_count": 0,
        "source_count": 0,
        "truncated": False,
        "top_level_dirs": top_level_entries(root),
        "top_level_file_counts": defaultdict(int),
    }

    for dirpath, dirnames, filenames in os.walk(root):
        # prune excluded dirs in place (also stops descent)
        dirnames[:] = sorted(d for d in dirnames if not is_excluded_dir(d))
        rel_dir = rel_posix(root, dirpath)
        if rel_dir != "." and is_excluded_path(rel_dir):
            dirnames[:] = []
            continue

        for fname in sorted(filenames):
            fpath = os.path.join(dirpath, fname)
            rel = rel_posix(root, fpath)
            if is_excluded_path(rel):
                continue
            try:
                size = os.path.getsize(fpath)
            except OSError:
                continue
            if size > MAX_FILE_BYTES:
                continue
            facts["file_count"] += 1
            top = rel.split("/", 1)[0] if "/" in rel else "(root)"
            facts["top_level_file_counts"][top] += 1
            ext = os.path.splitext(fname)[1].lower()

            _scan_manifest(root, fpath, fname, ext, facts)

            if ext not in SOURCE_EXTS:
                continue
            facts["source_count"] += 1
            lang = LANG_BY_EXT[ext]
            facts["languages"][lang] += 1

            if facts["source_count"] > max_files:
                facts["truncated"] = True
                continue

            text = read_text(fpath)
            if not text:
                continue
            _scan_source(rel, lang, text, facts)

    return facts


def _scan_manifest(root, fpath, fname, ext, facts):
    lname = fname.lower()
    rel = rel_posix(root, fpath)
    handlers = {
        "go.mod": lambda t: parse_go_mod(t),
        "package.json": lambda t: parse_json_manifest(t, ("dependencies", "devDependencies", "peerDependencies")),
        "requirements.txt": lambda t: parse_requirements(t),
        "pyproject.toml": lambda t: parse_pyproject(t),
        "cargo.toml": lambda t: {"dependencies": sorted(set(re.findall(r"^([A-Za-z0-9_\-]+)\s*=", t, re.M)))[:60]},
        "composer.json": lambda t: parse_json_manifest(t, ("require", "require-dev")),
        "gemfile": lambda t: {"dependencies": sorted(set(re.findall(r"gem\s+['\"]([^'\"]+)['\"]", t)))},
        "pom.xml": lambda t: {"dependencies": sorted(set(re.findall(r"<artifactId>([^<]+)</artifactId>", t)))[:60]},
        "build.gradle": lambda t: {"dependencies": sorted(set(re.findall(r"['\"]([\w.\-]+:[\w.\-]+):", t)))[:60]},
    }
    key = None
    parsed = None
    if lname in handlers:
        parsed = handlers[lname](read_text(fpath))
        key = rel
    elif lname == "dockerfile" or lname.endswith(".dockerfile") or lname.startswith("dockerfile"):
        parsed = parse_dockerfile(read_text(fpath))
        key = rel + " (Dockerfile)"
    elif lname in ("docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"):
        t = read_text(fpath)
        services = re.findall(r"^\s{2,4}([\w\-]+):\s*$", t, re.M)
        ports = re.findall(r"^\s*-?\s*['\"]?(\d+:\d+|\d+\.\d+\.\d+\.\d+:\d+:\d+)['\"]?", t, re.M)
        parsed = {"services": sorted(set(services))[:40], "ports": sorted(set(ports))}
        key = rel + " (compose)"
    elif lname == "chart.yaml":
        t = read_text(fpath)
        m = re.search(r"^name:\s*(\S+)", t, re.M)
        parsed = {"chart": m.group(1) if m else "(unnamed)"}
        key = rel + " (Helm chart)"
    if key and parsed is not None:
        facts["manifests"][key] = parsed


def _scan_source(rel, lang, text, facts):
    is_test = is_test_file(rel)

    # Bind / listen sites (deployment classification evidence), tagged by kind
    # (listener call vs bare literal) and whether they live in a test file.
    def record_bind(kind, pat):
        for m in pat.finditer(text):
            addr = m.group(1).strip()
            if not addr:
                continue
            line = text.count("\n", 0, m.start()) + 1
            facts["bind_sites"].append(
                {"file": rel, "line": line, "addr": addr, "kind": kind, "test": is_test}
            )
    for pat in LISTENER_PATTERNS:
        record_bind("listener", pat)
    for pat in LITERAL_PATTERNS:
        record_bind("literal", pat)
    if not is_test:
        for pat in LISTENER_PRESENCE:
            if pat.search(text):
                facts["listener_files"].add(rel)
                break

    # Env / config keys.
    for pat in ENV_PATTERNS:
        for m in pat.finditer(text):
            facts["env_keys"].add(m.group(1))

    # Entry points.
    for pat in ENTRY_PATTERNS:
        if pat.search(text):
            facts["entry_points"].append(rel)
            break

    # Security + egress signals.
    file_sec = []
    for fam, pat in SECURITY_SIGNALS.items():
        if pat.search(text):
            facts["security_signals"][fam].add(rel)
            file_sec.append(fam)
    file_eg = []
    for fam, pat in EGRESS_SIGNALS.items():
        if pat.search(text):
            facts["egress_signals"][fam].add(rel)
            file_eg.append(fam)

    # Declared symbols (component candidates).
    symbols = []
    for pat in SYMBOL_PATTERNS.get(lang, []):
        symbols.extend(pat.findall(text))
    symbols = sorted(dict.fromkeys(symbols))  # dedup, keep order-then-sort
    if symbols or file_sec or file_eg:
        facts["components"].append({
            "file": rel,
            "lang": lang,
            "symbols": symbols[:12],
            "signals": sorted(set(file_sec + file_eg)),
            "test": is_test,
        })


# --------------------------------------------------------------------------- #
# Deployment-class suggestion (evidence-based hint; model confirms)
# --------------------------------------------------------------------------- #

def _is_external_addr(a):
    return a in ("0.0.0.0", "::", "[::]") or a.startswith("0.0.0.0") or a.startswith("[::]")


def _is_loopback_addr(a):
    return a in ("127.0.0.1", "localhost", "::1") or a.startswith("127.0.0.1")


def suggest_deployment(facts):
    """Suggest a deployment class from evidence, weighting real listener binds
    over bare address literals and ignoring test-file binds. The model confirms
    or overrides (SKILL.md §2.4) — this only saves it the first hop."""
    # Only non-test binds are deployment evidence.
    binds = [b for b in facts["bind_sites"] if not b.get("test")]
    listeners = [b for b in binds if b.get("kind") == "listener"]

    external_listener = any(_is_external_addr(b["addr"]) for b in listeners)
    loopback_listener = any(_is_loopback_addr(b["addr"]) for b in listeners)
    # A bare 0.0.0.0 literal with no external listener call — worth flagging,
    # but not enough to declare the system internet-facing on its own.
    external_literal_only = (not external_listener) and any(
        _is_external_addr(b["addr"]) for b in binds if b.get("kind") == "literal"
    )
    has_dockerfile_expose = any(
        v.get("expose") for k, v in facts["manifests"].items() if "Dockerfile" in k
    )
    has_helm = any("Helm chart" in k for k in facts["manifests"])
    has_compose_ports = any(
        v.get("ports") for k, v in facts["manifests"].items() if "compose" in k
    )
    listener_call = bool(facts.get("listener_files"))

    note = " (plus a 0.0.0.0 literal not tied to a listener bind -- confirm whether it is reachable)" if external_literal_only else ""

    if has_helm:
        return "internet-facing", "Helm/k8s deployment present -- confirm Service/Ingress exposure" + note
    if external_listener or has_dockerfile_expose:
        ev = "external listener bind (0.0.0.0/::)" if external_listener else "Dockerfile EXPOSE"
        return "internet-facing", ev + " -- confirm ingress reachability" + note
    if loopback_listener:
        return "localhost-service", "listener(s) bound only to loopback (127.0.0.1/localhost)" + note
    if listener_call:
        # A real listener exists but its address is config/flag-driven, not a
        # literal — the common case for daemons whose default is loopback. Point
        # the model at the config rather than guessing wrong in either direction.
        return "localhost-service", (
            "a network listener exists but its bind address is config/flag-driven "
            "(not a literal) -- default is typically loopback; CONFIRM the default "
            "and any bind-to-all / allow-remote flag against config" + note
        )
    if has_compose_ports:
        return "internal-network", "docker-compose publishes ports -- confirm exposure" + note
    if external_literal_only:
        return "local-desktop", "no listener call found; a 0.0.0.0 literal exists but binds nothing -- confirm"
    return "local-desktop", "no network listener found -- appears to be a CLI/desktop tool"


# --------------------------------------------------------------------------- #
# Digest rendering (token-efficient stdout)
# --------------------------------------------------------------------------- #

def render(facts, full):
    cap = (lambda n: None) if full else (lambda n: n)
    out = []
    w = out.append

    w("# Threat-Model Recon")
    w("Root: `%s`" % facts["root"])
    w("Files scanned: %d  ·  source files: %d%s" % (
        facts["file_count"], facts["source_count"],
        "  ·  ⚠ source cap hit (--full for all)" if facts["truncated"] else "",
    ))
    w("")
    w("> Facts only. Every suggestion below (deployment class, security infra, "
      "component candidates) is evidence for the model to confirm or override — "
      "not a decision. Verify at the cited file before relying on it.")
    w("")

    # Top-level directories -> Coverage Ledger
    w("## Top-level directories -> Coverage Ledger")
    w("Every directory below must land in 0.1-architecture.md's Coverage "
      "Ledger as `Covered — <component>` or `Excluded — <reason>` (SKILL.md "
      "§2 step 6) — including the auto-excluded ones, which still need their "
      "own Excluded row rather than being silently dropped.")
    tld = facts.get("top_level_dirs") or []
    if tld:
        counts = facts.get("top_level_file_counts", {})
        for name, excluded in tld:
            if excluded:
                w("- `%s/` — auto-excluded pattern (vendored/build/VCS/cache) "
                  "— not walked; still record as Excluded, don't drop it" % name)
            else:
                w("- `%s/` — %d file(s) scanned" % (name, counts.get(name, 0)))
    else:
        w("- (no top-level directories found)")
    w("")

    # Git
    g = facts["git"]
    w("## Git")
    if g:
        w("- remote: `%s`" % (g["remote"] or "(none)"))
        w("- branch: `%s`  commit: `%s`  date: `%s`" % (g["branch"], g["commit"], g["commit_date"]))
    else:
        w("- (not a git repository)")
    w("")

    # Languages
    w("## Languages (source files by count)")
    langs = sorted(facts["languages"].items(), key=lambda kv: (-kv[1], kv[0]))
    if langs:
        w(" · ".join("%s %d" % (l, n) for l, n in langs))
    else:
        w("(no recognized source files)")
    w("")

    # Manifests & dependencies
    w("## Manifests & dependencies")
    if facts["manifests"]:
        for name, data in facts["manifests"].items():
            w("- **%s**" % name)
            for k, v in data.items():
                if isinstance(v, list):
                    shown = v if full else v[:20]
                    more = "" if (full or len(v) <= 20) else "  (+%d more)" % (len(v) - 20)
                    w("  - %s (%d): %s%s" % (k, len(v), ", ".join(map(str, shown)) or "—", more))
                else:
                    w("  - %s: %s" % (k, v))
    else:
        w("(no dependency manifests found)")
    w("")

    # Deployment signals
    w("## Deployment signals → Component Exposure Table")
    test_binds = sum(1 for b in facts["bind_sites"] if b.get("test"))
    # dedupe non-test binds by (file,addr) keeping lowest line
    seen = {}
    for b in facts["bind_sites"]:
        if b.get("test"):
            continue
        key = (b["file"], b["addr"])
        if key not in seen or b["line"] < seen[key]["line"]:
            seen[key] = b
    # listener binds first (stronger evidence), then literals
    binds = sorted(seen.values(), key=lambda b: (0 if b.get("kind") == "listener" else 1, b["file"], b["line"]))
    if binds:
        w("Bind / listen sites (test files excluded):")
        shown = binds if full else binds[:25]
        for b in shown:
            w("- `%s:%d`  →  `%s`  _(%s)_" % (b["file"], b["line"], b["addr"], b.get("kind", "literal")))
        if not full and len(binds) > 25:
            w("- … +%d more (--full)" % (len(binds) - 25))
    else:
        w("- no bind/listen literals found")
    if test_binds:
        w("- _(%d bind literals in test files omitted — not deployment evidence)_" % test_binds)
    lfiles = sorted(facts.get("listener_files", []))
    if lfiles:
        shown = lfiles if full else lfiles[:8]
        more = "" if (full or len(lfiles) <= 8) else "  (+%d)" % (len(lfiles) - 8)
        w("Listener call sites (address may be config-driven): %s%s"
          % (", ".join("`%s`" % f for f in shown), more))
    cls, why = suggest_deployment(facts)
    w("")
    w("**Suggested classification:** `%s` — %s.  *(model confirms in §2.4)*" % (cls, why))
    w("")

    # Entry points
    w("## Entry points")
    eps = sorted(set(facts["entry_points"]))
    if eps:
        for e in (eps if full else eps[:20]):
            w("- `%s`" % e)
        if not full and len(eps) > 20:
            w("- … +%d more" % (len(eps) - 20))
    else:
        w("- none detected (library, or unrecognized entry pattern)")
    w("")

    # Config / env keys
    w("## Config / environment keys")
    keys = sorted(facts["env_keys"])
    if keys:
        w(", ".join("`%s`" % k for k in (keys if full else keys[:40])))
        if not full and len(keys) > 40:
            w("… +%d more" % (len(keys) - 40))
    else:
        w("(none detected)")
    w("")

    # Security infrastructure
    w("## Security infrastructure signals → Security Infrastructure Inventory")
    if facts["security_signals"]:
        for fam in SECURITY_SIGNALS:
            files = sorted(facts["security_signals"].get(fam, []))
            if not files:
                continue
            shown = files if full else files[:6]
            more = "" if (full or len(files) <= 6) else "  (+%d)" % (len(files) - 6)
            w("- **%s** (%d): %s%s" % (fam, len(files), ", ".join("`%s`" % f for f in shown), more))
    else:
        w("- no security-infra keywords matched (⚠ verify before flagging absence — SKILL.md §3)")
    w("")

    # Egress / external deps
    w("## External / network egress signals")
    if facts["egress_signals"]:
        for fam in EGRESS_SIGNALS:
            files = sorted(facts["egress_signals"].get(fam, []))
            if not files:
                continue
            shown = files if full else files[:6]
            more = "" if (full or len(files) <= 6) else "  (+%d)" % (len(files) - 6)
            w("- **%s** (%d): %s%s" % (fam, len(files), ", ".join("`%s`" % f for f in shown), more))
    else:
        w("- none detected")
    w("")

    # Component candidates — ranked: security/egress-signalled files first.
    # Test files are excluded (never threat-model components) but counted.
    w("## Component candidates (declared symbols; security-relevant first)")
    w("*One row per non-test source file with a declared type/class or a "
      "security/egress signal. The model curates these into components per "
      "SKILL.md §2 eligibility — not every file is a component.*")
    comps = [c for c in facts["components"] if not c.get("test")]
    test_comps = sum(1 for c in facts["components"] if c.get("test"))
    comps.sort(key=lambda c: (0 if c["signals"] else 1, c["file"]))
    limit = None if full else 60
    shown = comps if limit is None else comps[:limit]
    for c in shown:
        sig = ("  [%s]" % ", ".join(c["signals"])) if c["signals"] else ""
        syms = ", ".join(c["symbols"]) if c["symbols"] else "—"
        w("- `%s` (%s): %s%s" % (c["file"], c["lang"], syms, sig))
    if limit is not None and len(comps) > limit:
        w("- … +%d more non-test source files (--full to list all)" % (len(comps) - limit))
    if test_comps:
        w("- _(%d test/spec files excluded from component candidates)_" % test_comps)
    w("")

    return "\n".join(out)


def to_jsonable(facts):
    d = dict(facts)
    d["languages"] = dict(facts["languages"])
    d["listener_files"] = sorted(facts["listener_files"])
    d["env_keys"] = sorted(facts["env_keys"])
    d["security_signals"] = {k: sorted(v) for k, v in facts["security_signals"].items()}
    d["egress_signals"] = {k: sorted(v) for k, v in facts["egress_signals"].items()}
    d["manifests"] = dict(facts["manifests"])
    d["top_level_dirs"] = [{"name": n, "excluded": ex} for n, ex in facts.get("top_level_dirs", [])]
    d["top_level_file_counts"] = dict(facts.get("top_level_file_counts", {}))
    d["suggested_deployment"] = dict(zip(("classification", "rationale"), suggest_deployment(facts)))
    return d


def main(argv=None):
    ap = argparse.ArgumentParser(description="Deterministic threat-model recon digest.")
    ap.add_argument("root", nargs="?", default=".", help="repo/dir to scan (default: cwd)")
    ap.add_argument("--json", metavar="PATH", help="also write full facts as JSON")
    ap.add_argument("--full", action="store_true", help="lift per-section truncation caps")
    ap.add_argument("--max-files", type=int, default=8000, help="max source files to scan (default 8000)")
    args = ap.parse_args(argv)

    # The digest uses a few Unicode glyphs (arrows, section signs). Default
    # Windows console encoding (cp1252) can't encode them; force UTF-8 so the
    # output is identical on every platform, with a graceful fallback.
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

    if not os.path.isdir(args.root):
        sys.stderr.write("recon.py: not a directory: %s\n" % args.root)
        return 2

    facts = scan(args.root, args.max_files)
    digest = render(facts, args.full)
    sys.stdout.write(digest + "\n")

    if args.json:
        try:
            with open(args.json, "w", encoding="utf-8") as fh:
                json.dump(to_jsonable(facts), fh, indent=2, sort_keys=True)
            sys.stderr.write("recon.py: wrote %s\n" % args.json)
        except OSError as e:
            sys.stderr.write("recon.py: could not write JSON: %s\n" % e)
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
