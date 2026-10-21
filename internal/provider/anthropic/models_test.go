package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

func TestParseModelID(t *testing.T) {
	cases := []struct {
		id           string
		family       string
		major, minor int
		ok           bool
	}{
		{"claude-opus-5-5", "opus", 5, 5, true},
		{"claude-sonnet-5", "sonnet", 5, 0, true},
		{"claude-haiku-4-5-20251001", "haiku", 4, 5, true},
		{"claude-3-5-sonnet-20241022", "sonnet", 3, 5, true},
		{"anthropic/claude-opus-4-8", "opus", 4, 8, true},
		{"anthropic.claude-sonnet-5-5", "sonnet", 5, 5, true},
		{"claude-opus-4-5@20251101", "opus", 4, 5, true},
		{"claude-fable-5-1", "fable", 5, 1, true},
		{"my-gateway-alias", "", 0, 0, false},
		{"gpt-4o", "", 0, 0, false},
	}
	for _, c := range cases {
		f, ma, mi, ok := parseModelID(c.id)
		if ok != c.ok || f != c.family || ma != c.major || mi != c.minor {
			t.Errorf("parseModelID(%q) = %q %d.%d %v; want %q %d.%d %v", c.id, f, ma, mi, ok, c.family, c.major, c.minor, c.ok)
		}
	}
}

func TestClampEffort(t *testing.T) {
	cases := []struct{ model, want, got string }{
		{"claude-opus-5-5", "xhigh", "xhigh"},
		{"claude-opus-4-6", "xhigh", "high"}, // 4.6 has no xhigh
		{"claude-opus-4-5", "max", "high"},   // 4.5 stops at high
		{"claude-haiku-4-5", "high", ""},     // no effort parameter at all
		{"claude-opus-5-5", "bogus", ""},
		{"claude-opus-5-5", "", ""},
	}
	for _, c := range cases {
		if got := clampEffort(traitsFor(c.model), c.want); got != c.got {
			t.Errorf("clampEffort(%s, %q) = %q; want %q", c.model, c.want, got, c.got)
		}
	}
}

// thinkingOf returns the request's thinking object, or nil when absent.
func thinkingOf(body map[string]any) map[string]any {
	th, _ := body["thinking"].(map[string]any)
	return th
}

func effortOf(body map[string]any) string {
	oc, _ := body["output_config"].(map[string]any)
	e, _ := oc["effort"].(string)
	return e
}

// TestCurrentModelsNeverGetBudgetTokens pins the bug this file exists for:
// `think: true` used to send {type:"enabled", budget_tokens} to every model,
// which every model from Opus 4.7 on rejects with a 400.
func TestCurrentModelsNeverGetBudgetTokens(t *testing.T) {
	for _, m := range []string{"claude-opus-5-5", "claude-opus-4-8", "claude-sonnet-5-5", "claude-sonnet-4-6", "claude-haiku-5-5", "claude-fable-5-1"} {
		temp := 0.5
		body := captureBodyReq(t, provider.Request{Model: m, MaxTokens: 8192, Temperature: &temp}, WithThinking(4096))
		th := thinkingOf(body)
		if th == nil || th["type"] != "adaptive" {
			t.Errorf("%s: thinking = %v; want adaptive", m, th)
		}
		if _, has := th["budget_tokens"]; has {
			t.Errorf("%s: budget_tokens sent: %v", m, th)
		}
		if _, has := body["temperature"]; has {
			t.Errorf("%s: temperature sent alongside thinking", m)
		}
	}
}

func TestAdaptiveThinkingAsksForSummaryWhereDisplayDefaultsToOmitted(t *testing.T) {
	th := thinkingOf(captureBodyReq(t, provider.Request{Model: "claude-opus-4-8", MaxTokens: 8192}, WithThinking(4096)))
	if th["display"] != "summarized" {
		t.Errorf("opus 4.8 thinking = %v; want display summarized", th)
	}
	th = thinkingOf(captureBodyReq(t, provider.Request{Model: "claude-opus-4-6", MaxTokens: 8192}, WithThinking(4096)))
	if _, has := th["display"]; has {
		t.Errorf("opus 4.6 summarizes by default; display should be omitted: %v", th)
	}
}

func TestLegacyModelsKeepBudgetThinking(t *testing.T) {
	for _, m := range []string{"claude-haiku-4-5", "unknown-alias"} {
		th := thinkingOf(captureBodyReq(t, provider.Request{Model: m, MaxTokens: 8192}, WithThinking(2048)))
		if th["type"] != "enabled" || th["budget_tokens"].(float64) != 2048 {
			t.Errorf("%s: thinking = %v; want enabled/2048", m, th)
		}
	}
}

func TestTemperatureDroppedWhereSamplingIsRejected(t *testing.T) {
	temp := 0.3
	if _, has := captureBodyReq(t, provider.Request{Model: "claude-opus-4-8", MaxTokens: 100, Temperature: &temp})["temperature"]; has {
		t.Error("opus 4.8 rejects temperature; it must not be sent")
	}
	if _, has := captureBodyReq(t, provider.Request{Model: "claude-sonnet-4-6", MaxTokens: 100, Temperature: &temp})["temperature"]; !has {
		t.Error("sonnet 4.6 accepts temperature; it should be sent")
	}
}

// TestGuardCallsSuppressThinkingPerModel: the guard's call must not spend its
// small budget thinking. Each model gets the off switch it has, and a model
// with none gets effort "low".
func TestGuardCallsSuppressThinkingPerModel(t *testing.T) {
	cases := []struct {
		model, thinkType, effort string
	}{
		{"claude-opus-5-5", "", "low"},                // no off switch
		{"claude-fable-5-1", "", "low"},               // no off switch
		{"claude-sonnet-5-5", "between_tools", "low"}, // its own off switch
		{"claude-opus-5", "disabled", "low"},
		{"claude-haiku-5-5", "disabled", "low"},
		{"claude-opus-4-8", "disabled", "low"},
		{"claude-haiku-4-5", "", ""}, // legacy: simply no thinking, no effort
	}
	for _, c := range cases {
		body := captureBodyReq(t, provider.Request{Model: c.model, MaxTokens: 1024, Purpose: provider.PurposeGuard},
			WithThinking(4096), WithEffort("xhigh"))
		th := thinkingOf(body)
		got := ""
		if th != nil {
			got, _ = th["type"].(string)
		}
		if got != c.thinkType {
			t.Errorf("%s: thinking = %v; want type %q", c.model, th, c.thinkType)
		}
		if e := effortOf(body); e != c.effort {
			t.Errorf("%s: effort = %q; want %q", c.model, e, c.effort)
		}
	}
}

func TestSuppressThinkingFieldAlsoHonored(t *testing.T) {
	body := captureBodyReq(t, provider.Request{Model: "claude-opus-4-8", MaxTokens: 1024, SuppressThinking: true}, WithThinking(4096))
	if th := thinkingOf(body); th == nil || th["type"] != "disabled" {
		t.Errorf("thinking = %v; want disabled", th)
	}
}

func TestExplicitThinkFalse(t *testing.T) {
	// Opus 5 thinks by default; think:false must say so.
	if th := thinkingOf(captureBodyReq(t, provider.Request{Model: "claude-opus-5", MaxTokens: 100}, WithThinkingDisabled())); th == nil || th["type"] != "disabled" {
		t.Errorf("opus 5: thinking = %v; want disabled", th)
	}
	// ...but not at xhigh, where the API refuses disabled.
	if th := thinkingOf(captureBodyReq(t, provider.Request{Model: "claude-opus-5", MaxTokens: 100}, WithThinkingDisabled(), WithEffort("xhigh"))); th != nil {
		t.Errorf("opus 5 at xhigh: thinking = %v; want omitted", th)
	}
	// Opus 5.5 has no off switch at all.
	if th := thinkingOf(captureBodyReq(t, provider.Request{Model: "claude-opus-5-5", MaxTokens: 100}, WithThinkingDisabled())); th != nil {
		t.Errorf("opus 5.5: thinking = %v; want omitted", th)
	}
	// Unset think leaves the model default: nothing sent.
	if th := thinkingOf(captureBodyReq(t, provider.Request{Model: "claude-opus-5", MaxTokens: 100})); th != nil {
		t.Errorf("unset think: thinking = %v; want omitted", th)
	}
}

func TestEffortSentAndClamped(t *testing.T) {
	if e := effortOf(captureBodyReq(t, provider.Request{Model: "claude-opus-4-6", MaxTokens: 100}, WithEffort("xhigh"))); e != "high" {
		t.Errorf("opus 4.6 effort = %q; want high", e)
	}
	if _, has := captureBodyReq(t, provider.Request{Model: "claude-haiku-4-5", MaxTokens: 100}, WithEffort("high"))["output_config"]; has {
		t.Error("haiku 4.5 takes no effort; output_config must be omitted")
	}
}

func TestEmptyThinkingTextIsStillReplayed(t *testing.T) {
	body := captureBodyReq(t, provider.Request{
		Model: "claude-opus-5-5", MaxTokens: 100,
		Messages: []provider.Message{{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ThinkingBlock{Text: "", Signature: "SIG"},
		}}},
	})
	block := body["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if v, has := block["thinking"]; !has || v != "" {
		t.Errorf("thinking block = %v; want an explicit empty thinking field", block)
	}
}

func TestCacheTTLOneHour(t *testing.T) {
	body := captureBodyReq(t, provider.Request{Model: "claude-opus-5-5", MaxTokens: 100, System: "sys"}, WithCacheTTL("1h"))
	cc := body["system"].([]any)[0].(map[string]any)["cache_control"].(map[string]any)
	if cc["ttl"] != "1h" {
		t.Errorf("cache_control = %v; want ttl 1h", cc)
	}
	body = captureBodyReq(t, provider.Request{Model: "claude-opus-5-5", MaxTokens: 100, System: "sys"})
	cc = body["system"].([]any)[0].(map[string]any)["cache_control"].(map[string]any)
	if _, has := cc["ttl"]; has {
		t.Errorf("default cache_control = %v; want no ttl", cc)
	}
}

func TestRefusalIsATerminalError(t *testing.T) {
	const refusal = `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"declined"}},"usage":{"output_tokens":3}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(refusal))
	}))
	defer srv.Close()
	stream, err := New("k", WithBaseURL(srv.URL)).Stream(context.Background(), provider.Request{Model: "claude-opus-5-5", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	var got error
	for ev := range stream {
		if ev.Type == provider.EventError {
			got = ev.Err
		}
	}
	var apiErr *provider.APIError
	if !errors.As(got, &apiErr) {
		t.Fatalf("want *provider.APIError, got %v", got)
	}
	if apiErr.Retryable() || provider.IsBackendUnavailableError(got) || provider.IsContextOverflowError(got) {
		t.Errorf("refusal must be terminal and unclassified: %v", got)
	}
	if !strings.Contains(got.Error(), "refusal") || !strings.Contains(got.Error(), "cyber") {
		t.Errorf("error text = %q", got.Error())
	}
}

func TestContextWindowFor(t *testing.T) {
	if w := ContextWindowFor("claude-opus-5-5"); w != 1_000_000 {
		t.Errorf("opus 5.5 window = %d", w)
	}
	if w := ContextWindowFor("claude-haiku-4-5"); w != 200_000 {
		t.Errorf("haiku 4.5 window = %d", w)
	}
	if w := ContextWindowFor("gpt-4o"); w != 0 {
		t.Errorf("non-claude window = %d; want 0", w)
	}
}
