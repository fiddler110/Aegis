package drive

import "fmt"

// researchBudget derives the deep-research skill's round cap and source
// target from the run's resolved context window (P71.11), instead of the
// flat cloud-sized numbers ("round cap: 8", "5-12 quality sources") the
// skill used to hard-code in prose regardless of what model was running it.
//
// At context_window: 16000 — this project's own shipped local-profile
// default — the old numbers are arithmetically impossible: 8 rounds times
// 5-12 sources times web_fetch's own ~5,000-token default output is one to
// two orders of magnitude past a window whose compaction trigger sits at
// 8,000 tokens (tokenest.CompactionTrigger(16000, 8192)). A live run at that
// window opened with "Budget: 8 rounds max, targeting 5-12 quality
// sources" — copied faithfully from the skill — and never got close.
//
// window <= 0 means unresolved (a cloud adapter with nothing to report, or a
// caller with no engine to ask) and keeps today's cloud-scale numbers
// unchanged, the same "only ever shrinks a small window, never touches an
// unknown or large one" posture defaultFetchLimit already takes for
// web_fetch's own output cap.
func researchBudget(window int) (rounds, sourcesLow, sourcesHigh int) {
	switch {
	case window <= 0:
		return 8, 5, 12
	case window <= 16000:
		return 4, 3, 4
	case window <= 32000:
		return 5, 4, 6
	case window <= 64000:
		return 6, 4, 8
	default:
		return 8, 5, 12
	}
}

// researchBudgetLine renders researchBudget's numbers as the sentence the
// deep-research skill's {budget} placeholder expands to, in each research
// round's own prompt — the one place the model sees a budget number on
// every round, so it takes precedence over the static, cloud-sized fallback
// SKILL.md's prose still describes for a reader who opens the file directly.
func researchBudgetLine(window int) string {
	rounds, low, high := researchBudget(window)
	return fmt.Sprintf("Round cap: %d; source target: roughly %d-%d quality sources", rounds, low, high)
}
