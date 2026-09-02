package profile

import "testing"

func TestNewResolver_CloudDefaultEngagesNothing(t *testing.T) {
	resolve := NewResolver(false, nil)
	h := resolve("gpt-5")
	if h.ProseToolCallSalvage || h.ArgumentShapeRepair {
		t.Errorf("cloud default should engage nothing, got %+v", h)
	}
}

func TestNewResolver_LocalDefaultEngagesBoth(t *testing.T) {
	resolve := NewResolver(true, nil)
	h := resolve("qwen3:14b")
	if !h.ProseToolCallSalvage || !h.ArgumentShapeRepair {
		t.Errorf("local default should engage both, got %+v", h)
	}
}

func TestNewResolver_OverrideCorrectsOneFieldOnly(t *testing.T) {
	no := false
	resolve := NewResolver(true, map[string]Override{
		"gpt-oss:20b": {ProseToolCallSalvage: &no},
	})
	h := resolve("gpt-oss:20b")
	if h.ProseToolCallSalvage {
		t.Error("override should have disabled ProseToolCallSalvage")
	}
	if !h.ArgumentShapeRepair {
		t.Error("override named only one field; ArgumentShapeRepair should stay at the local default (true)")
	}
}

func TestNewResolver_OverrideCanEnableOnCloudDefault(t *testing.T) {
	yes := true
	resolve := NewResolver(false, map[string]Override{
		"qwen2.5-coder:1.5b": {ArgumentShapeRepair: &yes},
	})
	h := resolve("qwen2.5-coder:1.5b")
	if h.ProseToolCallSalvage {
		t.Error("unnamed field should stay at the cloud default (false)")
	}
	if !h.ArgumentShapeRepair {
		t.Error("override should have enabled ArgumentShapeRepair despite the cloud default")
	}
}

func TestNewResolver_UnnamedModelUsesDefaultUnchanged(t *testing.T) {
	yes := true
	resolve := NewResolver(false, map[string]Override{
		"qwen2.5-coder:1.5b": {ArgumentShapeRepair: &yes},
	})
	h := resolve("claude-sonnet-5")
	if h.ProseToolCallSalvage || h.ArgumentShapeRepair {
		t.Errorf("a model with no override should get the unmodified default, got %+v", h)
	}
}

// TestNewResolver_PromptSuffixAndToolFieldsHaveNoDefault confirms the three
// P74.21 fields start empty for every model — unlike the two repair bools,
// nothing in NewResolver's `local` argument should ever populate them.
func TestNewResolver_PromptSuffixAndToolFieldsHaveNoDefault(t *testing.T) {
	for _, local := range []bool{false, true} {
		h := NewResolver(local, nil)("some-model")
		if h.PromptSuffix != "" || h.ToolDescriptionOverrides != nil || h.DeferredTools != nil {
			t.Errorf("local=%v: expected zero-value P74.21 fields with no overrides, got %+v", local, h)
		}
	}
}

// TestNewResolver_PromptSuffixAndToolFieldsLayerAdditively confirms an
// override sets these three fields for its named model without disturbing an
// unrelated model, mirroring the additive layering the two repair bools
// already have.
func TestNewResolver_PromptSuffixAndToolFieldsLayerAdditively(t *testing.T) {
	resolve := NewResolver(true, map[string]Override{
		"quirky-model": {
			PromptSuffix:             strPtr("use snake_case arguments"),
			ToolDescriptionOverrides: map[string]string{"read_file": "loads a file"},
			DeferredTools:            []string{"security_scan"},
		},
	})

	h := resolve("quirky-model")
	if h.PromptSuffix != "use snake_case arguments" {
		t.Errorf("PromptSuffix = %q, want the configured suffix", h.PromptSuffix)
	}
	if h.ToolDescriptionOverrides["read_file"] != "loads a file" {
		t.Errorf("ToolDescriptionOverrides = %+v, want read_file overridden", h.ToolDescriptionOverrides)
	}
	if len(h.DeferredTools) != 1 || h.DeferredTools[0] != "security_scan" {
		t.Errorf("DeferredTools = %+v, want [security_scan]", h.DeferredTools)
	}

	other := resolve("plain-model")
	if other.PromptSuffix != "" || other.ToolDescriptionOverrides != nil || other.DeferredTools != nil {
		t.Errorf("an unrelated model must not inherit another model's P74.21 fields, got %+v", other)
	}
}

func strPtr(s string) *string { return &s }

func TestValidateOverrides_RejectsDeferringToolSearch(t *testing.T) {
	err := ValidateOverrides(map[string]Override{
		"quirky-model": {DeferredTools: []string{"tool_search"}},
	})
	if err == nil {
		t.Fatal("expected an error deferring tool_search, got nil")
	}
}

func TestValidateOverrides_AllowsDeferringOtherTools(t *testing.T) {
	err := ValidateOverrides(map[string]Override{
		"quirky-model": {DeferredTools: []string{"security_scan", "diagram"}},
	})
	if err != nil {
		t.Errorf("expected no error deferring non-required tools, got %v", err)
	}
}

func TestValidateOverrides_NilAndEmptyAreFine(t *testing.T) {
	if err := ValidateOverrides(nil); err != nil {
		t.Errorf("nil overrides should validate cleanly, got %v", err)
	}
	if err := ValidateOverrides(map[string]Override{}); err != nil {
		t.Errorf("empty overrides should validate cleanly, got %v", err)
	}
}
