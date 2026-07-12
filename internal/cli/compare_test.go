package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
)

// TestParseCompareVote covers the accepted vote tokens (1/2/tie/skip), blank
// input treated as skip, case-insensitivity/whitespace tolerance, and the
// error path for anything else.
func TestParseCompareVote(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    compareVote
		wantErr bool
	}{
		{"one", "1", voteOne, false},
		{"two", "2", voteTwo, false},
		{"tie", "tie", voteTie, false},
		{"tie uppercase", "TIE", voteTie, false},
		{"tie padded with newline", "tie\n", voteTie, false},
		{"skip explicit", "skip", voteSkip, false},
		{"skip via blank input", "", voteSkip, false},
		{"skip via whitespace-only input", "   \n", voteSkip, false},
		{"one padded", "  1  ", voteOne, false},
		{"invalid word", "yes", voteSkip, true},
		{"invalid number", "3", voteSkip, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCompareVote(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseCompareVote(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("parseCompareVote(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestDescribeCompareVote verifies the reveal line names the correct slot's
// model for a 1/2 vote, and doesn't name any model for tie/skip.
func TestDescribeCompareVote(t *testing.T) {
	const modelA, modelB = "model-a", "model-b"

	if got := describeCompareVote(voteOne, modelA, modelB); !strings.Contains(got, modelA) {
		t.Errorf("voteOne description = %q, want it to name %q", got, modelA)
	}
	if got := describeCompareVote(voteTwo, modelA, modelB); !strings.Contains(got, modelB) {
		t.Errorf("voteTwo description = %q, want it to name %q", got, modelB)
	}
	if got := describeCompareVote(voteTie, modelA, modelB); strings.Contains(got, modelA) || strings.Contains(got, modelB) {
		t.Errorf("voteTie description = %q, should not name either model", got)
	}
	if got := describeCompareVote(voteSkip, modelA, modelB); strings.Contains(got, modelA) || strings.Contains(got, modelB) {
		t.Errorf("voteSkip description = %q, should not name either model", got)
	}
}

// TestRandomSwapProducesBothOrders is a statistical smoke test: across many
// calls, randomSwap must return both true and false — otherwise position
// would be a deterministic tell (e.g. model-A always lands in slot 1) rather
// than genuinely randomized. Failure probability for a fair coin over 200
// trials is astronomically small (~1e-59); a hardcoded constant would fail
// on the very first run instead.
func TestRandomSwapProducesBothOrders(t *testing.T) {
	var sawTrue, sawFalse bool
	for i := 0; i < 200; i++ {
		if randomSwap() {
			sawTrue = true
		} else {
			sawFalse = true
		}
		if sawTrue && sawFalse {
			return
		}
	}
	t.Fatalf("randomSwap() returned only one value (true=%v, false=%v) over 200 trials — not randomized", sawTrue, sawFalse)
}

// TestRunOneCompareNeverLogsModelIdentity is the P20.2 "identities hidden
// until vote" regression: it drives runOneCompare against a fake daemon that
// would happily let the model name leak if runOneCompare's progress logging
// passed it through, then asserts every line captured via logf mentions only
// the generic label, never the actual model id used to create/PATCH/message
// the session. This is the real mechanism (not just the random slot
// assignment) that keeps the two responses blind pre-vote.
func TestRunOneCompareNeverLogsModelIdentity(t *testing.T) {
	const secretModel = "totally-distinctive-model-id-42"

	var gotPatchModel string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(api.SessionMeta{ID: "sess-1"})
	})
	mux.HandleFunc("PATCH /sessions/sess-1", func(w http.ResponseWriter, r *http.Request) {
		var req api.UpdateSessionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != nil {
			gotPatchModel = *req.Model
		}
		json.NewEncoder(w).Encode(api.SessionMeta{ID: "sess-1", Model: gotPatchModel})
	})
	mux.HandleFunc("POST /sessions/sess-1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: tool_call\ndata: %s\n\n", mustJSONCompare(api.Event{Kind: api.KindToolCall, Tool: "read_file"}))
		flusher.Flush()
		fmt.Fprintf(w, "event: text\ndata: %s\n\n", mustJSONCompare(api.Event{Kind: api.KindText, Text: "hello from " + secretModel}))
		flusher.Flush()
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", mustJSONCompare(api.Event{Kind: api.KindDone}))
		flusher.Flush()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cl := client.New(srv.URL)

	var logMu sync.Mutex
	var logLines []string
	logf := func(format string, a ...any) {
		logMu.Lock()
		logLines = append(logLines, fmt.Sprintf(format, a...))
		logMu.Unlock()
	}

	res := runOneCompare(context.Background(), cl, "Response 1", secretModel, "build", "hi", false, logf)

	if res.err != nil {
		t.Fatalf("runOneCompare error: %v", res.err)
	}
	if gotPatchModel != secretModel {
		t.Fatalf("test setup: PATCH never received the model (got %q) — can't assert on leakage", gotPatchModel)
	}
	if res.model != secretModel {
		t.Errorf("res.model = %q, want %q (caller-side bookkeeping should still know it)", res.model, secretModel)
	}

	for _, line := range logLines {
		if strings.Contains(line, secretModel) {
			t.Errorf("progress log leaked the model identity before reveal: %q", line)
		}
		if !strings.Contains(line, "Response 1") {
			t.Errorf("progress log line missing the generic label: %q", line)
		}
	}
	if len(logLines) == 0 {
		t.Fatal("test setup: expected at least one progress log line")
	}
}

func mustJSONCompare(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestNewCompareCmdArgsValidation checks the cobra arg validation (at least
// two model arguments; the prompt itself is optional here since it can come
// from stdin) without executing RunE, which would need a live daemon.
func TestNewCompareCmdArgsValidation(t *testing.T) {
	cmd := newCompareCmd()
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("expected an error for zero arguments")
	}
	if err := cmd.Args(cmd, []string{"model-a"}); err == nil {
		t.Error("expected an error for a single (model-only) argument")
	}
	if err := cmd.Args(cmd, []string{"model-a", "model-b"}); err != nil {
		t.Errorf("unexpected error for two model arguments (prompt via stdin): %v", err)
	}
	if err := cmd.Args(cmd, []string{"model-a", "model-b", "explain X"}); err != nil {
		t.Errorf("unexpected error for two models plus a prompt: %v", err)
	}
}

// TestNewCompareCmdFlags verifies the command wires up the flags the P20.2
// spec calls for, with the documented defaults (--synthesize off by default,
// --mode/--synth-model empty so they fall back to config).
func TestNewCompareCmdFlags(t *testing.T) {
	cmd := newCompareCmd()

	modeFlag := cmd.Flags().Lookup("mode")
	if modeFlag == nil || modeFlag.DefValue != "" {
		t.Errorf("--mode flag = %+v, want present with empty default", modeFlag)
	}
	yesFlag := cmd.Flags().Lookup("yes")
	if yesFlag == nil || yesFlag.DefValue != "false" {
		t.Errorf("--yes flag = %+v, want present, default false", yesFlag)
	}
	synthFlag := cmd.Flags().Lookup("synthesize")
	if synthFlag == nil || synthFlag.DefValue != "false" {
		t.Errorf("--synthesize flag = %+v, want present, default false (off by default per spec)", synthFlag)
	}
	synthModelFlag := cmd.Flags().Lookup("synth-model")
	if synthModelFlag == nil || synthModelFlag.DefValue != "" {
		t.Errorf("--synth-model flag = %+v, want present with empty default", synthModelFlag)
	}

	if cmd.Use != "compare <model-A> <model-B> [prompt]" {
		t.Errorf("cmd.Use = %q", cmd.Use)
	}
}
