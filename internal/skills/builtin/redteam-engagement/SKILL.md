---
name: redteam-engagement
description: Use when asked to run a red-team exercise, penetration test, or attack-surface mapping against an authorized target such as the user's home lab or a named host/CIDR list. Triggers on "red team", "red-team exercise", "penetration test", "pentest", "attack surface mapping", "find vulnerabilities on my network", "scan my home lab".
---

# Red-Team Engagement Skill

A red-team exercise against real infrastructure has a failure mode a code
review doesn't: getting the target wrong is not a false positive, it's
scanning something nobody authorized. This skill exists to make scope
explicit before anything reaches the network, then walk the engagement
through a cognitive loop that resists two opposite failures — giving up on a
lead too early, and calling something exploited without evidence.

## 1. Confirm scope before anything else

Read `references/rules-of-engagement-template.md` and fill it in with the
user before running `recon_scan` or `dast_scan`: the exact target list
(hosts/IPs/CIDRs/URLs), what's explicitly out of bounds, and any time
window. State the scope back to the user in your own words and get
confirmation if there's any ambiguity — "my home lab" needs a concrete host
list or CIDR before a tool call, not an assumption.

This is a second check on top of the real one: `recon_scan`/`dast_scan`
enforce a hard, mode-independent target-authorization gate themselves
(loopback/private auto-allowed, anything else must be in
`security.dast.allowed_targets`) — a scan will be rejected before it runs if
the target isn't authorized at the config level. This step is about not
even attempting an out-of-scope call, not about working around the gate.

## 2. Map the attack surface

Run `recon_scan` with the confirmed target list:
- **nmap** finds live hosts, open ports, and service/version banners — the
  actual surface map.
- **nuclei** matches its community template library (CVEs, misconfigurations,
  exposed panels) against whatever nmap found alive.

For any in-scope running web application, follow up with `dast_scan`
(baseline mode first — passive spider + passive scan). For any in-scope
source repository, run `security_scan` (SAST/SCA/secrets/IaC). Read
`references/findings-ledger-template.md` and start populating it as results
come in — don't wait until the end to write things down.

## 3. Run the five-phase loop per lead

For each candidate weakness recon/security/dast turned up:

1. **PLAN** — write a falsifiable hypothesis: what result would prove this
   is *not* actually exploitable, not just what would confirm it is.
2. **EXECUTE + TRACK** — test it, then set the ledger row's status:
   `CONFIRMED` (verified with concrete evidence — command output, a response
   header, a scan finding ID with exact location), `REFUTED` (hypothesis
   disproven), `OPEN` (untested or inconclusive), or `NEXT` (queued
   follow-up). Three failed variants of the same attack/check class — switch
   tactics rather than retrying a dead approach.
3. **REFLECT** — every ~5 leads, check for tunnel vision: are you only
   looking at one host, one port, one layer of the stack? Broaden if so.
4. **SELF-CRITIQUE** — before marking anything `CONFIRMED`, ask what
   evidence actually proves it. A guess or a plausible-looking scanner hit
   that you haven't verified stays `OPEN` — an unverified claim in a
   red-team report is worse than an honest gap, because it misdirects
   remediation effort.

Map each `CONFIRMED`/`OPEN` finding to a MITRE ATT&CK technique where one
applies.

## 4. Report

Write the full engagement report to a file via `write_file`:
- **Attack surface map** — hosts, open ports/services, applications
  discovered.
- **Findings ledger** — every row from step 3, with its final status,
  severity, evidence, and remediation (use
  `references/findings-ledger-template.md`'s columns).
- **Residual risk** — what remains after the recommended remediations, and
  any `OPEN` items that need further authorized testing to resolve.

Do not stop at a chat summary or a partially-filled ledger — every row needs
a final status, and every `CONFIRMED`/`OPEN` row needs its evidence and
remediation before the engagement is done.
