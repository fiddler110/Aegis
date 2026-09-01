// Package persona provides named system prompts that shape the agent's
// behavior for different roles (general assistant, security architect, etc.).
package persona

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// Persona is a named behavioral profile. The override fields are populated by
// file-loaded personas; built-in personas leave them zero-valued.
type Persona struct {
	Name        string
	Description string
	System      string
	Model       string       // model id override (same provider); "" = global
	Mode        string       // permission mode; "" = session/config default
	Tools       []string     // advisory tool list; see PersonaToolGate (never a hard restriction unless ToolsEnforced)
	Rules       []string     // permission rules merged into the session gate
	Guard       *GuardConfig // nil = global default; Disabled = no guard
	// ToolsEnforced opts a persona into treating its Tools list as a hard
	// containment boundary (P81.20/FIND-20 item 4) instead of the P7.5
	// advisory default: a call to a tool outside the list is refused outright
	// by PersonaToolGate rather than routed through the approver's warn/prompt
	// path. Honoring this for a *loaded* (untrusted) persona file does not
	// violate P7.5's "content never gains enforcement teeth" rule the way an
	// honored Mode or an honored allow-Rule would — enforcing mode can only
	// narrow what a session may call, never widen it, so it is gated the same
	// way filterPersonaRules keeps a loaded persona's deny rules: restricting
	// is safe to honor from untrusted content, granting is not. Still gated by
	// honorControlFields like every other frontmatter control field, so
	// P27.7/FIND-09's "advisory-only injected persona" posture is unaffected.
	ToolsEnforced bool

	// Loaded is true for a persona parsed from a *.md file (user/project
	// personas dir, including ones installed by a bundle) rather than one of
	// the built-ins compiled into this package. The permission gate uses this
	// to decide whether the persona's Mode can be trusted implicitly (P7.5):
	// a bundle-installed persona file is less trusted than code reviewed and
	// shipped with Aegis, so its Mode should not silently escalate a session
	// past the configured default.
	Loaded bool
	// Path is the source file for a Loaded persona ("" for built-ins), so
	// tooling can point the user at the file to edit.
	Path string
}

// GuardConfig is a persona's output-validation override parsed from frontmatter.
type GuardConfig struct {
	Disabled   bool
	Mode       string   // "schema" | "llm"
	Schema     []string // schema mode: required top-level JSON keys
	Rubric     string   // llm mode rubric
	MaxRetries int
}

// The three shared blocks below — tool use, completing tasks, platform — are
// injected into every session regardless of persona, and each has a local
// variant selected by the *For functions (P62.9).
//
// The local variants say the same things in fewer words. That is the whole
// design rule and it is worth stating, because the tempting version of this
// change is to drop rules instead: every one of these rules was added after a
// real run went wrong (narrating instead of calling a tool, answering in chat
// instead of writing the requested file, a skeleton reported as finished, a
// PowerShell command written in bash), and what a small model loses when a rule
// is *removed* is exactly the question no unit test can answer. So nothing is
// removed here except genuine duplication across the three blocks, and a test
// asserts every rule still has a phrase in the local text.
//
// Measured 2026-08-14, windows/amd64: 1,001 estimated tokens across the three
// default blocks against 581 across the three local ones — 20.4% of the
// local-profile base prompt down to 13.5% of a smaller total. Windows is the
// largest case; the platform block is much smaller on darwin/linux under both
// profiles.

// PlatformBlock returns a system-prompt section describing the execution
// environment so the model generates correct shell commands for the current OS.
// It is appended to every session's effective system prompt regardless of persona.
func PlatformBlock() string { return PlatformBlockFor(false) }

// PlatformBlockFor returns the platform block for the active prompt profile.
//
// The local variant folds the Windows command table onto one line per group and
// drops the trailing "call the tool immediately" sentence, which the tool-use
// block's first rule already carries — the duplication is the single largest
// saving here and costs nothing, since both blocks are always injected together.
func PlatformBlockFor(local bool) string {
	if local {
		return localPlatformBlock()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Execution Environment\nOS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	switch runtime.GOOS {
	case "windows":
		b.WriteString(`Shell: PowerShell (powershell -NoProfile -NonInteractive -Command ...)

IMPORTANT — you are running on Windows. Every shell command MUST be valid PowerShell.
Unix commands (ls, cat, grep, find, rm, chmod, echo, which, etc.) do NOT exist in
PowerShell and will fail. Use their PowerShell equivalents:

  ls / dir     → Get-ChildItem (or gci)
  cat          → Get-Content
  grep         → Select-String
  find         → Get-ChildItem -Recurse -Filter
  rm           → Remove-Item
  rm -rf       → Remove-Item -Recurse -Force
  cp           → Copy-Item
  mv           → Move-Item
  mkdir        → New-Item -ItemType Directory
  which cmd    → (Get-Command cmd).Source
  $VAR         → $env:VAR
  echo text    → Write-Output "text"

Paths: forward-slash (/) and backslash (\) are both valid in PowerShell.
Absolute paths use Windows drive letters: C:\Users\...`)
	case "darwin":
		b.WriteString("Shell: /bin/sh (bash-compatible)\nUse standard Unix/POSIX shell commands and forward-slash paths.")
	default:
		b.WriteString("Shell: /bin/sh\nUse standard Unix/POSIX shell commands and forward-slash paths.")
	}
	b.WriteString("\n\nWhen a task requires running a command, reading a file, searching, or fetching a URL — call the tool immediately. Do not narrate \"I will run...\" or describe what you are about to do before calling the tool; just call it.")
	return b.String()
}

// localPlatformBlock is PlatformBlockFor(true). Same OS facts, same command
// mapping, no worked examples and no duplicated call-the-tool rule.
func localPlatformBlock() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Execution Environment\nOS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	switch runtime.GOOS {
	case "windows":
		b.WriteString(`Shell: PowerShell. Every shell command MUST be valid PowerShell — Unix commands (ls, cat, grep, find, rm, chmod, echo, which) do NOT exist and will fail. Use instead:
Get-ChildItem (ls; -Recurse -Filter for find), Get-Content (cat), Select-String (grep), Remove-Item (rm; add -Recurse -Force for rm -rf), Copy-Item (cp), Move-Item (mv), New-Item -ItemType Directory (mkdir), (Get-Command x).Source (which), $env:VAR ($VAR), Write-Output (echo).
Paths: / and \ both work; absolute paths use drive letters (C:\Users\...).`)
	case "darwin":
		b.WriteString("Shell: /bin/sh (bash-compatible). Use standard Unix/POSIX commands and forward-slash paths.")
	default:
		b.WriteString("Shell: /bin/sh. Use standard Unix/POSIX commands and forward-slash paths.")
	}
	return b.String()
}

// completingTasksBlock is injected into every session regardless of persona.
// It is kept here so the CLI and server paths share a single source of truth.
const completingTasksBlock = `## Completing tasks
- When the user gives you a compound instruction (e.g. answer a question AND save the result to a file), complete ALL parts. Do not stop after the first part.
- When the user asks you to write output to a specific file or path, call write_file with that path. A chat response is not a substitute for the requested file — use the tool, then confirm the path and what was written.
- When the user asks you to produce a document, report, review, or structured output without naming a file, STILL call write_file. Default to a sensible filename in the current working directory (e.g. "review.md", "security-review.md", "report.md", "analysis.md"). Do not return the document only as a chat message — write the file first, then confirm the path and what was written.
- Writing a skeleton or outline is NOT completing the task. Populate every section with real content before calling the task done.
- Do only what was explicitly asked. Do not add unrequested error handling, "robustness", or extra features. Do not create files (summaries, reports, notes) the user didn't ask for. Do not call remember to persist something to memory unless the user asked you to remember it.
- After completing a task — especially one that writes a file or makes a change — confirm what was done: state the action taken and the file path. Do not end with an open-ended "How can I help?" without first confirming the requested action was completed.
- If a tool result is truncated, note the truncation and decide whether you need the missing data before proceeding.
- If a tool returns "unknown tool" or any error, do NOT give up. Try the correct tool: use shell to run commands, read_file to read a file, glob to list files by pattern, grep to search content, write_file to write output. Explain what failed, then continue with an alternative approach.`

// localCompletingTasksBlock is CompletingTasksBlockFor(true): the same seven
// rules, compressed. The two the local text keeps closest to verbatim are the
// write_file ones, which are the rules the P25.x runs showed a small model
// dropping first.
const localCompletingTasksBlock = `## Completing tasks
- Complete EVERY part of a compound instruction, not just the first.
- Asked to write output to a path: call write_file with that path. Asked for a document, report, review or structured output with no path named: still call write_file, defaulting to a sensible name in the working directory (report.md, review.md, analysis.md). A chat response is never a substitute for the file.
- A skeleton or outline is not a completed task: fill every section with real content first.
- Do only what was explicitly asked. No unrequested files, features or error handling, and no remember call unless asked to remember something.
- Finish by stating what you did and the file path — not an open-ended "How can I help?".
- If a tool result is truncated, say so and decide whether you need the missing data before continuing.
- If a tool errors or is unknown, do NOT give up: switch to shell, read_file, glob, grep or write_file, say what failed, and keep going.`

// CompletingTasksBlock returns the shared task-completion rules that are
// appended to every session's effective system prompt regardless of persona.
func CompletingTasksBlock() string { return CompletingTasksBlockFor(false) }

// CompletingTasksBlockFor returns the task-completion rules for the active
// prompt profile.
func CompletingTasksBlockFor(local bool) string {
	if local {
		return localCompletingTasksBlock
	}
	return completingTasksBlock
}

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
  or proceed with an explicit caveat.
- For a task scoped to files in this repo, prefer local tools (read_file, grep,
  glob, shell) over network tools (web_search, web_fetch). Only reach for a
  network tool when the task actually needs information from outside this repo.`

// localToolUseBlock is ToolUseBlockFor(true). The truncation rule is the one
// that shrinks most, because the completing-tasks block states it too and the
// two blocks are always injected together.
const localToolUseBlock = `## Tool use
- When a step needs a file read, a command run, a search or a URL fetched: call the tool IMMEDIATELY. Never write "I'll run...", "Let me check...", or any narration of intent, and never describe what a tool would return — call it and use the real output.
- Base every factual claim about the codebase, system state or external data on tool output from this conversation, not prior knowledge.
- A tool result is input to your work, not the end of it: after results arrive, keep going.
- If a result is truncated, note it, then re-call or proceed with an explicit caveat.
- For a task scoped to files in this repo, prefer local tools (read_file, grep, glob, shell) over network tools (web_search, web_fetch); reach outside the repo only when the task actually needs it.`

// ToolUseBlock returns the shared tool-use rules injected into every session.
func ToolUseBlock() string { return ToolUseBlockFor(false) }

// ToolUseBlockFor returns the shared tool-use rules for the active prompt
// profile.
func ToolUseBlockFor(local bool) string {
	if local {
		return localToolUseBlock
	}
	return toolUseBlock
}

// builtins and builtinOrder are populated at package init from the embedded
// internal/persona/builtin/*.md files — see builtin.go. Declaring Tools in a
// persona's frontmatter makes its tool use advisory-checked: a call to a tool
// outside the list is logged and routed through the same approval flow as
// capability decisions — warn-and-allow under a non-interactive approver, a
// confirmation prompt under an interactive one (permission.PersonaToolGate)
// — never a hard block. The "general" persona deliberately declares no Tools
// (no restriction, no advisory) since it has no specific focus to check tool
// use against.

// mu guards loaded, loadedOrder, and refreshSig. builtins and builtinOrder are
// immutable after init and need no locking.
var (
	mu          sync.RWMutex
	loaded      = map[string]Persona{}
	loadedOrder []string
	refreshSig  string
)

// Get returns the persona by name, falling back to the general persona for an
// empty or unknown name. The boolean reports whether the name was recognized.
// File-loaded personas shadow built-ins of the same name.
func Get(name string) (Persona, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return builtins["general"], true
	}
	mu.RLock()
	p, ok := loaded[name]
	mu.RUnlock()
	if ok {
		return p, true
	}
	if p, ok := builtins[name]; ok {
		return p, true
	}
	return builtins["general"], false
}

// Names returns the available persona names: built-ins in display order, then
// file-loaded personas in registration order (names shadowing a built-in are
// listed once).
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(builtinOrder)+len(loadedOrder))
	out = append(out, builtinOrder...)
	for _, n := range loadedOrder {
		if _, shadows := builtins[n]; !shadows {
			out = append(out, n)
		}
	}
	return out
}
