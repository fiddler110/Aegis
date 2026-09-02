package tool

import (
	"context"
	"encoding/json"
	"testing"
)

// TestEffectiveEquivalenceKeyDefaultsFalse mirrors IsPollExempt/
// IsSignatureTransparent's default-false test shape: a tool that does not
// implement EquivalenceClassifier answers ("", false).
func TestEffectiveEquivalenceKeyDefaultsFalse(t *testing.T) {
	tl := &fakeTool{name: "t", desc: "d"}
	if k, ok := EffectiveEquivalenceKey(tl, json.RawMessage(`{}`)); ok || k != "" {
		t.Errorf("EffectiveEquivalenceKey() = (%q, %v), want (\"\", false) for a non-implementer", k, ok)
	}
}

type equivTool struct {
	fakeTool
	key string
	ok  bool
}

func (e *equivTool) EquivalenceKey(json.RawMessage) (string, bool) { return e.key, e.ok }

func TestEffectiveEquivalenceKeyUsesImplementer(t *testing.T) {
	tl := &equivTool{fakeTool: fakeTool{name: "t"}, key: "same-thing", ok: true}
	if k, ok := EffectiveEquivalenceKey(tl, json.RawMessage(`{"a":1}`)); !ok || k != "same-thing" {
		t.Errorf("EffectiveEquivalenceKey() = (%q, %v), want (\"same-thing\", true)", k, ok)
	}
	tl.ok = false
	if _, ok := EffectiveEquivalenceKey(tl, json.RawMessage(`{}`)); ok {
		t.Error("EffectiveEquivalenceKey() ok=true when the implementer declined")
	}
}

// TestEffectiveDestructiveDefaultsFalse mirrors the same shape for Destroyer.
func TestEffectiveDestructiveDefaultsFalse(t *testing.T) {
	tl := &fakeTool{name: "t", desc: "d"}
	if EffectiveDestructive(context.Background(), tl, json.RawMessage(`{}`)) {
		t.Error("EffectiveDestructive() = true for a non-implementer, want false")
	}
}

type destroyTool struct {
	fakeTool
	destructive bool
}

func (d *destroyTool) Destructive(context.Context, json.RawMessage) bool { return d.destructive }

func TestEffectiveDestructiveUsesImplementer(t *testing.T) {
	tl := &destroyTool{fakeTool: fakeTool{name: "t"}, destructive: true}
	if !EffectiveDestructive(context.Background(), tl, json.RawMessage(`{}`)) {
		t.Error("EffectiveDestructive() = false, want true from the implementer")
	}
	tl.destructive = false
	if EffectiveDestructive(context.Background(), tl, json.RawMessage(`{}`)) {
		t.Error("EffectiveDestructive() = true, want false from the implementer")
	}
}

// TestEffectivePreferFinishDefaultsFalse mirrors the same shape for
// InterruptPreference.
func TestEffectivePreferFinishDefaultsFalse(t *testing.T) {
	tl := &fakeTool{name: "t", desc: "d"}
	if EffectivePreferFinish(tl, json.RawMessage(`{}`)) {
		t.Error("EffectivePreferFinish() = true for a non-implementer, want false")
	}
}

type interruptTool struct {
	fakeTool
	prefer bool
}

func (i *interruptTool) PreferFinish(json.RawMessage) bool { return i.prefer }

func TestEffectivePreferFinishUsesImplementer(t *testing.T) {
	tl := &interruptTool{fakeTool: fakeTool{name: "t"}, prefer: true}
	if !EffectivePreferFinish(tl, json.RawMessage(`{}`)) {
		t.Error("EffectivePreferFinish() = false, want true from the implementer")
	}
}

// TestSearchHintDefaultsEmpty and TestDeferredCarriesSearchHint cover
// searchHint's fallback and Registry.Deferred's wiring (P67.10) — the
// keyword line is an addition to, not a replacement for, Summary/Description.
func TestSearchHintDefaultsEmpty(t *testing.T) {
	if got := searchHint(&fakeTool{name: "t", desc: "d"}); got != "" {
		t.Errorf("searchHint() = %q for a non-implementer, want \"\"", got)
	}
}

type hintTool struct {
	fakeTool
	hint string
}

func (h *hintTool) SearchHint() string { return h.hint }

func TestDeferredCarriesSearchHint(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDeferred(&hintTool{fakeTool: fakeTool{name: "hinted", desc: "does a thing"}, hint: "alpha, beta"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterDeferred(&fakeTool{name: "plain", desc: "does another thing"}); err != nil {
		t.Fatal(err)
	}
	byName := map[string]Info{}
	for _, d := range r.Deferred() {
		byName[d.Name] = d
	}
	if got := byName["hinted"].Keywords; got != "alpha, beta" {
		t.Errorf("Deferred()[hinted].Keywords = %q, want %q", got, "alpha, beta")
	}
	if got := byName["plain"].Keywords; got != "" {
		t.Errorf("Deferred()[plain].Keywords = %q, want \"\" for a non-implementer", got)
	}
}

var (
	_ EquivalenceClassifier = (*equivTool)(nil)
	_ Destroyer             = (*destroyTool)(nil)
	_ InterruptPreference   = (*interruptTool)(nil)
	_ SearchHinter          = (*hintTool)(nil)
)
