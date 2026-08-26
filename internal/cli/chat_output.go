package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
)

// emitChatSummary writes the run trailer in whichever output format was asked
// for.
func emitChatSummary(out, errOut io.Writer, format outputFormatKind, snap cost.Snapshot, answer string, toolCalls int, runErr error) {
	switch format {
	case outputJSON:
		emitFinalJSON(out, chatResult{
			Answer:       strings.TrimSpace(answer),
			CostUSD:      snap.TotalUSD,
			Turns:        snap.Turns,
			InputTokens:  snap.Usage.InputTokens,
			OutputTokens: snap.Usage.OutputTokens,
			ToolCalls:    toolCalls,
			Error:        errString(runErr),
		})
	case outputStreamJSON:
		// Final summary line so consumers can read cost without tracking usage.
		emitFinalJSON(out, chatResult{
			Type: "result", CostUSD: snap.TotalUSD, Turns: snap.Turns,
			InputTokens: snap.Usage.InputTokens, OutputTokens: snap.Usage.OutputTokens,
			ToolCalls: toolCalls, Error: errString(runErr),
		})
	default:
		// P35.13: the "in" figure is the summed per-turn input-token
		// count — total input *processed*, which is the billable-input
		// basis a cloud provider charges (each turn re-sends the whole
		// conversation; cache reads are priced separately in cost). This
		// trailer is gated on TotalUSD > 0, so it only prints for a
		// priced/cloud run, where that is exactly the number to show;
		// local runs (cost $0, and where the count is Ollama's full
		// per-turn context size, not prefill work — P35.13) never reach
		// this branch.
		if snap.TotalUSD > 0 {
			fmt.Fprintf(errOut, "\n[cost: $%.4f over %d turn(s), %d in / %d out tokens]\n",
				snap.TotalUSD, snap.Turns, snap.Usage.InputTokens, snap.Usage.OutputTokens)
		}
	}
}

type outputFormatKind int

const (
	outputText outputFormatKind = iota
	outputJSON
	outputStreamJSON
)

func parseOutputFormat(s string) (outputFormatKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text":
		return outputText, nil
	case "json":
		return outputJSON, nil
	case "stream-json", "stream_json", "streamjson":
		return outputStreamJSON, nil
	default:
		return outputText, fmt.Errorf("invalid --output-format %q (want text, json, or stream-json)", s)
	}
}

// chatResult is the machine-readable summary emitted in json / stream-json mode.
type chatResult struct {
	Type    string  `json:"type,omitempty"` // "result" in stream-json trailer
	Answer  string  `json:"answer,omitempty"`
	CostUSD float64 `json:"cost_usd"`
	Turns   int     `json:"turns"`
	// InputTokens is the sum of the per-turn prompt-token counts across every
	// turn of the run — i.e. total input tokens *processed*, which is the
	// billable-input basis for a cloud provider (each agentic turn re-sends the
	// growing conversation and is charged for it; prompt-cache reads are billed
	// separately and priced at the discounted rate — see internal/cost). It is
	// deliberately NOT a de-duplicated or cache-adjusted figure. On the
	// native-Ollama path this is prompt_eval_count, the full context size every
	// turn (P35.13), so the sum overstates the *local prefill work actually
	// done* by the KV-cache-hit factor — but local cost is $0, so the number is
	// informational there; it is the cloud cost figure it must be accurate for.
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	ToolCalls    int    `json:"tool_calls"`
	Error        string `json:"error,omitempty"`
}

// streamEvent is one line of stream-json output.
type streamEvent struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ToolResult string          `json:"tool_result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Error      string          `json:"error,omitempty"`
	// Usage fields, populated on a "turn_done" line (P38.3) so per-turn context
	// growth is observable from a stream-json consumer without SQLite
	// spelunking or debug-log tailing — previously this Kind fell through the
	// switch below with none of its Usage/CostUSD payload serialized at all.
	InputTokens          int     `json:"input_tokens,omitempty"`
	OutputTokens         int     `json:"output_tokens,omitempty"`
	CacheReadTokens      int     `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens  int     `json:"cache_creation_tokens,omitempty"`
	PromptEvalDurationMS int64   `json:"prompt_eval_duration_ms,omitempty"`
	CostUSD              float64 `json:"cost_usd,omitempty"`
}

func emitStreamEvent(w io.Writer, ev engine.Event) {
	se := streamEvent{Type: string(ev.Kind)}
	switch ev.Kind {
	case engine.KindText, engine.KindThinking, engine.KindNotice:
		// A notice carries its whole payload in Text — omitting it here shipped
		// bare `{"type":"notice"}` lines that told a stream-json consumer
		// nothing at all.
		se.Text = ev.Text
	case engine.KindToolCall:
		se.Tool, se.ToolInput = ev.ToolName, ev.ToolInput
	case engine.KindToolResult:
		se.Tool, se.ToolResult, se.IsError = ev.ToolName, ev.ToolResult, ev.ToolIsError
	case engine.KindError:
		se.Error = errString(ev.Err)
	case engine.KindTurnDone:
		if ev.Usage != nil {
			se.InputTokens = ev.Usage.InputTokens
			se.OutputTokens = ev.Usage.OutputTokens
			se.CacheReadTokens = ev.Usage.CacheReadTokens
			se.CacheCreationTokens = ev.Usage.CacheCreationTokens
			se.PromptEvalDurationMS = ev.Usage.PromptEvalDurationMS
		}
		se.CostUSD = ev.CostUSD
	case engine.KindTrace:
		return // server-internal; never emit
	}
	line, err := json.Marshal(se)
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(line))
}

func emitFinalJSON(w io.Writer, res chatResult) {
	line, err := json.Marshal(res)
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(line))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
