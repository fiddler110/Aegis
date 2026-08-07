package drive

import (
	"strings"
	"testing"
)

// terseTestParams mirrors the PhaseParams the other prompt tests build: a fresh
// context that knows only the workspace, the skill directory and the run dir.
func terseTestParams() PhaseParams {
	return PhaseParams{
		task:     "threat model this repo",
		skillDir: "/ws/.aegis/builtin-skills/threat-modeling",
		cwd:      "/ws",
		runDir:   "/ws/.aegis/security/threat-model/stride-app-2026-07-24-1200",
	}
}

// TestPromptsSuppressNarration guards the anti-narration instruction against
// silent drift. On the instrumented unattended threat-model drive, decode was
// ~71% of a 142-minute run and ~25% of the output tokens were narration prose —
// post-hoc "here is what I produced" recaps, summary tables and "Done."
// sign-offs that nobody reads under `--output-format stream-json`. Every prompt
// that seeds or continues a turn must carry the rule, especially the shared
// continuation prompt, which seeds the majority of a drive's turns and
// previously had no anti-narration clause at all.
func TestPromptsSuppressNarration(t *testing.T) {
	p := terseTestParams()
	carriers := map[string]string{
		"architecture": phasePromptArchitecture(p),
		"dfd":          phasePromptDFD(p),
		"analysis":     phasePromptAnalysis(p),
		"findings":     phasePromptFindings(p),
		"assessment":   phasePromptAssessment(p),
		"continue":     phaseContinuePrompt(ThreatModelPhases[2], []string{"2-stride-analysis.md"}),
		"hollow":       hollowBodyReentryPrompt(ThreatModelPhases[3], p.runDir, p.skillDir, "FAIL finding-bodies-nonempty\n  3-findings.md:12: empty body"),
		"phase6":       phase6TurnPrompt(p.runDir, p.skillDir, "Fix the failing checks.", false),
	}
	for name, prompt := range carriers {
		if !strings.Contains(prompt, terseOutputInstruction) {
			t.Errorf("%s prompt must carry the terse-output instruction", name)
		}
	}
}

// TestTerseOutputInstructionForbidsPostHocSummary pins the instruction's
// substance. The earlier per-prompt wording ("do not describe what you will do;
// do it") was pre-hoc only, and the measured waste was dominated by summaries
// emitted *after* the work was already on disk — so the constant must name the
// post-hoc case, not just brevity in general.
func TestTerseOutputInstructionForbidsPostHocSummary(t *testing.T) {
	low := strings.ToLower(terseOutputInstruction)
	for _, want := range []string{"summary of what you wrote", "recap table", "sign-off", "end the turn"} {
		if !strings.Contains(low, want) {
			t.Errorf("terseOutputInstruction should forbid %q", want)
		}
	}
	// It is prepended to every turn of a long drive, so its own token cost is
	// part of the trade — keep it short enough to stay worth carrying.
	if n := len(terseOutputInstruction); n > 400 {
		t.Errorf("terseOutputInstruction is %d chars; it rides every turn, keep it tight", n)
	}
}

// TestPhasePromptsDoNotStackNarrationRules checks the consolidation: the seeds
// that used to carry their own weaker wording ("do not describe what you will
// do; do it", "do it, do not narrate") must now express the rule exactly once,
// via the shared constant, rather than stacking overlapping sentences.
func TestPhasePromptsDoNotStackNarrationRules(t *testing.T) {
	p := terseTestParams()
	for name, prompt := range map[string]string{
		"architecture": phasePromptArchitecture(p),
		"dfd":          phasePromptDFD(p),
	} {
		low := strings.ToLower(prompt)
		for _, stale := range []string{"do not describe what you will do", "do it, do not narrate"} {
			if strings.Contains(low, stale) {
				t.Errorf("%s prompt still carries the superseded wording %q alongside terseOutputInstruction", name, stale)
			}
		}
		if strings.Count(prompt, terseOutputInstruction) != 1 {
			t.Errorf("%s prompt must state the terseness rule exactly once", name)
		}
		// The behavioral instruction the old wording shared the sentence with
		// must survive the consolidation.
		if !strings.Contains(low, "non-interactively") {
			t.Errorf("%s prompt lost its non-interactive instruction", name)
		}
	}
}
