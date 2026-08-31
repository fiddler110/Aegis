package modelpick

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/hwinfo"
)

// ram16 and ram32 are the two machines this ranking was designed against: a
// 16 GB desktop and a 32 GB unified-memory laptop.
var (
	ram16 = hwinfo.Info{CPUCores: 16, TotalRAMBytes: 16 << 30, RAMSource: hwinfo.SourceWinAPI}
	ram32 = hwinfo.Info{CPUCores: 12, TotalRAMBytes: 32 << 30, RAMSource: hwinfo.SourceSysctl}
)

// realTagsPayload is a verbatim GET /api/tags response from the machine this
// change was written on, trimmed to the fields the ranking reads. It is the
// regression that started P82: the pre-P82 rule took the first entry, which
// Ollama orders most-recently-modified — llama3.2:3b, pulled for one
// experiment — and pinned the whole machine to a 3B while a 9.2B sat beside it.
const realTagsPayload = `{"models":[
 {"name":"llama3.2:3b","size":2019393189,"modified_at":"2026-08-25T09:31:04Z",
  "details":{"family":"llama","parameter_size":"3.2B","quantization_level":"Q4_K_M"},
  "capabilities":["completion","tools"]},
 {"name":"aegis-qwen35-9b:16k","size":7056739633,"modified_at":"2026-08-17T13:39:12Z",
  "details":{"family":"qwen35","parameter_size":"9.2B","quantization_level":"Q4_K_M"},
  "capabilities":["completion","vision"]},
 {"name":"aegis-phi4-reasoning:16k","size":3152479140,"modified_at":"2026-08-17T13:37:54Z",
  "details":{"family":"phi3","parameter_size":"3.8B","quantization_level":"Q4_K_M"},
  "capabilities":["completion"]},
 {"name":"phi4-mini-reasoning:3.8b","size":3152479391,"modified_at":"2026-08-17T13:31:48Z",
  "details":{"family":"phi3","parameter_size":"3.8B","quantization_level":"Q4_K_M"},
  "capabilities":["completion"]},
 {"name":"aegis-qwen35-9b:32k","size":7056739969,"modified_at":"2026-08-16T20:42:54Z",
  "details":{"family":"qwen35","parameter_size":"9.2B","quantization_level":"Q4_K_M"},
  "capabilities":["completion","vision"]},
 {"name":"nomic-embed-text:latest","size":274302450,"modified_at":"2026-08-10T09:47:53Z",
  "details":{"family":"nomic-bert","parameter_size":"137M","quantization_level":"F16"},
  "capabilities":["embedding"]}]}`

func realTags(t *testing.T) []Model {
	t.Helper()
	var out struct {
		Models []struct {
			Name         string    `json:"name"`
			Size         int64     `json:"size"`
			ModifiedAt   time.Time `json:"modified_at"`
			Capabilities []string  `json:"capabilities"`
			Details      struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
				Quantization  string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(realTagsPayload), &out); err != nil {
		t.Fatalf("decode captured /api/tags: %v", err)
	}
	models := make([]Model, 0, len(out.Models))
	for _, m := range out.Models {
		models = append(models, Model{
			Name: m.Name, SizeBytes: m.Size, ModifiedAt: m.ModifiedAt,
			Capabilities:  m.Capabilities,
			Family:        m.Details.Family,
			ParameterSize: m.Details.ParameterSize,
			Quantization:  m.Details.Quantization,
		})
	}
	return models
}

// The headline regression: on a real machine, the 9.2B wins over the 3B that
// happens to be most recently pulled, background calls get the small
// non-thinking tool-caller, and think is turned on for a qwen3-family model
// whose manifest never says "thinking".
func TestSelectOnACapturedRealMachine(t *testing.T) {
	sel := Select(realTags(t), ram16, 0)

	if !strings.HasPrefix(sel.Main, "aegis-qwen35-9b") {
		t.Errorf("main = %q, want the 9.2B qwen — not whichever tag was pulled last", sel.Main)
	}
	if sel.Small != "llama3.2:3b" {
		t.Errorf("small = %q, want llama3.2:3b (smallest non-thinking, tool-capable)", sel.Small)
	}
	if !sel.Think {
		t.Error("think = false for a qwen3-family main model; the manifest omits the capability, so the name heuristic has to carry it")
	}
	if sel.Main == "nomic-embed-text:latest" || sel.Small == "nomic-embed-text:latest" {
		t.Error("an embedding-only model was selected; it cannot serve a chat turn at all")
	}
}

// Tool capability is a tiebreak, never a filter. aegis-qwen35-9b is imported
// from a GGUF and so reports no "tools" in its manifest while calling tools
// perfectly well; filtering on the claim would hand the machine to the 3B.
func TestToolCapabilityDoesNotFilterOutTheBestModel(t *testing.T) {
	sel := Select(realTags(t), ram16, 0)
	for _, m := range realTags(t) {
		if m.Name == sel.Main && m.ToolCapable() {
			t.Fatalf("test no longer exercises its case: %s now advertises tools", m.Name)
		}
	}
	if !strings.HasPrefix(sel.Main, "aegis-qwen35-9b") {
		t.Errorf("main = %q — a model without the tools claim was excluded rather than merely ranked lower", sel.Main)
	}
}

// At equal parameter counts the tools claim decides, which is what it is for.
func TestToolCapabilityBreaksATieOnSize(t *testing.T) {
	sel := Select([]Model{
		{Name: "plain:8b", ParameterSize: "8B", SizeBytes: 5 << 30, Capabilities: []string{"completion"}},
		{Name: "tooled:8b", ParameterSize: "8B", SizeBytes: 5 << 30, Capabilities: []string{"completion", "tools"}},
	}, ram32, 0)
	if sel.Main != "tooled:8b" {
		t.Errorf("main = %q, want tooled:8b — equal size, and only one can call tools", sel.Main)
	}
}

// The 32 GB laptop from the report: a 17 GB 27B model is the right pick there,
// and the RAM ceiling must not quietly exclude it.
func TestLargeModelFitsAThirtyTwoGigMachine(t *testing.T) {
	models := []Model{
		{Name: "phi4-mini:latest", ParameterSize: "3.8B", SizeBytes: 3500 * 1000 * 1000, Capabilities: []string{"completion", "tools"}},
		{Name: "qwen35:9b", ParameterSize: "9.2B", SizeBytes: 9 * 1000 * 1000 * 1000, Capabilities: []string{"completion"}},
		{Name: "qwen38:27b", ParameterSize: "27B", SizeBytes: 17 * 1000 * 1000 * 1000, Capabilities: []string{"completion"}},
	}
	sel := Select(models, ram32, 0)
	if sel.Main != "qwen38:27b" {
		t.Errorf("main = %q, want qwen38:27b — 17 GB of weights fits 32 GB of RAM", sel.Main)
	}
	if sel.Small != "phi4-mini:latest" {
		t.Errorf("small = %q, want phi4-mini:latest", sel.Small)
	}
}

// The same 27B on the 16 GB machine is the case the ceiling exists for.
func TestOversizedModelIsExcludedOnASmallMachine(t *testing.T) {
	models := []Model{
		{Name: "llama3.2:3b", ParameterSize: "3.2B", SizeBytes: 2 << 30, Capabilities: []string{"completion", "tools"}},
		{Name: "qwen35:9b", ParameterSize: "9.2B", SizeBytes: 7 << 30, Capabilities: []string{"completion"}},
		{Name: "huge:70b", ParameterSize: "70B", SizeBytes: 40 << 30, Capabilities: []string{"completion", "tools"}},
	}
	sel := Select(models, ram16, 0)
	if sel.Main != "qwen35:9b" {
		t.Errorf("main = %q, want qwen35:9b — the 70B does not fit 16 GB", sel.Main)
	}
	if !strings.Contains(strings.Join(sel.Reasons, " "), "excluded as too large") {
		t.Errorf("reasons never mention the exclusion, so the operator cannot tell why the 70B lost:\n%s",
			strings.Join(sel.Reasons, "\n"))
	}
}

// A stated provider.vram_budget_gb outranks detected RAM: it is the operator's
// own number for the same question, and the daemon already plans against it.
func TestStatedBudgetOutranksDetectedRAM(t *testing.T) {
	models := []Model{
		{Name: "small:3b", ParameterSize: "3B", SizeBytes: 2 << 30, Capabilities: []string{"completion"}},
		{Name: "big:14b", ParameterSize: "14B", SizeBytes: 10 << 30, Capabilities: []string{"completion"}},
	}
	if sel := Select(models, ram32, 6); sel.Main != "small:3b" {
		t.Errorf("main = %q under a 6 GiB budget, want small:3b", sel.Main)
	}
	if sel := Select(models, ram32, 0); sel.Main != "big:14b" {
		t.Errorf("main = %q with no budget on a 32 GB box, want big:14b", sel.Main)
	}
}

// Nothing fits: choosing anyway beats leaving "auto", which resolves to an
// arbitrary model with none of this said out loud.
func TestNothingFitsPicksTheSmallestAndSaysSo(t *testing.T) {
	sel := Select([]Model{
		{Name: "big:70b", ParameterSize: "70B", SizeBytes: 40 << 30, Capabilities: []string{"completion"}},
		{Name: "bigger:120b", ParameterSize: "120B", SizeBytes: 70 << 30, Capabilities: []string{"completion"}},
	}, ram16, 0)
	if sel.Main != "big:70b" {
		t.Errorf("main = %q, want the smallest of two over-budget models", sel.Main)
	}
	if !strings.Contains(strings.Join(sel.Reasons, " "), "spill to system RAM") {
		t.Errorf("the compromise was not stated:\n%s", strings.Join(sel.Reasons, "\n"))
	}
}

// small_model exists to make background calls cheap. A second model the same
// size as the primary buys nothing and costs a second resident set of weights,
// so it must not be named.
func TestNoSmallModelWhenNothingIsMeaningfullySmaller(t *testing.T) {
	sel := Select([]Model{
		{Name: "a:9b", ParameterSize: "9B", SizeBytes: 7 << 30, Capabilities: []string{"completion"}},
		{Name: "b:8b", ParameterSize: "8B", SizeBytes: 6 << 30, Capabilities: []string{"completion"}},
	}, ram16, 0)
	if sel.Small != "" {
		t.Errorf("small = %q, want none — an 8B is not a cheap companion to a 9B", sel.Small)
	}
}

// The template's own advice, applied: a small NON-thinking model. A reasoning
// model would drag a thinking pass through every session title.
func TestSmallModelPrefersNonThinking(t *testing.T) {
	sel := Select([]Model{
		{Name: "main:14b", ParameterSize: "14B", SizeBytes: 9 << 30, Capabilities: []string{"completion"}},
		{Name: "phi4-mini-reasoning:3.8b", ParameterSize: "3.8B", SizeBytes: 2 << 30, Capabilities: []string{"completion"}},
		{Name: "llama3.2:3b", ParameterSize: "3.2B", SizeBytes: 2100 * 1000 * 1000, Capabilities: []string{"completion", "tools"}},
	}, ram16, 0)
	if sel.Small != "llama3.2:3b" {
		t.Errorf("small = %q, want llama3.2:3b — the reasoning 3.8B is nominally similar but thinks", sel.Small)
	}
}

// An embedding-only model is the smallest thing on most machines, which is
// exactly why it kept being chosen before capabilities were consulted.
func TestEmbeddingOnlyModelIsNeverSelected(t *testing.T) {
	sel := Select([]Model{
		{Name: "chat:8b", ParameterSize: "8B", SizeBytes: 5 << 30, Capabilities: []string{"completion"}},
		{Name: "nomic-embed-text", ParameterSize: "137M", SizeBytes: 274302450, Capabilities: []string{"embedding"}},
	}, ram16, 0)
	if sel.Main != "chat:8b" || sel.Small != "" {
		t.Errorf("selected main=%q small=%q; the embedding model must be invisible to both", sel.Main, sel.Small)
	}
}

// A pre-0.6 Ollama reports no capabilities at all. Absence of the field is not
// evidence the model cannot chat, so it must still be rankable.
func TestModelWithNoCapabilitiesFieldIsStillSelectable(t *testing.T) {
	sel := Select([]Model{{Name: "mystery:7b", ParameterSize: "7B", SizeBytes: 4 << 30}}, ram16, 0)
	if sel.Main != "mystery:7b" {
		t.Errorf("main = %q, want mystery:7b", sel.Main)
	}
}

func TestThinksDetection(t *testing.T) {
	for _, tc := range []struct {
		m    Model
		want bool
	}{
		{Model{Name: "gemma3:12b", Capabilities: []string{"completion", "thinking"}}, true},
		{Model{Name: "aegis-qwen35-9b:32k", Family: "qwen35"}, true},
		{Model{Name: "qwen3:14b"}, true},
		{Model{Name: "deepseek-r1:8b"}, true},
		{Model{Name: "phi4-mini-reasoning:3.8b"}, true},
		{Model{Name: "qwq:32b"}, true},
		{Model{Name: "qwen3.6:35b-a3b-deep"}, true},
		{Model{Name: "llama3.2:3b", Family: "llama"}, false},
		{Model{Name: "qwen2.5-coder:7b"}, false},
		// The variants a bare "qwen3" prefix would have got wrong: both are
		// chosen precisely because they do not think.
		{Model{Name: "qwen3-coder:30b"}, false},
		{Model{Name: "qwen3-30b-a3b-instruct"}, false},
	} {
		if got := tc.m.Thinks(); got != tc.want {
			t.Errorf("Thinks(%q, caps=%v) = %v, want %v", tc.m.Name, tc.m.Capabilities, got, tc.want)
		}
	}
}

func TestParams(t *testing.T) {
	for _, tc := range []struct {
		m    Model
		want float64
	}{
		{Model{ParameterSize: "9.2B"}, 9.2},
		{Model{ParameterSize: "70b"}, 70},
		{Model{ParameterSize: "137M"}, 0.137},
		// No parameter_size: synthesized from on-disk size so the model still
		// sorts against the others instead of sorting as zero.
		{Model{SizeBytes: 6_200_000_000}, 10},
		{Model{}, 0},
	} {
		if got := tc.m.Params(); got < tc.want-0.001 || got > tc.want+0.001 {
			t.Errorf("Params(%+v) = %v, want %v", tc.m, got, tc.want)
		}
	}
}

// The ranking must be a total order, or two --first-init runs on an unchanged
// machine write different files.
func TestSelectIsDeterministicUnderReordering(t *testing.T) {
	models := realTags(t)
	want := Select(models, ram16, 0)
	for i := range models {
		rotated := append(append([]Model{}, models[i:]...), models[:i]...)
		got := Select(rotated, ram16, 0)
		if got.Main != want.Main || got.Small != want.Small || got.Think != want.Think {
			t.Fatalf("rotation by %d changed the pick: %s/%s vs %s/%s",
				i, got.Main, got.Small, want.Main, want.Small)
		}
	}
}

// With no budget and no detectable RAM the ranking still has to answer, and it
// has to say the ceiling was not knowable rather than imply one.
func TestUnknownRAMRanksUnbounded(t *testing.T) {
	sel := Select([]Model{
		{Name: "huge:70b", ParameterSize: "70B", SizeBytes: 40 << 30, Capabilities: []string{"completion"}},
		{Name: "small:3b", ParameterSize: "3B", SizeBytes: 2 << 30, Capabilities: []string{"completion"}},
	}, hwinfo.Info{CPUCores: 8, RAMSource: hwinfo.SourceUnknown}, 0)
	if sel.Main != "huge:70b" {
		t.Errorf("main = %q, want huge:70b — nothing was known to exclude it", sel.Main)
	}
	if !strings.Contains(sel.CeilingSource, "unbounded") {
		t.Errorf("ceiling source = %q, want it to admit no bound was knowable", sel.CeilingSource)
	}
}

func TestSelectWithNoChatCapableModels(t *testing.T) {
	sel := Select([]Model{{Name: "nomic-embed-text", Capabilities: []string{"embedding"}}}, ram16, 0)
	if sel.Main != "" {
		t.Errorf("main = %q, want empty so the caller leaves provider.model alone", sel.Main)
	}
	if len(sel.Reasons) == 0 {
		t.Error("no reason given for selecting nothing")
	}
}
