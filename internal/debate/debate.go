// Package debate implements a multi-agent-debate (MAD) primitive (originally
// P12, built for security analysis): a claim is proposed, adversarially
// critiqued, rebutted, and finally arbitrated, instead of a single
// unchallenged pass producing the answer. It stays decoupled from the
// engine/swarm the same way internal/swarm stays decoupled from the engine —
// the caller supplies a RunFunc that knows how to execute one role turn
// (spawn a sub-agent, call a model directly, whatever the caller's context
// allows); this package only orchestrates the round structure,
// evidence-grounding check, and budget bound.
//
// The claim itself is domain-agnostic — a security finding, a design
// assertion, or a statement about a plain document or plan all work the same
// way. Config.Domain selects which default persona trio plays the three
// roles: DomainSecurity (the default, for vulnerability/threat/design
// claims) or DomainGeneric (for reviewing documents, plans, or any other
// non-security claim). Either trio can be overridden per-role regardless of
// Domain.
package debate

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/persona"
)

// DefaultMaxRounds is the hard default round bound (P12.6): a debate round is
// 3+ model calls instead of 1, so an unbounded default would quietly multiply
// spend well past what a single-pass equivalent task costs.
const DefaultMaxRounds = 2

// MaxRoundsCeiling hard-caps Config.MaxRounds regardless of caller input
// (P32.4). Nothing upstream of withDefaults bounded it — the agent tool's
// JSON schema, the HTTP DebateRequest field, and executeDebate's own context
// timeout all scaled with whatever value arrived — so a model turn steered by
// prompt-injected content (a debate claim can load via WithFiles) could
// request an arbitrarily large round count, and budgetExhausted only helps
// when a cost.Tracker happens to be in context. This applies unconditionally.
const MaxRoundsCeiling = 10

// Domain selects which default persona trio Config.withDefaults fills in when
// a role persona isn't explicitly overridden. DomainSecurity is the zero
// value so every existing caller that never set Domain keeps its original
// security-persona defaults.
const (
	DomainSecurity = "security"
	DomainGeneric  = "generic"
)

// Default persona names for each debate role (P12.2), used when
// Config.Domain is DomainSecurity (or unset). Proposer defaults to
// security-researcher since most debated claims start life as a vulnerability
// finding; callers doing threat-model debate should pass ProposerPersona:
// "security-architect" explicitly (P12.5).
const (
	DefaultProposerPersona = "security-researcher"
	DefaultCriticPersona   = "security-critic"
	DefaultArbiterPersona  = "security-arbiter"
)

// Generic persona names for each debate role, used when Config.Domain is
// DomainGeneric — for debating claims about documents, plans, or anything
// else outside the security domain. "general" has no tool restriction, so it
// doubles as the generic proposer; "critic"/"arbiter" are the non-security
// counterparts of security-critic/security-arbiter.
const (
	GenericProposerPersona = "general"
	GenericCriticPersona   = "critic"
	GenericArbiterPersona  = "arbiter"
)

// RunFunc executes one role turn (a system prompt + a user prompt) and returns
// the role's text output. The caller supplies an implementation — typically
// spawning a sub-agent via swarm.Backend so the role gets real tool access
// (P12.3's evidence-grounding requirement depends on the critic actually being
// able to call grep/read_file/security_scan), but any model-calling function
// works for testing.
type RunFunc func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

// Config configures a debate run. Zero-valued fields fall back to their
// documented defaults via withDefaults.
type Config struct {
	// Domain selects the default persona trio (DomainSecurity, the zero
	// value, or DomainGeneric) when the corresponding *Persona field below is
	// left empty. Individual persona overrides always win regardless of Domain.
	Domain          string
	ProposerPersona string // default depends on Domain; see DefaultProposerPersona/GenericProposerPersona
	CriticPersona   string // default depends on Domain; see DefaultCriticPersona/GenericCriticPersona
	ArbiterPersona  string // default depends on Domain; see DefaultArbiterPersona/GenericArbiterPersona
	MaxRounds       int    // default DefaultMaxRounds; always clamped to >= 0

	// Tracker, BudgetUSD, and MaxTokens implement the P12.6 round bound: if set,
	// Run checks Tracker's cumulative spend against the caps before starting
	// each round (including the first) and, once within budgetHeadroomFraction
	// of the cap, skips straight to arbitration on the transcript gathered so
	// far rather than starting a round that the shared budget gate would abort
	// mid-critique with no verdict produced at all.
	Tracker   *cost.Tracker
	BudgetUSD float64
	MaxTokens int
}

// budgetHeadroomFraction is how close to a cap Run tolerates before forcing
// early arbitration. Matches the "close to its cap" language in the roadmap:
// leaves enough room for the arbitration call itself, which must always run.
const budgetHeadroomFraction = 0.9

func withDefaults(cfg Config) Config {
	proposer, critic, arbiter := DefaultProposerPersona, DefaultCriticPersona, DefaultArbiterPersona
	if cfg.Domain == DomainGeneric {
		proposer, critic, arbiter = GenericProposerPersona, GenericCriticPersona, GenericArbiterPersona
	}
	if cfg.ProposerPersona == "" {
		cfg.ProposerPersona = proposer
	}
	if cfg.CriticPersona == "" {
		cfg.CriticPersona = critic
	}
	if cfg.ArbiterPersona == "" {
		cfg.ArbiterPersona = arbiter
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = DefaultMaxRounds
	}
	if cfg.MaxRounds > MaxRoundsCeiling {
		cfg.MaxRounds = MaxRoundsCeiling
	}
	return cfg
}

// Round is one critique/rebuttal exchange.
type Round struct {
	N          int
	Critique   string
	Evidence   bool   // true if the critique cited retrievable evidence (P12.3)
	Conceded   bool   // true if the critic explicitly conceded
	Rebuttal   string // empty when Conceded
	BudgetStop bool   // true if this round never ran because the budget was exhausted
}

// Verdict is the arbiter's final ruling.
type Verdict struct {
	Outcome    string // "UPHOLD" | "REVISE" | "REJECT" | "" if unparsed
	Confidence string // "high" | "medium" | "low" | "" if unparsed
	Text       string // the arbiter's full raw response
}

// Transcript is the full record of one debate.
type Transcript struct {
	Claim   string
	Rounds  []Round
	Verdict Verdict
}

// concedeRe matches an explicit concession, case-insensitive. Anchored to the
// start of the (trimmed, markdown-emphasis-stripped) response — the critic
// persona instructs a concession to be the literal opening of the reply
// ("CONCEDE, followed by one sentence...") — rather than matching "concede"
// anywhere in the text (P43.1): an unanchored match misreads a hedge like "I
// won't concede this point — the claim is missing X" as a full concession,
// discarding a real evidence-cited challenge and telling the arbiter the
// critic backed down. Mirrors why verdictOutcomeRe/verdictConfidenceRe below
// anchor to line-start for the arbiter's structured output.
var concedeRe = regexp.MustCompile(`(?i)^[\s*_]*concede\b`)

// isConcession reports whether critique is an explicit concession per
// concedeRe, trimming surrounding whitespace first so the anchor lines up
// with the start of the model's actual content.
func isConcession(critique string) bool {
	return concedeRe.MatchString(strings.TrimSpace(critique))
}

// citationRe recognizes the evidence shapes the critic persona is instructed
// to cite: a file:line reference, an explicit "Evidence:" tag, or a mention of
// one of the grounding tools by name. This is a deliberately loose heuristic
// (P12.3 is a prompt+arbiter-rule constraint, not a hard verifier) — it exists
// to make "no evidence at all" mechanically detectable rather than trusting
// the arbiter's judgment alone to catch a rubber-stamped or hallucinated
// challenge.
var citationRe = regexp.MustCompile(`(?i)(evidence\s*:|[\w./\\-]+\.\w+:\d+|\b(grep|read_file|security_scan|glob)\b)`)

// hasEvidence reports whether a critique cites retrievable evidence. A
// concession is exempt — there is nothing to substantiate when the critic
// found no flaw.
func hasEvidence(critique string) bool {
	if isConcession(critique) {
		return true
	}
	return citationRe.MatchString(critique)
}

// verdictLineRe pulls "VERDICT: X" / "CONFIDENCE: Y" out of the arbiter's
// structured response regardless of surrounding whitespace or case.
var (
	verdictOutcomeRe    = regexp.MustCompile(`(?im)^\s*VERDICT\s*:\s*(UPHOLD|REVISE|REJECT)\b`)
	verdictConfidenceRe = regexp.MustCompile(`(?im)^\s*CONFIDENCE\s*:\s*(HIGH|MEDIUM|LOW)\b`)
)

func parseVerdict(text string) Verdict {
	v := Verdict{Text: text}
	if m := verdictOutcomeRe.FindStringSubmatch(text); m != nil {
		v.Outcome = strings.ToUpper(m[1])
	}
	if m := verdictConfidenceRe.FindStringSubmatch(text); m != nil {
		v.Confidence = strings.ToLower(m[1])
	}
	return v
}

// WithFiles appends a reference-material block naming files to claim,
// instructing the debate roles to read them before proposing, critiquing, or
// rebutting. It does not read the files itself — the roles do that with their
// own read_file tool once the debate is running, the same way a critic reads
// the codebase for a security claim's evidence (P12.3). Passing no files
// returns claim unchanged. Paths are included verbatim (relative to whatever
// working directory the debate roles' tools resolve paths against).
func WithFiles(claim string, files []string) string {
	if len(files) == 0 {
		return claim
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(claim))
	b.WriteString("\n\n## Reference material\nRead these files before proposing, critiquing, or rebutting — ground the debate in their actual content, not assumptions about what they say:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	return b.String()
}

// PersonaSystem resolves a debate role's persona name to its system prompt.
// Exported so callers assembling role prompts outside Run (e.g. a caller that
// wants to inspect/override a role's prompt) can reuse the same resolution
// Run itself uses.
func PersonaSystem(name string) string {
	p, _ := persona.Get(name)
	return p.System
}

// budgetExhausted reports whether cfg's shared tracker has crossed the
// headroom fraction of either configured cap. No tracker or no caps means the
// bound is not in effect (unlimited).
func budgetExhausted(cfg Config) bool {
	if cfg.Tracker == nil {
		return false
	}
	if cfg.BudgetUSD > 0 && cfg.Tracker.TotalUSD() >= cfg.BudgetUSD*budgetHeadroomFraction {
		return true
	}
	if cfg.MaxTokens > 0 && cfg.Tracker.TotalTokens() >= int(float64(cfg.MaxTokens)*budgetHeadroomFraction) {
		return true
	}
	return false
}

// Run drives the debate: for up to cfg.MaxRounds rounds, a critic challenges
// the claim (or concedes) and, unless it conceded, the proposer rebuts; the
// arbiter then synthesizes the whole transcript into a final verdict. Run
// always produces a Transcript with a Verdict — including when the budget cuts
// rounds short or a role call errors mid-debate, since a partial transcript
// arbitrated as-is is more useful than an aborted call with nothing at all.
func Run(ctx context.Context, claim string, cfg Config, run RunFunc) (Transcript, error) {
	cfg = withDefaults(cfg)
	t := Transcript{Claim: claim}

	proposerSys := PersonaSystem(cfg.ProposerPersona)
	criticSys := PersonaSystem(cfg.CriticPersona)
	arbiterSys := PersonaSystem(cfg.ArbiterPersona)

	for i := 1; i <= cfg.MaxRounds; i++ {
		if budgetExhausted(cfg) {
			t.Rounds = append(t.Rounds, Round{N: i, BudgetStop: true})
			break
		}

		critiquePrompt := fmt.Sprintf(
			"Claim under review:\n%s\n\n%s\nCritique this claim: attack its weakest part, grounded in cited evidence, or reply CONCEDE.",
			claim, renderRoundsSoFar(t.Rounds),
		)
		critique, err := run(ctx, criticSys, critiquePrompt)
		if err != nil {
			return t, fmt.Errorf("debate round %d critique: %w", i, err)
		}

		round := Round{N: i, Critique: critique, Evidence: hasEvidence(critique), Conceded: isConcession(critique)}
		if round.Conceded {
			t.Rounds = append(t.Rounds, round)
			break
		}

		rebuttalPrompt := fmt.Sprintf(
			"Your claim:\n%s\n\n%s\nCritic's challenge (round %d):\n%s\n\nRespond to the critique.",
			claim, renderRoundsSoFar(t.Rounds), i, critique,
		)
		rebuttal, err := run(ctx, proposerSys, rebuttalPrompt)
		if err != nil {
			round.Rebuttal = "(rebuttal failed: " + err.Error() + ")"
			t.Rounds = append(t.Rounds, round)
			break
		}
		round.Rebuttal = rebuttal
		t.Rounds = append(t.Rounds, round)
	}

	verdictPrompt := fmt.Sprintf(
		"Full debate transcript:\n\n%s\nIssue your verdict on the original claim.",
		Transcript{Claim: claim, Rounds: t.Rounds}.Format(),
	)
	verdictText, err := run(ctx, arbiterSys, verdictPrompt)
	if err != nil {
		return t, fmt.Errorf("debate arbitration: %w", err)
	}
	t.Verdict = parseVerdict(verdictText)
	return t, nil
}

// renderRoundsSoFar formats the rounds completed so far for inclusion in the
// next role prompt. Empty for round 1.
func renderRoundsSoFar(rounds []Round) string {
	if len(rounds) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Prior rounds:\n")
	b.WriteString(formatRounds(rounds))
	b.WriteString("\n")
	return b.String()
}

func formatRounds(rounds []Round) string {
	var b strings.Builder
	for _, r := range rounds {
		if r.BudgetStop {
			fmt.Fprintf(&b, "--- Round %d: skipped (budget exhausted) ---\n", r.N)
			continue
		}
		evidenceTag := "[unsubstantiated]"
		if r.Evidence {
			evidenceTag = "[evidence cited]"
		}
		fmt.Fprintf(&b, "--- Round %d critique %s ---\n%s\n", r.N, evidenceTag, strings.TrimSpace(r.Critique))
		switch {
		case r.Conceded:
			b.WriteString("(critic conceded — no rebuttal needed)\n")
		case r.Rebuttal != "":
			fmt.Fprintf(&b, "--- Round %d rebuttal ---\n%s\n", r.N, strings.TrimSpace(r.Rebuttal))
		}
	}
	return b.String()
}

// Format renders the transcript as markdown-ish text suitable for a tool
// result, CLI stdout, or TUI display (P12.4 builds a structured block renderer
// on top of the same Transcript struct; Format is the plain-text fallback).
func (t Transcript) Format() string {
	var b strings.Builder
	b.WriteString("=== Claim ===\n")
	b.WriteString(strings.TrimSpace(t.Claim))
	b.WriteString("\n\n")
	if len(t.Rounds) > 0 {
		b.WriteString(formatRounds(t.Rounds))
		b.WriteString("\n")
	}
	if t.Verdict.Text != "" {
		b.WriteString("=== Verdict ===\n")
		if t.Verdict.Outcome != "" {
			fmt.Fprintf(&b, "%s (confidence: %s)\n", t.Verdict.Outcome, orUnknown(t.Verdict.Confidence))
		}
		b.WriteString(strings.TrimSpace(t.Verdict.Text))
		b.WriteString("\n")
	}
	return b.String()
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
