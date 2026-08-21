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
