# Persona Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all four "stops before content generation" failure modes across Aegis's 17 personas by adding a shared ToolUseBlock and per-persona output-completion sections.

**Architecture:** Add a `ToolUseBlock()` function to `persona.go` (mirrors `CompletingTasksBlock()` and `PlatformBlock()`), inject it into every session in `effectiveSystem()` and the CLI chat path, rationalize duplicate inline tool-use rules in three persona constants, and append a `## Completing your output` section to all 17 persona constants.

**Tech Stack:** Go 1.21+, `strings` package. No new dependencies.

## Global Constraints

- Module path: `github.com/scottymacleod/aegis`
- Injection order must be: `persona.System → ToolUseBlock → CompletingTasksBlock → PlatformBlock → context/memory/skills`
- Do not add new personas, modify `CompletingTasksBlock`, or modify `PlatformBlock`
- `persona_test.go` is `package persona` (internal); `server_test.go` is `package server` — both can access unexported identifiers
- Use the existing `contains()` helper in `persona_test.go` instead of `strings.Contains`

---

### Task 1: Add ToolUseBlock() and rationalize inline tool-use rules

**Files:**
- Modify: `internal/persona/persona.go`
- Modify: `internal/persona/persona_test.go`

**Interfaces:**
- Produces: `ToolUseBlock() string` — exported function returning the shared tool-use rules constant; consumed by Task 3

- [ ] **Step 1: Add failing tests to persona_test.go**

Append to `internal/persona/persona_test.go` (inside `package persona`, after line 104):

```go
func TestToolUseBlock_content(t *testing.T) {
	block := ToolUseBlock()
	for _, want := range []string{
		"IMMEDIATELY",
		"narration of intent",
		"A tool result is input",
		"truncated",
	} {
		if !contains(block, want) {
			t.Errorf("ToolUseBlock() missing expected phrase: %q", want)
		}
	}
}

func TestGeneralSystem_noGenericToolRules(t *testing.T) {
	p, _ := Get("general")
	if contains(p.System, "call the appropriate tool IMMEDIATELY") {
		t.Error("generalSystem still contains generic call-immediately rule (should be removed — covered by shared ToolUseBlock)")
	}
}

func TestSecuritySystem_genericRulesRemoved(t *testing.T) {
	p, _ := Get("security")
	if contains(p.System, "Never narrate intent") {
		t.Error("securitySystem still contains generic narration rule (should be removed — covered by shared ToolUseBlock)")
	}
	if !contains(p.System, "LOCAL project or workspace") {
		t.Error("securitySystem must keep LOCAL/EXTERNAL tool selection guidance")
	}
}

func TestSecurityArchitectSystem_genericRulesRemoved(t *testing.T) {
	p, _ := Get("security-architect")
	if contains(p.System, "Call tools immediately. Do not write") {
		t.Error("securityArchitectSystem still contains generic call-immediately rule (should be removed — covered by shared ToolUseBlock)")
	}
	if !contains(p.System, "LOCAL project or workspace") {
		t.Error("securityArchitectSystem must keep LOCAL/EXTERNAL tool selection guidance")
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```
go test ./internal/persona/... -run "TestToolUseBlock|TestGeneralSystem|TestSecuritySystem|TestSecurityArchitect" -v
```

Expected: FAIL — `ToolUseBlock` undefined; string-content checks may also fail.

- [ ] **Step 3: Add toolUseBlock constant and ToolUseBlock() function to persona.go**

In `internal/persona/persona.go`, after the `completingTasksBlock` constant and `CompletingTasksBlock()` function (around line 503), insert:

```go
const toolUseBlock = `## Tool use
- When any task step requires inspecting files, running commands, searching, or
  fetching URLs: call the appropriate tool IMMEDIATELY. Do not write "I'll run...",
  "Let me check...", or any narration of intent — just call the tool.
- Never describe what a tool would return. Call it and use the actual output.
- Base every factual claim about the codebase, system state, or external data on
  tool output from this conversation, not prior knowledge.
- After tool results arrive, keep going: synthesize, analyze, or write the next
  step. A tool result is input to your work, not the final output — receiving one
  does not end the task.
- If a tool result is truncated, note the truncation and decide whether to re-call
  or proceed with an explicit caveat.`

// ToolUseBlock returns the shared tool-use rules injected into every session.
func ToolUseBlock() string { return toolUseBlock }
```

- [ ] **Step 4: Trim generalSystem — remove bullets 1–3, keep bullet 4**

In `internal/persona/persona.go`, in the `generalSystem` constant, replace:

```
## Tool use — MUST follow these rules
- When the task requires inspecting files, running commands, searching, or fetching URLs: call the appropriate tool IMMEDIATELY. Do not write "I'll run...", "Let me check...", or any narration of intent — just call the tool.
- Never describe what a tool would return. Call it and use the actual output.
- Base every factual claim about the codebase, system state, or external data on tool output from this conversation, not prior knowledge.
- Persist durable facts with the remember tool and reusable procedures with save_skill.
```

With:

```
## Tool use
- Persist durable facts with the remember tool and reusable procedures with save_skill.
```

- [ ] **Step 5: Trim securitySystem — remove bullets 3–5, keep bullets 1–2**

In `internal/persona/persona.go`, in the `securitySystem` constant, replace:

```
## Tool use — MUST follow these rules
- When the task involves the LOCAL project or workspace: call shell (Get-ChildItem,
  Get-Content, Select-String on Windows; ls/cat/grep on Unix), read_file, glob, and
  grep IMMEDIATELY to read actual files. Do not rely on prior knowledge.
- Use web_search/web_fetch only for EXTERNAL references: CVE databases, security
  advisories, NIST/OWASP frameworks. Do NOT web-search for the local project itself.
- Never narrate intent ("I'll explore...", "I'll check...") — call the tool, then
  report what you found.
- Never describe what a tool would return. Call it and use the actual output.
- Base every factual claim about the codebase on tool output from this conversation.
```

With:

```
## Tool use
- When the task involves the LOCAL project or workspace: call shell (Get-ChildItem,
  Get-Content, Select-String on Windows; ls/cat/grep on Unix), read_file, glob, and
  grep IMMEDIATELY to read actual files. Do not rely on prior knowledge.
- Use web_search/web_fetch only for EXTERNAL references: CVE databases, security
  advisories, NIST/OWASP frameworks. Do NOT web-search for the local project itself.
```

- [ ] **Step 6: Trim securityArchitectSystem — remove bullets 3–5, keep bullets 1–2**

In `internal/persona/persona.go`, in the `securityArchitectSystem` constant, replace:

```
## Tool use — MUST follow these rules
- When the task involves the LOCAL project or workspace: call shell (Get-ChildItem,
  Get-Content, Select-String on Windows; ls/cat/grep on Unix), read_file, glob, and
  grep IMMEDIATELY to read actual files. Do not rely on prior knowledge or web searches
  to understand the system being analyzed.
- Use web_search/web_fetch only for EXTERNAL references: CVE databases, NIST/OWASP
  documentation, security advisories, protocol specs. Do NOT web-search the local project.
- Call tools immediately. Do not write "I'll explore..." or "Let me check..." — just
  call the tool, then report what you found.
- Never describe what a tool would return. Call it and use the actual output.
- Base every factual claim about the codebase on tool output from this conversation.
```

With:

```
## Tool use
- When the task involves the LOCAL project or workspace: call shell (Get-ChildItem,
  Get-Content, Select-String on Windows; ls/cat/grep on Unix), read_file, glob, and
  grep IMMEDIATELY to read actual files. Do not rely on prior knowledge or web searches
  to understand the system being analyzed.
- Use web_search/web_fetch only for EXTERNAL references: CVE databases, NIST/OWASP
  documentation, security advisories, protocol specs. Do NOT web-search the local project.
```

- [ ] **Step 7: Run tests — confirm they pass**

```
go test ./internal/persona/... -v
```

Expected: all tests PASS including the four new ones.

- [ ] **Step 8: Verify build**

```
go build ./...
```

Expected: no errors.

- [ ] **Step 9: Commit**

```
git add internal/persona/persona.go internal/persona/persona_test.go
git commit -m "feat(persona): add ToolUseBlock and rationalize inline tool-use rules"
```

---

### Task 2: Add ## Completing your output sections to all 17 persona constants

**Files:**
- Modify: `internal/persona/persona.go`
- Modify: `internal/persona/persona_test.go`

**Interfaces:**
- Consumes: `Get(name string) (Persona, bool)`, `Names() []string` — existing exported functions in persona.go

- [ ] **Step 1: Add failing test to persona_test.go**

Append to `internal/persona/persona_test.go`:

```go
func TestPersonas_allHaveCompletingOutput(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok {
			t.Errorf("persona.Get(%q) returned false", name)
			continue
		}
		if !contains(p.System, "## Completing your output") {
			t.Errorf("persona %q missing ## Completing your output section", name)
		}
	}
}
```

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/persona/... -run TestPersonas_allHaveCompletingOutput -v
```

Expected: FAIL — all 17 personas missing `## Completing your output`.

- [ ] **Step 3: Append completion section to generalSystem**

At the end of the `generalSystem` raw string literal (before its closing backtick), append:

```

## Completing your output
After tools return results, synthesize into a direct answer and complete all parts
of the request. Do not stop after a tool call — the result is input to your work,
not the final response.`
```

- [ ] **Step 4: Append completion section to securitySystem**

```

## Completing your output
Each identified issue must include severity, location, impact, and a concrete
remediation step. Do not stop after listing raw scanner output — validate findings,
remove false positives, and add issues scanners miss. For threat models, populate
every STRIDE/LINDDUN cell before the task is done.`
```

- [ ] **Step 5: Append completion section to platformArchitectSystem**

```

## Completing your output
Produce complete ADRs with every section populated (context, decision, consequences).
Render full Mermaid diagrams — do not stop at a placeholder. For technology
evaluations, include an explicit trade-off comparison and a recommendation.`
```

- [ ] **Step 6: Append completion section to securityArchitectSystem**

```

## Completing your output
When producing a threat model or security review: write the full document to a file
via write_file with every section populated. Do not stop after writing an outline —
complete every finding, mitigation, and residual-risk entry before considering the
task done.`
```

- [ ] **Step 7: Append completion section to securityEngineerSystem**

```

## Completing your output
After running security_scan, triage every finding: validate exploitability, remove
false positives, and report each confirmed issue with severity, location, and
remediation. Do not stop after listing raw scanner output — the assessment is not
complete until findings are evaluated.`
```

- [ ] **Step 8: Append completion section to appSecEngineerSystem**

```

## Completing your output
For each vulnerability, report the attack scenario, affected location (file:line),
evidence, and remediation with corrected code. Do not stop at "this function is
vulnerable" — complete every finding before moving on.`
```

- [ ] **Step 9: Append completion section to developerSystem**

```

## Completing your output
After writing or modifying code, run the relevant build or test command to verify
correctness. Do not stop after writing code — confirm it compiles and tests pass.
For bug fixes, verify the specific failure case no longer reproduces.`
```

- [ ] **Step 10: Append completion section to securityResearcherSystem**

```

## Completing your output
After research tools return results, synthesize into structured output: root cause,
reproduction steps, impact, and mitigations. Cite every source with URL or CVE ID.
Do not stop at raw search results — produce the analysis.`
```

- [ ] **Step 11: Append completion section to riskAssessorSystem**

```

## Completing your output
Produce the complete risk register: every row must include risk description,
likelihood, impact, risk rating, existing controls, residual risk, treatment option,
and owner. Do not stop after listing risks — populate every column before the task
is done.`
```

- [ ] **Step 12: Append completion section to businessAnalystSystem**

```

## Completing your output
Every user story must have acceptance criteria. Every process diagram must be
rendered in Mermaid. Every recommendation must be grounded in stated requirements
or data. Do not stop at headings — populate every section.`
```

- [ ] **Step 13: Append completion section to dataAnalystSystem**

```

## Completing your output
After reading data files, produce the full analysis: summary statistics, key
findings, and rendered visualizations (Mermaid charts or code blocks). Do not stop
after describing the dataset — the analysis is not done until findings and
interpretation are written.`
```

- [ ] **Step 14: Append completion section to networkSecurityArchitectSystem**

```

## Completing your output
Produce complete Mermaid network diagrams with annotated trust zones and labeled
interfaces. Specify exact configurations: firewall rules with ports/protocols, ACL
entries, TLS versions. Do not stop at a high-level description — provide the
complete design.`
```

- [ ] **Step 15: Append completion section to reportWriterSystem**

```

## Completing your output
Write the complete report to a file via write_file — every section from executive
summary through appendices, fully populated. Do not stop at an outline or list of
headings. Return only the file path and a one-paragraph summary in chat after the
full document is written.`
```

- [ ] **Step 16: Append completion section to sreSystem**

```

## Completing your output
For incident investigations or post-mortems: read actual metrics, logs, or runbook
files before recommending. Produce complete runbooks with every step written out.
For SLO analysis, include the exact SLI formula and error budget calculation. Do
not stop after diagnosing — write the complete output.`
```

- [ ] **Step 17: Append completion section to infrastructureArchitectSystem**

```

## Completing your output
Produce complete IaC designs: module structure, input variables, outputs, and state
management. Render full Mermaid diagrams. Write complete operational runbooks. Do
not stop at a bullet-point architecture overview — deliver the full design artifact.`
```

- [ ] **Step 18: Append completion section to cloudArchitectSystem**

```

## Completing your output
For architecture designs, render the complete Mermaid C4 or cloud diagram. For
migration plans, cover every phase: assessment, wave planning, cutover, and
validation. For cost analysis, include current vs. projected spend with specific
right-sizing recommendations. Do not stop at a summary — complete the deliverable.`
```

- [ ] **Step 19: Append completion section to cloudSecurityEngineerSystem**

```

## Completing your output
After posture assessment, produce exact corrected configurations: IAM policy JSON,
resource policy blocks, or CLI remediation commands. Do not stop at "this resource
is misconfigured" — provide the specific corrected configuration for each finding.`
```

- [ ] **Step 20: Run tests — confirm all pass**

```
go test ./internal/persona/... -v
```

Expected: all tests PASS including `TestPersonas_allHaveCompletingOutput`.

- [ ] **Step 21: Verify build**

```
go build ./...
```

Expected: no errors.

- [ ] **Step 22: Commit**

```
git add internal/persona/persona.go internal/persona/persona_test.go
git commit -m "feat(persona): add Completing your output section to all 17 personas"
```

---

### Task 3: Inject ToolUseBlock in server.go and chat.go

**Files:**
- Modify: `internal/server/server.go` (function `effectiveSystem`, ~line 1164)
- Modify: `internal/cli/chat.go` (~line 115)
- Modify: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `persona.ToolUseBlock() string` — defined in Task 1

- [ ] **Step 1: Add failing test to server_test.go**

`server_test.go` is `package server` so it can access unexported `Server` fields. `strings` and `memory` are already imported. Append this test function:

```go
func TestEffectiveSystem_containsToolUseBlock(t *testing.T) {
	s := &Server{
		memory:    memory.Sources{},
		workspace: "",
	}
	out := s.effectiveSystem("base-system")

	if !strings.Contains(out, "A tool result is input") {
		t.Error("effectiveSystem output missing ToolUseBlock content")
	}
	// ToolUseBlock must appear before CompletingTasksBlock
	tuIdx := strings.Index(out, "A tool result is input")
	ctIdx := strings.Index(out, "## Completing tasks")
	if tuIdx == -1 || ctIdx == -1 {
		t.Fatalf("missing expected block markers: tuIdx=%d ctIdx=%d", tuIdx, ctIdx)
	}
	if tuIdx > ctIdx {
		t.Error("ToolUseBlock must appear before CompletingTasksBlock in effectiveSystem output")
	}
}
```

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/server/... -run TestEffectiveSystem_containsToolUseBlock -v
```

Expected: FAIL — "effectiveSystem output missing ToolUseBlock content".

- [ ] **Step 3: Inject ToolUseBlock in server.go effectiveSystem**

In `internal/server/server.go`, in `effectiveSystem` (~line 1169), replace:

```go
	parts = append(parts, persona.CompletingTasksBlock())
```

With:

```go
	parts = append(parts, persona.ToolUseBlock())
	parts = append(parts, persona.CompletingTasksBlock())
```

- [ ] **Step 4: Inject ToolUseBlock in chat.go**

In `internal/cli/chat.go` (~line 115), replace:

```go
			resolvedSystem = resolvedSystem + "\n\n" + persona.CompletingTasksBlock()
```

With:

```go
			resolvedSystem = resolvedSystem + "\n\n" + persona.ToolUseBlock()
			resolvedSystem = resolvedSystem + "\n\n" + persona.CompletingTasksBlock()
```

- [ ] **Step 5: Run targeted test — confirm it passes**

```
go test ./internal/server/... -run TestEffectiveSystem_containsToolUseBlock -v
```

Expected: PASS.

- [ ] **Step 6: Run full test suite**

```
go test ./...
```

Expected: all tests pass with no regressions.

- [ ] **Step 7: Verify build**

```
go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```
git add internal/server/server.go internal/cli/chat.go internal/server/server_test.go
git commit -m "feat(engine): inject ToolUseBlock into every session's system prompt"
```
