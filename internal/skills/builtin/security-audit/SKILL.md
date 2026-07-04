---
name: security-audit
description: Use when asked to find security vulnerabilities, do a security review, audit for OWASP-style issues, or check code before it ships. Triggers on "security review", "audit for vulnerabilities", "check this for security issues", "pentest my code", "is this safe to ship", "OWASP review".
---

# Security Audit Skill

A security audit finds two different kinds of problem, and neither one alone
is a complete audit: **known vulnerability patterns** (injection, secrets,
insecure crypto — the kind a scanner can pattern-match) and **business-logic
flaws** (an authorization check that's semantically wrong even though the
code around it is syntactically fine — the kind only a reasoning pass
catches). Run both.

## 1. Run the automated pass first

Call the `security_scan` tool over the target (whole workspace, or a
subdirectory if the ask is scoped) before reading anything by hand. It runs
whatever scanners are installed (semgrep, trivy, gitleaks, kubescape, hadolint) and normalizes
their output to severity + location + rule + remediation, noting any scanner
that's skipped because it isn't installed — mention skipped scanners in your
final report so the reader knows what wasn't covered.

Treat scanner findings as a starting list, not a final one: dedupe near
-duplicates, drop anything you can confirm is a false positive (say why), and
use the flagged locations as a map of where to spend your manual-review time.

## 2. Walk trust boundaries by hand

Scanners are blind to anything that requires understanding what the code is
*for*. For each trust boundary the target crosses, ask "what stops the
wrong actor from doing this, and does that check actually run on this
path":

- **Input → action**: user input, an API payload, a file upload, a webhook,
  or (for an agent/LLM-backed system) model or tool output flowing into a
  shell command, SQL query, file path, template, or another prompt.
- **Auth → data**: does every code path that returns or mutates a resource
  check that the caller is allowed to touch *that specific* resource, not
  just that they're logged in (the classic IDOR: "user is authenticated"
  substituting for "user owns this record")?
- **Permission/capability boundaries**: in a tool-calling or plugin system,
  is a capability check (read/write/execute/network) enforced at every call
  site that can reach a dangerous action, or only at the obvious one? A
  check duplicated in most places but missing from one sibling path is the
  single most common finding in a codebase's second security review.
- **Secrets**: logged, persisted in plaintext, embedded in a URL, or
  returned in an error message/stack trace that reaches a less-trusted
  audience than the secret's owner.
- **Egress after ingress**: does untrusted content ever get to influence a
  network call or file write that happens *after* it was read (e.g. a
  fetched web page's content steering a subsequent action)?

## 3. Report

Merge scanner findings and manual findings into one ranked list — critical,
high, medium, low (see the `content-review` skill's severity rubric if it's
available; otherwise: critical = exploitable now with real impact, high =
exploitable under specific conditions, medium = real but narrow/low-impact,
low = hardening/defense-in-depth). For each finding: one-line summary,
`file:line`, and the concrete exploit scenario ("attacker with X access
can Y") — not just "this could be a vulnerability."

State plainly which scanners ran and which were skipped (not installed), and
don't present a hand-wave ("looks fine") as equivalent to a scanner pass that
didn't run. If nothing significant turned up, say so directly rather than
padding the report with low-severity nitpicks to look thorough.
