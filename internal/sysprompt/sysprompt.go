// Package sysprompt holds the system-prompt blocks and budget caps that the
// daemon and the CLI must assemble identically.
//
// It was extracted for P66.13/QUAL-02, which found `buildChatSystem` claiming in
// its doc comment to be "equivalent to the daemon's effectiveSystem" while
// diverging in four ways — the load-bearing one being that it omitted
// <deferred_tools> entirely, so the 26 deferred tools the whole P62.6 line is
// about were undiscoverable via `tool_search` on the CLI path. That is a pure
// capability loss: the token saving of deferring them had already been banked,
// and nothing told the model they existed.
//
// The daemon still owns *assembly* — internal/server's promptSections carries
// the P67.2 stable/volatile split, which is a property of a long-lived session
// and means nothing to a one-shot process. What lives here is what both sides
// must agree on: the block renderers and the two local-profile byte caps.
package sysprompt

import (
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
)

// LocalRepoMapMaxBytes caps the repo map injected under the local prompt
// profile (P25.6): a large repo map is one of the heavier always-injected
// blocks, and small local models pay for it in prompt-processing latency on
// every turn regardless of whether the task touches most of the repo. The
// default profile never applies this cap.
const LocalRepoMapMaxBytes = 4000

// LocalContextFilesMaxBytes caps the project context files (AGENTS.md,
// CLAUDE.md, .aegis/context.md) injected under the local prompt profile
// (P66.7, LLM-01). Before this they were the one always-injected block with no
// bound at all, which is how the most carefully budgeted prompt in the project
// came to be blown by the file documenting the budget.
//
// The number is derived, not measured off one repository — LLM-01's 11,611
// tokens were this repo's CLAUDE.md at the time, and it reads 2,560 tokens
// today, so sizing against it would be sizing against a number that moves.
// The derivation:
//
//   - The served window under the documented local configuration is 32,768
//     (docs/providers.md pins num_ctx in a Modelfile). The always-injected
//     prefix should fit in a quarter of it — 8,192 tokens — so the three
//     quarters left for the transcript are what the LLM-16 50%-of-window
//     notice is spent reporting on, rather than the prefix tripping it alone.
//   - Of that quarter, localBasePromptCeilingTokens already claims 4,550
//     (persona blocks + <deferred_tools> + tool schemas) and LocalRepoMapMaxBytes
//     another ~1,000. That leaves ~2,600 tokens ≈ 10,400 bytes.
//   - 8,000 bytes (~2,000 tokens at tokenest's ASCII rate) takes that with
//     room to spare, and lands on exactly twice LocalRepoMapMaxBytes — which
//     is the ordering worth stating: hand-written project instructions are
//     worth more per byte than a generated repo map, so they get double its
//     room, while still staying under half the base-prompt ceiling.
//
// Nothing here rescues Ollama's *default* 4,096-token window: the base prompt
// alone exceeds it, which is a fact about the default, not about this cap. The
// cap's job is to stop context files from being the multiplier on top.
//
// The default profile never applies this cap, exactly as with the repo map.
const LocalContextFilesMaxBytes = 8000

// ContextFilesBudget is the byte cap to read project context files under, given
// the active prompt profile: the local cap, or 0 (uncapped) on the default
// profile. Both sides call this rather than branching on `local` themselves, so
// a change to the posture reaches both.
func ContextFilesBudget(local bool) int {
	if local {
		return LocalContextFilesMaxBytes
	}
	return 0
}

// RepoMapFits reports whether a rendered repo map may be injected under the
// active prompt profile.
//
// Note the posture difference from context files: an over-cap repo map is
// dropped whole, because it is generated, ranked and degrades gracefully to
// nothing. Context files are the project's instructions — dropping them whole
// would change how the session behaves, so they are truncated head-first with a
// notice instead (memory.LoadContextCapped).
func RepoMapFits(repoMap string, local bool) bool {
	if repoMap == "" {
		return false
	}
	return !local || len(repoMap) <= LocalRepoMapMaxBytes
}

// LocalPromptSuffixMaxTokens caps a per-model profile.Harness.PromptSuffix
// (P74.21) under the local prompt profile. It exists for the same reason
// LocalRepoMapMaxBytes and LocalContextFilesMaxBytes do: an always-injected
// block with no bound at all is how the most carefully budgeted prompt in the
// project gets blown, and unlike those two this one is authored by an
// operator in config.Provider.ModelHarness rather than generated, so there is
// no natural degrade-gracefully behavior to fall back on if it's oversized —
// providerfactory rejects the config outright instead (see FitsLocalBudget).
//
// 200 tokens is a paragraph, not a document: the cargo this item was filed
// for is a one- or two-line quirk note ("this model's tool calls use snake_
// case argument names"), not a persona rewrite — a suffix that needs more
// room than that belongs in a persona file, which already has its own budget
// story, not bolted onto every request through the harness.
const LocalPromptSuffixMaxTokens = 200

// FitsLocalBudget reports whether suffix is small enough to inject on every
// request under the local prompt profile. The default profile has no prompt
// budget to protect, so it always fits there.
func FitsLocalBudget(suffix string, local bool) bool {
	if !local || suffix == "" {
		return true
	}
	return tokenest.Estimate(suffix) <= LocalPromptSuffixMaxTokens
}

// DeferredToolsBlock advertises tools that are registered but not exposed by
// default (P4.6). The model loads them on demand with the tool_search tool.
//
// One line per tool, and that line is the tool's Summary rather than its full
// Description (P62.6). Printing the manuals made this block 2,953 tokens — 38%
// of the local profile's entire base prompt, spent on 26 tools that are *not
// loaded*, against 3,614 for the 27 that are. Deferral had stopped being a
// saving.
//
// Nothing is lost to discovery by shortening it, which is what makes this
// cheap: tool_search matches its query against the full Name+Description via
// Registry.SearchDeferred, and that text lives in the registry, not in the
// prompt. A scanner name or a synonym that no longer appears here still finds
// its tool, and the full description comes back with the schema on load.
func DeferredToolsBlock(reg *tool.Registry) string {
	if reg == nil {
		return ""
	}
	deferred := reg.Deferred()
	if len(deferred) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<deferred_tools>\n")
	sb.WriteString("These tools are not loaded yet. When a task needs one, call `tool_search` with keywords to load it before use.\n")
	for _, d := range deferred {
		line := fmt.Sprintf("- %s: %s", d.Name, d.Summary)
		if d.Keywords != "" {
			line += " [" + d.Keywords + "]"
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString("</deferred_tools>")
	return sb.String()
}

// DebateIntegrationBlock returns the P12.5 opt-in instruction text wiring the
// `agent` tool's debate mode into the two existing security workflows that
// benefit from adversarial review, or "" if neither toggle is enabled (the
// default — debate multiplies model calls per item, so this is never
// injected silently). Both toggles can be on independently; the block only
// mentions the ones actually enabled.
func DebateIntegrationBlock(cfg config.DebateIntegrationConfig) string {
	if !cfg.ThreatModel && !cfg.Triage {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Debate mode (P12)\n")
	if cfg.ThreatModel {
		b.WriteString("- Threat modeling: before writing an identified threat/mitigation pair into the threat model document, call the `agent` tool with mode:\"debate\" and claim set to that threat/mitigation pair. Adjust the entry's severity/mitigation per the arbiter's verdict before finalizing it.\n")
	}
	if cfg.Triage {
		b.WriteString("- Security-audit triage: before suppressing a borderline or disputed-severity scan finding via the baseline, call the `agent` tool with mode:\"debate\" and claim set to the finding (severity, location, rationale). Only suppress if the verdict upholds the low-risk assessment.\n")
	}
	return b.String()
}
