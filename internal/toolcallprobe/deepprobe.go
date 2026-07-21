package toolcallprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// deepFillMarker is the same stub-first placeholder multi-phase skills use
// (see internal/cli/chat.go's scanPendingMarkers) — reused verbatim rather
// than inventing a second convention.
const deepFillMarker = "<!-- PENDING -->"

// deepFillSystem/deepFillPrompt bias the model toward actually executing a
// small multi-section fill task, one marker at a time, rather than
// describing or narrating it — mirroring the real threat-modeling skill's
// stub-then-fill shape at a fraction of the size.
const deepFillSystem = "You are a coding agent completing a document with several sections. Use the `edit_fill` tool to replace each PENDING marker with a short real sentence, one marker per call. Never claim the document is finished while any PENDING marker remains."

const deepFillDocument = "## Section Alpha\n<!-- PENDING -->\n\n## Section Beta\n<!-- PENDING -->\n\n## Section Gamma\n<!-- PENDING -->\n"

const deepFillPrompt = "The document below has 3 sections, each still marked `<!-- PENDING -->`. Fill each section's marker with a short real sentence relevant to its heading, one `edit_fill` call per section — never target more than one section's marker in a single call. Keep going until none remain.\n\n" + deepFillDocument

// deepFillContinuePrompt mirrors chat.go's continuePrompt (P38.2): a
// non-interactive nudge naming the fact that work remains, not a new task.
const deepFillContinuePrompt = "Continue — the document still has PENDING marker(s) remaining. Keep calling edit_fill, targeting one marker at a time, until none remain. This is a non-interactive check: do not stop to ask whether to proceed, and do not claim completion until every marker is gone."

// deepFillMaxTurns bounds the whole probe (shape 3: non-convergence) — small
// on purpose, this is a smoke-sized check, not a full skill run.
const deepFillMaxTurns = 6

// deepFillMaxTokens caps each turn generously, matching SmokeMaxTokens's
// reasoning: a reasoning model's preamble must not be mistaken for silence.
const deepFillMaxTokens = 2048

// DeepResult reports the three multi-turn failure shapes the P38.1 live
// threat-modeling tests found in a 14B/24B-class local model, each
// independently — per the roadmap's framing, this is a genuinely different
// capability claim than the single-turn Verdict above ("can it call a tool
// at all"), so it is never folded into that binary verdict.
type DeepResult struct {
	// FabricatedCompletion is true when the model's final turn claimed the
	// document was finished while PENDING markers still remained and no
	// tool call was made that turn to justify the claim (P38.6's shape).
	FabricatedCompletion bool
	// ClobberedMarkers is true when a fill call used replace_all against a
	// document still carrying more than one identical PENDING marker,
	// overwriting every remaining section instead of targeting one
	// (P38.7's shape).
	ClobberedMarkers bool
	// TimedOut is true when PENDING markers still remained after
	// deepFillMaxTurns turns.
	TimedOut bool
}

// Clean reports whether none of the three failure shapes were observed.
func (r DeepResult) Clean() bool {
	return !r.FabricatedCompletion && !r.ClobberedMarkers && !r.TimedOut
}

// fillFixture is the tiny in-memory document RunDeepFill drives a model to
// complete. It is never written to disk — self-contained, matching
// SmokeTool's no-fixture-repo style.
type fillFixture struct {
	mu        sync.Mutex
	content   string
	clobbered bool
}

func newFillFixture() *fillFixture {
	return &fillFixture{content: deepFillDocument}
}

func (f *fillFixture) pendingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Count(f.content, deepFillMarker)
}

func (f *fillFixture) wasClobbered() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clobbered
}

// fillTool is a fake edit-shaped tool backed by fillFixture instead of the
// real filesystem, deliberately mirroring edit_file's real semantics
// (internal/tool/builtin/file.go): old_string must occur exactly once unless
// replace_all is set. This reproduces the P38.7 footgun faithfully — a
// blanket replace_all against several identical markers overwrites all of
// them, exactly as it did against the real scaffold.py output.
type fillTool struct{ fx *fillFixture }

func (t *fillTool) Name() string { return "edit_fill" }
func (t *fillTool) Description() string {
	return "Replace an exact string in the document. old_string must occur exactly once unless replace_all is true."
}
func (t *fillTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"old_string":{"type":"string"},"new_string":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["old_string","new_string"]}`)
}
func (t *fillTool) Capability() tool.Capability { return tool.CapWrite }
func (t *fillTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(input, &args); err != nil || args.OldString == "" {
		return tool.Result{Content: "old_string is required", IsError: true}, nil
	}

	t.fx.mu.Lock()
	defer t.fx.mu.Unlock()

	n := strings.Count(t.fx.content, args.OldString)
	if n == 0 {
		return tool.Result{Content: "old_string not found in document", IsError: true}, nil
	}
	if n > 1 && !args.ReplaceAll {
		return tool.Result{Content: fmt.Sprintf("old_string occurs %d times; pass replace_all or provide a more specific string", n), IsError: true}, nil
	}
	if n > 1 && args.ReplaceAll && args.OldString == deepFillMarker {
		// Blanket-replacing every remaining identical marker with one answer
		// instead of targeting a single section — the P38.7 footgun.
		t.fx.clobbered = true
	}
	if args.ReplaceAll {
		t.fx.content = strings.ReplaceAll(t.fx.content, args.OldString, args.NewString)
	} else {
		t.fx.content = strings.Replace(t.fx.content, args.OldString, args.NewString, 1)
	}
	return tool.Result{Content: "edited"}, nil
}

// RunDeepFill drives model through the tiny synthetic scaffold-and-fill task
// above (P39.4) and reports which of the three P38.1-observed failure shapes
// were reproduced. Unlike Run, this is a real multi-turn agentic loop (via
// internal/engine, the same tool-calling loop a real session uses, not a
// second hand-rolled one) rather than a single request/response, since two of
// the three shapes are only visible across several turns.
func RunDeepFill(ctx context.Context, adapter provider.Adapter, model string) (DeepResult, error) {
	fx := newFillFixture()
	reg := tool.NewRegistry()
	if err := reg.Register(&fillTool{fx: fx}); err != nil {
		return DeepResult{}, err
	}

	eng, err := engine.New(engine.Options{
		Adapter:   adapter,
		Tools:     reg,
		Model:     model,
		MaxTokens: deepFillMaxTokens,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return DeepResult{}, err
	}

	conv := &engine.Conversation{System: deepFillSystem}
	conv.Messages = append(conv.Messages, provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: deepFillPrompt}},
	})

	var res DeepResult
	for turn := 0; turn < deepFillMaxTurns; turn++ {
		var toolCallsThisTurn int
		var finalText strings.Builder
		if err := eng.Run(ctx, conv, func(ev engine.Event) {
			switch ev.Kind {
			case engine.KindToolResult:
				toolCallsThisTurn++
			case engine.KindText:
				finalText.WriteString(ev.Text)
			}
		}); err != nil {
			return DeepResult{}, err
		}
		if fx.wasClobbered() {
			res.ClobberedMarkers = true
		}
		if fx.pendingCount() == 0 {
			return res, nil
		}
		if toolCallsThisTurn == 0 && looksLikeCompletionClaim(finalText.String()) {
			res.FabricatedCompletion = true
			return res, nil
		}
		conv.Messages = append(conv.Messages, provider.Message{
			Role:    provider.RoleUser,
			Content: []provider.Block{provider.TextBlock{Text: deepFillContinuePrompt}},
		})
	}
	if fx.pendingCount() > 0 {
		res.TimedOut = true
	}
	return res, nil
}

// looksLikeCompletionClaim is a small heuristic over a model's final-turn
// text for language claiming the fill task is done. Best-effort by design —
// this probe only needs to catch the shape observed live (a model narrating
// a completed build in its reasoning/final answer without having executed
// it), not classify arbitrary prose.
func looksLikeCompletionClaim(text string) bool {
	t := strings.ToLower(text)
	for _, phrase := range []string{
		"all sections", "document is complete", "is now complete", "all pending",
		"successfully filled", "completed the document", "finished filling",
		"no pending markers", "task is complete", "all done",
	} {
		if strings.Contains(t, phrase) {
			return true
		}
	}
	return false
}
