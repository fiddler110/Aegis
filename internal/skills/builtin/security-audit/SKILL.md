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

The tool already dedupes the same CVE/rule flagged at the same location by
more than one scanner into a single finding (tagged `[also flagged by: ...]`
when that happened), and tags a best-effort OWASP ASVS chapter on findings
where one is confidently derivable — trust both, but still drop anything you
can confirm is a false positive yourself (say why), and use the flagged
locations as a map of where to spend your manual-review time.

If the audit's scope includes reachable network targets or hosts, not just
the local workspace, also consider the `recon_scan` tool (nmap + nuclei —
attack-surface mapping and template-driven vulnerability/misconfiguration
matching). Its findings flow through the exact same Report/dedup/ASVS
pipeline as `security_scan`, so triage them identically — no separate rules.
It only runs against loopback/private hosts by default, or hosts explicitly
declared in `security.dast.allowed_targets`; a target-authorization refusal
is expected behavior for an out-of-scope host, not a scan failure — note it
in the report rather than treating it as "clean".

Check the report for a `Suppressed by baseline: N` line and any
`Baseline entry ...` notices. Suppressed findings are accepted risk an
operator already reviewed (`.aegis/security-baseline.yaml`, each entry with a
reason and expiry) — don't re-report them as new, but do flag it plainly if
one shows up as expired (its finding is back in the report — the suppression
lapsed and needs a fresh decision) or invalid (malformed entry, never
applied). If you find a real issue that a knowledgeable reviewer explicitly
wants to accept rather than fix right now, propose adding a baseline entry
(rule_id, location if it should be scoped, a concrete reason, and a real
expiry date) instead of just leaving it unaddressed with no paper trail —
don't add one yourself without that explicit decision, since it silences a
real finding.

If your system prompt's "Debate mode (P12)" section marks triage enabled,
route a borderline or disputed-severity finding (ambiguous impact, a
severity you're not confident in, or a suggested suppression) through the
`agent` tool's `mode:"debate"` before deciding to suppress it — call with
`claim` set to the finding's severity/location/rationale, and only propose
suppression if the arbiter's verdict upholds the low-risk read. Skip this
for clear-cut findings (obviously critical or obviously a non-issue); it
exists for the ones where a single pass could go either way.

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
`file:line`, the concrete exploit scenario ("attacker with X access can Y")
— not just "this could be a vulnerability" — and its ASVS chapter when the
scan report tagged one (e.g. "ASVS V5.3 Output Encoding and Injection
Prevention"), so the report reads against a recognized standard rather than
just a raw tool/rule ID. Leave it off rather than guessing one yourself for
a manual (non-scanner) finding unless you're confident of the mapping.

State plainly which scanners ran and which were skipped (not installed), and
don't present a hand-wave ("looks fine") as equivalent to a scanner pass that
didn't run. If nothing significant turned up, say so directly rather than
padding the report with low-severity nitpicks to look thorough.

## 4. Close the loop (when asked to fix, not just report)

If the ask is "fix this" / "make this safe to ship" rather than just
"review," don't stop at the report: for each critical/high finding you can
concretely fix, propose the change, apply it once agreed, and **re-run
`security_scan` scoped to the affected path** to confirm the finding is
actually gone — don't just assert the fix worked. If it's still present
(wrong rule assumed, fix incomplete, a dedup merge hid a second instance),
say so and iterate rather than reporting success on an unconfirmed fix. This
mirrors the same close-the-loop posture as `git_pr` pushing and opening a PR
once code work is done — a fix isn't finished until it's verified, not just
written.
