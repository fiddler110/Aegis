# Companion Techniques

These are optional add-ons layered on top of a primary framework
(`stride.md`, `linddun.md`, `pasta.md`, `trike.md`, `vast.md`, or
`nist-800-154.md`) — not replacements for one. Use them when the ask
specifically calls for attacker realism, standardized technique references,
or Agile-native framing beyond what the primary framework produces on its
own.

## Attack Trees

A visual decomposition of how an attacker reaches a goal: the root is the
attacker's objective (e.g. "exfiltrate customer PII"), each child node is a
sub-goal or step that would achieve the parent (combined with AND — every
child required — or OR — any one child suffices), and leaf nodes are
concrete, executable attack steps.

Use attack trees to:
- Make PASTA's stage 6 (attack modeling) concrete — build one tree per
  credible threat from stage 4/5.
- Supplement any other framework's threat entries when "how would an
  attacker actually get there" needs to be shown, not just asserted.

```
Goal: Exfiltrate customer PII
├── (OR) Compromise database credentials
│   ├── (OR) Phish an engineer with DB access
│   └── (OR) Find credential in a leaked config/log
├── (OR) Exploit an IDOR in the customer API
│   └── (AND) Find endpoint missing ownership check
│       (AND) Enumerate victim record IDs
└── (OR) Compromise a backup with weaker access controls
```

## MITRE ATT&CK mapping

Map identified threats/attack steps to MITRE ATT&CK tactic and technique IDs
where a real mapping applies (e.g. an identified credential-theft threat →
`T1552` Unsecured Credentials). This grounds a threat in a catalogued,
observed-in-the-wild technique rather than a purely theoretical one, and
lets the finding cross-reference the `security-researcher` persona's own
ATT&CK-mapping work if both are in play for the same system. Only add a
mapping you're confident is accurate — an ID guessed to look thorough is
worse than no ID.

## Evil User Stories

An Agile-native format that pairs naturally with VAST's backlog-shaped
threat entries: write the threat from the attacker's point of view, then
pair it with the mitigating story.

```
Evil story:   As an attacker, I want to replay an expired session token,
              so that I can access a victim's account after they log out.
Mitigating
story:        As a user, I want my session invalidated server-side on
              logout, so that a captured token can't be replayed afterward.
```

Use this when the destination for threat output is a sprint backlog rather
than a standalone document — it slots directly next to normal user stories
without translation.

## Technology-specific sweep

Run this after the primary framework pass, whichever framework was used.
Per-element analysis reasons from the diagram outward and reliably misses
gaps that belong to a *technology* rather than a component — a database
whose auth is off by default, a Dockerfile with no `USER` line. Scan the
workspace for each technology below; for each one present, either an
existing threat entry already covers its checks or add one.

| Technology present | Check for |
|---|---|
| Redis / Memcached | auth disabled by default (`requirepass`), no TLS, exposed port |
| SQL databases | superuser connections, `ssl=false`, injection paths, credentials in connection strings |
| Docker / containers | running as root (no `USER`), host mounts, unpinned base images |
| Kubernetes / Helm | no NetworkPolicy, missing resource limits, secrets in values.yaml, no security contexts |
| LLM / cloud AI calls | PII or secrets sent to external endpoints, prompt injection, unvalidated model output driving actions |
| Message queues | unauthenticated pub/sub, unencrypted or unsigned messages |
| REST / gRPC APIs | missing auth, no rate limiting, verbose errors, gRPC reflection in production |
| CI/CD pipelines | secrets in logs, unpinned/mutable dependencies, no artifact signing |
| File shares / NFS | path traversal, world-readable mounts, no access control |
| Logging | secrets/PII in logs, no security-event logging, log injection |

The evidence rules from SKILL.md §3 still apply: verify the insecure
default actually holds in *this* deployment before flagging it — a Redis
with `requirepass` set and a bound-to-localhost port is a documented
control, not a finding.

## Hybrid Threat Modeling — a one-paragraph note

Combining frameworks in one exercise is legitimate when a single system
genuinely raises two distinct concerns a single framework doesn't cover well
on its own — e.g. STRIDE per-element for the general threat surface, plus a
NIST 800-154 data-centric pass specifically for the one regulated dataset
the system touches. If combining, keep it in one document and be explicit
about which section came from which framework — don't blend their
terminology into an ad hoc taxonomy that's neither. Don't reach for a hybrid
by default; it adds real effort and is only worth it when one framework
demonstrably leaves a stated concern uncovered.
