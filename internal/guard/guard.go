// Package guard validates a persona's final answer before it is returned to the
// user. Two modes: a deterministic JSON schema check and an LLM rubric check.
// Guards always fail open — any internal error yields a pass so a flaky
// validator never blocks the user's answer.
package guard

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/provider"
)

// Func validates a final answer. ok=false means it failed; reason is a short
// explanation appended to the corrective retry prompt.
type Func func(ctx context.Context, text string) (ok bool, reason string)

// Config is the resolved guard configuration for a single persona/session.
type Config struct {
	Disabled   bool
	Mode       string   // "schema" | "llm"
	Schema     []string // schema mode: required top-level JSON keys
	Rubric     string   // llm mode: pass/fail rubric
	MaxRetries int
}

// SchemaGuard requires text to parse as a JSON object containing every required
// key. A leading ```json fence is tolerated.
func SchemaGuard(required []string) Func {
	return func(_ context.Context, text string) (bool, string) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stripFence(text)), &obj); err != nil {
			return false, "output is not a valid JSON object"
		}
		var missing []string
		for _, k := range required {
			if _, ok := obj[k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			return false, "missing required keys: " + strings.Join(missing, ", ")
		}
		return true, ""
	}
}

// LLMGuard asks model whether text satisfies rubric, expecting "PASS" or
// "FAIL: <reason>". Any error or unparseable reply fails open.
func LLMGuard(adapter provider.Adapter, model, rubric string) Func {
	return func(ctx context.Context, text string) (bool, string) {
		if adapter == nil || model == "" {
			return true, ""
		}
		prompt := "You are an output validator. Given the RUBRIC and the OUTPUT, reply with exactly " +
			"\"PASS\" if the output satisfies the rubric, or \"FAIL: <one-line reason>\" if it does not. " +
			"Reply with nothing else.\n\nRUBRIC:\n" + rubric + "\n\nOUTPUT:\n" + text
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		ch, err := adapter.Stream(cctx, provider.Request{
			Model:     model,
			MaxTokens: 256,
			Messages: []provider.Message{
				{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: prompt}}},
			},
		})
		if err != nil {
			return true, ""
		}
		var sb strings.Builder
		for ev := range ch {
			if ev.Type == provider.EventTextDelta {
				sb.WriteString(ev.Text)
			}
		}
		return parseVerdict(sb.String())
	}
}

// Resolve builds a concrete guard and its retry count from a Config. Returns
// (nil, 0) when guards are disabled or an llm guard lacks the model/adapter it
// needs (skipped, fail open).
func Resolve(c Config, adapter provider.Adapter, model string) (Func, int) {
	if c.Disabled {
		return nil, 0
	}
	retries := c.MaxRetries
	if retries <= 0 {
		retries = 1
	}
	switch c.Mode {
	case "schema":
		return SchemaGuard(c.Schema), retries
	case "llm":
		if adapter == nil || model == "" || strings.TrimSpace(c.Rubric) == "" {
			return nil, 0
		}
		return LLMGuard(adapter, model, c.Rubric), retries
	default:
		return nil, 0
	}
}

func parseVerdict(s string) (bool, string) {
	s = strings.TrimSpace(stripThink(s))
	upper := strings.ToUpper(s)
	if strings.HasPrefix(upper, "PASS") {
		return true, ""
	}
	if i := strings.Index(upper, "FAIL"); i >= 0 {
		reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s[i+4:]), ":"))
		if reason == "" {
			reason = "output did not satisfy the rubric"
		}
		return false, reason
	}
	return true, "" // unparseable → fail open
}

// stripFence removes a single ```lang … ``` code fence if present.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// stripThink removes <think>…</think> reasoning blocks from a validator reply.
func stripThink(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start:], "</think>")
		if end < 0 {
			return strings.TrimSpace(s[:start])
		}
		s = s[:start] + s[start+end+len("</think>"):]
	}
}
