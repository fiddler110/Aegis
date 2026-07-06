# Findings Ledger Template

One row per candidate weakness investigated during the engagement. Update
the `Status` column as evidence comes in — don't wait until the end.

| ID | Status | Target | Category | MITRE ATT&CK | Evidence | Severity | Remediation |
|----|--------|--------|----------|--------------|----------|----------|-------------|
| F1 | OPEN | `192.168.1.10:6379` | Unauthenticated service | T1046 (Network Service Discovery) | nmap: open 6379/tcp, no AUTH required on connect | HIGH | Set `requirepass`/ACLs, or bind to loopback only |
| F2 | CONFIRMED | `192.168.1.5` | Default credentials | T1078 (Valid Accounts) | Logged in via `admin/admin` on device web UI, screenshot saved | CRITICAL | Change default credentials, disable remote admin if unused |

## Status values

- **CONFIRMED** — verified with concrete evidence (command output, a
  response header, a scan finding ID with exact location). Never set this
  from a plausible-looking hit alone.
- **REFUTED** — hypothesis tested and disproven; keep the row so the
  engagement shows what was checked, not just what was found.
- **OPEN** — untested, inconclusive, or a hit you haven't personally
  verified yet.
- **NEXT** — a queued follow-up (e.g. blocked on a scope expansion, or
  waiting on a re-test after a fix).

## Evidence column

Always cite something concrete: exact command and output, a response
header/body snippet, a scan tool's finding ID plus location. "Looks
vulnerable" is not evidence.
