package ollamainfo

// Resident-set planning (P69.6).
//
// Fit (kvfit.go) answers "how big a window fits this model in this budget",
// which is one model in and one number out. That signature is the bug the moment
// two models must be resident at the same time: since P69.1 each debate seat
// resolves its own model, so a debate holds two or three models in VRAM at once
// and every one of them is sized as if it were alone. Three seats each fitted to
// the whole budget do not fit the whole budget.
//
// PlanResidentSet takes the *set* and solves once. Everything below is pure
// arithmetic over P69.5's KVGeometry — no VRAM detection here either (P17.5);
// the budget is still a number the operator states, and Footprint.FullyOnGPU is
// still the empirical check that the plan was right.

import (
	"context"
	"fmt"
	"strings"
)

// Member is one distinct model that must be resident at the same time as the
// others in its set.
//
// "Distinct" is load-bearing. Ollama holds one runner per model *name*, so two
// debate seats on the same model share one runner, one copy of the weights and
// one KV cache. A planner whose unit is the seat rather than the model
// double-counts the shared pair and refuses sets that fit — see the dedupe in
// PlanResidentSet, which is a correctness step, not a tidiness one.
type Member struct {
	Model        string
	Geometry     KVGeometry
	WeightsBytes int64
}

// Plan is a per-model window assignment for a resident set: what each model
// should be served at so that all of them fit the budget simultaneously.
type Plan struct {
	// Models are the deduplicated member names in input order, so a printed
	// plan is deterministic and reads in the order the caller thinks in
	// (proposer, critic, arbiter).
	Models  []string
	Windows map[string]int
	KVBytes map[string]int64
	// MemberWeights is each model's resident weight size, kept alongside the sum
	// so a report can show where the budget went without re-measuring.
	MemberWeights map[string]int64
	Weights       int64 // sum of member weights
	Total         int64 // Weights plus the KV cache at the assigned windows
	Budget        int64
	KVType        KVCacheType
	// Collapsed counts the members that were folded into an earlier one because
	// they name the same model. It is not bookkeeping: it is the difference
	// between "three seats" and "two runners", and a report that omits it looks
	// like it lost a model.
	Collapsed int
}

// Spare is the budget left unspent by the plan. It is the margin between a plan
// that fits on paper and one that survives Ollama's own overheads, so it is
// worth printing rather than deriving at each call site.
func (p Plan) Spare() int64 { return p.Budget - p.Total }

// PlanResidentSet solves for the largest per-model context windows that hold
// every member resident at once within budgetBytes.
//
// ok is false when no assignment exists; reason then says which wall was hit,
// because the operator's fix differs — a budget too small for the weights alone
// needs a smaller model, whereas one that merely squeezes the windows below the
// floor needs q8_0 or one fewer seat.
//
// Allocation is by equal *token* windows, not equal bytes: solve for the largest
// T with
//
//	sum over members of  min(T, ContextMax_i) x BytesPerToken_i  <=  budget - sum(weights)
//
// The window is the number the engine budgets its conversation against
// (tokenest.CompactionTrigger), and two seats reading the same debate transcript
// need comparable room to hold it. An equal-*byte* split would hand the model
// with the cheap KV cache a window it can never fill while starving the
// expensive one on the identical prompt. The min(T, ContextMax_i) clamp also
// gives redistribution for free: a member that reaches its training maximum
// stops consuming budget as T climbs, and the search keeps climbing for the rest.
//
// BaselineContextWindow is deliberately not consulted. That floor (32768) exists
// so a single-model install is not sized below what a skill-driven run needs
// (P35.3), and it is not lowered here — it is simply not on this path, which
// floors at MinFittedContextWindow like every other fitted answer. A co-resident
// set is a different question from the one the baseline answers.
func PlanResidentSet(members []Member, budgetBytes int64, t KVCacheType) (Plan, bool, string) {
	members, dropped := dedupeMembers(members)
	if len(members) == 0 {
		return Plan{}, false, "no models to plan for"
	}
	if budgetBytes <= 0 {
		return Plan{}, false, "no memory budget given (set provider.vram_budget_gb)"
	}

	perToken := make([]int64, len(members))
	var weights int64
	for i, m := range members {
		bpt, ok := m.Geometry.BytesPerToken(t)
		if !ok || bpt <= 0 {
			return Plan{}, false, fmt.Sprintf("model %q has no usable KV geometry (blocks=%d kv_heads=%d key=%d value=%d, kv type %q)",
				m.Model, m.Geometry.BlockCount, m.Geometry.HeadCountKV, m.Geometry.KeyLength, m.Geometry.ValueLength, kvTypeName(t))
		}
		if m.WeightsBytes <= 0 {
			return Plan{}, false, fmt.Sprintf("model %q has no measured weight size; load it once and re-plan rather than planning against a guess", m.Model)
		}
		perToken[i] = bpt
		weights += m.WeightsBytes
	}

	avail := budgetBytes - weights
	if avail <= 0 {
		return Plan{}, false, fmt.Sprintf("the weights alone need %s, which exceeds the %s budget before any KV cache: %s",
			FormatGiB(weights), FormatGiB(budgetBytes), describeMembers(members))
	}

	tokens := largestEqualWindow(members, perToken, avail)

	p := Plan{
		Models:        make([]string, 0, len(members)),
		Windows:       make(map[string]int, len(members)),
		KVBytes:       make(map[string]int64, len(members)),
		MemberWeights: make(map[string]int64, len(members)),
		Weights:       weights,
		Total:         weights,
		Budget:        budgetBytes,
		KVType:        t,
		Collapsed:     dropped,
	}
	for i, m := range members {
		win := int(clampToMax(tokens, m.Geometry.ContextMax)) / fitStep * fitStep
		if win < MinFittedContextWindow {
			return Plan{}, false, fmt.Sprintf(
				"no window above %d tokens fits %d models in %s (weights %s, %s left for KV): %s",
				MinFittedContextWindow, len(members), FormatGiB(budgetBytes), FormatGiB(weights), FormatGiB(avail),
				describeMembers(members))
		}
		kv := perToken[i] * int64(win)
		p.Models = append(p.Models, m.Model)
		p.Windows[m.Model] = win
		p.KVBytes[m.Model] = kv
		p.MemberWeights[m.Model] = m.WeightsBytes
		p.Total += kv
	}
	return p, true, ""
}

// largestEqualWindow binary-searches the largest token count whose summed KV
// cost fits avail. cost is monotone non-decreasing in T, which is what makes the
// search valid; the min() clamp only flattens it.
func largestEqualWindow(members []Member, perToken []int64, avail int64) int64 {
	cost := func(tok int64) int64 {
		var sum int64
		for i, m := range members {
			sum += clampToMax(tok, m.Geometry.ContextMax) * perToken[i]
		}
		return sum
	}

	// An upper bound that is certainly unreachable-or-exact: the cheapest member
	// could at most buy avail/minPerToken tokens on its own. Raise it to the
	// largest training maximum so a set where every member caps out early still
	// reaches its ceiling rather than stopping at an arbitrary bound.
	minPer := perToken[0]
	var maxCap int64
	for i, m := range members {
		if perToken[i] < minPer {
			minPer = perToken[i]
		}
		if int64(m.Geometry.ContextMax) > maxCap {
			maxCap = int64(m.Geometry.ContextMax)
		}
	}
	hi := avail / minPer
	if maxCap > hi {
		hi = maxCap
	}
	if cost(hi) <= avail {
		return hi
	}

	lo := int64(0) // cost(0) == 0, always affordable
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if cost(mid) <= avail {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// clampToMax applies a member's training-context ceiling. A zero ContextMax
// means "unknown", which is treated as uncapped: over-reserving costs context,
// under-reserving costs an OOM or a silent spill, and the same safe direction is
// taken throughout this package.
func clampToMax(tokens int64, contextMax int) int64 {
	if contextMax > 0 && tokens > int64(contextMax) {
		return int64(contextMax)
	}
	return tokens
}

// dedupeMembers collapses repeated model names, keeping the first occurrence and
// input order, and drops entries with no model name. It returns how many were
// collapsed so a caller can say "two seats share this model" rather than leaving
// the arithmetic looking like it lost one.
func dedupeMembers(in []Member) ([]Member, int) {
	seen := make(map[string]bool, len(in))
	out := make([]Member, 0, len(in))
	dropped := 0
	for _, m := range in {
		name := strings.TrimSpace(m.Model)
		if name == "" {
			continue
		}
		if seen[name] {
			dropped++
			continue
		}
		seen[name] = true
		m.Model = name
		out = append(out, m)
	}
	return out, dropped
}

func describeMembers(members []Member) string {
	parts := make([]string, 0, len(members))
	for _, m := range members {
		parts = append(parts, fmt.Sprintf("%s (%s)", m.Model, FormatGiB(m.WeightsBytes)))
	}
	return strings.Join(parts, ", ")
}

func kvTypeName(t KVCacheType) string {
	if t == "" {
		return string(KVTypeF16)
	}
	return string(t)
}

// PlanFor gathers each model's geometry (/api/show) and resident weights
// (/api/ps) from a live Ollama server, then plans.
//
// A model whose weights cannot be measured yields no plan rather than a guessed
// one. /api/tags' on-disk size is the tempting substitute and is wrong by 2.57
// GiB on qwen35-9b — a vision projector that is never resident unless an image
// is sent — which is more than the margin a co-resident plan has to spend. See
// WeightsBytes.
func PlanFor(ctx context.Context, nativeBase string, models []string, budgetBytes int64, t KVCacheType) (Plan, bool, string) {
	names, collapsed := dedupeNames(models)
	if len(names) == 0 {
		return Plan{}, false, "no models to plan for"
	}
	members := make([]Member, 0, len(names))
	for _, name := range names {
		g, ok := Geometry(ctx, nativeBase, name)
		if !ok {
			return Plan{}, false, fmt.Sprintf("could not read model_info for %q from %s (is Ollama running, and the model pulled?)", name, nativeBase)
		}
		f, loaded := Loaded(ctx, nativeBase, name)
		if !loaded {
			return Plan{}, false, fmt.Sprintf("model %q is not loaded, so its resident weights cannot be measured; run it once (`ollama run %s ''`) and re-plan", name, name)
		}
		w, ok := WeightsBytes(f, g, t)
		if !ok {
			return Plan{}, false, fmt.Sprintf("could not derive resident weights for %q from its loaded footprint (%s at window %d)", name, FormatGiB(f.Size), f.ContextLength)
		}
		members = append(members, Member{Model: name, Geometry: g, WeightsBytes: w})
	}
	p, ok, reason := PlanResidentSet(members, budgetBytes, t)
	// PlanResidentSet sees an already-deduplicated list, so the collapse count it
	// derives is zero; the real one was taken above, before any network call, so
	// a shared model is fetched once as well as planned once.
	p.Collapsed = collapsed
	return p, ok, reason
}

// dedupeNames is dedupeMembers for bare names, used before any network call so a
// shared model is fetched — and planned — once.
func dedupeNames(in []string) ([]string, int) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	dropped := 0
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if seen[n] {
			dropped++
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, dropped
}

// BudgetBytes converts a GiB budget to bytes, returning 0 for a non-positive
// figure — the "no budget stated, plan nothing" value every caller checks.
func BudgetBytes(gib float64) int64 {
	if gib <= 0 {
		return 0
	}
	return int64(gib * float64(int64(1)<<30))
}
