package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/discover"
	"github.com/fiddler110/aegis/internal/hwinfo"
	"github.com/fiddler110/aegis/internal/modelcatalog"
)

func runModels(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newModelsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestModelsCommandDefaultListsCatalog(t *testing.T) {
	out, err := runModels(t)
	if err != nil {
		t.Fatalf("models: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "PROVIDER") || !strings.Contains(out, "qwen3") {
		t.Errorf("expected curated catalog table, got:\n%s", out)
	}
	if strings.Contains(out, "Detected hardware") {
		t.Errorf("--recommend output should not appear without the flag, got:\n%s", out)
	}
}

func TestModelsCommandRecommendFlagPrintsHardwareAndList(t *testing.T) {
	out, err := runModels(t, "--recommend")
	if err != nil {
		t.Fatalf("models --recommend: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Detected hardware:") {
		t.Errorf("expected a 'Detected hardware:' line, got:\n%s", out)
	}
	// CPUCores is always detectable, so the CPU count must show up somewhere
	// in the describe line even when RAM detection fails on this platform.
	if !strings.Contains(out, "CPU cores") {
		t.Errorf("expected CPU core count in hardware summary, got:\n%s", out)
	}
}

func TestFamilyPulled(t *testing.T) {
	cases := []struct {
		name   string
		family string
		pulled []string
		want   bool
	}{
		{"exact bare match", "qwen3", []string{"qwen3"}, true},
		{"tagged match", "qwen3", []string{"qwen3:8b"}, true},
		{"case-insensitive", "Qwen3", []string{"qwen3:8b"}, true},
		{"no match", "llama3.1", []string{"qwen3:8b", "deepseek-r1:7b"}, false},
		{"empty pulled list", "qwen3", nil, false},
		{"different family, same prefix substring must not match", "qwen3", []string{"qwen3-coder-nightly:latest"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := familyPulled(tc.family, tc.pulled); got != tc.want {
				t.Errorf("familyPulled(%q, %v) = %v, want %v", tc.family, tc.pulled, got, tc.want)
			}
		})
	}
}

func TestPrintRecommendationSuggestsPullForMissingModels(t *testing.T) {
	var buf bytes.Buffer
	hw := hwinfo.Info{CPUCores: 8, RAMSource: hwinfo.SourceProcMeminfo, TotalRAMBytes: 32 << 30}
	printRecommendation(&buf, hw, []string{"qwen3:8b"}) // qwen3 already pulled, others aren't

	out := buf.String()
	if !strings.Contains(out, "Detected hardware:") {
		t.Errorf("expected hardware line, got:\n%s", out)
	}
	if strings.Contains(out, "ollama pull qwen3\n") {
		t.Errorf("qwen3 is already pulled and should not be suggested, got:\n%s", out)
	}
	// deepseek-r1 requires 16GB (see modelcatalog.Curated) and we gave 32GB,
	// so it should be recommended and, since it's not in the pulled list,
	// suggested for pull.
	if !strings.Contains(out, "ollama pull deepseek-r1") {
		t.Errorf("expected an ollama pull suggestion for deepseek-r1, got:\n%s", out)
	}
}

func TestPrintRecommendationAllPulledNoSuggestions(t *testing.T) {
	var buf bytes.Buffer
	hw := hwinfo.Info{CPUCores: 8, RAMSource: hwinfo.SourceProcMeminfo, TotalRAMBytes: 32 << 30}
	var allFamilies []string
	for _, m := range modelcatalog.ForTier(modelcatalog.TierLocal) {
		allFamilies = append(allFamilies, m.ID+":latest")
	}
	printRecommendation(&buf, hw, allFamilies)

	out := buf.String()
	if strings.Contains(out, "ollama pull") {
		t.Errorf("no pull suggestions expected when everything is already pulled, got:\n%s", out)
	}
	if !strings.Contains(out, "already appear to be pulled") {
		t.Errorf("expected the all-pulled confirmation message, got:\n%s", out)
	}
}

func TestPrintRecommendationUnknownRAMShowsFullListUnnarrowed(t *testing.T) {
	var buf bytes.Buffer
	hw := hwinfo.Info{CPUCores: 4, RAMSource: hwinfo.SourceUnknown}
	printRecommendation(&buf, hw, nil)

	out := buf.String()
	if !strings.Contains(out, "RAM undetected") {
		t.Errorf("expected an unnarrowed-list disclaimer for unknown RAM, got:\n%s", out)
	}
	for _, m := range modelcatalog.ForTier(modelcatalog.TierLocal) {
		if !strings.Contains(out, m.ID) {
			t.Errorf("expected unnarrowed output to include %s, got:\n%s", m.ID, out)
		}
	}
}

func TestFilterOllamaNames(t *testing.T) {
	found := []discover.Model{
		{Name: "qwen3:8b", Provider: "ollama", Endpoint: "http://localhost:11434"},
		{Name: "llama3.1:8b", Provider: "ollama", Endpoint: "http://localhost:11434"},
		{Name: "mistral-7b", Provider: "lmstudio", Endpoint: "http://localhost:1234"},
		{Name: "gpt-oss", Provider: "litellm", Endpoint: "http://localhost:4000"},
	}
	got := filterOllamaNames(found)
	want := []string{"qwen3:8b", "llama3.1:8b"}
	if len(got) != len(want) {
		t.Fatalf("filterOllamaNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filterOllamaNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if got := filterOllamaNames(nil); got != nil {
		t.Errorf("filterOllamaNames(nil) = %v, want nil", got)
	}
	if got := filterOllamaNames([]discover.Model{{Name: "x", Provider: "lmstudio"}}); got != nil {
		t.Errorf("filterOllamaNames() with no ollama entries = %v, want nil", got)
	}
}
