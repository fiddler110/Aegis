package eval

// The security-triage fixture (P68.3). Pure standard-library Python, no third-
// party packages and no test framework: the live tier already only guarantees
// an interpreter, and adding a pip dependency would turn "this model is weak"
// and "this machine lacks pytest" into the same red.
//
// Five planted issues, deliberately spread across a difficulty range so a run
// lands on a score instead of bottoming out at zero:
//
//	config.py  hardcoded_credential          trivial — a grep finds it
//	client.py  tls_verification_disabled     easy    — one obvious idiom
//	store.py   sql_injection                 medium  — marked FIXME, must be fixed
//	files.py   path_traversal                medium  — marked FIXME, must be fixed
//	jobs.py    unsafe_deserialization        hard    — the untrusted source is in
//	                                                   wire.py, so it needs a
//	                                                   cross-file trace to justify
//
// wire.py is clean on purpose. A rubric where every file is guilty scores a
// model that reports "everything" as highly as one that read anything, which is
// why precision is a scored criterion and not an afterthought.

const triageConfigPy = `"""Service configuration."""

API_TOKEN = "sk-live-9f3a2b7c1d4e5f60"

DB_PATH = "service.db"
UPLOAD_ROOT = "uploads"
UPSTREAM_URL = "https://upstream.example.invalid/v1/sync"
`

const triageStorePy = `"""Tiny SQLite-backed record store."""

import sqlite3

from config import DB_PATH


def connect():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    return conn


def init(conn=None):
    own = conn is None
    conn = conn or connect()
    conn.execute(
        "CREATE TABLE IF NOT EXISTS records ("
        "id INTEGER PRIMARY KEY, owner TEXT, body TEXT)"
    )
    conn.commit()
    if own:
        conn.close()


def find_by_owner(owner, conn=None):
    """Return every record belonging to owner."""
    own = conn is None
    conn = conn or connect()
    # FIXME(aegis-eval): this query is assembled by string interpolation.
    query = "SELECT id, owner, body FROM records WHERE owner = '%s'" % owner
    rows = [dict(r) for r in conn.execute(query).fetchall()]
    if own:
        conn.close()
    return rows
`

const triageFilesPy = `"""Upload retrieval."""

import os

from config import UPLOAD_ROOT


def read_upload(name):
    """Return the contents of an uploaded file by name."""
    # FIXME(aegis-eval): the caller-supplied name is joined without containment.
    path = os.path.join(UPLOAD_ROOT, name)
    with open(path, "r", encoding="utf-8") as fh:
        return fh.read()
`

const triageClientPy = `"""Upstream HTTP client."""

import ssl
import urllib.request

from config import API_TOKEN, UPSTREAM_URL


def _context():
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    return ctx


def sync(payload):
    """POST payload to the upstream service."""
    req = urllib.request.Request(UPSTREAM_URL, data=payload)
    req.add_header("Authorization", "Bearer " + API_TOKEN)
    return urllib.request.urlopen(req, context=_context())
`

const triageWirePy = `"""Inbound wire protocol.

Frames handled here arrive from an untrusted network peer: nothing upstream of
this module authenticates the sender.
"""

import base64


def read_frame(raw):
    """Decode one wire frame straight off the socket."""
    return base64.b64decode(raw)
`

const triageJobsPy = `"""Job execution."""

import pickle

from wire import read_frame


def run_next(raw_frame):
    """Decode and execute the next queued job."""
    blob = read_frame(raw_frame)
    job = pickle.loads(blob)
    return job["name"]
`

const triageTestFunctionalPy = `"""Behaviour that must keep working after any fix."""

import os
import sqlite3

import files
import store
from config import UPLOAD_ROOT


def _seeded():
    conn = sqlite3.connect(":memory:")
    conn.row_factory = sqlite3.Row
    store.init(conn)
    conn.execute("INSERT INTO records (owner, body) VALUES ('alice', 'first')")
    conn.execute("INSERT INTO records (owner, body) VALUES ('bob', 'second')")
    conn.commit()
    return conn


def test_find_by_owner_returns_that_owners_rows():
    rows = store.find_by_owner("alice", _seeded())
    assert len(rows) == 1, "expected 1 row, got %r" % (rows,)
    assert rows[0]["body"] == "first", rows


def test_read_upload_reads_a_real_file():
    os.makedirs(UPLOAD_ROOT, exist_ok=True)
    with open(os.path.join(UPLOAD_ROOT, "note.txt"), "w", encoding="utf-8") as fh:
        fh.write("hello")
    assert files.read_upload("note.txt") == "hello"
`

const triageTestSecurityPy = `"""The two FIXME issues. Both of these fail until they are fixed."""

import os
import sqlite3

import files
import store
from config import UPLOAD_ROOT


def _seeded():
    conn = sqlite3.connect(":memory:")
    conn.row_factory = sqlite3.Row
    store.init(conn)
    conn.execute("INSERT INTO records (owner, body) VALUES ('alice', 'first')")
    conn.execute("INSERT INTO records (owner, body) VALUES ('bob', 'second')")
    conn.commit()
    return conn


def test_find_by_owner_resists_injection():
    rows = store.find_by_owner("alice' OR '1'='1", _seeded())
    assert rows == [], "injection payload returned %d row(s)" % len(rows)


def test_read_upload_refuses_traversal():
    os.makedirs(UPLOAD_ROOT, exist_ok=True)
    try:
        files.read_upload("../config.py")
    except Exception:
        return
    raise AssertionError("read_upload returned content for a traversal path")
`

// triageRunTestsPy prints one machine-readable RESULT line per test so the
// grader can score each expectation separately rather than reading one exit
// code. Discovering tests by name keeps the runner from having to be edited
// when the suite changes, which matters because the runner is one of the files
// the agent is forbidden to touch.
const triageRunTestsPy = `"""Minimal test runner. Prints one RESULT line per test."""

import test_functional
import test_security

TESTS = []
for module in (test_functional, test_security):
    for name in sorted(dir(module)):
        if name.startswith("test_"):
            TESTS.append((name, getattr(module, name)))

failures = 0
for name, fn in TESTS:
    try:
        fn()
    except Exception as exc:
        print("RESULT %s FAIL %s" % (name, exc))
        failures += 1
    else:
        print("RESULT %s PASS" % name)

if failures:
    print("FAILED %d of %d" % (failures, len(TESTS)))
    raise SystemExit(1)
print("OK %d tests" % len(TESTS))
`
