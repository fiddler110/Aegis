package tokenest

import "sync"

// Calibration bounds. The asymmetry between them is the whole safety argument
// and is deliberate.
const (
	// minScale clamps the correction so it can never shrink an estimate below
	// the raw heuristic. The two failure directions are not symmetric: an
	// over-estimate compacts a conversation slightly earlier than it had to
	// (wasted work, recoverable), while an under-estimate lets a local server
	// silently drop the oldest tokens — including the system prompt — with no
	// error anywhere, which is the failure P2.7/P52.1/P59.1 all exist to
	// prevent and the one P62.4 measured happening. So the correction is
	// allowed to make the engine more cautious and never less.
	//
	// The consequence to accept: if a model genuinely tokenizes denser than
	// chars/4 the scale sits at 1.0 and the raw estimate stands, exactly as
	// before this existed. Doing nothing is the pre-P62.4 behaviour, so the
	// clamp can only decline to help — it cannot introduce a regression.
	minScale = 1.0
	// maxScale bounds how far one anomalous turn can move the correction. A
	// runaway scale would put the trigger below the base prompt and compact on
	// every turn forever, which is a worse failure than the undercount: it never
	// self-corrects, because a conversation held at minimum length keeps
	// producing the same ratio. 3.0 is well clear of the 1.25-1.5 range P62.4
	// measured while still bounding the damage.
	maxScale = 3.0

	// riseAlpha / fallAlpha are the EWMA weights for a new sample, split by
	// direction for the same reason minScale exists. Moving *up* (the estimate
	// is under the truth) is urgent and takes most of the step at once; moving
	// down is a relaxation of a safety margin and is taken slowly, so a single
	// unrepresentative turn — a short prompt after compaction, say — cannot undo
	// a correction many turns established.
	riseAlpha = 0.7
	fallAlpha = 0.2
)

// Calibrator learns a multiplicative correction for the heuristic in this
// package from token counts a provider actually reports.
//
// The heuristic is not a tokenizer and never will be; the point here is that a
// backend which reports prompt_eval_count is telling us, every single turn, how
// wrong it currently is. P62.4 measured the engine running at 67-80% of the
// provider's real count — a 20-33% undercount — which at an 85% compaction
// trigger means the real prompt reaches ~128% of the window before the estimate
// says 85%. That run produced zero notices of any kind while Ollama silently
// context-shifted for five consecutive turns.
//
// The correction is applied as (raw + overhead) * scale, where overhead is the
// structurally-known constant the caller can account for itself (the tool
// schemas — see Tools) and scale is the residual this type learns. Factoring the
// known part out first matters: it keeps scale near 1.0 and about the
// *heuristic's* accuracy, instead of folding a fixed additive cost into a
// multiplier that would then over-correct as the conversation grows.
//
// A Calibrator is safe for concurrent use. The zero value is ready and behaves
// as an exact 1.0 correction until the first sample.
type Calibrator struct {
	mu      sync.RWMutex
	scale   float64
	samples int
}

// Observe folds one turn's evidence into the correction: raw is the uncorrected
// heuristic estimate for the request that was sent, overhead the structural
// constant the caller added to it, actual the provider's reported prompt token
// count, and window the context window that request was served under.
//
// It deliberately ignores several kinds of sample rather than learning from
// them badly:
//
//   - actual at or above the window. A backend that truncated the prompt reports
//     the clamp, not the prompt (Ollama reports num_ctx-1), so such a sample says
//     how big the window is and nothing about how big the prompt was. Worse, it
//     is biased in the dangerous direction — a clamped count *understates* the
//     true ratio, so learning from it would shrink the correction exactly on the
//     turns where the undercount is doing damage. This is the same clamp check
//     the live tier's saturationReason applies for the same reason.
//   - a non-positive basis or count, which carries no ratio at all.
func (c *Calibrator) Observe(raw, overhead, actual, window int) {
	basis := raw + overhead
	if basis <= 0 || actual <= 0 {
		return
	}
	if window > 0 && actual >= window-1 {
		return
	}

	ratio := float64(actual) / float64(basis)
	if ratio < minScale {
		ratio = minScale
	}
	if ratio > maxScale {
		ratio = maxScale
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.samples == 0 {
		// Seed on the first sample rather than easing toward it from 1.0: the
		// first turn of a run is often the only one before the conversation is
		// already large, and easing in would spend the run's most correctable
		// turns still believing the uncorrected estimate.
		c.scale = ratio
	} else {
		alpha := fallAlpha
		if ratio > c.scale {
			alpha = riseAlpha
		}
		c.scale += alpha * (ratio - c.scale)
	}
	c.samples++
}

// Scale reports the current correction factor and how many samples produced it.
// A sample count of zero means nothing has been observed and the factor is an
// exact 1.0 — the callers that log this need to tell "calibrated to 1.0" apart
// from "never calibrated".
func (c *Calibrator) Scale() (float64, int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.samples == 0 {
		return 1.0, 0
	}
	return c.scale, c.samples
}

// Apply corrects an estimate that already includes overhead. It rounds up: the
// whole point of this correction is to stop under-reporting prompt size, and
// truncating toward zero would give back a token of the margin on every call.
func (c *Calibrator) Apply(estimate int) int {
	if estimate <= 0 {
		return estimate
	}
	scale, samples := c.Scale()
	if samples == 0 {
		return estimate
	}
	scaled := float64(estimate) * scale
	out := int(scaled)
	if float64(out) < scaled {
		out++
	}
	return out
}
