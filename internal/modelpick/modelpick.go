// Package modelpick ranks the models a local model server already has pulled
// and picks the pair Aegis should be configured with: a main model and a
// smaller companion for background work (provider.small_model).
//
// It exists because "which model" was previously answered three different ways
// in three places, and all three answered it badly:
//
//   - `aegis --first-init` took GET /api/tags' first entry, which is Ollama's
//     most-recently-*modified* model. Pull a 3B for a one-off and the next
//     --first-init pins the machine to it, ignoring the 9B sitting beside it.
//   - The `/config` wizard took discover.Discover's first entry, which is
//     sorted alphabetically. Same defect, different arbitrary order.
//   - provider.model: "auto" resolves at runtime to the same tags[0].
//
// None of them looked at parameter count, capabilities, or whether the machine
// could hold the thing. This package is the one answer all of them now use.
//
// # What it ranks on, and what it deliberately does not
//
// The main model is the largest model (by parameter count) whose weights fit a
// stated memory ceiling. Bigger is a coarse proxy for better, but it is the
// only quality signal available locally without running an eval — and Aegis
// already owns the two real ones (internal/toolcallprobe measures whether a
// model can actually call tools; internal/eval scores behavior). Those need a
// live model and minutes of wall clock; this needs a 2-second HTTP call at
// setup time. So the rule is: rank on size, break ties on capabilities, and
// leave the measured verdicts to the machinery built for them.
//
// Tool-calling capability is a *tiebreak*, never a filter. Ollama reports
// capabilities from the model's manifest, and a model imported from a raw GGUF
// via a custom Modelfile loses the claim while keeping the ability — measured
// on this project's own aegis-qwen35-9b, which reports ["completion","vision"]
// and calls tools fine. Filtering on the claim would reject the best model on
// the machine in favor of a 3B that happens to have a complete manifest.
//
// No GPU/VRAM detection is attempted, ever. See internal/hwinfo's package note
// and P17.5: Ollama does not expose its own VRAM budget, so the alternative is
// reimplementing that heuristic blind from nvidia-smi. What this package uses
// instead is, in order of preference, the operator's stated
// provider.vram_budget_gb, then total system RAM as a sanity bound (which stops
// a 70B being pinned on a 16 GB laptop without pretending to know the card),
// then no bound at all.
package modelpick

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/hwinfo"
)

// Model is one locally-pulled model, carrying the fields GET /api/tags already
// returns. Every field except Name is optional: an older server, or a model
// imported from a GGUF, may report none of them, and the ranking degrades to
// on-disk size rather than refusing to choose.
type Model struct {
	Name          string
	Family        string    // details.family, e.g. "qwen35"
	ParameterSize string    // details.parameter_size, e.g. "9.2B"
	Quantization  string    // details.quantization_level, e.g. "Q4_K_M"
	SizeBytes     int64     // on-disk size — see kvfit.go on why this overstates resident weights for a multimodal model
	Capabilities  []string  // e.g. ["completion","tools","thinking","vision"]
	ModifiedAt    time.Time // last tiebreak, so the ranking is total and stable
}

// Selection is the outcome: what to write into provider.model,
// provider.small_model and provider.think, plus the sentences explaining why.
type Selection struct {
	Main  string
	Small string // "" when nothing on the machine is meaningfully smaller
	// Think is whether the *main* model looks like it reasons before answering.
	// A wrong guess in either direction is cheap: the native adapter latches and
	// silently drops `think` the first time a model 400s on it (P52.5).
	Think bool

	// Ceiling is the weight budget the ranking was solved against, in bytes, and
	// CeilingSource names where it came from. Ceiling 0 means unbounded.
	Ceiling       int64
	CeilingSource string

	// Reasons are display lines, in order, explaining the pick. Callers print
	// them; nothing parses them.
	Reasons []string
}

// ─── Capability predicates ───────────────────────────────────────────────────

// HasCapability reports whether m's manifest advertises c.
func (m Model) HasCapability(c string) bool {
	for _, got := range m.Capabilities {
		if strings.EqualFold(got, c) {
			return true
		}
	}
	return false
}

// ChatCapable reports whether m can serve chat completions at all. An
// embedding-only model (nomic-embed-text and friends) reports
// capabilities:["embedding"] with no "completion" and is typically the smallest
// thing pulled, which made it the accidental small_model before this check
// existed. A model reporting *no* capabilities (a pre-0.6 server) is assumed
// capable: absence of the field is not evidence either way.
func (m Model) ChatCapable() bool {
	if len(m.Capabilities) == 0 {
		return true
	}
	return m.HasCapability("completion")
}

// ToolCapable reports the manifest's tools claim. Advisory only — see the
// package note on why this never filters.
func (m Model) ToolCapable() bool { return m.HasCapability("tools") }

// thinkingMarkers are substrings of a model name or family that identify a
// reasoning/thinking model when the manifest does not say so itself. Ollama
// only reports the "thinking" capability for models whose manifest carries it,
// which a GGUF import does not, so the name remains the fallback signal.
var thinkingMarkers = []string{
	"thinking", "reasoning", "deepseek", "-r1", ":r1", "qwq", "-deep",
	"magistral", "gpt-oss", "qwen3", "o1-", "o3-",
}

// nonThinkingMarkers override thinkingMarkers. Several families ship a
// reasoning line and an instruct/coder line under one prefix — qwen3-coder and
// qwen3-*-instruct do not think — so a bare prefix match would flip `think` on
// for exactly the variants chosen to avoid it.
var nonThinkingMarkers = []string{"coder", "instruct", "-nothink", "non-thinking"}

// Thinks reports whether m reasons before answering: the manifest's own
// "thinking" capability when it has one, else the name/family heuristic above.
func (m Model) Thinks() bool {
	if m.HasCapability("thinking") {
		return true
	}
	return NameSuggestsThinking(m.Name) || NameSuggestsThinking(m.Family)
}

// NameSuggestsThinking is Thinks's pure name half, exported because `aegis
// doctor` warns from the same heuristic and the two must not drift.
func NameSuggestsThinking(name string) bool {
	n := strings.ToLower(name)
	if n == "" {
		return false
	}
	for _, marker := range nonThinkingMarkers {
		if strings.Contains(n, marker) && !strings.Contains(n, "thinking") {
			return false
		}
	}
	for _, marker := range thinkingMarkers {
		if strings.Contains(n, marker) {
			return true
		}
	}
	return false
}

// ─── Size ────────────────────────────────────────────────────────────────────

// bytesPerBillionParams is the rough on-disk cost of a billion parameters at
// the Q4_K_M quantization almost every locally-pulled model uses. Only used to
// synthesize a parameter count when details.parameter_size is absent, so that
// such a model still sorts against the others rather than sorting as zero.
const bytesPerBillionParams = 620_000_000

// Params returns m's parameter count in billions, from
// details.parameter_size when present and from on-disk size otherwise.
// 0 means neither was available.
func (m Model) Params() float64 {
	if b, ok := parseParamSize(m.ParameterSize); ok {
		return b
	}
	if m.SizeBytes > 0 {
		return float64(m.SizeBytes) / bytesPerBillionParams
	}
	return 0
}

// parseParamSize parses Ollama's details.parameter_size — "9.2B", "3.8B",
// "137M", "70b" — into billions of parameters.
func parseParamSize(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(strings.ToUpper(s), "B"):
		s = s[:len(s)-1]
	case strings.HasSuffix(strings.ToUpper(s), "M"):
		s, mult = s[:len(s)-1], 1.0/1000
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v * mult, true
}

// ─── Memory ceiling ──────────────────────────────────────────────────────────

// ramCeilingFraction is the share of total system RAM a model's weights may
// occupy when no explicit budget was stated. It is a sanity bound, not a fit:
// its whole job is to stop a 70B being pinned on a 16 GB machine, while still
// admitting a 17 GB model on a 32 GB unified-memory laptop where that is
// exactly the right pick.
const ramCeilingFraction = 0.75

// kvHeadroom is the multiplier applied to a model's weights before comparing
// them to the ceiling, reserving room for the KV cache the serving window will
// need on top. Deliberately small: internal/ollamainfo/kvfit.go computes the
// cache exactly from the model's geometry, and that arithmetic — not this
// fudge factor — is what sizes context_window. This only has to keep the
// ranking from picking something with no room to serve at all.
const kvHeadroom = 1.15

// Ceiling returns the weight budget the ranking is solved against, in bytes,
// with a phrase naming its source. A stated provider.vram_budget_gb wins; total
// system RAM is the fallback sanity bound; 0 means nothing was knowable and the
// ranking runs unbounded.
func Ceiling(hw hwinfo.Info, budgetGB float64) (int64, string) {
	if budgetGB > 0 {
		return int64(budgetGB * float64(int64(1)<<30)), fmt.Sprintf("provider.vram_budget_gb (%.4g GiB)", budgetGB)
	}
	if hw.RAMKnown() {
		return int64(float64(hw.TotalRAMBytes) * ramCeilingFraction),
			fmt.Sprintf("%.0f%% of ~%.0f GB detected system RAM (no VRAM detection — see internal/hwinfo)",
				ramCeilingFraction*100, hw.TotalRAMGB())
	}
	return 0, "unbounded (neither provider.vram_budget_gb nor system RAM was knowable)"
}

// fits reports whether m's weights plus KV headroom sit inside ceiling. A zero
// ceiling admits everything; a model that reports no size at all is admitted
// rather than excluded, for the same reason an absent capabilities list is.
func fits(m Model, ceiling int64) bool {
	if ceiling <= 0 || m.SizeBytes <= 0 {
		return true
	}
	return float64(m.SizeBytes)*kvHeadroom <= float64(ceiling)
}

// ─── Selection ───────────────────────────────────────────────────────────────

// smallModelParamRatio is how much smaller than the main model a candidate must
// be to be worth naming as small_model. small_model exists to make session
// titles, compaction and guard verdicts *cheap*; routing them to a model the
// same size as the primary buys nothing and costs a second resident set of
// weights.
const smallModelParamRatio = 0.7

// Select ranks models and returns the configuration to write. hw supplies the
// fallback memory ceiling and budgetGB the operator's stated one (0 when none
// has been stated yet, which is the case during --first-init).
//
// An empty or all-embedding list returns a zero Selection with a Reason saying
// so; callers must treat Main == "" as "leave provider.model alone".
func Select(models []Model, hw hwinfo.Info, budgetGB float64) Selection {
	ceiling, ceilingSrc := Ceiling(hw, budgetGB)
	sel := Selection{Ceiling: ceiling, CeilingSource: ceilingSrc}

	chat := make([]Model, 0, len(models))
	for _, m := range models {
		if m.Name != "" && m.ChatCapable() {
			chat = append(chat, m)
		}
	}
	if len(chat) == 0 {
		sel.Reasons = append(sel.Reasons, "No pulled model can serve chat completions, so no model was selected.")
		return sel
	}

	candidates := make([]Model, 0, len(chat))
	for _, m := range chat {
		if fits(m, ceiling) {
			candidates = append(candidates, m)
		}
	}
	overBudget := len(chat) - len(candidates)
	if len(candidates) == 0 {
		// Every model is over budget. Refusing to choose would leave the
		// operator with provider.model: "auto", which resolves to an arbitrary
		// one anyway — so pick the smallest and say plainly that it is a
		// compromise.
		candidates = append(candidates, smallestBySize(chat))
		sel.Reasons = append(sel.Reasons,
			fmt.Sprintf("Every pulled model is larger than the memory ceiling (%s); picked the smallest and it may still spill to system RAM.", ceilingSrc))
		overBudget = 0
	}

	sort.SliceStable(candidates, func(i, j int) bool { return betterMain(candidates[i], candidates[j]) })
	main := candidates[0]
	sel.Main = main.Name
	sel.Think = main.Thinks()

	sel.Reasons = append(sel.Reasons, fmt.Sprintf("Main model: %s%s — %s.",
		main.Name, describeSize(main), mainRationale(candidates, overBudget)))
	sel.Reasons = append(sel.Reasons, "Memory ceiling: "+ceilingSrc+".")
	if sel.Think {
		sel.Reasons = append(sel.Reasons, fmt.Sprintf(
			"provider.think: true — %s. Set it false if answers ramble or the model turns out not to reason.", thinkRationale(main)))
	}

	if small, ok := selectSmall(chat, main, ceiling); ok {
		sel.Small = small.Name
		sel.Reasons = append(sel.Reasons, fmt.Sprintf("Small model: %s%s — %s.",
			small.Name, describeSize(small), smallRationale(small)))
	} else {
		sel.Reasons = append(sel.Reasons,
			"No small_model: nothing pulled is meaningfully smaller than the main model, so background calls stay on it.")
	}
	return sel
}

// betterMain is the main-model ordering: parameter count first, because it is
// the only locally-observable quality proxy; then the manifest's tools claim,
// then a larger on-disk size at equal parameters (a less aggressive
// quantization), then recency, then name — the last two only so the order is
// total and a re-run of --first-init on an unchanged machine writes the same
// file.
func betterMain(a, b Model) bool {
	if pa, pb := a.Params(), b.Params(); !nearlyEqual(pa, pb) {
		return pa > pb
	}
	if a.ToolCapable() != b.ToolCapable() {
		return a.ToolCapable()
	}
	if a.SizeBytes != b.SizeBytes {
		return a.SizeBytes > b.SizeBytes
	}
	if !a.ModifiedAt.Equal(b.ModifiedAt) {
		return a.ModifiedAt.After(b.ModifiedAt)
	}
	return a.Name < b.Name
}

// selectSmall picks provider.small_model: the cheapest model that can serve the
// background calls without dragging a reasoning trace through them. Preference
// order is non-thinking, then tool-capable, then smallest — the template's own
// advice ("pick a small NON-thinking model you have pulled"), applied.
func selectSmall(chat []Model, main Model, ceiling int64) (Model, bool) {
	mainParams := main.Params()
	var pool []Model
	for _, m := range chat {
		if m.Name == main.Name || !fits(m, ceiling) {
			continue
		}
		if mainParams > 0 && m.Params() > mainParams*smallModelParamRatio {
			continue
		}
		pool = append(pool, m)
	}
	if len(pool) == 0 {
		return Model{}, false
	}
	sort.SliceStable(pool, func(i, j int) bool { return betterSmall(pool[i], pool[j]) })
	return pool[0], true
}

func betterSmall(a, b Model) bool {
	if a.Thinks() != b.Thinks() {
		return !a.Thinks()
	}
	if a.ToolCapable() != b.ToolCapable() {
		return a.ToolCapable()
	}
	if pa, pb := a.Params(), b.Params(); !nearlyEqual(pa, pb) {
		return pa < pb
	}
	if a.SizeBytes != b.SizeBytes {
		return a.SizeBytes < b.SizeBytes
	}
	if !a.ModifiedAt.Equal(b.ModifiedAt) {
		return a.ModifiedAt.After(b.ModifiedAt)
	}
	return a.Name < b.Name
}

// nearlyEqual treats parameter counts within 2% as a tie, so "9.2B" and "9B"
// tags of one model are separated by capabilities rather than by rounding in
// whoever wrote the manifest.
func nearlyEqual(a, b float64) bool {
	if a == b {
		return true
	}
	hi := a
	if b > hi {
		hi = b
	}
	if hi == 0 {
		return true
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff/hi < 0.02
}

func smallestBySize(models []Model) Model {
	best := models[0]
	for _, m := range models[1:] {
		if m.SizeBytes > 0 && (best.SizeBytes == 0 || m.SizeBytes < best.SizeBytes) {
			best = m
		}
	}
	return best
}

// ─── Display ─────────────────────────────────────────────────────────────────

func describeSize(m Model) string {
	var parts []string
	if p := strings.TrimSpace(m.ParameterSize); p != "" {
		parts = append(parts, p)
	}
	if m.SizeBytes > 0 {
		parts = append(parts, fmt.Sprintf("%.1f GiB", float64(m.SizeBytes)/float64(int64(1)<<30)))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func mainRationale(candidates []Model, overBudget int) string {
	switch {
	case len(candidates) == 1 && overBudget == 0:
		return "the only chat-capable model pulled"
	case overBudget > 0:
		return fmt.Sprintf("the largest of %d that fit the memory ceiling (%d excluded as too large)", len(candidates), overBudget)
	default:
		return fmt.Sprintf("the largest of %d chat-capable models pulled", len(candidates))
	}
}

func thinkRationale(m Model) string {
	if m.HasCapability("thinking") {
		return "its Ollama manifest advertises the \"thinking\" capability"
	}
	return "its name/family matches a known reasoning-model family"
}

func smallRationale(m Model) string {
	switch {
	case !m.Thinks() && m.ToolCapable():
		return "the smallest non-thinking, tool-capable model pulled"
	case !m.Thinks():
		return "the smallest non-thinking model pulled"
	default:
		return "the smallest model pulled; it appears to be a reasoning model, so background calls will carry a thinking pass"
	}
}
