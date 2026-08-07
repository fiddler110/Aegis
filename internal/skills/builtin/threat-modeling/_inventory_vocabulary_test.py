#!/usr/bin/env python3
"""Tests for inventory.py's component-vocabulary tolerance (norm_type /
norm_anchor).

Run it directly — stdlib `unittest`, no third-party runner, matching the
scripts' own "Python 3.7+, standard library only" contract:

    python3 _inventory_vocabulary_test.py

The leading underscore is load-bearing, exactly as in
`_verify_substance_test.py`: `internal/skills/embedded.go` embeds this tree
with `//go:embed builtin`, and a plain directory pattern excludes every file
whose name starts with `_` or `.`. So this file is tracked source but never
ships inside a built-in skill.

What it pins, and why. A real threat-model drive produced a correct suite whose
`inventory.py --check` failed on two purely mechanical mismatches, each costing
a model round-trip at ~80k context:

    FAIL  components match 0.1-architecture.md
      Web Client.anchor: inventory='web-client/src/MainApp.jsx'
                      docs='web-client/src/MainApp.jsx (active entrypoint per README.md)'
      User.type:      inventory='external_interactor' docs='external_entity'

Neither is a disagreement about the model: `External Entity` is the DFD
vocabulary this skill's own stride.md / diagram-conventions.md teach, and the
trailing parenthetical is annotation on an anchor, not a different artifact. The
negative tests below are the other half of the contract — a genuinely different
anchor path, a genuinely different type, and an unmapped element kind
(`Data Flow`) must all still FAIL.
"""

import os
import shutil
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import inventory        # noqa: E402


# --------------------------------------------------------------------------- #
# Fixture: a small complete STRIDE suite (%-templated component rows)
# --------------------------------------------------------------------------- #

ARCHITECTURE = """\
# Architecture Overview — demo

## System Purpose

A demo target used to exercise the inventory sidecar's component checks.

## Key Components
| Component | Type | Anchor | Description |
|-----------|------|--------|-------------|
| WebClient | %(spa_type)s | %(spa_anchor)s | Single-page app |
| Database | Data Store | internal/store/schema.sql | Stores accounts |

## Deployment Classification

internet-facing
"""

MODEL = """\
# Data Flow Model — demo

## Data Flow Table
| ID | Source | Target | Protocol | Description |
|----|--------|--------|----------|-------------|
| DF01 | WebClient | Database | TCP/TLS | Account lookups |
"""

ANALYSIS = """\
# STRIDE-A Threat Analysis — demo

> Deployment classification: internet-facing

## WebClient

| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|--------------|------------|---------------|----------|
| T1 | S | Unauthenticated request reaches the admin route | src/MainApp.jsx:44 | None | Require a token | Low | High |

## Database

| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|--------------|------------|---------------|----------|
| T2 | I | Account rows readable without filtering | internal/store/schema.sql:12 | Internal Network | Tenant predicate | Low | Medium |
"""

FINDINGS = """\
# Findings — demo

### FIND-01 — Unauthenticated admin route

| Attribute | Value |
|-----------|-------|
| Component | WebClient |
| Related Threats | T1 |

### FIND-02 — Unfiltered account reads

| Attribute | Value |
|-----------|-------|
| Component | Database |
| Related Threats | T2 |

## Threat Coverage Verification

| Threat ID | Finding ID | Status |
|-----------|------------|--------|
| T1 | FIND-01 | Covered |
| T2 | FIND-02 | Covered |
"""

CANONICAL = {"spa_type": "Process", "spa_anchor": "web-client/src/MainApp.jsx"}


def write_suite(run_dir, subs):
    os.makedirs(run_dir)
    files = {
        "0.1-architecture.md": ARCHITECTURE % subs,
        "1-model.md": MODEL,
        "2-stride-analysis.md": ANALYSIS,
        "3-findings.md": FINDINGS,
    }
    for name, body in files.items():
        with open(os.path.join(run_dir, name), "w", encoding="utf-8") as fh:
            fh.write(body)
    return run_dir


# --------------------------------------------------------------------------- #
# Unit level: norm_type
# --------------------------------------------------------------------------- #

class NormTypeTest(unittest.TestCase):

    def test_canonical_values_are_unchanged(self):
        """The axis itself, in every spelling the docs write it in."""
        for cell in ("process", "Process", "`Process`", "**Process**"):
            self.assertEqual(inventory.norm_type(cell), "process", cell)
        for cell in ("External Interactor", "external_interactor",
                     "external-interactor"):
            self.assertEqual(inventory.norm_type(cell), "external_interactor", cell)
        for cell in ("Data Store", "data_store", "data-store"):
            self.assertEqual(inventory.norm_type(cell), "data_store", cell)

    def test_dfd_vocabulary_maps_onto_the_axis(self):
        """The measured failure: the standard DFD word for the same concept.

        Every term here appears in this skill's own references — stride.md
        ("*External entity*", "external entities, processes, data stores"),
        diagram-conventions.md (the `external`/`datastore` classDef names, and
        "Process (a service, an agent, a handler)" / "Data store (a database, a
        file store, a cache)" / "External entity (a user, an external
        service)"), trike.md and pasta.md (actors), output-formats.md
        ("Data Stores").
        """
        cases = {
            # -> external_interactor
            "External Entity": "external_interactor",
            "external entities": "external_interactor",
            "Entity": "external_interactor",
            "External": "external_interactor",
            "Actor": "external_interactor",
            "External Actor": "external_interactor",
            "Interactor": "external_interactor",
            "External Service": "external_interactor",
            "User": "external_interactor",
            # -> process
            "Processes": "process",
            "Service": "process",
            "Handler": "process",
            # -> data_store
            "Data Stores": "data_store",
            "DataStore": "data_store",
            "Store": "data_store",
            "Database": "data_store",
            "File Store": "data_store",
            "Cache": "data_store",
        }
        for cell, want in cases.items():
            self.assertEqual(inventory.norm_type(cell), want, cell)

    def test_mapping_is_idempotent(self):
        """Canonicalizing twice is canonicalizing once — the check runs it on
        both sides, one of which may already be canonical."""
        for cell in list(inventory.TYPE_SYNONYMS) + list(inventory.CANONICAL_TYPES):
            once = inventory.norm_type(cell)
            self.assertEqual(inventory.norm_type(once), once, cell)

    def test_every_synonym_lands_on_the_fixed_axis(self):
        """No entry may introduce a fourth value on a three-value axis."""
        for src, dst in inventory.TYPE_SYNONYMS.items():
            self.assertIn(dst, inventory.CANONICAL_TYPES, src)
        for canon in inventory.CANONICAL_TYPES:
            self.assertNotIn(canon, inventory.TYPE_SYNONYMS,
                             "%s is canonical; mapping it would hide a real value" % canon)

    def test_distinct_element_kinds_are_not_mapped(self):
        """A data flow, a trust boundary or an ambiguous `agent` in the Type
        column is a real modeling error and must still mismatch — folded to a
        token, never silently canonicalized onto the component axis."""
        for cell, want in (("Data Flow", "data_flow"),
                           ("data flows", "data_flows"),
                           ("Flow", "flow"),
                           ("Trust Boundary", "trust_boundary"),
                           ("Agent", "agent"),
                           ("Asset", "asset")):
            got = inventory.norm_type(cell)
            self.assertEqual(got, want, cell)
            self.assertNotIn(got, inventory.CANONICAL_TYPES, cell)

    def test_empty_and_junk_survive(self):
        self.assertEqual(inventory.norm_type(""), "")
        self.assertEqual(inventory.norm_type("  --  "), "")
        self.assertEqual(inventory.norm_type("Widget Thing"), "widget_thing")


# --------------------------------------------------------------------------- #
# Unit level: norm_anchor
# --------------------------------------------------------------------------- #

class NormAnchorTest(unittest.TestCase):

    def test_trailing_annotation_is_stripped(self):
        cases = {
            "web-client/src/MainApp.jsx (active entrypoint per README.md)":
                "web-client/src/MainApp.jsx",
            "internal/store/schema.sql (lines 40-120)": "internal/store/schema.sql",
            "cmd/proxy/main.go   (see also the sidecar)": "cmd/proxy/main.go",
            "src/a.js (one) (two)": "src/a.js",
        }
        for cell, want in cases.items():
            self.assertEqual(inventory.norm_anchor(cell), want, cell)

    def test_callable_and_path_parentheses_survive(self):
        """Parentheses that are part of the artifact's own name are identity,
        not annotation — the `\\s+` before `(` is what separates the two."""
        for cell in ("internal/server/proxy.go:NewProxy()",
                     "Router.handle(req)",
                     "app/(auth)/login/page.tsx",
                     "pkg/util/Parse(string)"):
            self.assertEqual(inventory.norm_anchor(cell), cell, cell)

    def test_annotation_only_cell_is_kept(self):
        """Stripping to nothing would erase the only evidence there is."""
        self.assertEqual(inventory.norm_anchor("(see README)"), "(see README)")

    def test_is_idempotent_and_composes_with_clean_cell(self):
        cell = "`web-client/src/MainApp.jsx` (active entrypoint)"
        once = inventory.norm_anchor(cell)
        self.assertEqual(once, "web-client/src/MainApp.jsx")
        self.assertEqual(inventory.norm_anchor(once), once)

    def test_a_different_path_is_untouched(self):
        """The tolerance must not collapse two different files onto each
        other — that is the check this fix must not weaken."""
        self.assertNotEqual(
            inventory.norm_anchor("src/MainApp.jsx (active entrypoint)"),
            inventory.norm_anchor("src/OtherApp.jsx (active entrypoint)"))


# --------------------------------------------------------------------------- #
# End to end: build + --check over a real run directory
# --------------------------------------------------------------------------- #

COMPONENTS_CHECK = "components match 0.1-architecture.md"


class ComponentCheckTest(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="tm-inventory-")
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.n = 0

    def _run(self, subs, sidecar_edit=None):
        """Write a suite, write the sidecar inventory.py itself would emit
        (optionally hand-edited), and return the components check's
        (passed, evidence)."""
        self.n += 1
        run_dir = write_suite(
            os.path.join(self.tmp, "stride-demo-2026-08-07-1200-%d" % self.n), subs)

        # The sidecar is always generated from the CANONICAL docs, so a
        # difference in `subs` is a difference between docs and sidecar.
        canonical_dir = write_suite(
            os.path.join(self.tmp, "canon-%d" % self.n), CANONICAL)
        inv, _ = inventory.build_inventory(canonical_dir)
        inv["metadata"] = dict(inv["metadata"])
        inv["metadata"]["directory"] = os.path.basename(run_dir)
        text = inventory.emit_yaml(inv)
        if sidecar_edit:
            before, after = sidecar_edit
            self.assertIn(before, text, "fixture drift: %r not in sidecar" % before)
            text = text.replace(before, after)
        with open(os.path.join(run_dir, "inventory.yaml"), "w", encoding="utf-8") as fh:
            fh.write(text)

        results, _ = inventory.run_checks(run_dir)
        for name, passed, evidence in results:
            if name == COMPONENTS_CHECK:
                return passed, evidence
        self.fail("no %r check in results" % COMPONENTS_CHECK)

    # -- the baseline -------------------------------------------------------- #

    def test_canonical_suite_passes(self):
        passed, evidence = self._run(CANONICAL)
        self.assertTrue(passed, evidence)

    # -- the measured failures, now tolerated -------------------------------- #

    def test_docs_written_in_dfd_vocabulary_pass(self):
        """`External Entity` in 0.1-architecture.md vs `external_interactor`
        in the sidecar — the `User.type` line from the run log."""
        subs = dict(CANONICAL, spa_type="External Entity")
        passed, evidence = self._run(
            subs, sidecar_edit=('type: "process"', 'type: "external_interactor"'))
        self.assertTrue(passed, evidence)

    def test_annotated_anchor_in_the_docs_passes(self):
        """`...jsx (active entrypoint per README.md)` in the docs vs the bare
        path in the sidecar — the `Web Client.anchor` line from the run log."""
        subs = dict(CANONICAL,
                    spa_anchor="web-client/src/MainApp.jsx (active entrypoint per README.md)")
        passed, evidence = self._run(subs)
        self.assertTrue(passed, evidence)

    def test_tolerance_is_symmetric(self):
        """The same two variants written into the *sidecar* instead — a
        hand-written inventory.yaml reaches for the DFD word just as readily."""
        passed, evidence = self._run(CANONICAL, sidecar_edit=(
            'anchor: "web-client/src/MainApp.jsx", type: "process"',
            'anchor: "web-client/src/MainApp.jsx (active entrypoint)", type: "Service"'))
        self.assertTrue(passed, evidence)

    # -- what must still fail ------------------------------------------------ #

    def test_a_genuinely_different_anchor_still_fails(self):
        """Same trailing-annotation shape, different file: the annotation
        tolerance must not turn an anchor check into no check at all."""
        subs = dict(CANONICAL,
                    spa_anchor="web-client/src/OtherApp.jsx (active entrypoint per README.md)")
        passed, evidence = self._run(subs)
        self.assertFalse(passed)
        joined = "\n".join(evidence)
        self.assertIn("WebClient.anchor", joined)
        self.assertIn("OtherApp.jsx", joined)

    def test_a_different_directory_in_the_anchor_still_fails(self):
        subs = dict(CANONICAL, spa_anchor="other-app/src/MainApp.jsx")
        passed, evidence = self._run(subs)
        self.assertFalse(passed)
        self.assertIn("WebClient.anchor", "\n".join(evidence))

    def test_a_genuinely_different_type_still_fails(self):
        """A component the docs call a data store and the sidecar calls a
        process is a real disagreement, synonyms or not."""
        subs = dict(CANONICAL, spa_type="Datastore")
        passed, evidence = self._run(subs)
        self.assertFalse(passed)
        joined = "\n".join(evidence)
        self.assertIn("WebClient.type", joined)
        self.assertIn("data_store", joined)

    def test_an_unmapped_element_kind_still_fails(self):
        """`Data Flow` is a different DFD element, not a synonym."""
        subs = dict(CANONICAL, spa_type="Data Flow")
        passed, evidence = self._run(subs)
        self.assertFalse(passed)
        self.assertIn("data_flow", "\n".join(evidence))


if __name__ == "__main__":
    unittest.main()
