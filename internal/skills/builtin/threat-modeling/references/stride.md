# STRIDE

**Focus:** a 6-category threat taxonomy applied per element of a data-flow
diagram (DFD). **Best for:** the general-purpose default — use it when no
other framework's specialty (privacy, business-risk traceability, explicit
risk acceptance, multi-team scale, data-centric compliance) is the actual
driver.

## The six categories

| Letter | Category | Violates | Ask |
|---|---|---|---|
| S | Spoofing | Authentication | Can an actor convincingly claim to be someone/something it isn't? |
| T | Tampering | Integrity | Can data or code be modified without authorization, in transit or at rest? |
| R | Repudiation | Non-repudiation | Can an actor deny performing an action, and is there no evidence to refute it? |
| I | Information Disclosure | Confidentiality | Can data be read by someone not authorized to read it? |
| D | Denial of Service | Availability | Can an actor degrade or block legitimate access to the system? |
| E | Elevation of Privilege | Authorization | Can an actor gain capabilities beyond what they were granted? |

## Optional seventh category: Abuse (STRIDE-A)

When the system's *legitimate features* are themselves capabilities an
adversary would want — agent tool execution, workflow automation, quota'd
resources, money-moving business logic — add **A — Abuse**: misuse of a
feature exactly as designed, violating intent rather than a security
property (workflow manipulation, quota/logic abuse, prompt injection
steering an agent into misusing its own tools). None of the six classic
categories catches these, because nothing is spoofed, tampered, or
escalated — the feature just does what it does for the wrong party.
If used, title the output STRIDE-A, apply A to processes that expose
such features, and include A rows in the same threats table. Plain
authorization failures still belong under E, not A.

## Process

1. **Build the DFD** from what you found exploring the workspace: external
   entities, processes, data stores, data flows, and trust boundaries
   (draw trust boundaries wherever privilege, network, or process isolation
   changes — e.g. client/server, unauthenticated/authenticated, container
   boundary).
2. **Apply the applicable STRIDE categories per element type** — not every
   category applies to every element kind:
   - *External entity*: Spoofing, Repudiation.
   - *Process*: all six.
   - *Data store*: Tampering, Information Disclosure, Denial of Service, and
     Repudiation if the store lacks integrity-protected logging.
   - *Data flow*: Tampering, Information Disclosure, Denial of Service.
3. **For each applicable (element, category) pair**, decide: does a
   concrete threat exist given what the code actually does? If yes, write it
   up. If the mitigation already fully addresses it, still record the
   category with its mitigation and residual risk. If no concrete threat
   exists at all, record `none identified — <one-line why>` for that pair —
   an explicit empty cell is checkable, a missing one is indistinguishable
   from a category you forgot.
4. **Map a mitigation to every identified threat.** A threat with no
   mitigation is an open finding, not a paperwork gap — flag it as such.

## Output template

```
# STRIDE Threat Model — <system/feature name>

## Scope
<what was modeled, what was explicitly out of scope>

## Deployment classification
<internet-facing / internal-network / localhost-service / local-desktop, and the evidence for it>

## Assets
<data, credentials, capabilities worth protecting>

## Data-flow diagram
<Mermaid/PlantUML diagram with trust boundaries annotated>

## Threats

| ID | Element | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|---|---|---|---|---|---|---|---|---|
| T1 | <element> | Spoofing | <description> | <file/config that makes it real> | <none/authenticated/internal network/local process/host compromise> | <control, or "none — open finding"> | <what remains after mitigation> | <critical/high/medium/low> |

## Summary
<count of threats by category and severity; anything left unmitigated>
```
