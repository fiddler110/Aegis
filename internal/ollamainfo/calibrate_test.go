package ollamainfo

import (
	"context"
	"testing"
)

// qwen9b is the geometry this project's own calibration was measured against:
// 132 KiB of KV cache per token, so a 16 GB card's usable range lands in the
// tens of thousands of tokens.
var qwen9b = KVGeometry{
	Arch: "qwen35", BlockCount: 33, HeadCountKV: 4,
	KeyLength: 256, ValueLength: 256, ContextMax: 262144,
}

// fakeMachine is a synthetic host with a fixed VRAM capacity and a model of a
// fixed weight. It answers a probe exactly as Ollama does: everything is in
// VRAM while weights + KV fit, and once they do not, the report shows a
// partial placement. Every real probe is a model reload costing tens of
// seconds, so this is the only way the search itself can be tested.
type fakeMachine struct {
	capacity int64 // total VRAM bytes available to the runner
	weights  int64
	g        KVGeometry
	t        KVCacheType
	probes   []int // every window it was asked about, in order
	// cacheDivisor models a hybrid-attention stack, where only 1 in N blocks
	// holds a KV cache and the geometry in /api/show does not say so. 0 or 1
	// is a conventional model, where the formula is right.
	cacheDivisor int64
}

func (m *fakeMachine) probe() Probe {
	return func(_ context.Context, numCtx int) (Footprint, bool) {
		m.probes = append(m.probes, numCtx)
		kv, ok := KVBytes(m.g, numCtx, m.t)
		if !ok {
			return Footprint{}, false
		}
		if m.cacheDivisor > 1 {
			kv /= m.cacheDivisor
		}
		total := m.weights + kv
		f := Footprint{Size: total, SizeVRAM: total, ContextLength: numCtx}
		if total > m.capacity {
			// Ollama offloads what it can and leaves the rest in system RAM.
			f.SizeVRAM = m.capacity
		}
		return f, true
	}
}

func newFakeMachine(capacityGiB, weightsGiB float64) *fakeMachine {
	gib := func(f float64) int64 { return int64(f * float64(int64(1)<<30)) }
	return &fakeMachine{
		capacity: gib(capacityGiB), weights: gib(weightsGiB),
		g: qwen9b, t: KVTypeF16,
	}
}

// The headline case: a 14.5 GiB card holding a 4 GiB model. The measured
// capacity must land close to the truth, and — this is the part that matters —
// it must never come out *above* it, since an overstated budget produces a
// window that spills on the first real turn.
func TestCalibrateMeasuresCapacityWithoutOverstatingIt(t *testing.T) {
	m := newFakeMachine(14.5, 4)
	cal, ok := Calibrate(context.Background(), m.g, m.t, DefaultCalibrationProbes, m.probe())
	if !ok {
		t.Fatal("calibration did not complete on a healthy machine")
	}
	if cal.Capacity > m.capacity {
		t.Errorf("measured capacity %d exceeds the real %d — a budget above the truth spills on the first turn",
			cal.Capacity, m.capacity)
	}
	// Within one probe's resolution of the real capacity.
	slack := float64(m.capacity-cal.Capacity) / float64(m.capacity)
	if slack > 0.10 {
		t.Errorf("measured %.2f GiB against a real %.2f GiB — %.0f%% low, too coarse to be useful",
			cal.CapacityGiB(), float64(m.capacity)/float64(int64(1)<<30), slack*100)
	}
	if cal.Weights != m.weights {
		t.Errorf("weights = %d, want the exact %d — they are window-invariant and measured once", cal.Weights, m.weights)
	}
}

// Every window above the largest observed fit must actually spill, and every
// window below it must fit. Bisection is only sound because the property is
// monotonic, so the search must not report a bracket that violates it.
func TestCalibrateBracketIsConsistent(t *testing.T) {
	m := newFakeMachine(14.5, 4)
	cal, _ := Calibrate(context.Background(), m.g, m.t, DefaultCalibrationProbes, m.probe())
	for _, s := range cal.Steps {
		if s.Fit && s.Window > cal.LargestFit {
			t.Errorf("window %d fit but is above LargestFit %d", s.Window, cal.LargestFit)
		}
		if !s.Fit && cal.SmallestSpill > 0 && s.Window < cal.SmallestSpill {
			t.Errorf("window %d spilled but is below SmallestSpill %d", s.Window, cal.SmallestSpill)
		}
	}
	if cal.SmallestSpill > 0 && cal.SmallestSpill <= cal.LargestFit {
		t.Errorf("bracket is inverted: fit %d, spill %d", cal.LargestFit, cal.SmallestSpill)
	}
}

// The probe budget is the whole cost of this command — each one is a full model
// reload — so it has to be honoured exactly.
func TestCalibrateRespectsTheProbeBudget(t *testing.T) {
	for _, max := range []int{1, 2, 3, 5, 8} {
		m := newFakeMachine(14.5, 4)
		cal, ok := Calibrate(context.Background(), m.g, m.t, max, m.probe())
		if !ok {
			t.Fatalf("max=%d: calibration failed", max)
		}
		if len(m.probes) > max {
			t.Errorf("max=%d: spent %d probes", max, len(m.probes))
		}
		if len(cal.Steps) != len(m.probes) {
			t.Errorf("max=%d: logged %d steps for %d probes", max, len(cal.Steps), len(m.probes))
		}
		// Even a single probe must produce a usable, conservative answer.
		if cal.Capacity > m.capacity {
			t.Errorf("max=%d: overstated capacity", max)
		}
	}
}

// More probes must never make the answer worse — that is what makes the budget
// flag safe to lower on a slow machine.
func TestMoreProbesNeverDegradeTheEstimate(t *testing.T) {
	prev := int64(0)
	for _, max := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		m := newFakeMachine(14.5, 4)
		cal, _ := Calibrate(context.Background(), m.g, m.t, max, m.probe())
		if cal.Capacity < prev {
			t.Errorf("max=%d measured %d, less than max=%d's %d", max, cal.Capacity, max-1, prev)
		}
		prev = cal.Capacity
	}
}

// A CPU-only host reports nothing in VRAM. There is no budget to state, and
// inventing one would be worse than saying so.
func TestCalibrateDetectsACPUOnlyHost(t *testing.T) {
	m := newFakeMachine(0, 4)
	cal, ok := Calibrate(context.Background(), m.g, m.t, DefaultCalibrationProbes, m.probe())
	if !ok {
		t.Fatal("calibration should still reach a verdict on a CPU-only host")
	}
	if !cal.NoGPU {
		t.Error("did not detect the absence of any GPU placement")
	}
	if len(m.probes) != 1 {
		t.Errorf("spent %d probes discovering there is no GPU; one is enough", len(m.probes))
	}
	if cal.Capacity != 0 {
		t.Errorf("reported a capacity of %d on a host with no VRAM", cal.Capacity)
	}
}

// Weights larger than the card: no window is small enough, and the fix is a
// different model. Searching windows would waste the whole probe budget
// proving something the first probe already showed.
func TestCalibrateDetectsWeightsThatCannotFit(t *testing.T) {
	m := newFakeMachine(4, 8)
	cal, ok := Calibrate(context.Background(), m.g, m.t, DefaultCalibrationProbes, m.probe())
	if !ok {
		t.Fatal("calibration should reach a verdict")
	}
	if !cal.WeightsSpill {
		t.Error("did not detect that the weights alone exceed VRAM")
	}
	if len(m.probes) != 1 {
		t.Errorf("spent %d probes; the floor probe already settled it", len(m.probes))
	}
	if got := cal.Explain("big:70b"); got == "" {
		t.Error("no explanation for an unfittable model")
	}
}

// A machine with more memory than the model can use never spills, so the
// measurement is a lower bound and has to say so rather than imply it found
// the card's limit.
func TestCalibrateReportsAFloorWhenNothingSpills(t *testing.T) {
	m := newFakeMachine(200, 4)
	cal, ok := Calibrate(context.Background(), m.g, m.t, 12, m.probe())
	if !ok {
		t.Fatal("calibration failed")
	}
	if !cal.AtCeiling {
		t.Error("nothing spilled, but the result does not say it is a lower bound")
	}
	if cal.LargestFit != qwen9b.ContextMax {
		t.Errorf("largest fit = %d, want the model's training max %d", cal.LargestFit, qwen9b.ContextMax)
	}
	if cal.SmallestSpill != 0 {
		t.Errorf("reported a spill at %d when nothing spilled", cal.SmallestSpill)
	}
}

// The probe ladder must never ask for a window above the model's training
// maximum: Ollama would silently clamp it, and the verdict would then belong to
// a different window than the one recorded.
func TestCalibrateNeverProbesAboveTheTrainingMax(t *testing.T) {
	m := newFakeMachine(200, 4)
	Calibrate(context.Background(), m.g, m.t, 12, m.probe())
	for _, w := range m.probes {
		if w > qwen9b.ContextMax {
			t.Errorf("probed %d, above the model's %d-token maximum", w, qwen9b.ContextMax)
		}
	}
}

// A probe that never completes ends the ladder with what has been measured so
// far, rather than discarding the run.
func TestCalibrateKeepsWhatItMeasuredWhenAProbeFails(t *testing.T) {
	m := newFakeMachine(14.5, 4)
	real := m.probe()
	calls := 0
	flaky := func(ctx context.Context, numCtx int) (Footprint, bool) {
		calls++
		if calls > 2 {
			return Footprint{}, false // a wedged load
		}
		return real(ctx, numCtx)
	}
	cal, ok := Calibrate(context.Background(), m.g, m.t, DefaultCalibrationProbes, flaky)
	if !ok {
		t.Fatal("a failure after the floor probe should not discard the run")
	}
	if cal.Capacity <= 0 || cal.LargestFit <= 0 {
		t.Errorf("no usable measurement survived: %+v", cal)
	}
	if cal.Capacity > m.capacity {
		t.Error("a truncated run overstated the capacity")
	}
}

// A floor probe that fails is the one case with nothing to report.
func TestCalibrateFailsWhenTheFirstProbeDoes(t *testing.T) {
	dead := func(context.Context, int) (Footprint, bool) { return Footprint{}, false }
	if _, ok := Calibrate(context.Background(), qwen9b, KVTypeF16, 8, dead); ok {
		t.Error("reported success with no measurement at all")
	}
}

// Quantized KV roughly halves the cache, so the same card must measure a
// meaningfully larger window — the flag has to actually reach the search.
func TestCalibrateHonoursTheKVCacheType(t *testing.T) {
	f16 := newFakeMachine(14.5, 4)
	f16.t = KVTypeF16
	a, _ := Calibrate(context.Background(), f16.g, f16.t, DefaultCalibrationProbes, f16.probe())

	q8 := newFakeMachine(14.5, 4)
	q8.t = KVTypeQ8_0
	b, _ := Calibrate(context.Background(), q8.g, q8.t, DefaultCalibrationProbes, q8.probe())

	if b.LargestFit <= a.LargestFit {
		t.Errorf("q8_0 fitted %d tokens vs f16's %d; the cheaper cache should fit more",
			b.LargestFit, a.LargestFit)
	}
}

// Windows land on the same fitStep grid Fit uses, so a calibrated number reads
// as a decision rather than a remainder.
func TestCalibrateProbesLandOnTheFitGrid(t *testing.T) {
	m := newFakeMachine(14.5, 4)
	Calibrate(context.Background(), m.g, m.t, DefaultCalibrationProbes, m.probe())
	for _, w := range m.probes {
		if w%fitStep != 0 {
			t.Errorf("probed %d, which is not a multiple of the %d-token fit step", w, fitStep)
		}
	}
}

// The point of reading size_vram off a spilled probe rather than halving
// blindly: convergence in a handful of loads, each of which costs a full model
// reload on a real machine. A doubling ladder needed eight probes to land 16%
// low on this same machine.
func TestCalibrateConvergesInAFewProbes(t *testing.T) {
	for _, tc := range []struct{ capacityGiB, weightsGiB float64 }{
		{14.5, 4}, {8, 3}, {24, 9}, {48, 17}, {11.5, 6.01},
	} {
		m := newFakeMachine(tc.capacityGiB, tc.weightsGiB)
		cal, ok := Calibrate(context.Background(), m.g, m.t, 4, m.probe())
		if !ok {
			t.Fatalf("%.1f GiB card: calibration failed", tc.capacityGiB)
		}
		if cal.Capacity > m.capacity {
			t.Errorf("%.1f GiB card: overstated as %.2f", tc.capacityGiB, cal.CapacityGiB())
		}
		off := (tc.capacityGiB - cal.CapacityGiB()) / tc.capacityGiB
		if off > 0.03 {
			t.Errorf("%.1f GiB card (%.1f GiB weights): measured %.2f in %d probes — %.1f%% low",
				tc.capacityGiB, tc.weightsGiB, cal.CapacityGiB(), len(m.probes), off*100)
		}
		t.Logf("%.1f GiB card: measured %.2f GiB in %d probes (%.2f%% low)",
			tc.capacityGiB, cal.CapacityGiB(), len(m.probes), off*100)
	}
}

// The case that broke the first live run. aegis-qwen35-9b's real cache growth
// is 33 KiB/token against KVGeometry's predicted 132 — three layers in four use
// linear attention and hold no KV, which /api/show's geometry does not say. A
// calibration that trusted the formula computed a 38 GiB "capacity" for a 16 GB
// card; one that measures must not.
func newHybridMachine(capacityGiB, weightsGiB float64, cacheDivisor int64) *fakeMachine {
	m := newFakeMachine(capacityGiB, weightsGiB)
	m.cacheDivisor = cacheDivisor
	return m
}

func TestCalibrateMeasuresHybridAttentionRatherThanPredictingIt(t *testing.T) {
	m := newHybridMachine(14.5, 5.55, 4)
	cal, ok := Calibrate(context.Background(), m.g, m.t, DefaultCalibrationProbes, m.probe())
	if !ok {
		t.Fatal("calibration failed")
	}
	if cal.Capacity > m.capacity {
		t.Fatalf("measured %.2f GiB on a %.2f GiB card — the formula's error leaked into the result",
			cal.CapacityGiB(), float64(m.capacity)/float64(int64(1)<<30))
	}
	// The measured per-token cost must reflect reality, not the geometry.
	predicted, _ := m.g.BytesPerToken(m.t)
	wantPerToken := predicted / 4
	if off := float64(cal.BytesPerToken-wantPerToken) / float64(wantPerToken); off > 0.05 || off < -0.05 {
		t.Errorf("measured %d bytes/token, want ~%d (the formula says %d)",
			cal.BytesPerToken, wantPerToken, predicted)
	}
	if cal.PredictedPerToken != predicted {
		t.Errorf("PredictedPerToken = %d, want the formula's %d so the gap can be reported",
			cal.PredictedPerToken, predicted)
	}
	if cal.Weights <= 0 {
		t.Error("weights were not recovered from the ladder")
	}
	// And the whole point: a window sized from the measurement is bigger than
	// one sized from the formula, because the formula over-reserves 4x.
	measured := cal.WindowForBudget(m.capacity, m.g.ContextMax)
	fromFormula, _ := Fit(m.g, m.capacity, cal.Weights, m.t)
	if measured <= fromFormula {
		t.Errorf("measured window %d is not larger than the formula's %d", measured, fromFormula)
	}
	t.Logf("measured %d tokens vs the formula's %d, capacity %.2f GiB (real %.2f)",
		measured, fromFormula, cal.CapacityGiB(), float64(m.capacity)/float64(int64(1)<<30))
}

// A conventional model, where the formula is right, must still calibrate
// cleanly — the measurement has to agree with geometry when geometry is correct.
func TestCalibrateAgreesWithTheFormulaOnAConventionalModel(t *testing.T) {
	m := newFakeMachine(14.5, 4)
	cal, _ := Calibrate(context.Background(), m.g, m.t, DefaultCalibrationProbes, m.probe())
	predicted, _ := m.g.BytesPerToken(m.t)
	off := float64(cal.BytesPerToken-predicted) / float64(predicted)
	if off > 0.05 || off < -0.05 {
		t.Errorf("measured %d bytes/token where the formula's %d is correct", cal.BytesPerToken, predicted)
	}
}
