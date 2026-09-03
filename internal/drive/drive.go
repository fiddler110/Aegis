// Package drive implements the phased skill drive: the in-harness form of the
// parked P38.8 per-phase wrapper. The generic `--skill` drive (chat.go) runs a
// whole multi-phase build in one ever-growing conversation; on a small local
// context window that rising peak is what stalls the threat-model build — the
// P38.1 wall every P39.x fix has only been chipping at. A phased drive instead
// runs each phase in its OWN fresh conversation, seeded only with a compact
// phase-specific prompt, so a phase's peak context is ~one phase's worth of
// work (its prompt + the one reference it reads + the prior files it pulls
// from disk), not the accumulation of every prior phase. That bounded-context
// property is exactly what let the external P38.8 wrapper reach a
// verify-clean suite where the single-context drive never has; this brings it
// inside the supported code path, reusing the generic drive's guards (PENDING
// oracle, P39.7 no-progress nudge, --max-turns, the P39.6 verify + P38.1
// quality round) but resetting context at every phase boundary. Prior
// phases' outputs are grounded from disk, not from conversation history, so
// the reset loses nothing the model needs.
package drive

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/skills"
)

// Phase is one bounded work unit of a phased drive: a set of run-dir-relative
// file globs it must clear of `<!-- PENDING` markers, and a compact prompt that
// seeds its fresh context. setup marks the first phase, which runs before the run
// directory exists (it does recon, creates the directory, and scaffolds).
type Phase struct {
	name     string   // notice/log label, e.g. "architecture"
	globs    []string // run-dir-relative file globs this phase must clear of PENDING
	setup    bool     // true only for the first phase: the run dir does not exist when it starts
	promptFn func(p PhaseParams) string
	// tools narrows the schemas offered to the model for this phase's turns.
	// Empty means "offer everything", which is what every phase did before
	// P39.14 and what a large model needs no help with. A small local model
	// does: offered 50+ schemas it picks a plausible-sounding wrong one, and
	// no amount of prompt text reliably stops it (the shell tool's description
	// already spells out that Unix commands do not work, and a 2.6B ran
	// `ls -la` anyway). Removing the schema is the only instruction it cannot
	// ignore. Names must match the registry's; an unknown name simply never
	// matches and is therefore inert.
	tools []string
	// requirePattern, requireCount and requireHint are the P73.1 mechanical
	// content gate: once every `<!-- PENDING` marker is gone, the phase's
	// owned files must also match requirePattern at least requireCount times
	// before complete() returns true. nil requirePattern means no gate (every
	// phase before P73.1). See skills.PhaseSpec.RequirePattern for why a
	// marker-only oracle isn't enough — it says "the model decided this is
	// done," not "this is done."
	requirePattern *regexp.Regexp
	requireCount   int
	requireHint    string
}

// PhaseParams carries everything a per-phase prompt needs to orient a fresh
// context: the raw task, where the skill's on-disk assets live, the workspace
// root, and (once scaffolded) the run directory. runDir is "" for the setup phase.
type PhaseParams struct {
	task     string
	skillDir string
	cwd      string
	runDir   string
	// contextWindow is this run's resolved serving context window in tokens
	// (P71.11), read once per phase from st.Engine.EffectiveContextWindow() —
	// the same figure a mid-turn tool call reads via
	// tool.ContextWindowFromContext. 0 means unresolved; declaredPhasePrompt's
	// {budget} substitution treats that the same as an unknown/cloud-scale
	// window.
	contextWindow int
}

func (ph Phase) label() string { return strings.ReplaceAll(ph.name, "-", " ") }

// contextWindow returns st.Engine's resolved serving context window
// (P71.11), or 0 when st.Engine is nil — a handful of tests build a State
// with no Engine at all (they exercise conversation-building helpers
// directly, never Run), and 0 is EffectiveContextWindow's own "unresolved"
// reading, so this just extends that convention to "no engine to ask."
func (st *State) contextWindow() int {
	if st.Engine == nil {
		return 0
	}
	return st.Engine.EffectiveContextWindow()
}

// threatModelSkill is the one skill with a built-in, hand-tuned plan (and a
// built-in verifier and run-dir layout to match). Every other skill declares
// its plan in frontmatter — see PlanFor and RunDirResolver, the two places this
// name is allowed to appear.
const threatModelSkill = "threat-modeling"

// ThreatModelPhases is the dependency-ordered phase plan for the threat-modeling
// skill (SKILL.md §4.2), mirroring the external P38.8 wrapper's sequence:
// architecture → DFD → framework analysis → findings → assessment, each in its
// own bounded context, then the phase-6 verify+quality round (run separately,
// see runPhasedVerifyAndQuality). The globs match what scaffold.py writes; the
// analysis file is `2-<framework>-analysis.md`, matched by glob because the
// framework short-name is the model's choice at setup, not known here.
var ThreatModelPhases = []Phase{
	{name: "architecture", setup: true, globs: []string{"0.1-architecture.md"}, promptFn: phasePromptArchitecture, tools: setupPhaseTools},
	{name: "data-flow-diagram", globs: []string{"1.1-model.mmd", "1-model.md"}, promptFn: phasePromptDFD, tools: dfdPhaseTools},
	{name: "framework-analysis", globs: []string{"2-*-analysis.md"}, promptFn: phasePromptAnalysis, tools: fillPhaseTools},
	{name: "findings", globs: []string{"3-findings.md"}, promptFn: phasePromptFindings, tools: fillPhaseTools},
	{name: "assessment", globs: []string{"0-assessment.md", "inventory.yaml"}, promptFn: phasePromptAssessment, tools: assessmentPhaseTools},
}

// setupPhaseTools is the tool surface of the setup phase: it runs the recon
// digest and the scaffolder, reads what they produce, and writes the first
// file. fillPhaseTools drops shell entirely — once the suite is scaffolded,
// every remaining phase is "read the evidence, fill the markers", and a shell
// call during a fill phase has been a detour every time it appeared.
//
// P39.18 removed shell from the setup phase too. It was exposed there for
// exactly three command lines — the recon digest, `date`, and the scaffolder —
// and each was a string the model had to compose exactly right; the run that
// motivated this emitted `scaffold.py --framework` with the value omitted.
// threat_model_recon/threat_model_scaffold are the same three steps as typed
// tools: the framework is a required enum the harness renders, and the
// timestamped run directory is created by the tool rather than by a `date` call
// plus string concatenation. With those in place shell had no remaining use
// here, so it is gone: an argument error for a bundled script is now
// structurally impossible rather than merely corrected.
var (
	setupPhaseTools = []string{"threat_model_recon", "threat_model_scaffold", "read_file", "write_file", "edit_file", "edit_section", "fill_marker", "glob", "grep", "ls"}
	// Fill phases get no write_file either. Their files already exist — the
	// setup phase scaffolded them — so a whole-file write can only overwrite
	// real content with a fresh set of empty markers. The prompt has told the
	// model "never write_file a suite file" since P39.6 and a 14B model did it
	// anyway, reconstructing 2-<framework>-analysis.md from the skill's
	// documentation with the placeholder left unsubstituted (2026-08-09).
	// Removing the tool is the version of that instruction that holds.
	fillPhaseTools = []string{"read_file", "edit_file", "edit_section", "fill_marker", "glob", "grep", "ls"}
	// The DFD phase writes a .mmd diagram; the assessment phase writes
	// inventory.yaml. Each gets the one extra tool its output format needs
	// rather than widening the shared fill set.
	dfdPhaseTools = append(append([]string{}, fillPhaseTools...), "render_diagram")
	// The assessment phase needs one more capability than the other fill
	// phases: its prompt instructs the model to generate inventory.yaml
	// deterministically from the bundled inventory.py, and that sidecar's
	// PENDING marker is part of the phase's own completion condition. Without
	// it the phase can fill 0-assessment.md perfectly and still never complete
	// — which is exactly what happened when this set was first narrowed
	// (2026-08-09). That was shell until P39.18; it is now the typed
	// threat_model_inventory tool, which was shell's only use here.
	assessmentPhaseTools = append(append([]string{}, fillPhaseTools...), "yaml_validate", "threat_model_inventory")
	// phase6Tools is the verify/fix + quality surface (P62.10). Phase 6 is
	// typically where a build spends the most turns — MaxVerifyRounds fix rounds
	// plus the quality pass, each its own fresh context — and until now it was
	// the one phase that narrowed nothing at all, running on the session's whole
	// registered surface. On the CLI path that includes web_search, which
	// TestScopeToolsPerPhase asserts no content phase offers and which P39.14
	// records as "the detour that opened a real run".
	//
	// The list is read off phase 6's own prompts rather than guessed, the way
	// fillPhaseTools was. They say: read the suite first (read_file/glob/grep/ls),
	// fix in place with fill_marker/edit_section/edit_file, never re-scaffold and
	// never write_file a suite file — so write_file stays out for the same reason
	// the fill phases dropped it. threat_model_inventory is the one addition:
	// inventory.yaml is generated, not authored, so a failing inventory check is
	// fixed by regenerating the sidecar, and hand-editing it is what the
	// assessment phase's prompt forbids in the first place.
	phase6Tools = append(append([]string{}, fillPhaseTools...), "threat_model_inventory")
)

// phase6Phase returns the synthetic Phase whose tool list scopes phase 6, or a
// zero Phase (which scopeTools treats as "narrow nothing") when this plan's
// phases declare no tools of their own.
//
// The gate on the plan is not incidental. phase6Tools is read off the built-in
// threat-modeling plan's phase-6 prompts; a plan assembled from a skill's
// `phases:` frontmatter declares no per-phase tools (planFromSpecs sets none),
// and narrowing *its* verify round to a threat-model surface would take
// capabilities away from a skill that never opted into narrowing — deep-research
// wants web_search in a fix round exactly as much as a threat model does not.
// Declared narrowing stays declared.
func phase6Phase(plan []Phase) Phase {
	for _, ph := range plan {
		if len(ph.tools) > 0 {
			return Phase{name: "verify-and-quality", tools: phase6Tools}
		}
	}
	return Phase{name: "verify-and-quality"}
}

// Name returns the phase's log/notice label, for hosts that render progress.
func (ph Phase) Name() string { return ph.name }

// PlanFor returns the phased drive plan for a skill, or nil when the skill has
// no plan (the caller then uses the generic single-context drive).
//
// specs is the skill's own `phases:` frontmatter (skills.Skill.Phases), which
// is how any skill opts in without a code change (P52.12). deep-research is
// the one other built-in that has actually declared a plan this way (P71.8:
// research → synthesize, working from the P71.9 findings file as its phase
// artifact). latex-report, structured-build and documentation-as-code have
// the same single-context problem threat-modeling had but do not yet declare
// `phases:` — naming them here as if they already did was the P71.8 finding;
// do not re-add them to this comment without giving them frontmatter first.
// Pass nil when the skill has not been loaded yet; the built-in plan below
// still resolves, which is enough for a caller that only needs to know
// *whether* a run will be phased.
//
// The built-in threat-modeling plan wins over frontmatter for that one name.
// Its per-phase prompts are hand-tuned Go functions carrying guardrails a
// frontmatter string cannot express (the P47.3 no-self-verify instruction, the
// P39.14 anti-monolithic-write rule, framework-specific scaffolding), and every
// P38.1/P47.x live run was tuned against them — letting an edited SKILL.md
// silently replace them with a plain prompt would be a regression wearing the
// clothes of a generalization.
func PlanFor(skillName string, specs []skills.PhaseSpec) []Phase {
	if skillName == threatModelSkill {
		return ThreatModelPhases
	}
	return planFromSpecs(specs)
}

// planFromSpecs converts declared phase specs into runnable phases.
func planFromSpecs(specs []skills.PhaseSpec) []Phase {
	if len(specs) == 0 {
		return nil
	}
	out := make([]Phase, 0, len(specs))
	for i, s := range specs {
		spec := s
		// Only the first phase may be the setup phase: a later phase running
		// before the run dir exists would have nothing to scaffold into, and
		// the drive's completion oracle assumes one pre-scaffold step.
		setup := spec.Setup && i == 0
		ph := Phase{
			name:  spec.Name,
			globs: spec.Files,
			setup: setup,
			promptFn: func(p PhaseParams) string {
				return declaredPhasePrompt(spec, p)
			},
		}
		// An invalid regexp is silently not applied — a malformed
		// `require_pattern` in a skill's frontmatter must never turn into a
		// phase that can never complete.
		if pat := spec.RequirePattern; pat != "" {
			if re, err := regexp.Compile(pat); err == nil {
				ph.requirePattern = re
				ph.requireCount = spec.RequireCount
				if ph.requireCount <= 0 {
					ph.requireCount = 1
				}
				ph.requireHint = spec.RequireHint
			}
		}
		out = append(out, ph)
	}
	return out
}

// declaredPhasePrompt renders a frontmatter-declared phase prompt, substituting
// the run-time placeholders and appending the guardrails every phase needs
// regardless of what its author wrote — the incremental-edit rule (P39.14) and
// the non-interactive instruction. A phase that declares no prompt at all still
// gets a usable one built from its name and files, so `phases:` entries can be
// as terse as a name plus globs.
func declaredPhasePrompt(spec skills.PhaseSpec, p PhaseParams) string {
	body := spec.Prompt
	if body == "" {
		body = fmt.Sprintf("Complete the %s phase: fill every `<!-- PENDING: … -->` marker in {files} under `{run_dir}` with real, evidence-grounded content.", strings.ReplaceAll(spec.Name, "-", " "))
	}
	runDir := p.runDir
	if runDir == "" {
		runDir = p.cwd
	}
	r := strings.NewReplacer(
		"{task}", p.task,
		"{run_dir}", filepath.ToSlash(runDir),
		"{skill_dir}", filepath.ToSlash(p.skillDir),
		"{cwd}", filepath.ToSlash(p.cwd),
		"{phase}", spec.Name,
		"{files}", strings.Join(spec.Files, ", "),
		// P71.11: only deep-research's frontmatter prompt currently contains
		// this placeholder; every other skill's Replace call leaves it
		// untouched as a no-op, the same as {run_dir} etc. would for a skill
		// that never mentions them.
		"{budget}", researchBudgetLine(p.contextWindow),
	)
	return r.Replace(body) + "\n\n" + phase6IncrementalEditRule +
		" Work one marker at a time and keep going until this phase's files carry no `<!-- PENDING` markers. " +
		"This is a non-interactive run: do not stop to ask whether to proceed, and edit only the file(s) this phase owns."
}

// LinearForced lets AEGIS_SKILL_DRIVE=linear force the generic
// single-context drive even for a phased skill — an escape hatch for comparing
// the two approaches or working around a phased-drive issue. Off by default.
func LinearForced() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AEGIS_SKILL_DRIVE")), "linear")
}

// growingPhaseConvForced lets AEGIS_PHASE_CONV=growing restore the pre-P47.4
// behaviour where in-phase continuations accumulate in one growing conversation
// (reset only on a context overflow), rather than the P47.4 default of a fresh
// near-stateless context every turn. It exists to measure the two side by side —
// P47.4 is a measure-first item — and as a fallback if a model ever needs the
// within-phase history the stateless reset drops. Off by default (stateless).
func growingPhaseConvForced() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AEGIS_PHASE_CONV")), "growing")
}

// resolveFiles returns the existing (non-directory) files under runDir that match
// this phase's globs. An empty result means the phase's files have not been
// scaffolded yet. runDir must be non-empty.
func (ph Phase) resolveFiles(runDir string) []string {
	var out []string
	for _, g := range ph.globs {
		matches, _ := filepath.Glob(filepath.Join(runDir, g))
		for _, m := range matches {
			if info, err := os.Stat(m); err == nil && !info.IsDir() {
				out = append(out, m)
			}
		}
	}
	return out
}

// pending returns this phase's owned files that still carry a `<!-- PENDING`
// marker, as run-dir-relative slash paths. Before anything is scaffolded (runDir
// == "" or no file matches a glob), it returns the globs themselves as
// placeholders so the drive treats the phase as unfinished.
func (ph Phase) pending(runDir string) []string {
	if runDir == "" {
		return append([]string(nil), ph.globs...)
	}
	files := ph.resolveFiles(runDir)
	if len(files) == 0 {
		return append([]string(nil), ph.globs...)
	}
	var out []string
	for _, f := range files {
		if fileHasPendingMarker(f) {
			if rel, err := filepath.Rel(runDir, f); err == nil {
				out = append(out, filepath.ToSlash(rel))
			} else {
				out = append(out, filepath.ToSlash(f))
			}
		}
	}
	sort.Strings(out)
	return out
}

// complete reports whether every file this phase owns exists and is free of
// PENDING markers. It is false while nothing is scaffolded (runDir == "" or no
// glob matches yet), so the setup phase always runs at least once.
func (ph Phase) complete(runDir string) bool {
	if runDir == "" {
		return false
	}
	files := ph.resolveFiles(runDir)
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if fileHasPendingMarker(f) {
			return false
		}
	}
	return ph.contentGateReason(runDir) == ""
}

// contentGateReason reports why this phase's P73.1 mechanical content gate
// (requirePattern/requireCount) is not yet satisfied, or "" when it is (or
// the phase declares none). Distinct from pending(): the gate can fail even
// after every `<!-- PENDING` marker is gone, which pending()'s marker scan
// alone cannot see — the model decided the phase was done, and the output
// still doesn't carry what the phase's author required. Callers that already
// know files exist (complete(), having just checked resolveFiles is
// non-empty) still pay one more resolveFiles call here; the phase file sets
// are small enough (single-digit files) that this is not worth threading an
// extra parameter through complete()'s simple boolean contract for.
func (ph Phase) contentGateReason(runDir string) string {
	if ph.requirePattern == nil || runDir == "" {
		return ""
	}
	files := ph.resolveFiles(runDir)
	if len(files) == 0 {
		return "" // not scaffolded yet — the ordinary "phase not complete" path already covers this
	}
	if countPatternMatches(files, ph.requirePattern) >= ph.requireCount {
		return ""
	}
	if ph.requireHint != "" {
		return ph.requireHint
	}
	return fmt.Sprintf("needs at least %d match(es) of `%s`", ph.requireCount, ph.requirePattern.String())
}

// countPatternMatches sums re's non-overlapping matches across files, capped
// per file at the same size fileHasPendingMarker uses — a mechanical content
// gate has no business reading a multi-megabyte file to decide a phase is
// unfinished.
func countPatternMatches(files []string, re *regexp.Regexp) int {
	total := 0
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.Size() > maxPhaseFileSize {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		total += len(re.FindAll(data, -1))
	}
	return total
}

// maxPhaseFileSize bounds both fileHasPendingMarker's and
// countPatternMatches' reads — generated report/findings files are far
// smaller than this in practice.
const maxPhaseFileSize = 1 << 20 // 1 MiB

// fileHasPendingMarker reports whether one file still contains the `<!-- PENDING`
// prefix scaffold.py writes for unfilled sections. Mirrors scanPendingMarkers'
// match, scoped to a single file so a phase can be judged complete without
// walking the whole .aegis tree.
func fileHasPendingMarker(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxPhaseFileSize {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "<!-- PENDING")
}

// State bundles the deps Run shares with the RunE
// closure: the engine, the assembled system prompt, the event sink, and the two
// per-turn counters onEvent maintains (passed by pointer so a phase can reset
// them before each turn and read iterMutations after, exactly as the generic
// drive does).
type State struct {
	// Engine runs each turn. The drive is orchestration *above* engine.Run —
	// it owns no model, gate, or tool wiring of its own, which is what lets the
	// same state machine serve the CLI, the TUI and the daemon (P52.12).
	Engine  *engine.Engine
	System  string
	OnEvent engine.EmitFunc
	Logger  *slog.Logger
	// ErrOut receives the drive's operator-facing `[notice: …]` lines. The CLI
	// passes stderr; a daemon-hosted drive passes a writer that turns each line
	// into an SSE notice, so a UI sees the narration a terminal does.
	ErrOut     io.Writer
	Cwd        string
	SkillName  string
	SkillDir   string
	TaskPrompt string
	MaxTurns   int
	// EscalateWindow raises the serving context window (num_ctx) toward the
	// model max on a context overflow and reports the new window and whether it
	// grew (P47.5b). Nil when the provider can't escalate; the overflow paths
	// then rely on the fresh-context reset alone.
	EscalateWindow func() (int, bool)
	// CheckBackend probes the model backend's liveness (P50.1), returning
	// (healthy, supported). supported is false when the adapter has no liveness
	// probe (a cloud adapter) — the drive then does not wait on it. Nil when the
	// hook wasn't wired; treated as unsupported.
	CheckBackend func(context.Context) (bool, bool)
	// Progress carries the live per-phase progress the P50.4 heartbeat ticker
	// reads and each turn updates. Nil disables the heartbeat.
	Progress *Progress
	// ScopeTools narrows the tool schemas offered to the model for one phase,
	// returning a function that restores the previous exposure. Wire it to
	// tool.Registry.ScopeExposed. Nil disables per-phase narrowing entirely,
	// which is the pre-P39.14 behaviour and what a host that shares one
	// registry across concurrent sessions must keep: the narrowing is
	// registry-wide, so it is only safe where the drive owns the registry.
	ScopeTools func(allow []string) (restore func())
	// IterToolCalls and IterMutations are the caller's per-turn counters: the
	// drive zeroes them before each engine.Run and reads them after to apply
	// the P39.7 no-progress guard. Pointers because the caller's own event
	// handler is what increments them.
	IterToolCalls *int
	IterMutations *int

	// RunDir resolves the directory this plan's file globs are relative to,
	// re-resolved on every use because the setup phase creates it mid-turn.
	// Build it with RunDirResolver; nil keeps the built-in threat-model layout,
	// which is what every caller predating the frontmatter-declared plans meant.
	RunDir func(cwd string) string

	// plan is the phase list Run was given, kept so the phase-6 loop can route
	// a content-substance failure back to its owning phase. Resolved once by
	// Run rather than re-derived from SkillName: with frontmatter-declared
	// plans (P52.12) the name alone no longer determines the plan.
	plan []Phase
}

// runDir resolves the current run directory for this drive's plan. Every phase
// file lookup goes through here rather than calling LatestRunDir directly, so
// a frontmatter-declared plan is not silently judged against the threat-model
// layout — under which none of its files exist and no phase ever completes.
func (st *State) runDir() string {
	if st.RunDir == nil {
		return LatestRunDir(st.Cwd)
	}
	return st.RunDir(st.Cwd)
}

// Run drives a phased skill (currently threat-modeling) to
// completion, running each phase in its own fresh conversation so peak context
// stays bounded to one phase. It reuses the generic drive's guards but resets the
// conversation at every phase boundary. It returns the engine error if a turn
// fails, and nil otherwise — including resumable stops (--max-turns, a stall, or
// ctx cancel) — matching the generic drive's contract so the caller's tail logic
// (the P38.6 floor check, cost trailer) is unchanged.
func Run(ctx context.Context, st *State, phases []Phase) error {
	st.plan = phases
	stopHeartbeat := st.startHeartbeat() // P50.4: periodic sign-of-life during long turns
	defer stopHeartbeat()
	totalTurns := 0
	// One restore closure carried across iterations: each phase undoes the
	// previous phase's narrowing *before* taking its own snapshot, or a phase
	// would capture the narrowed set as its baseline and restore to that. The
	// deferred call covers the drive's many early returns.
	restoreTools := func() {}
	defer func() { restoreTools() }()

	for pi := range phases {
		ph := phases[pi]
		runDir := st.runDir()
		st.repairSkillAssets()
		restoreTools()
		restoreTools = st.scopeTools(ph)
		if ph.complete(runDir) {
			st.Logger.Info("phased drive: phase already complete, skipping", "phase", ph.name)
			fmt.Fprintf(st.ErrOut, "\n[notice: phase %d/%d (%s) already complete on disk — skipping]\n", pi+1, len(phases), ph.label())
			continue
		}
		if st.Progress != nil {
			st.Progress.enter(ph.name)
		}
		// Phase boundaries are the drive's coarse progress signal, and until
		// P52.12 they existed only in the daemon's log — a client watching a
		// multi-hour build saw tool calls and prose with no way to tell which
		// phase they belonged to, or that a phase had ended at all. The
		// heartbeat's per-turn detail stays in the log; the boundaries go to the
		// operator stream both hosts already render.
		fmt.Fprintf(st.ErrOut, "\n[notice: phase %d/%d — %s]\n", pi+1, len(phases), ph.label())
		conv := &engine.Conversation{System: st.System}
		conv.Append(userMessage(ph.promptFn(PhaseParams{
			task: st.TaskPrompt, skillDir: st.SkillDir, cwd: st.Cwd, runDir: runDir,
			contextWindow: st.contextWindow(),
		})))
		st.Logger.Info("phased drive: starting phase", "phase", ph.name, "run_dir", runDir)

		noProgress := 0
		toolFailResets := 0
		loopResets := 0     // P57.1, per phase — one phase's loop must not spend another's budget
		overflowResets := 0 // P47.2 reset budget, per phase — see maxPhaseOverflowResets
		var prevPending []string
		for {
			*st.IterToolCalls = 0
			*st.IterMutations = 0
			st.logTurn(ph.name, len(ph.pending(runDir))) // P50.4: per-turn progress line
			if err := st.Engine.Run(ctx, conv, st.OnEvent); err != nil {
				// P50.1: a dead/unreachable backend is resumable — the phase's
				// `<!-- PENDING -->` files persist on disk. Wait for the server
				// to return, then reset to a fresh context and resume, exactly as
				// the overflow path does. Checked before the overflow branch
				// because the two are distinct classifications.
				switch st.recoverBackendDown(ctx, err, ph.label()+" phase") {
				case backendRecovered:
					runDir = st.runDir()
					pending := ph.pending(runDir)
					totalTurns++
					if totalTurns >= st.MaxTurns {
						st.stopMaxTurns(ph, pending)
						return nil
					}
					conv = st.freshPhaseConv(ph, runDir, pending, "")
					continue
				case backendGaveUp:
					st.stopBackendUnavailable(ph, ph.pending(st.runDir()))
					return nil
				}
				// P47.2: a context-overflow error is terminal to the engine but
				// resumable at the phase level — the phase's `<!-- PENDING -->`
				// files persist on disk, so a fresh, near-empty context re-reads
				// them and continues (exactly why the 2026-07-24 manual re-runs
				// worked). Reset the conversation — the same fresh-context reset
				// the drive does at phase *boundaries*, applied within a phase —
				// and retry instead of aborting the whole drive. Counts as a turn
				// so the --max-turns guard still bounds it; any other engine error
				// is still fatal.
				if provider.IsContextOverflowError(err) {
					runDir = st.runDir()
					pending := ph.pending(runDir)
					totalTurns++
					if totalTurns >= st.MaxTurns {
						st.stopMaxTurns(ph, pending)
						return nil
					}
					// A bare reset only fixes an overflow caused by accumulated
					// context. When the cause is the model's own plan being too
					// big for one generation, the plan is re-derived identically
					// from the same on-disk inputs and truncates again — observed
					// live as five identical truncations and zero findings written
					// (see OverflowEscalationDirective). So the budget is bounded
					// and each reset carries an escalating directive that shrinks
					// the unit of work, rather than replaying the same attempt.
					if overflowResets++; overflowResets > maxPhaseOverflowResets {
						st.stopPhaseOverflow(ph, pending)
						return nil
					}
					// P47.5(b): give Ollama more physical headroom before the
					// reset — best-effort, additive to the reset (the overflowed
					// prompt is discarded either way).
					st.tryEscalateWindow(ph.label())
					st.Logger.Warn("phased drive: context overflowed, resetting phase context and retrying",
						"phase", ph.name, "pending", len(pending), "reset", overflowResets, "err", err)
					fmt.Fprintf(st.ErrOut, "\n[notice: context overflowed during the %s phase; resetting to a fresh context and resuming from disk (%d file(s) still PENDING, reset %d/%d — narrowing to one edit per turn)]\n",
						ph.label(), len(pending), overflowResets, maxPhaseOverflowResets)
					conv = st.freshPhaseConv(ph, runDir, pending,
						OverflowEscalationDirective(overflowResets, maxPhaseOverflowResets))
					continue
				}
				// P52.3 + P47.2: a consecutive-tool-failure abort is resumable the
				// same way an overflow is — reset to a fresh context re-read from
				// disk rather than killing the whole drive.
				switch st.recoverToolFailureStall(err, ph.label()+" phase", &toolFailResets) {
				case overflowRetry:
					runDir = st.runDir()
					pending := ph.pending(runDir)
					totalTurns++
					if totalTurns >= st.MaxTurns {
						st.stopMaxTurns(ph, pending)
						return nil
					}
					conv = st.freshPhaseConv(ph, runDir, pending, "")
					continue
				case overflowStop:
					return nil
				}
				// P57.1: an engine loop-guard abort is resumable here too. A
				// content phase still has its `<!-- PENDING -->` markers on disk,
				// so the reset resumes exactly as the overflow path does; the
				// nudge tells the fresh context not to rebuild the theory it was
				// looping on. Observed in the phase-6 re-entry rather than here,
				// but nothing about the loop guard is specific to phase 6, and a
				// content phase dying on it would waste strictly more work.
				switch st.recoverReasoningLoop(err, ph.label()+" phase", &loopResets) {
				case loopRetry:
					runDir = st.runDir()
					pending := ph.pending(runDir)
					totalTurns++
					if totalTurns >= st.MaxTurns {
						st.stopMaxTurns(ph, pending)
						return nil
					}
					conv = st.freshPhaseConv(ph, runDir, pending, StuckLoopDirective(false))
					continue
				case overflowStop:
					return nil
				}
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
			runDir = st.runDir() // the setup phase creates it mid-turn
			if ph.complete(runDir) {
				st.Logger.Info("phased drive: phase complete", "phase", ph.name)
				fmt.Fprintf(st.ErrOut, "\n[notice: phase %d/%d (%s) complete]\n", pi+1, len(phases), ph.label())
				break
			}
			totalTurns++
			pending := ph.pending(runDir)
			if totalTurns >= st.MaxTurns {
				st.stopMaxTurns(ph, pending)
				return nil
			}
			// P39.7 no-progress guard, per phase: a turn that mutated no suite file
			// and left the PENDING set unchanged is an "announce then yield" stall,
			// so re-prompt with the "act now" nudge, bounded before stopping.
			madeProgress := *st.IterMutations > 0 || !SameStrings(pending, prevPending)
			prevPending = pending
			nudge := ""
			if !madeProgress {
				if noProgress++; noProgress >= MaxNoProgressTurns {
					fmt.Fprintf(st.ErrOut, "\n[notice: model stalled %d turns without mutating a file during the %s phase while %d file(s) remain PENDING; stopping — re-run to resume]\n",
						noProgress, ph.label(), len(pending))
					return nil
				}
				nudge = ActNowNudge()
			} else {
				noProgress = 0
			}
			// P47.4: make in-phase continuations near-stateless. The disk is the
			// source of truth (the `<!-- PENDING -->` files persist), so instead of
			// appending each continuation to an ever-growing conversation — where
			// every re-read of the ~400-line findings file or ~210-line analysis is
			// retained for the rest of the phase and peak context climbs
			// cumulatively — reset to a fresh [system + continuation] context each
			// turn. The model re-reads only what it needs, so a phase's peak context
			// is capped at roughly one turn's reads rather than the whole phase's.
			// This is the always-on form of the P47.2 on-overflow reset; it fires
			// every turn, not just after an overflow, so overflows become rarer.
			// AEGIS_PHASE_CONV=growing restores the old accumulate-then-reset
			// behaviour for comparison (the measure-first escape hatch).
			if growingPhaseConvForced() {
				conv.Append(userMessage(nudge + phaseContinuePrompt(ph, pending, ph.contentGateReason(runDir))))
			} else {
				conv = st.freshPhaseConv(ph, runDir, pending, nudge)
			}
		}
	}
	// All content phases complete — phase 6: verify + quality, each in its own
	// fresh focused context. Undo the last content phase's narrowing first, so
	// phase 6 scopes from the session's surface rather than layering its list on
	// top of whichever phase happened to run last.
	restoreTools()
	restoreTools = func() {}
	return runPhasedVerifyAndQuality(ctx, st)
}

// stopMaxTurns logs and prints the resumable --max-turns notice for a phase.
// Shared by the normal per-turn cap and the P47.2 overflow-reset path so both
// emit the identical "re-run to resume" message.
func (st *State) stopMaxTurns(ph Phase, pending []string) {
	msg := fmt.Sprintf("phased drive hit --max-turns=%d during the %s phase with %d file(s) still PENDING: %s",
		st.MaxTurns, ph.label(), len(pending), strings.Join(pending, ", "))
	st.Logger.Warn("chat: " + msg)
	fmt.Fprintf(st.ErrOut, "\n[notice: %s — re-run to resume]\n", msg)
}

// maxPhaseOverflowResets bounds a content phase's context-overflow resets. The
// reset itself is sound recovery (the phase's `<!-- PENDING -->` files are on
// disk, so a fresh context resumes from them), but it is only recovery when the
// next attempt differs — hence OverflowEscalationDirective. This caps how many
// escalations are tried before stopping with a resumable partial suite, so a
// phase whose fill is simply too large for this model and window terminates
// with a clear reason instead of consuming the whole --max-turns budget. Sized
// like maxPhase6OverflowResets: by the third reset the directive has already
// narrowed to "one edit, then stop", and a model that still overflows is not
// going to be rescued by a fourth try.
const maxPhaseOverflowResets = 3

// stopPhaseOverflow ends the drive after a content phase exhausted its overflow
// budget. Unlike stopMaxTurns this is not "ran out of turns" — the phase's fill
// is too large for this model/window even at one edit per turn — so the notice
// names that cause and points at the levers that actually change it, rather than
// suggesting a bare re-run that would fail the same way.
func (st *State) stopPhaseOverflow(ph Phase, pending []string) {
	msg := fmt.Sprintf("phased drive stopped: the %s phase overflowed the context %d times even after narrowing to one edit per turn, with %d file(s) still PENDING: %s",
		ph.label(), maxPhaseOverflowResets, len(pending), strings.Join(pending, ", "))
	st.Logger.Warn("chat: " + msg)
	fmt.Fprintf(st.ErrOut, "\n[notice: %s — the partial suite on disk is resumable; re-run to continue, or use a model with a larger context window or a smaller scope for this phase]\n", msg)
}

// tryEscalateWindow raises the serving context window toward the model max
// after a context overflow (P47.5b), logging and printing a notice when it
// actually grows. A no-op when the drive can't escalate (non-Ollama provider,
// or num_ctx already at the model max) — the caller's fresh-context reset is the
// recovery in that case. `where` names the phase or phase-6 step for the notice.
func (st *State) tryEscalateWindow(where string) {
	if st.EscalateWindow == nil {
		return
	}
	if newWin, raised := st.EscalateWindow(); raised {
		st.Logger.Warn("phased drive: escalating serving context window after overflow", "where", where, "num_ctx", newWin)
		fmt.Fprintf(st.ErrOut, "\n[notice: raising the serving context window to %d tokens (toward the model max) after a context overflow during %s (P47.5)]\n", newWin, where)
	}
}

// freshPhaseConv builds a fresh, near-empty conversation for a phase: system
// prompt + one seed message and nothing else. It is used both by the P47.2
// on-overflow reset (nudge == "") and, as the P47.4 always-on continuation, by
// every in-phase turn (see Run). If the phase has already
// scaffolded its files (runDir set), it resumes from disk with the in-phase
// continuation prompt — the model re-reads the persisted `<!-- PENDING -->`
// files. If the setup phase overflowed/continued before the run directory even
// exists (runDir == ""), there is nothing on disk to resume from, so it restarts
// from the phase's full seed prompt. nudge (the P39.7 "act now" prefix, or "")
// is prepended to whichever prompt is chosen.
func (st *State) freshPhaseConv(ph Phase, runDir string, pending []string, nudge string) *engine.Conversation {
	conv := &engine.Conversation{System: st.System}
	if runDir == "" {
		conv.Append(userMessage(nudge + ph.promptFn(PhaseParams{
			task: st.TaskPrompt, skillDir: st.SkillDir, cwd: st.Cwd, runDir: runDir,
			contextWindow: st.contextWindow(),
		})))
	} else {
		conv.Append(userMessage(nudge + phaseContinuePrompt(ph, pending, ph.contentGateReason(runDir))))
	}
	return conv
}

// runPhasedVerifyAndQuality is phase 6 of a phased drive: run the bundled
// mechanical checks, and when they are clean run one substantive quality pass —
// each as its own fresh, run-dir-oriented turn (the P38.8 "run the checks, then
// loop failures back to the model" round, done in-harness). Mirrors the generic
// drive's verify/quality logic but with a fresh context per turn instead of
// appending to the whole-build conversation. Bounded by MaxVerifyRounds and the
// single quality pass, so it always terminates.
func runPhasedVerifyAndQuality(ctx context.Context, st *State) error {
	// P62.10: narrow to the phase's own surface for the whole round, the way
	// every content phase does. Held across the loop rather than per turn
	// because every iteration is the same phase; a re-opened content phase
	// inside the loop narrows further on top of this and restores back to it.
	defer st.scopeTools(phase6Phase(st.plan))()
	verifyRounds := 0
	overflowResets := 0
	toolFailResets := 0 // P52.3 breaker's own budget, separate from overflowResets
	loopResets := 0     // P57.1 loop guard's own budget, separate again
	// stuck carries the P57.1 escalation across one loop iteration: set when the
	// previous phase-6 turn was aborted by the engine's loop guard, consumed by
	// the next turn's prompt so the fresh context is told the verifier report is
	// authoritative rather than left to re-derive the mismatch.
	stuck := false
	qualityReviewed := false
	reopened := map[string]bool{} // P47.9: content phases already re-opened this session
	// preQuality holds a snapshot of the suite taken at the moment the mechanical
	// checks first pass, immediately before the quality pass runs (P50.3). It is
	// a known-clean state; if the quality pass edits the suite into something the
	// bounded fix rounds can't re-clean, the drive rolls back to it rather than
	// shipping a regression. Nil until the quality pass is about to run.
	var preQuality map[string][]byte
	for {
		// P50.2: canonicalize threat/finding IDs deterministically before every
		// verify, so invented `T#.<suffix>` forms and any duplicate/gapped
		// `FIND-##` are auto-fixed by a script instead of costing a model round
		// (or letting a quality-pass hand-renumber regress the suite). Idempotent,
		// so a canonical suite is untouched; best-effort — a normalizer error is
		// logged and verify.py still gates correctness.
		if ran, err := normalizeSkillIDs(st.SkillName, st.SkillDir, st.Cwd); err != nil {
			st.Logger.Warn("phased drive: ID normalizer reported an error (continuing; verify.py still gates)", "err", err)
		} else if ran {
			st.Logger.Debug("phased drive: ran deterministic ID normalizer (P50.2)")
		}
		failures, ran := VerifySkillOutputs(st.SkillName, st.SkillDir, st.Cwd)
		if !ran {
			return nil // nothing to verify (no verifier / no run dir / no python) — done
		}
		runDir := st.runDir()
		if failures == "" {
			if qualityReviewed {
				// The quality pass ran this session and the re-verify is clean.
				// Stamp the FINAL on-disk suite (computed now, after any
				// quality-pass edits) so a future unchanged re-run skips the
				// expensive pass. Best-effort: a stamp-write failure must not
				// fail an otherwise-clean drive.
				if fp, err := SuiteFingerprint(runDir); err != nil {
					st.Logger.Warn("phased drive: could not fingerprint suite for quality stamp", "err", err)
				} else if err := WriteQualityStamp(runDir, fp); err != nil {
					st.Logger.Warn("phased drive: could not write quality stamp", "err", err)
				} else {
					st.Logger.Info("phased drive: quality pass clean, wrote completion stamp", "run_dir", runDir)
				}
				return nil // verified clean and quality-reviewed — done
			}
			// Completion-stamp short-circuit: if a prior run already quality-reviewed
			// this exact suite (a valid .quality-stamp.json whose fingerprint matches
			// the current on-disk suite), skip the ~25-30 min LLM quality pass. The
			// mechanical VerifySkillOutputs above still ran and is clean, so
			// correctness is still gated; only the expensive substantive pass is skipped.
			if ShouldSkipQualityPass(runDir) {
				st.Logger.Info("phased drive: quality pass already satisfied for unchanged suite, skipping (stamp)", "run_dir", runDir)
				return nil
			}
			// P50.3: snapshot the known-clean suite before the quality pass, so a
			// pass that regresses it can be rolled back rather than shipped. Taken
			// here (mechanical checks clean, quality pass about to run) so it is
			// clean by construction. A snapshot failure is non-fatal — the guard
			// just isn't available (preQuality stays nil), matching pre-P50.3.
			if snap, err := suiteSnapshot(runDir); err != nil {
				st.Logger.Warn("phased drive: could not snapshot suite before quality pass (rollback guard disabled)", "err", err)
			} else {
				preQuality = snap
			}
			st.Logger.Info("phased drive: mechanical checks clean, running final quality pass (P38.1)")
			if err := st.runPhase6Turn(ctx, runDir, QualityReviewPrompt(), stuck); err != nil {
				switch st.recoverPhase6Error(ctx, err, "phase-6 quality pass", &overflowResets, &toolFailResets, &loopResets) {
				case overflowRetry:
					stuck = false
					continue // reset to a fresh context and re-run the quality pass
				case loopRetry:
					stuck = true // P57.1: escalate the retry's prompt, don't just reset
					continue
				case overflowStop:
					return nil // resumable stop already announced — end the drive cleanly
				default:
					return err // a non-overflow engine error is still terminal
				}
			}
			stuck = false // the turn completed — the next one starts unescalated
			// Mark reviewed only after the pass actually completed — a turn that
			// overflowed (handled above) did not finish the review, so it must not
			// be treated as done (P47.7). Otherwise the next clean re-verify would
			// stamp a suite whose quality pass never ran.
			qualityReviewed = true
			continue
		}
		// P47.9: a content-substance failure — empty finding bodies
		// (`finding-bodies-nonempty`), a mis-filed coverage row
		// (`coverage-matches-related-threats`), or any of the suite-wide
		// substance checks (P52.7's `section-bodies-nonempty`, P52.8's
		// `evidence-cells-cited` / `no-placeholder-cells` /
		// `none-identified-fraction` / `prose-sections-substantive`) — is
		// substantive authoring, not a mechanical patch. On a hollow resume
		// (markers deleted, bodies empty) the marker oracle marked every content
		// phase "complete" and jumped straight here, so ALL that authoring lands
		// on this bounded verify-fix loop; on a slow local model one large fill
		// overflows the context (2026-07-27, FirewallRiskRater) and even short of
		// that a few rounds can't author ~60 sections. Route the failure back
		// through the content phase that OWNS the failing file — resolved from
		// the `file:line` evidence against the phase globs, so a suite-wide check
		// re-opens whichever phase actually owns the gap — whose per-phase prompt
		// frames the authoring correctly and carries the incremental-edit
		// guardrail, giving it a full phase's turn budget instead of a fix round.
		// Once per phase (several phases can each be re-opened across the loop);
		// if a re-entry can't fully clear its file, fall through to the bounded
		// generic verify-fix loop below rather than looping on re-entry forever.
		if ph, ok := ownerPhaseForContentFailure(st.plan, failures); ok && !reopened[ph.name] {
			reopened[ph.name] = true
			if err := st.runReopenedContentPhase(ctx, ph); err != nil {
				return err // a non-overflow engine error is terminal (overflow is handled inside)
			}
			continue // re-verify: cleared checks fall through, residue hits the generic loop
		}
		if verifyRounds++; verifyRounds > MaxVerifyRounds {
			// P50.3: if the quality pass had a known-clean snapshot and the fix
			// rounds since could not re-clean the suite, the failures were
			// introduced by the quality pass — roll back to that clean state and
			// stamp it, rather than shipping a suite that verifies worse than the
			// one the quality pass was handed. The snapshot passed the same
			// mechanical checks by construction, so the restored suite is clean;
			// we stamp its fingerprint directly.
			if preQuality != nil {
				st.Logger.Warn("phased drive: quality pass regressed the suite and fix rounds could not heal it; rolling back to the pre-quality clean snapshot (P50.3)", "rounds", MaxVerifyRounds)
				fmt.Fprintf(st.ErrOut, "\n[notice: the final quality pass left the suite failing a mechanical check the fix rounds couldn't resolve; rolling back to the verified-clean state from just before the quality pass (P50.3)]\n")
				if err := restoreSuiteSnapshot(runDir, preQuality); err != nil {
					st.Logger.Warn("phased drive: rollback to pre-quality snapshot failed; stopping with the unverified suite", "err", err)
				} else if fp, err := SuiteFingerprint(runDir); err != nil {
					st.Logger.Warn("phased drive: rolled back but could not fingerprint for the stamp", "err", err)
					return nil
				} else if err := WriteQualityStamp(runDir, fp); err != nil {
					st.Logger.Warn("phased drive: rolled back but could not write the completion stamp", "err", err)
					return nil
				} else {
					st.Logger.Info("phased drive: rolled back to the clean pre-quality suite and stamped it", "run_dir", runDir)
					return nil
				}
			}
			st.Logger.Warn("chat: verification still failing after max rounds", "rounds", MaxVerifyRounds)
			fmt.Fprintf(st.ErrOut, "\n[notice: phase-6 verification still failing after %d fix round(s); stopping with an unverified suite — inspect the run directory and the failures above]\n", MaxVerifyRounds)
			fmt.Fprintf(st.ErrOut, "%s\n", failures)
			return nil
		}
		st.Logger.Info("phased drive: verification failed, feeding back for fix", "round", verifyRounds)
		if err := st.runPhase6Turn(ctx, runDir, VerifyFixPrompt(failures), stuck); err != nil {
			switch st.recoverPhase6Error(ctx, err, "phase-6 verify fix", &overflowResets, &toolFailResets, &loopResets) {
			case overflowRetry:
				stuck = false
				verifyRounds-- // an overflow/backend-reset is not a spent fix attempt — don't burn the round on it
				continue       // reset to a fresh context and re-run verify + fix from disk
			case loopRetry:
				stuck = true   // P57.1: escalate the retry's prompt, don't just reset
				verifyRounds-- // a loop abort made no fix either — don't burn the round on it
				continue
			case overflowStop:
				return nil // resumable stop already announced — end the drive cleanly
			default:
				return err
			}
		}
		stuck = false
	}
}

// scopeTools narrows the model's tool surface to what this phase needs,
// returning the restore. A nil hook or an empty allowlist yields a no-op, so
// callers and phases that don't opt in behave exactly as before.
func (st *State) scopeTools(ph Phase) func() {
	if st.ScopeTools == nil || len(ph.tools) == 0 {
		return func() {}
	}
	restore := st.ScopeTools(ph.tools)
	if restore == nil {
		return func() {}
	}
	st.Logger.Info("phased drive: narrowed tool surface for phase", "phase", ph.name, "tools", ph.tools)
	return restore
}

// repairSkillAssets restores any materialized built-in skill file under
// <cwd>/.aegis/builtin-skills that no longer matches the copy compiled into
// this binary, at every phase boundary.
//
// resolveWrite already refuses the file tools a path in that tree, but the
// shell tool can still reach it — a model running `python -c`, a redirect, or
// a stray `>` has no such gate. Observed live (P38.1 re-test, 2026-08-09): a
// model replaced recon.py with the command line it meant to run, and every
// later phase inherited a script that raised SyntaxError with nothing pointing
// at why. Phase boundaries are the natural repair point: the drive already
// resets context there, and a phase starting against corrupted tooling is
// exactly the case that cannot recover on its own.
//
// Best-effort by design. A failure to repair must not end a drive that might
// still complete, so this logs and returns; the phase then runs against
// whatever is on disk, as it did before.
func (st *State) repairSkillAssets() {
	if st.Cwd == "" {
		return
	}
	restored, err := skills.RefreshProjectBuiltins(st.Cwd)
	if err != nil {
		st.Logger.Warn("phased drive: could not refresh materialized skill assets", "err", err)
		return
	}
	if len(restored) == 0 {
		return
	}
	st.Logger.Warn("phased drive: restored modified built-in skill files", "files", restored)
	fmt.Fprintf(st.ErrOut, "\n[notice: restored %d built-in skill file(s) modified during the run: %s]\n",
		len(restored), strings.Join(restored, ", "))
}

// runPhase6Turn runs one phase-6 turn (a verify fix or the quality pass) in its
// own fresh conversation, prefixed with an orientation preamble naming the run
// directory — the fresh context has no memory of building the suite, so it must
// be told where the files are and to read them first. stuck (P57.1) prefixes the
// StuckLoopDirective when the previous attempt was aborted by the engine's loop
// guard.
func (st *State) runPhase6Turn(ctx context.Context, runDir, instruction string, stuck bool) error {
	conv := &engine.Conversation{System: st.System}
	conv.Append(userMessage(phase6TurnPrompt(runDir, st.SkillDir, instruction, stuck)))
	*st.IterToolCalls = 0
	*st.IterMutations = 0
	return st.Engine.Run(ctx, conv, st.OnEvent)
}

// --- P47.9: route hollow-body / content-substance failures to the owning phase ---

// contentSubstanceCheck names a verify.py check whose failure is substantive
// authoring work a content phase should re-do — not a mechanical patch the
// generic phase-6 fix turn can make — together with the phase that owns the file
// the check reads.
type contentSubstanceCheck struct {
	check string // verify.py check name, as it appears after "FAIL " in the report
	// phase names the Phase that owns this check's file, for a check that
	// only ever reads one file. Empty when perFile is set.
	phase string
	// perFile marks a check that runs across the whole suite, so which phase
	// owns a given failure is a property of the *evidence*, not of the check.
	// The owner comes from the `file:line` prefix on each evidence line,
	// matched against the phase globs (see fileOwnerPhase).
	perFile bool
}

// contentSubstanceChecks are the verify.py checks the phased drive routes back
// through their owning content phase (P47.9) instead of the generic verify-fix
// turn — the failure is substantive authoring work, not a mechanical patch a
// bounded fix turn can make in one pass.
//
// The first two read `3-findings.md` alone, which the findings phase owns: a
// hollow resume (markers deleted, bodies left empty) fails
// `finding-bodies-nonempty`, and a coverage row filed under the wrong finding
// fails `coverage-matches-related-threats`.
//
// The rest are suite-wide (P52.7's check 15 and P52.8's checks 16-19), and
// before file-aware routing they had no way to say which phase should re-open:
// `contentSubstanceChecks` mapped check name → phase, and one name covers all
// seven suite files. They fell through to the generic verify-fix turn — the
// exact fall-through P47.9 exists to prevent, reintroduced by making the checks
// broader. They now route on the failing file instead.
//
// Ordered so routing is deterministic.
var contentSubstanceChecks = []contentSubstanceCheck{
	{check: "finding-bodies-nonempty", phase: "findings"},
	{check: "coverage-matches-related-threats", phase: "findings"},
	// component-name-consistency is authoring, not patching: when the analysis
	// file omits components the architecture and DFD both name, closing it means
	// writing a full STRIDE section per missing component. The bounded
	// three-round verify-fix loop cannot do that — measured on qwen3:14b
	// (2026-08-09), eleven components stayed missing through all three rounds
	// and the drive stopped with an unverified suite. It routes by phase rather
	// than perFile because verify.py's evidence for this check names components,
	// not `file:line`, so there is no file for the per-file router to match.
	{check: "component-name-consistency", phase: "framework-analysis"},
	{check: "section-bodies-nonempty", perFile: true},
	{check: "evidence-cells-cited", perFile: true},
	{check: "no-placeholder-cells", perFile: true},
	{check: "none-identified-fraction", perFile: true},
	{check: "prose-sections-substantive", perFile: true},
}

// contentEvidenceFileRE pulls the filename off a verify.py evidence line.
// verify.py formats every piece of evidence as `<basename>:<line>  <text>`
// (its ev() helper) and prints it indented under the FAIL line, so the
// basename is everything up to the first colon.
var contentEvidenceFileRE = regexp.MustCompile(`^\s*-\s+([^\s:]+):\d+\s`)

// failuresContainCheck reports whether the verify report text carries a failing
// entry for the named check. verify.py prints one `FAIL <check-name>` line per
// failing check (see its main()), so a prefix match on that line is exact.
func failuresContainCheck(failures, check string) bool {
	for _, ln := range strings.Split(failures, "\n") {
		if strings.TrimSpace(ln) == "FAIL "+check {
			return true
		}
	}
	return false
}

// phaseByName returns the phase with the given name from a plan.
func phaseByName(phases []Phase, name string) (Phase, bool) {
	for _, ph := range phases {
		if ph.name == name {
			return ph, true
		}
	}
	return Phase{}, false
}

// fileOwnerPhase returns the phase that owns a suite file, matching the
// basename against each phase's globs.
//
// `Phase.globs` is already a file→phase table — the phased drive reads it
// the other way (phase → the files it must clear of PENDING markers), and this
// reads it as written. That is why file-aware routing needs no new mapping to
// maintain: a phase that gains a file automatically gains its failures.
func fileOwnerPhase(phases []Phase, file string) (Phase, bool) {
	base := filepath.Base(file)
	for _, ph := range phases {
		for _, g := range ph.globs {
			if ok, err := filepath.Match(g, base); err == nil && ok {
				return ph, true
			}
		}
	}
	return Phase{}, false
}

// contentFailureFiles returns the distinct files named in the evidence of a
// failing check, in report order. A check with no parseable evidence yields
// nothing, so it simply does not route.
func contentFailureFiles(failures, check string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, ln := range checkEvidenceLines(failures, check) {
		m := contentEvidenceFileRE.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// checkEvidenceLines returns the indented evidence lines belonging to one
// failing check. verify.py prints evidence as indented lines under each FAIL
// line; any non-indented line (the next PASS/FAIL, the summary, or
// VerifySkillOutputs' `$ …` header) ends the block.
func checkEvidenceLines(failures, check string) []string {
	var out []string
	capturing := false
	for _, ln := range strings.Split(failures, "\n") {
		if strings.HasPrefix(ln, "FAIL ") {
			capturing = strings.TrimSpace(strings.TrimPrefix(ln, "FAIL ")) == check
			continue
		}
		if !capturing {
			continue
		}
		if len(ln) > 0 && (ln[0] == ' ' || ln[0] == '\t') {
			out = append(out, ln)
			continue
		}
		capturing = false
	}
	return out
}

// ownerPhaseForContentFailure returns the content phase that owns the first
// content-substance failure present in the verify report, if any.
//
// Phases are resolved against the given plan, so a skill whose plan has no
// matching phase never routes: the fixed-route check names are
// threat-model-specific (only that plan has a "findings" phase), and a per-file
// failure only routes when some phase's globs claim the failing file.
func ownerPhaseForContentFailure(phases []Phase, failures string) (Phase, bool) {
	for _, c := range contentSubstanceChecks {
		if !failuresContainCheck(failures, c.check) {
			continue
		}
		if !c.perFile {
			if ph, ok := phaseByName(phases, c.phase); ok {
				return ph, true
			}
			continue
		}
		for _, f := range contentFailureFiles(failures, c.check) {
			if ph, ok := fileOwnerPhase(phases, f); ok {
				return ph, true
			}
		}
	}
	return Phase{}, false
}

// phaseHasContentFailure reports whether any content-substance failure this
// phase owns is still present — the completion oracle for a re-opened phase
// (P47.9), which cannot use the PENDING-marker oracle because a hollow resume
// has no markers left.
//
// For a per-file check that means "this check fails on a file this phase owns",
// not "this check fails": with suite-wide checks the latter would keep a phase
// re-opening over another phase's file, which it is told not to edit and so
// could never clear.
func phaseHasContentFailure(ph Phase, failures string) bool {
	for _, c := range contentSubstanceChecks {
		if !failuresContainCheck(failures, c.check) {
			continue
		}
		if !c.perFile {
			if c.phase == ph.name {
				return true
			}
			continue
		}
		for _, f := range contentFailureFiles(failures, c.check) {
			if owner, ok := fileOwnerPhase([]Phase{ph}, f); ok && owner.name == ph.name {
				return true
			}
		}
	}
	return false
}

// checksForPhase returns the content-substance check names whose failures this
// phase owns, used to extract just that phase's evidence for its re-entry
// prompt. A per-file check is included only when it actually fails on one of
// this phase's files — including it unconditionally would put another phase's
// evidence in the prompt.
func checksForPhase(ph Phase, failures string) []string {
	var out []string
	for _, c := range contentSubstanceChecks {
		if !c.perFile {
			if c.phase == ph.name {
				out = append(out, c.check)
			}
			continue
		}
		for _, f := range contentFailureFiles(failures, c.check) {
			if owner, ok := fileOwnerPhase([]Phase{ph}, f); ok && owner.name == ph.name {
				out = append(out, c.check)
				break
			}
		}
	}
	return out
}

// phaseOwnsEvidenceLine reports whether an evidence line names a file this
// phase owns. A line with no parseable `file:line` prefix belongs to whoever
// asked — it carries no routing information of its own, so dropping it would
// lose evidence.
func phaseOwnsEvidenceLine(ph Phase, line string) bool {
	m := contentEvidenceFileRE.FindStringSubmatch(line)
	if m == nil {
		return true
	}
	owner, ok := fileOwnerPhase([]Phase{ph}, m[1])
	return ok && owner.name == ph.name
}

// extractCheckFailures pulls just the `FAIL <check>` blocks (the FAIL line plus
// its indented evidence lines) for the named checks out of a full verify report,
// so a re-entry prompt names the empty sections without dumping every unrelated
// failing check at the model. verify.py prints evidence as indented `- …` lines
// under each FAIL line; a non-indented line (the next PASS/FAIL, the summary, or
// VerifySkillOutputs' `$ …` header) ends the block. Returns "" if nothing
// matched, so the caller can fall back to the full text.
//
// keep, when non-nil, additionally filters the evidence lines within a kept
// block. That is what confines a suite-wide check's evidence to the re-opening
// phase's own files: without it, re-opening "framework analysis" over a
// `section-bodies-nonempty` failure would hand the model every other file's
// empty sections too and invite it to edit files the phase does not own. A
// block whose evidence is entirely filtered out is dropped along with its FAIL
// line, so the prompt never names a check with nothing under it.
func extractCheckFailures(failures string, checks []string, keep func(line string) bool) string {
	want := make(map[string]bool, len(checks))
	for _, c := range checks {
		want[c] = true
	}
	var out []string
	var block []string
	var sawEvidence bool // the block had evidence before filtering
	capturing := false
	flush := func() {
		// Drop a block only when filtering removed every line it had. A check
		// that failed with no evidence at all still gets its FAIL line, the
		// same as before file-aware filtering existed.
		if len(block) > 0 && (len(block) > 1 || !sawEvidence) {
			out = append(out, block...)
		}
		block, sawEvidence = nil, false
	}
	for _, ln := range strings.Split(failures, "\n") {
		if strings.HasPrefix(ln, "FAIL ") {
			flush()
			capturing = want[strings.TrimSpace(strings.TrimPrefix(ln, "FAIL "))]
			if capturing {
				block = []string{ln}
			}
			continue
		}
		if !capturing {
			continue
		}
		if len(ln) > 0 && (ln[0] == ' ' || ln[0] == '\t') {
			sawEvidence = true
			if keep == nil || keep(ln) {
				block = append(block, ln) // an indented evidence line
			}
			continue
		}
		flush() // any non-indented line ends the block
		capturing = false
	}
	flush()
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// runReopenedContentPhase re-drives a content phase whose file failed a
// content-substance check (P47.9), in its own bounded, near-stateless
// fresh-context loop. Unlike a normal content phase its completion oracle is the
// verify check clearing (a hollow resume has no PENDING markers to count), and
// each turn re-reads the suite from disk and re-runs the verifier for fresh
// evidence — so peak context stays bounded exactly as P47.4 does for the content
// phases. It returns nil on every resumable outcome (checks cleared, turn/stall
// budget, or a persistent overflow — all of which hand control back to the
// phase-6 loop) and a non-nil error only on a terminal (non-overflow) engine
// error.
func (st *State) runReopenedContentPhase(ctx context.Context, ph Phase) error {
	runDir := st.runDir()
	failures, _ := VerifySkillOutputs(st.SkillName, st.SkillDir, st.Cwd)
	st.Logger.Info("phased drive: re-opening content phase for a content-substance failure (P47.9)", "phase", ph.name)
	fmt.Fprintf(st.ErrOut, "\n[notice: the %s file failed a content-substance check the bounded phase-6 fix loop can't author in one pass; re-opening the %s phase to fill it (P47.9)]\n", ph.label(), ph.label())

	// The re-entry is a phase turn and must get the phase's tool surface. It
	// was left out when per-phase narrowing landed, so a re-opened phase ran
	// with every tool exposed — including the write_file the narrowing exists
	// to withhold from a fill phase.
	defer st.scopeTools(ph)()

	conv := st.hollowReentryConv(ph, runDir, failures, "")
	turns := 0
	overflowResets := 0
	toolFailResets := 0 // P52.3 breaker's own budget, separate from overflowResets
	loopResets := 0     // P57.1 loop guard's own budget, separate again
	noProgress := 0
	for {
		*st.IterToolCalls = 0
		*st.IterMutations = 0
		if err := st.Engine.Run(ctx, conv, st.OnEvent); err != nil {
			// P50.1: a dead backend during a re-entry is resumable too — wait,
			// then re-read the suite from disk into a fresh context.
			switch st.recoverBackendDown(ctx, err, ph.label()+" re-entry") {
			case backendRecovered:
				runDir = st.runDir()
				failures, _ = VerifySkillOutputs(st.SkillName, st.SkillDir, st.Cwd)
				conv = st.hollowReentryConv(ph, runDir, failures, "")
				continue
			case backendGaveUp:
				return nil
			}
			if provider.IsContextOverflowError(err) {
				if overflowResets++; overflowResets > maxPhase6OverflowResets {
					st.Logger.Warn("phased drive: hollow-body re-entry overflow persists after max resets", "phase", ph.name)
					fmt.Fprintf(st.ErrOut, "\n[notice: the %s re-entry kept overflowing after %d reset(s); handing back to the phase-6 fix loop]\n", ph.label(), maxPhase6OverflowResets)
					return nil
				}
				st.tryEscalateWindow(ph.label() + " re-entry")
				fmt.Fprintf(st.ErrOut, "\n[notice: context overflowed re-authoring the %s file; resetting to a fresh context and re-reading from disk (reset %d/%d)]\n", ph.label(), overflowResets, maxPhase6OverflowResets)
				runDir = st.runDir()
				failures, _ = VerifySkillOutputs(st.SkillName, st.SkillDir, st.Cwd)
				conv = st.hollowReentryConv(ph, runDir, failures, "")
				continue
			}
			// P52.3: same treatment as an overflow — a breaker trip during a
			// re-entry is resumable from disk. On give-up hand back to the
			// phase-6 fix loop (nil) rather than killing the drive, exactly as
			// the overflow budget above does.
			switch st.recoverToolFailureStall(err, ph.label()+" re-entry", &toolFailResets) {
			case overflowRetry:
				runDir = st.runDir()
				failures, _ = VerifySkillOutputs(st.SkillName, st.SkillDir, st.Cwd)
				conv = st.hollowReentryConv(ph, runDir, failures, "")
				continue
			case overflowStop:
				return nil
			}
			// P57.1: and the same again for a loop-guard abort — the failure
			// this whole re-entry path exists to survive. This is where the
			// 2026-08-03 run died: a terminal error here killed the drive even
			// though the suite on disk was intact and the verifier's report
			// still named exactly what to fix. The retry additionally carries
			// StuckLoopDirective so the fresh context is handed the report as
			// ground truth instead of being invited to re-derive it.
			switch st.recoverReasoningLoop(err, ph.label()+" re-entry", &loopResets) {
			case loopRetry:
				runDir = st.runDir()
				failures, _ = VerifySkillOutputs(st.SkillName, st.SkillDir, st.Cwd)
				conv = st.hollowReentryConv(ph, runDir, failures, StuckLoopDirective(true))
				continue
			case overflowStop:
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		failures, _ = VerifySkillOutputs(st.SkillName, st.SkillDir, st.Cwd)
		if !phaseHasContentFailure(ph, failures) {
			st.Logger.Info("phased drive: content-substance re-entry cleared the owning check(s)", "phase", ph.name)
			return nil
		}
		if turns++; turns >= st.MaxTurns {
			fmt.Fprintf(st.ErrOut, "\n[notice: the %s re-entry hit --max-turns=%d with the content-substance check still failing; handing back to the phase-6 fix loop]\n", ph.label(), st.MaxTurns)
			return nil
		}
		nudge := ""
		if *st.IterMutations > 0 {
			noProgress = 0
		} else {
			if noProgress++; noProgress >= MaxNoProgressTurns {
				fmt.Fprintf(st.ErrOut, "\n[notice: the %s re-entry stalled %d turns without an edit; handing back to the phase-6 fix loop]\n", ph.label(), noProgress)
				return nil
			}
			nudge = ActNowNudge()
		}
		runDir = st.runDir()
		conv = st.hollowReentryConv(ph, runDir, failures, nudge)
	}
}

// hollowReentryConv builds the fresh, near-stateless conversation each re-entry
// turn runs with: system prompt + one hollow-body authoring message and nothing
// else (P47.9, sharing P47.4's bounded-context discipline).
func (st *State) hollowReentryConv(ph Phase, runDir, failures, nudge string) *engine.Conversation {
	conv := &engine.Conversation{System: st.System}
	conv.Append(userMessage(nudge + hollowBodyReentryPrompt(ph, runDir, st.SkillDir, failures)))
	return conv
}

// userMessage wraps text as a user-role message.
func userMessage(text string) provider.Message {
	return provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: text}}}
}
