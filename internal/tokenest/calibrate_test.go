package tokenest

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// TestCalibratorUncalibratedIsIdentity: with no samples the correction must be
// exactly the pre-P62.4 behaviour. Everything else here loosens a safety
// margin, so the untouched path has to be provably untouched.
func TestCalibratorUncalibratedIsIdentity(t *testing.T) {
	var c Calibrator
	for _, n := range []int{0, 1, 1000, 123456} {
		if got := c.Apply(n); got != n {
			t.Errorf("Apply(%d) with no samples = %d, want %d", n, got, n)
		}
	}
	if scale, samples := c.Scale(); scale != 1.0 || samples != 0 {
		t.Errorf("Scale() with no samples = (%v, %d), want (1, 0)", scale, samples)
	}
}

// TestCalibratorSeedsOnFirstSample: the first sample is adopted whole rather
// than eased toward from 1.0. A run's early turns are the ones where correcting
// still buys something, so easing in would spend them uncorrected.
func TestCalibratorSeedsOnFirstSample(t *testing.T) {
	var c Calibrator
	c.Observe(1000, 0, 1400, 8192)
	scale, samples := c.Scale()
	if samples != 1 {
		t.Fatalf("samples = %d, want 1", samples)
	}
	if math.Abs(scale-1.4) > 1e-9 {
		t.Errorf("scale after one 1.4x sample = %v, want 1.4", scale)
	}
	if got := c.Apply(1000); got != 1400 {
		t.Errorf("Apply(1000) = %d, want 1400", got)
	}
}

// TestCalibratorFactorsOutOverheadBeforeLearning is the reason the correction is
// (raw+overhead)*scale rather than raw*scale. When the overhead fully explains
// the gap the residual scale must come out at 1.0 — folding a fixed additive
// cost into a multiplier would instead learn a factor that over-corrects more
// and more as the conversation grows.
func TestCalibratorFactorsOutOverheadBeforeLearning(t *testing.T) {
	var c Calibrator
	// Estimate 1000, overhead 500, provider reports exactly 1500: the structural
	// part accounts for all of it.
	c.Observe(1000, 500, 1500, 8192)
	if scale, _ := c.Scale(); math.Abs(scale-1.0) > 1e-9 {
		t.Fatalf("scale = %v, want 1.0 — the overhead explained the whole gap", scale)
	}

	// Same absolute gap at 10x the conversation size must stay at 1.0 too. A
	// multiplier that had absorbed the overhead would read ~1.5 here.
	var d Calibrator
	d.Observe(10000, 500, 10500, 65536)
	if scale, _ := d.Scale(); math.Abs(scale-1.0) > 1e-9 {
		t.Errorf("scale on a large conversation = %v, want 1.0", scale)
	}
}

// TestCalibratorNeverShrinksAnEstimate pins the asymmetry the safety argument
// rests on: over-estimating costs an early compaction, under-estimating costs a
// silent truncation, so a provider reporting *fewer* tokens than estimated must
// not be allowed to relax the estimate below the raw heuristic.
func TestCalibratorNeverShrinksAnEstimate(t *testing.T) {
	var c Calibrator
	c.Observe(1000, 0, 400, 8192) // provider says 0.4x
	scale, _ := c.Scale()
	if scale < 1.0 {
		t.Fatalf("scale = %v, want >= 1.0 — a correction must never shrink the estimate", scale)
	}
	if got := c.Apply(1000); got < 1000 {
		t.Errorf("Apply(1000) = %d, want >= 1000", got)
	}
}

// TestCalibratorBoundsRunaway: one anomalous turn must not push the correction
// somewhere that compacts on every turn forever. That failure never
// self-corrects, because a conversation held at minimum length keeps producing
// the same ratio.
func TestCalibratorBoundsRunaway(t *testing.T) {
	var c Calibrator
	c.Observe(100, 0, 100000, 0) // 1000x
	if scale, _ := c.Scale(); scale != maxScale {
		t.Errorf("scale after an absurd sample = %v, want it clamped to %v", scale, maxScale)
	}
}

// TestCalibratorIgnoresSaturatedSamples is the check that keeps the correction
// from collapsing exactly when it is needed. A truncated prompt reports the
// clamp (Ollama reports num_ctx-1), which *understates* the true ratio — so
// learning from it would shrink the correction on precisely the turns where the
// undercount is causing the damage.
func TestCalibratorIgnoresSaturatedSamples(t *testing.T) {
	var c Calibrator
	c.Observe(2000, 0, 3000, 8192) // a good 1.5x sample
	want, _ := c.Scale()

	// The window is now full: the provider reports the clamp, and the implied
	// ratio (4095/4000 ~ 1.02) is far below the truth.
	c.Observe(4000, 0, 4095, 4096)
	c.Observe(4000, 0, 4096, 4096)
	got, samples := c.Scale()
	if got != want || samples != 1 {
		t.Errorf("scale moved to %v over %d samples on saturated evidence, want %v over 1", got, samples, want)
	}
}

// TestCalibratorIgnoresUnusableSamples: a non-positive basis or count carries no
// ratio at all and must not be counted as evidence.
func TestCalibratorIgnoresUnusableSamples(t *testing.T) {
	var c Calibrator
	c.Observe(0, 0, 5000, 8192)
	c.Observe(1000, 0, 0, 8192)
	c.Observe(-5, 0, 5000, 8192)
	if _, samples := c.Scale(); samples != 0 {
		t.Errorf("samples = %d after only unusable evidence, want 0", samples)
	}
}

// TestCalibratorRisesFasterThanItFalls pins the directional asymmetry rather
// than just asserting movement: discovering the estimate is low is urgent,
// while giving margin back is a relaxation and is taken slowly, so one
// unrepresentative turn cannot undo a correction many turns established.
func TestCalibratorRisesFasterThanItFalls(t *testing.T) {
	// Same starting point, same size of step, opposite directions.
	up := &Calibrator{}
	up.Observe(1000, 0, 1500, 0) // seed 1.5
	up.Observe(1000, 0, 2500, 0) // sample 2.5, a +1.0 step
	upScale, _ := up.Scale()

	down := &Calibrator{}
	down.Observe(1000, 0, 2500, 0) // seed 2.5
	down.Observe(1000, 0, 1500, 0) // sample 1.5, a -1.0 step
	downScale, _ := down.Scale()

	movedUp := upScale - 1.5
	movedDown := 2.5 - downScale
	if movedUp <= movedDown {
		t.Errorf("rise moved %v and fall moved %v; the rise must be the larger step", movedUp, movedDown)
	}
}

// TestCalibratorApplyRoundsUp: truncating toward zero would hand back a token of
// margin on every single call, in the one direction this whole mechanism exists
// to stop giving away.
func TestCalibratorApplyRoundsUp(t *testing.T) {
	var c Calibrator
	c.Observe(1000, 0, 1001, 0) // scale 1.001
	if got := c.Apply(1000); got != 1001 {
		t.Errorf("Apply(1000) at 1.001x = %d, want 1001", got)
	}
	if got := c.Apply(1); got != 2 {
		t.Errorf("Apply(1) at 1.001x = %d, want 2 (rounded up)", got)
	}
}

// TestToolsCountsSchemas is P62.4's structural half: the schemas ride every
// native-tool-calling request and the backend counts them, so an estimate that
// returns zero for them is understating every prompt it ever prices.
func TestToolsCountsSchemas(t *testing.T) {
	if got := Tools(nil); got != 0 {
		t.Errorf("Tools(nil) = %d, want 0", got)
	}

	one := []provider.ToolSchema{{
		Name:        "read_file",
		Description: "Read a file from the workspace and return its contents.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}}
	got := Tools(one)
	if got <= 0 {
		t.Fatalf("Tools(one schema) = %d, want > 0", got)
	}
	// It must actually price the payload, not just charge the envelope.
	if got <= toolSchemaEnvelope {
		t.Errorf("Tools(one schema) = %d, which is no more than the envelope constant (%d) — the fields are not being counted",
			got, toolSchemaEnvelope)
	}
	// And it must scale with the catalog: the defect was thousands of tokens
	// going unpriced because there are 50+ builtin tools.
	many := make([]provider.ToolSchema, 0, 50)
	for i := 0; i < 50; i++ {
		many = append(many, one[0])
	}
	if gotMany := Tools(many); gotMany != 50*got {
		t.Errorf("Tools(50 copies) = %d, want %d", gotMany, 50*got)
	}
}

// TestToolsIgnoresOutputSchema is P62.6's instrument fix, and it is a claim
// about the adapters rather than about arithmetic: OutputSchema is a P3.6
// affordance for clients and validators, and no adapter serializes it, so a
// request never carries those bytes. Counting them charged the local profile's
// 27 exposed tools ~339 tokens that do not exist, and the one production caller
// of this estimator — engine's compactionGuard.requestOverhead — spends the
// result as real context headroom.
//
// The guard rail this test is really placing: if an adapter ever *does* start
// sending output schemas, the omission flips from an overcount to an undercount,
// which is the dangerous direction for a compaction trigger. This failing is the
// signal to re-add the term, not to delete the test. See the wire builders in
// provider/anthropic (wireTool) and provider/openai (Function.Parameters).
func TestToolsIgnoresOutputSchema(t *testing.T) {
	base := provider.ToolSchema{
		Name:        "read_file",
		Description: "Read a file from the workspace and return its contents.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}
	withOutput := base
	withOutput.OutputSchema = json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"},"path":{"type":"string"},"bytes":{"type":"integer"}},"required":["content","path"]}`)

	if len(withOutput.OutputSchema) < 100 {
		t.Fatalf("fixture output schema is only %d bytes — too small for this test to distinguish counted from uncounted", len(withOutput.OutputSchema))
	}
	got, want := Tools([]provider.ToolSchema{withOutput}), Tools([]provider.ToolSchema{base})
	if got != want {
		t.Errorf("Tools() with an output schema = %d, without = %d: the output schema is being priced, but no adapter sends it", got, want)
	}
}
