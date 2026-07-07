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
	Tools       []string     // advisory tool list; see PersonaToolGate (never a hard restriction)
	Rules       []string     // permission rules merged into the session gate
	Guard       *GuardConfig // nil = global default; Disabled = no guard
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

// PlatformBlock returns a system-prompt section describing the execution
// environment so the model generates correct shell commands for the current OS.
// It is appended to every session's effective system prompt regardless of persona.
func PlatformBlock() string {
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

// completingTasksBlock is injected into every session regardless of persona.
// It is kept here so the CLI and server paths share a single source of truth.
const completingTasksBlock = `## Completing tasks
- When the user gives you a compound instruction (e.g. answer a question AND save the result to a file), complete ALL parts. Do not stop after the first part.
- When the user asks you to write output to a specific file or path, call write_file with that path. A chat response is not a substitute for the requested file — use the tool, then confirm the path and what was written.
- When the user asks you to produce a document, report, review, or structured output without naming a file, STILL call write_file. Default to a sensible filename in the current working directory (e.g. "review.md", "security-review.md", "report.md", "analysis.md"). Do not return the document only as a chat message — write the file first, then confirm the path and what was written.
- Writing a skeleton or outline is NOT completing the task. Populate every section with real content before calling the task done.
- After completing a task — especially one that writes a file or makes a change — confirm what was done: state the action taken and the file path. Do not end with an open-ended "How can I help?" without first confirming the requested action was completed.
- If a tool result is truncated, note the truncation and decide whether you need the missing data before proceeding.
- If a tool returns "unknown tool" or any error, do NOT give up. Try the correct tool: use shell to run commands, read_file to read a file, glob to list files by pattern, grep to search content, write_file to write output. Explain what failed, then continue with an alternative approach.`

// CompletingTasksBlock returns the shared task-completion rules that are
// appended to every session's effective system prompt regardless of persona.
func CompletingTasksBlock() string { return completingTasksBlock }

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
