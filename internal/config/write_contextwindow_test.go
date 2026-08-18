package config

import (
	"strings"
	"testing"
)

// The reason this patch exists rather than reusing PatchGlobalProvider: a
// calibrated window is meaningless without the comment explaining what it was
// calibrated against, and rebuilding the provider block deletes it. Every
// surrounding line must survive byte-identical.
func TestPatchContextWindowKeepsSurroundingComments(t *testing.T) {
	in := `# top of file
provider:
  default: ollama
  model: "aegis-qwen35-9b:16k"
  # 16000 is load-bearing for the debate topology, NOT a spare knob.
  # It overrides each model's Modelfile num_ctx pin.
  context_window: 16000
  max_tokens: 8192

personas:
  arbiter: { model: aegis-phi4-reasoning:16k }
`
	out, err := patchContextWindowLine([]byte(in), 20480)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, "  context_window: 20480") {
		t.Errorf("new value not written:\n%s", got)
	}
	// Only the value line changes. "16000" still appears in the comment above
	// it, and must: preserving that text is the whole reason for this patch,
	// even though it leaves the comment naming a number that moved. A comment
	// that has to be re-read by a human is a better failure than one deleted
	// without being read at all.
	if strings.Contains(got, "context_window: 16000") {
		t.Errorf("old value survived on the value line:\n%s", got)
	}
	for _, keep := range []string{
		"# top of file",
		"# 16000 is load-bearing for the debate topology, NOT a spare knob.",
		"# It overrides each model's Modelfile num_ctx pin.",
		`  model: "aegis-qwen35-9b:16k"`,
		"  max_tokens: 8192",
		"  arbiter: { model: aegis-phi4-reasoning:16k }",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("patch dropped a line it must preserve: %q\n%s", keep, got)
		}
	}
	if a, b := strings.Count(in, "\n"), strings.Count(got, "\n"); a != b {
		t.Errorf("line count changed %d -> %d; the patch should replace in place", a, b)
	}
}

func TestPatchContextWindowInsertsAfterModelWhenAbsent(t *testing.T) {
	in := `provider:
  default: ollama
  model: "m"
  max_tokens: 8192

cost:
  budget_usd: 0.0
`
	out, err := patchContextWindowLine([]byte(in), 4096)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	lines := strings.Split(string(out), "\n")
	var modelAt, winAt = -1, -1
	for i, l := range lines {
		if strings.Contains(l, "model:") {
			modelAt = i
		}
		if strings.Contains(l, "context_window:") {
			winAt = i
		}
	}
	if winAt < 0 {
		t.Fatalf("context_window not inserted:\n%s", out)
	}
	if winAt != modelAt+1 {
		t.Errorf("inserted at line %d, want right after model: (%d)", winAt, modelAt)
	}
	if !strings.Contains(string(out), "budget_usd: 0.0") {
		t.Error("patch damaged a later section")
	}
}

// A context_window line in another top-level section is not the provider's.
func TestPatchContextWindowOnlyTouchesTheProviderBlock(t *testing.T) {
	in := `provider:
  default: ollama
  model: "m"
  context_window: 8192

guard:
  context_window: 999
`
	out, err := patchContextWindowLine([]byte(in), 4096)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if !strings.Contains(string(out), "  context_window: 999") {
		t.Errorf("patched a line outside the provider block:\n%s", out)
	}
	if !strings.Contains(string(out), "  context_window: 4096") {
		t.Errorf("provider window not patched:\n%s", out)
	}
}

// Creating a provider block would silently drop the adapter and base URL a
// real one carries, so a config without one is an error rather than a write.
func TestPatchContextWindowRefusesWithoutAProviderBlock(t *testing.T) {
	if _, err := patchContextWindowLine([]byte("cost:\n  budget_usd: 0.0\n"), 4096); err == nil {
		t.Error("expected an error when there is no provider block")
	}
}
