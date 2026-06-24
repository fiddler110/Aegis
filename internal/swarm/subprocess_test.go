package swarm

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// TestMain lets this test binary double as a fake worker process when the
// SubprocessBackend re-executes it. We detect the marker env var before the test
// framework parses flags and act as the worker, then exit.
func TestMain(m *testing.M) {
	if os.Getenv("SWARM_TEST_WORKER") == "1" {
		fakeWorkerMain()
		return
	}
	os.Exit(m.Run())
}

// fakeWorkerMain simulates the headless worker: it reads the spec and records a
// result in the mailbox, varying behavior by prompt to exercise each path.
func fakeWorkerMain() {
	var specPath string
	for i, a := range os.Args {
		if a == "--spec" && i+1 < len(os.Args) {
			specPath = os.Args[i+1]
		}
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		os.Exit(3)
	}
	var spec WorkerSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		os.Exit(3)
	}

	switch spec.Config.Prompt {
	case "crash":
		// Exit without recording a result -> parent must synthesize a failure.
		os.Exit(2)
	case "fail":
		mb, _ := OpenMailbox(spec.MailboxRoot, spec.Identity)
		_ = mb.Send(Message{Type: MsgResult, Sender: spec.Identity.AgentID, Payload: map[string]any{"error": "deliberate failure"}})
		os.Exit(1)
	default:
		mb, _ := OpenMailbox(spec.MailboxRoot, spec.Identity)
		_ = mb.Send(Message{Type: MsgResult, Sender: spec.Identity.AgentID, Text: "worker handled: " + spec.Config.Prompt, Payload: map[string]any{"error": ""}})
		os.Exit(0)
	}
}

func newTestSubprocessBackend(t *testing.T) (*SubprocessBackend, *Registry) {
	t.Helper()
	t.Setenv("SWARM_TEST_WORKER", "1") // children inherit; marks them as workers
	reg := NewRegistry()
	b := NewSubprocessBackend(os.Args[0], "__worker", reg, MailboxRoot(t.TempDir()))
	return b, reg
}

func TestSubprocessSpawnSuccess(t *testing.T) {
	b, reg := newTestSubprocessBackend(t)
	h, err := b.Spawn(context.Background(), SpawnConfig{Name: "w", Prompt: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed() || res.Output != "worker handled: do it" {
		t.Errorf("result = %+v", res)
	}
	if m, ok := reg.Get(res.AgentID); !ok || m.Status != StatusDone {
		t.Errorf("registry status = %+v", m)
	}
}

func TestSubprocessSpawnReportedFailure(t *testing.T) {
	b, _ := newTestSubprocessBackend(t)
	h, _ := b.Spawn(context.Background(), SpawnConfig{Name: "w", Prompt: "fail"})
	res, _ := h.Wait(context.Background())
	if !res.Failed() || res.Err != "deliberate failure" {
		t.Errorf("expected reported failure, got %+v", res)
	}
}

func TestSubprocessSpawnCrashSynthesizesError(t *testing.T) {
	b, _ := newTestSubprocessBackend(t)
	h, _ := b.Spawn(context.Background(), SpawnConfig{Name: "w", Prompt: "crash"})
	res, _ := h.Wait(context.Background())
	if !res.Failed() {
		t.Fatalf("expected failure for a crashing worker, got %+v", res)
	}
	if res.Output != "" {
		t.Errorf("crash should yield no output, got %q", res.Output)
	}
}
