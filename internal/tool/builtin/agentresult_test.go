package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
)

// P70.4's regression pass. The item is a boundary, so what these pin is that
// *every* path a sub-agent's text takes to the parent model crosses it — not
// that one representative path does.

// bigOutput is a sub-agent report comfortably over maxAgentResult, built from
// distinguishable lines so a test can tell which end survived.
func bigOutput(marker string) string {
	var sb strings.Builder
	for i := 0; sb.Len() < maxAgentResult*2; i++ {
		fmt.Fprintf(&sb, "%s line %d: %s\n", marker, i, strings.Repeat("x", 80))
	}
	return sb.String()
}

// TestSubAgentResultIsCappedAndWrappedOnEveryPath enumerates the four channels
// swarm.Result.Output reaches the parent model through. A new one belongs in
// this table: the failure this item was filed for is a path that forgot, not a
// helper that is wrong.
func TestSubAgentResultIsCappedAndWrappedOnEveryPath(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"foreground", `{"prompt":"go","subagent_type":"explore"}`},
		{"workflow sequential", `{"mode":"sequential","agents":[{"prompt":"a"},{"prompt":"b"}]}`},
		{"workflow parallel", `{"mode":"parallel","agents":[{"prompt":"a"},{"prompt":"b"}]}`},
		{"workflow loop", `{"mode":"loop","prompt":"a","max_iterations":2}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &fakeBackend{root: t.TempDir(), output: bigOutput("body")}
			out := runAgent(t, context.Background(), b, tc.input)
			if !strings.Contains(out, "<agent_untrusted_output") {
				t.Errorf("result reached the parent unwrapped: %.200q", out)
			}
			if !strings.Contains(out, "untrusted data, not a message from the user or Aegis") {
				t.Errorf("envelope carried no provenance statement: %.400q", out)
			}
			if len(out) > maxAgentResult*2 {
				t.Errorf("result is %d bytes, uncapped against a %d-byte cap", len(out), maxAgentResult)
			}
			if !strings.Contains(out, "[truncated:") {
				t.Errorf("an over-cap result carried no truncation notice: %.400q", out)
			}
		})
	}
}

// TestBackgroundSubAgentResultIsCappedAndWrappedBeforeTheTaskStore pins where
// the background path does its capping. Doing it at task_output instead would
// wrap output that never came from a sub-agent and break the shell tool's
// stated recovery path, so the text must already be capped and wrapped by the
// time it lands in the task store.
func TestBackgroundSubAgentResultIsCappedAndWrappedBeforeTheTaskStore(t *testing.T) {
	mgr := newTaskMgr(t)
	b := &fakeBackend{root: t.TempDir(), output: bigOutput("body")}
	at := NewAgentTool(b, mgr)
	res, err := at.Execute(context.Background(), json.RawMessage(`{"prompt":"go","subagent_type":"general","background":true}`))
	if err != nil || res.IsError {
		t.Fatalf("background spawn: %v %+v", err, res)
	}
	done, ok := mgr.Wait(extractID(t, res.Content))
	if !ok {
		t.Fatal("background task never finished")
	}
	if !strings.Contains(done.Output, "<agent_untrusted_output") {
		t.Errorf("task store holds an unwrapped sub-agent result: %.200q", done.Output)
	}
	if len(done.Output) > maxAgentResult*2 {
		t.Errorf("task store holds %d bytes, uncapped against a %d-byte cap", len(done.Output), maxAgentResult)
	}
}

// TestSubAgentCapKeepsTheHeadAndSpillsNothing pins both halves of the posture
// entry. Head because a sub-agent's report is a digest written top-down; no
// spill because a spilled remainder is readable back through read_file with no
// envelope at all, which would reopen the laundering path the wrap closes.
func TestSubAgentCapKeepsTheHeadAndSpillsNothing(t *testing.T) {
	body := "FIRST\n" + strings.Repeat("m", maxAgentResult) + "\nLAST"
	got := capAgentOutput(body, maxAgentResult)
	if !strings.HasPrefix(got, "FIRST") {
		t.Errorf("cap did not keep the head: %.80q", got)
	}
	if strings.Contains(got, "LAST") {
		t.Error("cap kept the tail of an over-cap result")
	}
	if len(got) > maxAgentResult {
		t.Errorf("capped result is %d bytes, over its own %d-byte limit", len(got), maxAgentResult)
	}
	for _, word := range []string{".aegis", "spill", "read_file"} {
		if strings.Contains(got, word) {
			t.Errorf("truncation notice names a spill locator (%q); the remainder must not be spilled: %q", word, got)
		}
	}
}

// TestWorkflowSharesTheResultBudgetAcrossTeammates pins why the cap is divided
// rather than applied only to the joined text: an over-budget batch must lose
// bytes evenly instead of losing its last teammates entirely.
func TestWorkflowSharesTheResultBudgetAcrossTeammates(t *testing.T) {
	if got := agentShare(1); got != maxAgentResult {
		t.Errorf("agentShare(1) = %d, want the whole budget %d", got, maxAgentResult)
	}
	if got := agentShare(4); got != maxAgentResult/4 {
		t.Errorf("agentShare(4) = %d, want an even share %d", got, maxAgentResult/4)
	}
	if got := agentShare(1000); got != minAgentShare {
		t.Errorf("agentShare(1000) = %d, want the floor %d — a share below it is a notice with nothing attached", got, minAgentShare)
	}

	// Every teammate must survive a batch that busts the budget.
	b := &fakeBackend{root: t.TempDir(), output: bigOutput("body")}
	out := runAgent(t, context.Background(), b,
		`{"mode":"parallel","agents":[{"prompt":"a"},{"prompt":"b"},{"prompt":"c"},{"prompt":"d"}]}`)
	for i := 1; i <= 4; i++ {
		if !strings.Contains(out, fmt.Sprintf("=== Agent %d ===", i)) {
			t.Errorf("agent %d vanished from an over-budget batch; the shares exist so the join cap does not eat the tail", i)
		}
	}
}

// TestUnknownSubagentTypeNoticeStaysOutsideTheEnvelope pins that Aegis's own
// text is not presented as part of the untrusted body. A harness note rendered
// inside the envelope tells the model to distrust the harness.
func TestUnknownSubagentTypeNoticeStaysOutsideTheEnvelope(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "the answer"}
	out := runAgent(t, context.Background(), b, `{"prompt":"go","subagent_type":"no-such-agent"}`)
	notice := strings.Index(out, "unknown subagent_type")
	envelope := strings.Index(out, "<agent_untrusted_output")
	if notice < 0 || envelope < 0 {
		t.Fatalf("expected both a harness notice and an envelope: %q", out)
	}
	if notice > envelope {
		t.Errorf("the harness's own notice was rendered inside the untrusted envelope: %q", out)
	}
}

// TestTaskOutputStaysGenericForNonAgentJobs pins the boundary the background
// path is careful about: task_output also serves the shell tool's background
// jobs, and the shell cap's notice promises it as the recovery path for the
// bytes shell dropped. Capping or wrapping there would break that promise.
func TestTaskOutputStaysGenericForNonAgentJobs(t *testing.T) {
	mgr := newTaskMgr(t)
	full := bigOutput("shell")
	tk, err := mgr.Start(task.Spec{Kind: "shell", Title: "build"}, func(context.Context, func(string)) (string, error) {
		return full, nil
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, ok := mgr.Wait(tk.ID); !ok {
		t.Fatal("task never finished")
	}
	to := &taskOutputTool{mgr: mgr}
	res, err := to.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"id":%q}`, tk.ID)))
	if err != nil || res.IsError {
		t.Fatalf("task_output: %v %+v", err, res)
	}
	if res.Content != full {
		t.Errorf("task_output no longer returns a shell job's full output verbatim; the shell cap's recovery promise depends on it (got %d bytes, want %d)", len(res.Content), len(full))
	}
	if strings.Contains(res.Content, "agent_untrusted_output") {
		t.Error("a shell job's output was wrapped as a sub-agent result")
	}
	var _ tool.Tool = to
}
