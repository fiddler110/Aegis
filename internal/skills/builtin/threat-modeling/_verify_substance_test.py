#!/usr/bin/env python3
"""Tests for verify.py's substance floor (checks 16-19, P52.8).

Run it directly — stdlib `unittest`, no third-party runner, matching the
scripts' own "Python 3.7+, standard library only" contract:

    python3 _verify_substance_test.py            # run the tests
    python3 _verify_substance_test.py --emit DIR # write both fixtures to DIR

The leading underscore is load-bearing: `internal/skills/embedded.go` embeds
this tree with `//go:embed builtin`, and a plain directory pattern excludes
every file whose name starts with `_` or `.` — the same rule that keeps
`__pycache__` out of the binary. So this file is tracked source but is never
shipped inside a built-in skill.

The two fixtures are the point of the item. `legit_suite()` is a small but
*complete* STRIDE suite that passes all nineteen checks, including the cases
that must never be flagged: a lone "none identified" row, an `Anchor` column of
bare filenames, a one-word Deployment Classification, and Evidence cells that
are short but real (`internal/server/router.go:212`, `config key
provider.small_model`, `NewProxy()`). `vacuous_suite()` is the same suite with
the *content* hollowed out and the structure left intact — it passes checks 1-15
exactly as the roadmap describes, and must fail 16-19.
"""

import json
import os
import shutil
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import verify        # noqa: E402


# --------------------------------------------------------------------------- #
# Fixtures
# --------------------------------------------------------------------------- #

ASSESSMENT = """\
# Threat Model Assessment — demo

> Deployment classification: internet-facing
> Date: 2026-07-31

## Executive Summary

%(exec_summary)s

## Action Summary

| Tier | Description | Threats | Findings | Priority |
|------|-------------|---------|----------|----------|
| 1 | Direct exposure | 1 | 1 | Now |
| 2 | Conditional risk | 1 | 1 | Next |
| **Total** | — | **2** | **2** | — |
"""

ARCHITECTURE = """\
# Architecture Overview — demo

## System Purpose

%(system_purpose)s

## Key Components

| Component | Type | Anchor | Description |
|-----------|------|--------|-------------|
| Frontend Proxy | Process | cmd/proxy/main.go | Terminates TLS and routes requests |
| Database | Data Store | internal/store/schema.sql | Stores accounts and sessions |

## Deployment Classification

internet-facing

## Component Exposure Table

| Component | Listen Address | Auth Barrier | External Reachability | Min Prerequisite |
|-----------|----------------|--------------|-----------------------|------------------|
| Frontend Proxy | 0.0.0.0:443 | Session token | internet | None |
| Database | 127.0.0.1:5432 | Password | none | Internal Network |

## Coverage Ledger

| Directory | Status | Notes |
|-----------|--------|-------|
| cmd | Covered — Frontend Proxy | Entry point |
| internal | Covered — Database | Storage layer |
| vendor | Excluded — vendored dependencies | Not our code |
"""

MODEL = """\
# Data Flow Model — demo

## Element Table

| Element | Type | Description | Trust Boundary |
|---------|------|-------------|----------------|
| Frontend Proxy | Process | TLS terminator | Public Edge |
| Database | Data Store | Account storage | Internal |

## Data Flow Table

| ID | Source | Target | Protocol | Description |
|----|--------|--------|----------|-------------|
| DF01 | Frontend Proxy | Database | TCP/TLS | Account lookups |

## Trust Boundary Table

| Boundary | Description | Contains |
|----------|-------------|----------|
| Public Edge | Internet-facing surface | Frontend Proxy |
| Internal | Private network | Database |
"""

ANALYSIS = """\
# STRIDE-A Threat Analysis — demo

> Framework: STRIDE-A — default, no stronger signal
> Deployment classification: internet-facing
> Date: 2026-07-31

## Summary

| Component | S | T | R | I | D | E | A | Total | Tier 1 | Tier 2 | Tier 3 |
|-----------|---|---|---|---|---|---|---|-------|--------|--------|--------|
| Frontend Proxy | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 | 1 | 0 | 0 |
| Database | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 1 | 0 |
| **Total** | 1 | 0 | 0 | 1 | 0 | 0 | 0 | **2** | **1** | **1** | **0** |

## Frontend Proxy

**Trust Boundary:** Public Edge
**Data Flows:** DF01

#### Tier 1 — Direct Exposure (No Prerequisites)

| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|--------------|------------|---------------|----------|
| T1 | S | Unauthenticated request reaches the admin route | %(ev1)s | None | %(mit1)s | Low — the token check is centralised | High |
| — | T | none identified — every mutating route is signed | — | None | — | — | Low |

## Database

**Trust Boundary:** Internal
**Data Flows:** DF01

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|--------------|------------|---------------|----------|
| T2 | I | Account rows readable without row-level filtering | %(ev2)s | Internal Network | %(mit2)s | Low — the pool is loopback-only | Medium |
%(extra_rows)s
## Handing off to findings

Both threats carry a mitigation, so both become findings.
"""

FINDINGS = """\
# Findings — demo

## Tier 1 — Direct Exposure (No Prerequisites)

### FIND-01 — Unauthenticated admin route

| Attribute | Value |
|-----------|-------|
| Component | Frontend Proxy |
| CVSS 4.0 | AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:N |
| Related Threats | T1 |

#### Description

%(body)s

#### Evidence

%(body)s

#### Remediation

%(body)s

#### Verification

%(body)s

## Tier 2 — Conditional Risk (Single Prerequisite)

### FIND-02 — Unfiltered account reads

| Attribute | Value |
|-----------|-------|
| Component | Database |
| CVSS 4.0 | AV:A/AC:L/AT:N/PR:L/UI:N/VC:H/VI:N/VA:N |
| Related Threats | T2 |

#### Description

%(body)s

#### Evidence

%(body)s

#### Remediation

%(body)s

#### Verification

%(body)s

## Tier 3 — Defense-in-Depth (Prior Compromise / Host Access)

No Tier 3 findings for this target.

## Threat Coverage Verification

| Threat ID | Finding ID | Status |
|-----------|------------|--------|
| T1 | FIND-01 | Covered |
| T2 | FIND-02 | Covered |
"""

MANIFEST = {
    "manifest_version": 1,
    "generator": "scaffold.py",
    "framework": "stride",
    "sections": [
        {"file": "0-assessment.md", "key": "executive-summary",
         "heading": "Executive Summary", "level": 2, "kind": "prose",
         "columns": None, "to_eof": False},
        {"file": "0-assessment.md", "key": "action-summary",
         "heading": "Action Summary", "level": 2, "kind": "table",
         "columns": ["Tier", "Description", "Threats", "Findings", "Priority"],
         "to_eof": False},
        {"file": "0.1-architecture.md", "key": "system-purpose",
         "heading": "System Purpose", "level": 2, "kind": "prose",
         "columns": None, "to_eof": False},
        {"file": "0.1-architecture.md", "key": "deployment-classification",
         "heading": "Deployment Classification", "level": 2, "kind": "prose",
         "columns": None, "to_eof": False},
        {"file": "3-findings.md", "key": "tier-1-findings",
         "heading": "Tier 1 — Direct Exposure (No Prerequisites)", "level": 2,
         "kind": "prose", "columns": None, "to_eof": False},
    ],
}

# Seven rows, every one of them "none identified" — the shape the fraction cap
# exists to catch. Written without threat IDs so the counts stay balanced and
# checks 4/7 still pass, exactly as they would in the wild.
VACUOUS_EXTRA_ROWS = """
#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|--------------|------------|---------------|----------|
| — | S | none identified | see code | Host Compromise | TBD | TBD | Low |
| — | T | none identified | see code | Host Compromise | TBD | TBD | Low |
| — | R | none identified | see code | Host Compromise | TBD | TBD | Low |
| — | I | none identified | see code | Host Compromise | TBD | TBD | Low |
| — | D | none identified | see code | Host Compromise | TBD | TBD | Low |
| — | E | none identified | see code | Host Compromise | TBD | TBD | Low |
| — | A | none identified | see code | Host Compromise | TBD | TBD | Low |
"""


def legit_suite():
    """A complete, substantive suite that must pass all nineteen checks."""
    return {
        "0-assessment.md": ASSESSMENT % {
            "exec_summary": "Two threats were confirmed against the proxy and "
                            "the account store; both have concrete mitigations "
                            "and neither is externally exploitable today.",
        },
        "0.1-architecture.md": ARCHITECTURE % {
            "system_purpose": "A TLS-terminating reverse proxy in front of a "
                              "single-tenant account database.",
        },
        "1-model.md": MODEL,
        "2-stride-analysis.md": ANALYSIS % {
            # Short but real citations: a line number, a symbol, a config key.
            "ev1": "internal/server/router.go:212",
            "ev2": "`QueryAccounts()` in internal/store/accounts.go, config key "
                   "`store.row_filter`",
            "mit1": "Require a session token on /admin (`RequireSession`)",
            "mit2": "Add a tenant predicate to `QueryAccounts()`",
            "extra_rows": "",
        },
        "3-findings.md": FINDINGS % {
            "body": "The admin router registers the handler before the session "
                    "middleware, so the check never runs for that prefix.",
        },
        ".scaffold-manifest.json": json.dumps(MANIFEST, indent=2) + "\n",
    }


def vacuous_suite():
    """Structurally identical, substantively empty — must fail checks 16-19."""
    files = legit_suite()
    files["0-assessment.md"] = ASSESSMENT % {"exec_summary": "TBD."}
    files["0.1-architecture.md"] = ARCHITECTURE % {"system_purpose": "See above."}
    files["2-stride-analysis.md"] = ANALYSIS % {
        "ev1": "internal/server/router.go",
        "ev2": "accounts.go",
        "mit1": "TBD",
        "mit2": "N/A",
        "extra_rows": VACUOUS_EXTRA_ROWS,
    }
    files["3-findings.md"] = FINDINGS % {"body": "TBD"}
    return files


def write_suite(run_dir, files):
    """Materialize a fixture dict into a run directory."""
    if not os.path.isdir(run_dir):
        os.makedirs(run_dir)
    for name, body in files.items():
        with open(os.path.join(run_dir, name), "w", encoding="utf-8",
                  newline="\n") as fh:
            fh.write(body)
    return run_dir


# --------------------------------------------------------------------------- #
# Tests
# --------------------------------------------------------------------------- #

SUBSTANCE_CHECKS = ("evidence-cells-cited", "no-placeholder-cells",
                    "none-identified-fraction", "prose-sections-substantive")


class SubstanceFloorTest(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="verify-substance-")
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self._defaults = dict(verify.SUBSTANCE)
        self.addCleanup(self._restore)

    def _restore(self):
        verify.SUBSTANCE.clear()
        verify.SUBSTANCE.update(self._defaults)

    def run_checks(self, files, name=None):
        """Run every check over a fixture; return {check-name: [evidence]}."""
        run_dir = write_suite(os.path.join(self.tmp, name or "run"), files)
        suite = verify.Suite(run_dir)
        self.assertEqual(suite.errors, [], "fixture is not a loadable suite")
        out = {}
        for check in verify.ALL_CHECKS:
            cname, ok, evidence = check(suite)
            if not ok:
                out[cname] = evidence
        return out

    # -- the legitimate suite ------------------------------------------------ #

    def test_legit_suite_passes_every_check(self):
        fails = self.run_checks(legit_suite(), "legit")
        self.assertEqual(fails, {}, "legitimate suite must pass all checks")

    def test_legit_suite_still_passes_without_a_manifest(self):
        files = legit_suite()
        del files[".scaffold-manifest.json"]
        fails = self.run_checks(files, "legit-nomanifest")
        self.assertEqual(fails, {})

    # -- the vacuous suite --------------------------------------------------- #

    def test_vacuous_suite_passes_the_structural_checks(self):
        """The premise of P52.8: checks 1-15 cannot see vacuous content."""
        fails = self.run_checks(vacuous_suite(), "vacuous")
        structural = sorted(k for k in fails if k not in SUBSTANCE_CHECKS)
        self.assertEqual(structural, [], "checks 1-15 should not fire here")

    def test_vacuous_suite_fails_every_substance_check(self):
        fails = self.run_checks(vacuous_suite(), "vacuous2")
        self.assertEqual(sorted(fails), sorted(SUBSTANCE_CHECKS))
        for name in SUBSTANCE_CHECKS:
            for line in fails[name]:
                self.assertRegex(line, r"^[\w.\-]+\.md:\d+  ",
                                 "%s evidence must name file:line" % name)

    def test_bare_filename_evidence_is_named(self):
        fails = self.run_checks(vacuous_suite(), "vacuous3")
        joined = "\n".join(fails["evidence-cells-cited"])
        self.assertIn("internal/server/router.go", joined)
        self.assertIn("accounts.go", joined)

    def test_placeholder_cells_are_named(self):
        fails = self.run_checks(vacuous_suite(), "vacuous4")
        joined = "\n".join(fails["no-placeholder-cells"])
        self.assertIn("'TBD'", joined)
        self.assertIn("'N/A'", joined)

    # -- the cases that must never be flagged -------------------------------- #

    def test_lone_none_identified_row_is_not_flagged(self):
        """The legit analysis carries one "none identified" row on purpose."""
        text = legit_suite()["2-stride-analysis.md"]
        self.assertIn("none identified", text)
        self.assertEqual(self.run_checks(legit_suite(), "lone"), {})

    def test_short_but_real_evidence_cells_pass(self):
        for cell in ("internal/server/auth.go:88",
                     "config key provider.small_model",
                     "`NewProxy()`",
                     "Dockerfile",
                     ".env",
                     "provider.small_model",
                     "auth.go — Authenticate() has no rate limit",
                     "[auth.go:88](2-stride-analysis.md#proxy)",
                     "internal/store/accounts.go, lines 20-40",
                     ""):
            self.assertTrue(verify.evidence_is_cited(cell),
                            "%r must not be flagged" % cell)

    def test_bare_filenames_are_flagged(self):
        for cell in ("internal/server/auth.go", "auth.go", "config.yaml",
                     "**`internal/server/auth.go`**", "cmd/aegis/"):
            self.assertFalse(verify.evidence_is_cited(cell),
                             "%r must be flagged" % cell)

    def test_placeholder_matching_is_exact_not_substring(self):
        tokens = set(verify.SUBSTANCE["placeholder_tokens"])
        for cell in ("TBD", "**TBD**", "`n/a`", "— See above —", "TODO."):
            self.assertIn(verify.norm_cell(cell), tokens, cell)
        for cell in ("TBD — owner to confirm by Q3",
                     "N/A because the port is loopback-only",
                     "See above, plus the mTLS policy in mesh.yaml"):
            self.assertNotIn(verify.norm_cell(cell), tokens, cell)

    def test_none_identified_fraction_ignores_small_tables(self):
        """A sparse 3-row table that is all "none identified" is legitimate."""
        files = legit_suite()
        files["2-stride-analysis.md"] = ANALYSIS % {
            "ev1": "internal/server/router.go:212",
            "ev2": "config key `store.row_filter`",
            "mit1": "Require a session token on /admin",
            "mit2": "Add a tenant predicate",
            "extra_rows": (
                "\n#### Tier 3 — Defense-in-Depth\n\n"
                "| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |\n"
                "|----|----------|--------|----------|--------------|------------|---------------|----------|\n"
                "| — | S | none identified | — | Host Compromise | — | — | Low |\n"
                "| — | T | none identified | — | Host Compromise | — | — | Low |\n"
                "| — | R | none identified | — | Host Compromise | — | — | Low |\n"),
        }
        self.assertEqual(self.run_checks(files, "sparse"), {})

    # -- tunability ---------------------------------------------------------- #

    def test_thresholds_are_tunable(self):
        files = legit_suite()
        # A stricter cap turns the 3-row sparse table above into a failure.
        verify.SUBSTANCE["none_identified_min_rows"] = 1
        verify.SUBSTANCE["none_identified_max_fraction"] = 0.0
        fails = self.run_checks(files, "tuned")
        self.assertIn("none-identified-fraction", fails)

    def test_prose_floor_is_tunable_and_exemptible(self):
        files = legit_suite()
        verify.SUBSTANCE["min_prose_chars"] = 4000
        fails = self.run_checks(files, "tuned-prose")
        joined = "\n".join(fails.get("prose-sections-substantive", []))
        self.assertIn("Executive Summary", joined)
        # The exempt key stays exempt even at an absurd floor.
        self.assertNotIn("Deployment Classification", joined)

    def test_cli_overrides_reach_the_config(self):
        run_dir = write_suite(os.path.join(self.tmp, "cli"), legit_suite())
        rc = verify.main([run_dir, "--quiet", "--min-prose-chars", "1",
                          "--none-identified-max-fraction", "0.5",
                          "--placeholder-tokens", "wat",
                          "--evidence-columns", ""])
        self.assertEqual(verify.SUBSTANCE["min_prose_chars"], 1)
        self.assertEqual(verify.SUBSTANCE["none_identified_max_fraction"], 0.5)
        self.assertEqual(verify.SUBSTANCE["placeholder_tokens"], ("wat",))
        self.assertEqual(verify.SUBSTANCE["evidence_columns"], ())
        self.assertEqual(rc, 0)

    # -- degradation --------------------------------------------------------- #

    def test_broken_manifest_degrades_to_a_pass(self):
        files = vacuous_suite()
        files[".scaffold-manifest.json"] = "{ not json"
        fails = self.run_checks(files, "brokenmanifest")
        self.assertNotIn("prose-sections-substantive", fails)
        self.assertIn("no-placeholder-cells", fails)


def _emit(dest):
    write_suite(os.path.join(dest, "legit"), legit_suite())
    write_suite(os.path.join(dest, "vacuous"), vacuous_suite())
    sys.stdout.write("wrote %s/{legit,vacuous}\n" % dest)


if __name__ == "__main__":
    if len(sys.argv) > 2 and sys.argv[1] == "--emit":
        _emit(sys.argv[2])
    else:
        unittest.main()
