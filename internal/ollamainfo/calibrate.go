package ollamainfo

// Measuring the memory budget instead of asking for it (P83).
//
// kvfit.go computes exactly how much KV cache a window costs, and
// internal/server/autofit.go re-solves context_window from that at every daemon
// start. Both are exact. Both depend on one number that is not: the memory
// budget, which provider.vram_budget_gb states because P17.5 ruled out asking
// the GPU. The consequence is that the whole "size the window to the hardware"
// chain terminates in a figure a human guessed, and it does not travel — move
// the config to a different machine and the budget is still the old card's.
//
// This file closes that without reopening P17.5. The rejected approach was
// *introspection*: query nvidia-smi or a platform driver for total VRAM, then
// reimplement Ollama's offload heuristic to predict what would fit. That is
// still rejected, for the reason it always was — Aegis would be guessing at a
// decision another process makes.
//
// What this does instead is *measurement*. Ollama already publishes the verdict
// on its own placement: /api/ps reports size and size_vram, and
// Footprint.FullyOnGPU is the difference. So load the model at a window, ask
// whether it fit, and search. The binding constraint is discovered rather than
// predicted, and the authority consulted is the one actually doing the placing.
// kvfit.go's own package note already named this as the trustworthy signal;
// this is that signal used as a search oracle rather than only as a check.
//
// Two properties make the search cheap enough to be worth it. Fit is monotonic
// in window size — KV cache grows linearly, weights do not move — so a spill at
// w implies a spill at everything above it, and bisection is valid. And the
// answer is a property of the machine, not of the session: it is measured once,
// written to config, and reused by every later run.
//
// One consequence is worth stating separately, because it turned out to matter
// more than the budget itself. Nothing here uses BytesPerToken to decide
// anything. The analytic KV formula assumes every transformer block holds a KV
// cache, and on a hybrid-attention architecture that is simply false: measured
// on aegis-qwen35-9b, real cache growth is 33 KiB per token against the
// formula's 132 KiB — exactly 4x, the ratio you get when 3 of every 4 layers use
// linear attention and hold no KV at all. Predicting from geometry therefore
// over-reserves fourfold on that family, which is why the first live run of this
// command reported a 38 GiB "capacity" for a 16 GB card. Everything below is
// derived from bytes Ollama reports instead, and the per-token cost is
// *measured* off the probe ladder rather than computed. The formula is still
// printed beside it, because a large disagreement is a fact about the model
// worth showing.
//
// What it cannot do is make a point-in-time measurement permanent. Ollama
// places against VRAM that is *free right now*, so a calibration taken with a
// browser open measures a smaller card than one taken without. That is a real
// limitation and the honest response is to report it rather than smooth it
// over: Calibration carries the probe log, and the caller subtracts whatever
// headroom it wants for what the desktop might do later.

import (
	"context"
	"fmt"
)

// Probe loads model at numCtx and reports the footprint Ollama ends up with.
// The seam exists so the search can be tested against a synthetic machine —
// every real probe is a model load costing tens of seconds, which is not
// something a unit test can spend.
type Probe func(ctx context.Context, numCtx int) (Footprint, bool)

// CalibrationStep is one probe and its verdict, kept so the report can show its
// working. An operator who disagrees with the number needs to see the ladder,
// not just the answer.
type CalibrationStep struct {
	Window   int
	Fit      bool
	Size     int64
	SizeVRAM int64
}

// Calibration is the measured outcome. Every byte figure in it comes from
// Ollama's own accounting, never from the KV formula — see the package note.
type Calibration struct {
	// Weights is the model's resident weight bytes, extrapolated back to a
	// zero-length window from the probe ladder. Window-invariant by definition,
	// so any two probes determine it.
	Weights int64
	// BytesPerToken is the KV-cache cost per token as *measured* across the
	// ladder: the slope of resident bytes against window size. PredictedPerToken
	// is what KVGeometry's formula claims for comparison. They agree on a
	// conventional attention stack and diverge sharply on a hybrid one.
	BytesPerToken     int64
	PredictedPerToken int64
	// LargestFit is the largest window observed entirely in VRAM, and Capacity
	// is the resident bytes Ollama reported at it — a direct reading, not
	// weights plus a computed cache. Capacity is the figure
	// provider.vram_budget_gb wants.
	//
	// In the WeightsSpill case there is no fitting window, and Capacity instead
	// carries the card reading taken from that spill — size_vram, what Ollama
	// placed before giving up. Still a measurement of the machine, just not one
	// this model can use.
	LargestFit int
	Capacity   int64
	// SmallestSpill is the smallest window observed to spill, or 0 when nothing
	// did. Together with LargestFit it brackets the true capacity; the gap
	// between them is the measurement's resolution.
	SmallestSpill int
	// AtCeiling is true when even the model's training maximum fit, so Capacity
	// is a lower bound rather than a measurement of the card. Nothing is wrong
	// in that case — the machine simply has more memory than this model can use.
	AtCeiling bool

	// NoGPU means Ollama reported nothing in VRAM at all: a CPU-only host, or a
	// build without GPU support. There is no VRAM budget to state, and the
	// caller should say so rather than write a number.
	NoGPU bool
	// WeightsSpill means the model spilled at the smallest viable window, so
	// its weights alone exceed what this machine can hold on the GPU. The fix
	// is a smaller model, not a smaller window.
	WeightsSpill bool

	Steps []CalibrationStep
}

// CapacityGiB renders Capacity for a config file.
func (c Calibration) CapacityGiB() float64 { return float64(c.Capacity) / float64(int64(1)<<30) }

// measure derives the per-token KV cost and the weights from the probe ladder,
// by least-squares over every successful probe. Two probes are enough; more
// tighten it. ok is false with fewer than two distinct windows, where there is
// a point but no slope.
//
// Deliberately fitted rather than computed: see the package note on the
// fourfold error the formula makes on a hybrid-attention model.
func (c *Calibration) measure() bool {
	var n, sumX, sumY, sumXY, sumXX float64
	for _, s := range c.Steps {
		if s.Size <= 0 {
			continue
		}
		x, y := float64(s.Window), float64(s.Size)
		n, sumX, sumY, sumXY, sumXX = n+1, sumX+x, sumY+y, sumXY+x*y, sumXX+x*x
	}
	if n < 2 {
		return false
	}
	den := n*sumXX - sumX*sumX
	if den == 0 {
		return false // every probe used the same window
	}
	slope := (n*sumXY - sumX*sumY) / den
	intercept := (sumY - slope*sumX) / n
	if slope < 0 || intercept <= 0 {
		return false
	}
	c.BytesPerToken = int64(slope)
	c.Weights = int64(intercept)
	return true
}

// WindowForBudget returns the largest window whose measured KV cost fits budget
// alongside the measured weights, on the same grid Fit uses. This is Fit's
// arithmetic against a measured per-token cost instead of a predicted one.
func (c Calibration) WindowForBudget(budget int64, ceiling int) int {
	if c.BytesPerToken <= 0 {
		return 0
	}
	avail := budget - c.Weights
	if avail <= 0 {
		return 0
	}
	win := int(avail/c.BytesPerToken) / fitStep * fitStep
	if ceiling > 0 && win > ceiling {
		win = ceiling
	}
	if win < MinFittedContextWindow {
		return 0
	}
	return win
}

// Resolution is how wide the bracket around Capacity is, in bytes — the gap
// between the largest window that fit and the smallest that did not. 0 when
// nothing spilled.
func (c Calibration) Resolution() int64 {
	if c.SmallestSpill <= 0 {
		return 0
	}
	if c.BytesPerToken <= 0 {
		return 0
	}
	return int64(c.SmallestSpill-c.LargestFit) * c.BytesPerToken
}

// DefaultCalibrationProbes bounds how many model loads a calibration spends.
// Each is a full reload — Ollama rebuilds the runner whenever num_ctx changes —
// so on a mid-range card this is minutes, not seconds. Eight probes bracket a
// 16 GB card to well under a gibibyte, which is finer than the headroom any
// sensible operator leaves anyway.
const DefaultCalibrationProbes = 8

// calibrationResolutionTokens stops the bisection once the bracket is this
// narrow, before the probe budget is exhausted. Continuing past it spends a
// reload to refine a number that a headroom allowance is about to round off.
const calibrationResolutionTokens = 2048

// Calibrate measures how much memory this machine can give the model, by
// loading it at a ladder of context windows and reading what Ollama reports
// each time.
//
// Two readings do the work. Footprint.FullyOnGPU says whether the window fit,
// and on a spill size_vram says how many bytes Ollama did manage to place —
// which is a direct reading of the card, not an inference about it. So one
// deliberately oversized probe either finishes the job (the model fits at its
// training maximum, and there is no limit left to find) or hands back the
// capacity outright.
//
// Nothing here is computed from KVGeometry. The per-token cache cost is fitted
// to the ladder instead, because the formula assumes every block holds a KV
// cache and is fourfold wrong when three layers in four use linear attention —
// see the package note. Measuring the slope costs nothing extra: the probes had
// to be run anyway.
//
// Every estimate is refined only downward from an observed spill, so the result
// is a lower bound on what the machine holds. That asymmetry is deliberate: an
// understated budget costs a smaller window, an overstated one costs a model
// that spills to system RAM on its first real turn.
//
// ok is false only when the first probe fails — an unreachable server, or a load
// that never completes. Every other outcome, "the weights do not fit" included,
// is a real finding the caller should report rather than an error.
func Calibrate(ctx context.Context, g KVGeometry, t KVCacheType, maxProbes int, probe Probe) (Calibration, bool) {
	if maxProbes <= 0 {
		maxProbes = DefaultCalibrationProbes
	}
	var c Calibration
	if per, ok := g.BytesPerToken(t); ok {
		c.PredictedPerToken = per
	}

	step := func(win int) (Footprint, bool) {
		f, ok := probe(ctx, win)
		if !ok {
			return f, false
		}
		c.Steps = append(c.Steps, CalibrationStep{
			Window: win, Fit: f.FullyOnGPU(), Size: f.Size, SizeVRAM: f.SizeVRAM,
		})
		if f.FullyOnGPU() && f.Size > c.Capacity {
			// Capacity is simply the largest footprint seen entirely in VRAM.
			c.Capacity = f.Size
			c.LargestFit = win
		}
		c.measure()
		return f, true
	}

	floor := MinFittedContextWindow
	f, ok := step(floor)
	if !ok {
		return c, false
	}
	if f.SizeVRAM <= 0 {
		c.NoGPU = true
		c.Capacity = 0
		return c, true
	}
	if !f.FullyOnGPU() {
		// Even the smallest usable window spilled, so the weights are the
		// binding constraint. The probe still measured the card — size_vram is
		// what Ollama fit before giving up — and reporting that is how the
		// operator learns which model would fit.
		c.WeightsSpill = true
		c.Capacity = f.SizeVRAM
		c.Weights = f.Size
		return c, true
	}

	ceiling := g.ContextMax
	if ceiling <= 0 {
		ceiling = 1 << 20 // no declared maximum: let a spill find the limit
	}
	ceiling = ceiling / fitStep * fitStep

	// One oversized probe, straight to the model's ceiling.
	var capEstimate int64
	if len(c.Steps) < maxProbes && ceiling > floor {
		f, ok := step(ceiling)
		if !ok {
			return c, true
		}
		if f.FullyOnGPU() {
			c.AtCeiling = true
			return c, true
		}
		c.SmallestSpill = ceiling
		capEstimate = f.SizeVRAM
	}

	// Converge on the largest window that stays resident. Each iteration either
	// confirms a window or spills and tightens the reading of the card.
	for len(c.Steps) < maxProbes && c.SmallestSpill > 0 {
		if c.SmallestSpill-c.LargestFit <= calibrationResolutionTokens {
			break
		}
		next := c.WindowForBudget(capEstimate, ceiling)
		// The measured guess is only useful while it lands strictly inside the
		// bracket. Once it stops moving, fall back to halving, which always
		// makes progress.
		if next <= c.LargestFit || next >= c.SmallestSpill {
			next = (c.LargestFit + c.SmallestSpill) / 2 / fitStep * fitStep
		}
		if next <= c.LargestFit || next >= c.SmallestSpill {
			break
		}
		f, ok := step(next)
		if !ok {
			break
		}
		if f.FullyOnGPU() {
			continue // step() already raised Capacity and LargestFit
		}
		c.SmallestSpill = next
		if f.SizeVRAM > 0 && (capEstimate == 0 || f.SizeVRAM < capEstimate) {
			capEstimate = f.SizeVRAM
		}
	}
	return c, true
}

// LiveProbe is the real Probe: load the model at numCtx, then read back what
// Ollama did with it.
//
// The load is issued through WarmAt, the same call the daemon's boot-time fit
// uses, so a calibration leaves the model resident at its last probed window
// rather than in some state only this path produces.
func LiveProbe(nativeBase, model string) Probe {
	return func(ctx context.Context, numCtx int) (Footprint, bool) {
		if err := WarmAt(ctx, nativeBase, model, numCtx); err != nil {
			return Footprint{}, false
		}
		f, ok := Loaded(ctx, nativeBase, model)
		if !ok {
			return Footprint{}, false
		}
		// Ollama serves the request from an existing runner when the requested
		// window matches what is already loaded, so a footprint reporting a
		// different window than the one asked for means the load did not take
		// effect and the verdict belongs to some other window.
		if f.ContextLength > 0 && f.ContextLength != numCtx {
			return f, false
		}
		return f, true
	}
}

// Explain renders the verdict as the sentence a report leads with.
func (c Calibration) Explain(model string) string {
	switch {
	case c.NoGPU:
		return fmt.Sprintf("Ollama placed none of %s in VRAM — this host is serving on CPU, so there is no VRAM budget to state.", model)
	case c.WeightsSpill:
		return fmt.Sprintf("%s spilled to system RAM at the smallest usable window (%d tokens), so its weights alone exceed this machine's VRAM. A smaller model is the fix, not a smaller window.", model, MinFittedContextWindow)
	case c.AtCeiling:
		return fmt.Sprintf("%s fit entirely in VRAM at its training maximum (%d tokens) using %s, so this is a lower bound on the machine rather than its limit — the model runs out of context before the card runs out of memory.", model, c.LargestFit, FormatGiB(c.Capacity))
	default:
		return fmt.Sprintf("%s fit at %d tokens and spilled at %d, measured across %d loads.", model, c.LargestFit, c.SmallestSpill, len(c.Steps))
	}
}
