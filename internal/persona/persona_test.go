package persona

import "testing"

func TestGet(t *testing.T) {
	if p, ok := Get("security"); !ok || p.Name != "security" {
		t.Errorf("security persona not found: %+v ok=%v", p, ok)
	}
	if p, ok := Get(""); !ok || p.Name != "general" {
		t.Errorf("empty should default to general, got %+v ok=%v", p, ok)
	}
	if p, ok := Get("nonexistent"); ok || p.Name != "general" {
		t.Errorf("unknown should fall back to general and report not-ok, got %+v ok=%v", p, ok)
	}
}

// TestBuiltinPersonasAreNotMarkedLoaded guards the P7.5 trust boundary: only
// personas parsed from a *.md file (LoadFromDirs) are Loaded=true. Built-ins
// must stay Loaded=false since callers use that flag to decide whether a
// persona's Mode can be trusted to implicitly set a session's permission mode.
func TestBuiltinPersonasAreNotMarkedLoaded(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok {
			continue
		}
		if p.Loaded {
			t.Errorf("built-in persona %q unexpectedly has Loaded=true", name)
		}
	}
}

func TestAllRegisteredPersonasResolvable(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok {
			t.Errorf("persona %q listed in Names() but not found in registry", name)
		}
		if p.Name != name {
			t.Errorf("persona %q has mismatched Name field: %q", name, p.Name)
		}
		if p.Description == "" {
			t.Errorf("persona %q has empty Description", name)
		}
		if p.System == "" {
			t.Errorf("persona %q has empty System prompt", name)
		}
	}
}

func TestNamesMatchesRegistry(t *testing.T) {
	names := Names()
	if len(names) < len(builtins) {
		t.Errorf("Names() has %d entries but there are %d built-ins", len(names), len(builtins))
	}
	seen := make(map[string]bool)
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate name in Names(): %q", n)
		}
		seen[n] = true
	}
}

func TestSecurityPromptCoversModes(t *testing.T) {
	p, _ := Get("security")
	for _, want := range []string{"STRIDE", "THREAT MODELING", "ISSUE IDENTIFICATION", "security_scan", "Mermaid"} {
		if !contains(p.System, want) {
			t.Errorf("security prompt missing %q", want)
		}
	}
}

func TestPersonaPromptKeywords(t *testing.T) {
	tests := []struct {
		name     string
		keywords []string
	}{
		{"platform-architect", []string{"PLATFORM ARCHITECT", "ARCHITECTURE DESIGN", "CAPACITY", "THREAT MODELING", "PROOF OF CONCEPT", "AUTOMATION DEVELOPMENT", "ROADMAP PLANNING", "PROCESS DEVELOPMENT", "DOCUMENTATION & REPORTING"}},
		{"security-architect", []string{"SECURITY ARCHITECT", "STRIDE", "THREAT MODELING"}},
		{"security-engineer", []string{"SECURITY ENGINEER", "VULNERABILITY MANAGEMENT", "INCIDENT RESPONSE"}},
		{"appsec-engineer", []string{"APPLICATION SECURITY", "OWASP", "SECURE CODE REVIEW"}},
		{"developer", []string{"SOFTWARE DEVELOPER", "DEBUGGING", "TESTING"}},
		{"security-researcher", []string{"SECURITY RESEARCHER", "MITRE ATT&CK", "VULNERABILITY RESEARCH"}},
		{"risk-assessor", []string{"RISK ASSESSOR", "RISK IDENTIFICATION", "RISK TREATMENT"}},
		{"business-analyst", []string{"BUSINESS ANALYST", "REQUIREMENTS", "STAKEHOLDER"}},
		{"data-analyst", []string{"DATA ANALYST", "STATISTICAL", "VISUALIZATION"}},
		{"network-security-architect", []string{"NETWORK SECURITY ARCHITECT", "micro-segmentation", "zero-trust"}},
		{"report-writer", []string{"REPORT WRITER", "executive summary", "TECHNICAL WRITING"}},
		{"sre", []string{"SITE RELIABILITY ENGINEER", "SLO", "OBSERVABILITY", "INCIDENT"}},
		{"infrastructure-architect", []string{"INFRASTRUCTURE ARCHITECT", "INFRASTRUCTURE AS CODE", "Terraform"}},
		{"cloud-architect", []string{"CLOUD ARCHITECT", "well-architected", "CLOUD MIGRATION"}},
		{"cloud-security-engineer", []string{"CLOUD SECURITY ENGINEER", "IAM", "CIS Benchmarks", "GuardDuty"}},
	}
	for _, tt := range tests {
		p, ok := Get(tt.name)
		if !ok {
			t.Errorf("persona %q not found", tt.name)
			continue
		}
		for _, kw := range tt.keywords {
			if !contains(p.System, kw) {
				t.Errorf("persona %q prompt missing keyword %q", tt.name, kw)
			}
		}
	}
}

// profiles is the pair every shared-block test runs over: the local variants
// (P62.9) say the same things in fewer words, so a rule the default block
// carries and the local one does not is a rule that was silently dropped in a
// token-cutting pass rather than removed on purpose.
var profiles = []struct {
	name  string
	local bool
}{{"default", false}, {"local", true}}

func TestToolUseBlock_content(t *testing.T) {
	for _, p := range profiles {
		block := ToolUseBlockFor(p.local)
		for _, want := range []string{
			"IMMEDIATELY",
			"narration of intent",
			"A tool result is input",
			"truncated",
		} {
			if !contains(block, want) {
				t.Errorf("%s ToolUseBlock missing expected phrase: %q", p.name, want)
			}
		}
	}
}

// TestToolUseBlock_preferLocalOverNetwork covers P25.6(b) rule 1: the shared
// ToolUseBlock, injected into every session regardless of persona or prompt
// profile, must steer the model toward local file tools over network tools
// for file-scoped tasks.
func TestToolUseBlock_preferLocalOverNetwork(t *testing.T) {
	for _, p := range profiles {
		block := ToolUseBlockFor(p.local)
		for _, want := range []string{"read_file", "grep", "glob", "web_search", "web_fetch"} {
			if !contains(block, want) {
				t.Errorf("%s ToolUseBlock missing expected local-vs-network tool name: %q", p.name, want)
			}
		}
		if !contains(block, "prefer local tools") {
			t.Errorf("%s ToolUseBlock missing the prefer-local-over-network guidance", p.name)
		}
	}
}

// TestCompletingTasksBlock_noScopeCreep covers P25.6(b) rule 2: the shared
// CompletingTasksBlock, injected into every session regardless of persona or
// prompt profile, must instruct the model not to write unrequested files,
// call remember unprompted, or add unrequested features/robustness — the
// qwen3coder:30b over-delivery behavior the roadmap item calls out.
func TestCompletingTasksBlock_noScopeCreep(t *testing.T) {
	for _, p := range profiles {
		block := CompletingTasksBlockFor(p.local)
		for _, want := range []string{
			"only what was explicitly asked",
			"remember",
			"unrequested",
		} {
			if !contains(block, want) {
				t.Errorf("%s CompletingTasksBlock missing expected phrase: %q", p.name, want)
			}
		}
	}
}

// TestLocalBlocksKeepEveryRule is the assertion behind P62.9's claim that the
// local variants compress rather than delete. Each entry is one rule the
// default blocks carry, with a phrase that must survive into the local text —
// the rules are not paraphrases of each other, and every one of them was added
// after a run went wrong, so a missing entry is a regression and not a saving.
//
// The one deliberate deletion is recorded here as an exception rather than
// left to inference: the default platform block ends by repeating the tool-use
// block's "call the tool immediately, do not narrate" rule, and the local
// platform block does not, because both blocks are always injected together.
func TestLocalBlocksKeepEveryRule(t *testing.T) {
	toolUse := []struct{ rule, phrase string }{
		{"call the tool immediately", "IMMEDIATELY"},
		{"no narration of intent", "narration of intent"},
		{"never describe what a tool would return", "describe what a tool would return"},
		{"ground claims in tool output", "not prior knowledge"},
		{"a tool result is not the end of the task", "A tool result is input"},
		{"handle truncation", "truncated"},
		{"prefer local tools over network tools", "prefer local tools"},
	}
	for _, c := range toolUse {
		if !contains(localToolUseBlock, c.phrase) {
			t.Errorf("local tool-use block dropped the %q rule (looked for %q)", c.rule, c.phrase)
		}
	}

	completing := []struct{ rule, phrase string }{
		{"finish compound instructions", "compound instruction"},
		{"write_file at the named path", "write_file with that path"},
		{"write_file even with no path named", "write_file"},
		{"an outline is not done", "outline"},
		{"no scope creep", "only what was explicitly asked"},
		{"do not persist to memory unprompted", "remember"},
		{"confirm the action and the path", "file path"},
		{"handle truncation", "truncated"},
		{"a tool error is not a reason to stop", "do NOT give up"},
	}
	for _, c := range completing {
		if !contains(localCompletingTasksBlock, c.phrase) {
			t.Errorf("local completing-tasks block dropped the %q rule (looked for %q)", c.rule, c.phrase)
		}
	}

	// The platform block is generated per-GOOS, so assert the invariant that
	// holds on every platform: the OS/arch line and the shell are named, and
	// the duplicated call-the-tool sentence is gone.
	local := PlatformBlockFor(true)
	for _, want := range []string{"OS: ", "Shell: "} {
		if !contains(local, want) {
			t.Errorf("local platform block missing %q", want)
		}
	}
	if contains(local, "call the tool immediately") {
		t.Error("local platform block still duplicates the tool-use block's call-the-tool rule")
	}
	if !contains(PlatformBlock(), "call the tool immediately") {
		t.Error("default platform block lost the rule the local one deliberately drops — the exception above is now describing nothing")
	}
}

// The local variants have to actually be smaller, or they are three more
// strings to keep in sync for nothing. This is a direction check, not a budget:
// internal/server's localBasePromptCeilingTokens holds the number.
func TestLocalBlocksAreSmaller(t *testing.T) {
	cases := []struct {
		name             string
		def, local       string
		minSavedFraction float64
	}{
		{"tool-use", ToolUseBlock(), ToolUseBlockFor(true), 0.15},
		{"completing-tasks", CompletingTasksBlock(), CompletingTasksBlockFor(true), 0.3},
		{"platform", PlatformBlock(), PlatformBlockFor(true), 0.15},
	}
	for _, c := range cases {
		saved := float64(len(c.def)-len(c.local)) / float64(len(c.def))
		if saved < c.minSavedFraction {
			t.Errorf("local %s block saves %.0f%% (%d → %d bytes), want at least %.0f%%",
				c.name, saved*100, len(c.def), len(c.local), c.minSavedFraction*100)
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

// knownToolNames mirrors the tool names registered by internal/tool/builtin
// (see Register in builtin.go). Kept as a literal here rather than importing
// tool/builtin — which would need live LSP/cron/task stores wired up just to
// enumerate names — so update this list alongside any built-in tool rename
// or addition.
var knownToolNames = map[string]bool{
	"read_file": true, "write_file": true, "edit_file": true, "multi_edit": true,
	"ls": true, "glob": true, "grep": true,
	"git": true, "git_commit": true, "git_pr": true,
	"shell": true, "web_fetch": true, "web_search": true, "list_models": true,
	"security_scan": true, "dast_scan": true, "recon_scan": true, "security_advise": true, "skill": true, "tool_search": true,
	"render_diagram": true, "latex_build": true, "latex_new_document": true,
	"remember": true, "save_skill": true,
	"task_create": true, "task_list": true, "task_get": true, "task_update": true, "task_output": true, "task_stop": true,
	"cron_create": true, "cron_list": true, "cron_delete": true, "cron_toggle": true,
	"definition": true, "references": true, "hover": true, "diagnostics": true,
	"document_symbols": true, "workspace_symbols": true, "call_hierarchy": true,
	"todo_add": true, "todo_list": true, "todo_update": true,
	"ask_user":          true,
	"project_knowledge": true,
	"entity_remember":   true, "entity_recall": true,
	"team_inbox": true, "team_send": true, "team_task_add": true, "team_task_claim": true, "team_task_complete": true, "team_task_list": true,
	"agent": true,
}

// TestBuiltinPersonaToolsAreKnown guards against typos or renames drifting a
// persona's declared Tools list out of sync with the real tool registry —
// the advisory gate (PersonaToolGate) would otherwise silently flag every
// call to a genuinely valid tool just because it was misspelled here.
func TestBuiltinPersonaToolsAreKnown(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok || p.Loaded {
			continue
		}
		for _, tl := range p.Tools {
			if !knownToolNames[tl] {
				t.Errorf("persona %q declares unknown tool %q", name, tl)
			}
		}
	}
}

// TestBuiltinPersonasDeclareTools ensures every built-in except "general" (the
// no-specific-focus fallback, left unrestricted by design) declares a Tools
// list, so the advisory gate has something meaningful to check.
func TestBuiltinPersonasDeclareTools(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok || p.Loaded {
			continue
		}
		if name == "general" {
			if len(p.Tools) != 0 {
				t.Errorf("general persona should stay unrestricted (empty Tools), got %v", p.Tools)
			}
			continue
		}
		if len(p.Tools) == 0 {
			t.Errorf("persona %q has no declared Tools", name)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
