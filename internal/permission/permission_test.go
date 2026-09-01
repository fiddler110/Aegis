package permission

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

type fakeTool struct {
	name   string
	cap    tool.Capability
	schema json.RawMessage // optional; defaults to "{}" (no properties) when unset
}

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "" }
func (f fakeTool) InputSchema() json.RawMessage {
	if len(f.schema) == 0 {
		return json.RawMessage(`{}`)
	}
	return f.schema
}
func (f fakeTool) Capability() tool.Capability { return f.cap }
func (f fakeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}

// fakeOverrideTool implements tool.CapabilityOverrider on top of fakeTool,
// for exercising the P25.4c EffectiveCapability seam without a real shell
// tool: overrideCap is returned for every call regardless of input, while
// Capability() (embedded from fakeTool) keeps reporting the static cap.
type fakeOverrideTool struct {
	fakeTool
	overrideCap tool.Capability
}

func (f fakeOverrideTool) CapabilityFor(context.Context, json.RawMessage) tool.Capability {
	return f.overrideCap
}

func TestPolicyDecide(t *testing.T) {
	tests := []struct {
		mode Mode
		cap  tool.Capability
		want Decision
	}{
		{ModePlan, tool.CapRead, Allow},
		{ModePlan, tool.CapNetwork, Ask},
		{ModePlan, tool.CapWrite, Deny},
		{ModePlan, tool.CapExecute, Deny},
		{ModeBuild, tool.CapRead, Allow},
		{ModeBuild, tool.CapWrite, Allow},
		{ModeBuild, tool.CapNetwork, Allow},
		{ModeBuild, tool.CapExecute, Ask},
	}
	for _, tt := range tests {
		got := Policy{Mode: tt.mode}.Decide(tt.cap)
		if got != tt.want {
			t.Errorf("Decide(%s,%s) = %s, want %s", tt.mode, tt.cap, got, tt.want)
		}
	}
}

func TestGateCheck(t *testing.T) {
	ctx := context.Background()
	write := fakeTool{name: "write_file", cap: tool.CapWrite}
	exec := fakeTool{name: "shell", cap: tool.CapExecute}

	// Plan mode blocks writes.
	if ok, _ := New(ModePlan, nil).Check(ctx, write, nil); ok {
		t.Error("plan mode should block write")
	}
	// Build mode allows writes.
	if ok, _ := New(ModeBuild, nil).Check(ctx, write, nil); !ok {
		t.Error("build mode should allow write")
	}
	// Build mode asks for execute; AutoDeny -> blocked.
	if ok, _ := New(ModeBuild, AutoDeny{}).Check(ctx, exec, nil); ok {
		t.Error("execute should be denied by AutoDeny approver")
	}
	// Build mode asks for execute; AutoApprove -> allowed.
	if ok, _ := New(ModeBuild, AutoApprove{}).Check(ctx, exec, nil); !ok {
		t.Error("execute should be allowed by AutoApprove approver")
	}
}

// recordingApprover captures the reason string it was asked to approve, for
// tests that need to inspect what the operator would actually see.
type recordingApprover struct {
	approve bool
	reason  string
}

func (a *recordingApprover) Approve(_ context.Context, _ string, reason string, _ json.RawMessage) bool {
	a.reason = reason
	return a.approve
}

// TestGateCheckAnnotatesExecuteReasonWithSandboxBackend is the P81.22/FIND-22
// regression: an execute-capability approval prompt must say what will
// actually contain the command, not only a startup log line the operator may
// never see. A write approval (not execute) must NOT carry the annotation —
// it isn't a command-execution decision.
func TestGateCheckAnnotatesExecuteReasonWithSandboxBackend(t *testing.T) {
	ctx := context.Background()
	exec := fakeTool{name: "shell", cap: tool.CapExecute}
	write := fakeTool{name: "write_file", cap: tool.CapWrite}

	approver := &recordingApprover{approve: true}
	gate := New(ModeBuild, approver)
	gate.SandboxBackendLabel = "local"

	if ok, _ := gate.Check(ctx, exec, nil); !ok {
		t.Fatal("expected approval")
	}
	if !strings.Contains(approver.reason, "sandbox: local") {
		t.Errorf("expected execute reason to name the sandbox backend, got %q", approver.reason)
	}

	// Build mode allows write outright — no Ask, so no reason is ever asked
	// for. Force it through Ask by using plan mode instead, where write is
	// Ask... actually write is Deny in plan mode, not Ask (see TestPolicyDecide),
	// so use auto mode's CapNetwork-shaped mid-tier: simplest is to directly
	// confirm a Write call never reaches SandboxBackendLabel logic by checking
	// build mode's Allow path never calls the approver at all.
	approver.reason = ""
	if ok, _ := gate.Check(ctx, write, nil); !ok {
		t.Fatal("expected write to be allowed outright in build mode")
	}
	if approver.reason != "" {
		t.Errorf("expected the approver not to be consulted for an outright Allow, got reason %q", approver.reason)
	}
}

// TestGateCheckUsesEffectiveCapability covers P25.4c: a call a tool
// reclassifies via CapabilityOverrider (e.g. shell's read-only allowlist) is
// gated on that narrower capability, not the tool's static one — both in
// build mode (no approval prompt) and plan mode (allowed instead of denied).
func TestGateCheckUsesEffectiveCapability(t *testing.T) {
	ctx := context.Background()
	readClassified := fakeOverrideTool{
		fakeTool:    fakeTool{name: "shell", cap: tool.CapExecute},
		overrideCap: tool.CapRead,
	}

	if ok, _ := New(ModeBuild, AutoDeny{}).Check(ctx, readClassified, nil); !ok {
		t.Error("expected a read-classified call to be allowed outright in build mode, no approver consulted")
	}
	if ok, _ := New(ModePlan, AutoDeny{}).Check(ctx, readClassified, nil); !ok {
		t.Error("expected a read-classified call to be allowed under the plan-mode read gate, not denied")
	}

	// Sanity: an unclassified call to the same tool still follows its static
	// CapExecute behavior (Ask in build mode; AutoDeny blocks it).
	plainExec := fakeTool{name: "shell", cap: tool.CapExecute}
	if ok, _ := New(ModeBuild, AutoDeny{}).Check(ctx, plainExec, nil); ok {
		t.Error("expected an unclassified execute call to still require approval")
	}
}

// TestGateRecordsCapabilityDowngrades is M7. A tool that reclassifies a call
// below its static capability is the mechanism the three classifier findings
// (CRIT-1/2/3) all ride on: shell is statically CapExecute, and a call
// classified CapRead is allowed silently in every mode — so an operator
// reviewing an audit trail saw an execute-capable tool run with no approval and
// no record of why. Every other layer of the gate stack reports its decisions;
// this one reported nothing.
func TestGateRecordsCapabilityDowngrades(t *testing.T) {
	var got []ContextualDecision
	g := New(ModePlan, AutoDeny{})
	g.OnDecision = func(d ContextualDecision) { got = append(got, d) }

	downgraded := fakeOverrideTool{
		fakeTool:    fakeTool{name: "shell", cap: tool.CapExecute},
		overrideCap: tool.CapRead,
	}
	if allowed, reason := g.Check(context.Background(), downgraded, nil); !allowed {
		t.Fatalf("a read-classified call must still be allowed in plan mode: %s", reason)
	}
	if len(got) != 1 {
		t.Fatalf("decisions recorded = %d, want 1: %+v", len(got), got)
	}
	d := got[0]
	if d.Rule != CapabilityDowngradeRule {
		t.Errorf("Rule = %q, want %q", d.Rule, CapabilityDowngradeRule)
	}
	if d.Tool != "shell" || d.Cap != string(tool.CapRead) || d.Decision != Allow {
		t.Errorf("record does not describe the downgrade: %+v", d)
	}
	if !strings.Contains(d.Reason, string(tool.CapExecute)) {
		t.Errorf("Reason %q does not name the capability that was given up", d.Reason)
	}

	// A tool gated on its own declared capability is the ordinary case and
	// must not add noise to the audit stream.
	got = nil
	plain := fakeTool{name: "read_file", cap: tool.CapRead}
	if allowed, _ := g.Check(context.Background(), plain, nil); !allowed {
		t.Fatal("a plain read must be allowed in plan mode")
	}
	if len(got) != 0 {
		t.Errorf("a call gated on its static capability recorded %+v, want nothing", got)
	}
}

// TestGateWithoutAnObserverStillDecides keeps the record optional: a Gate built
// by a caller that wires no observer — a bare permission.New — behaves exactly
// as it did before the record existed.
func TestGateWithoutAnObserverStillDecides(t *testing.T) {
	g := New(ModePlan, AutoDeny{})
	downgraded := fakeOverrideTool{
		fakeTool:    fakeTool{name: "shell", cap: tool.CapExecute},
		overrideCap: tool.CapRead,
	}
	if allowed, reason := g.Check(context.Background(), downgraded, nil); !allowed {
		t.Fatalf("allowed = false (%s), want true", reason)
	}
}

// TestStrictPlanModeRefusesDowngrades is DR-2. Plan mode's documented guarantee
// is that "the workspace may not be mutated or commands run at all", but
// EffectiveCapability is consulted before Decide, so the shell tool runs
// commands in plan mode whenever its classifier answers CapRead — which makes
// every defect in ~1,080 lines of argument parsing a plan-mode defect. The
// opt-in strict posture judges a tool on the capability it declares instead.
func TestStrictPlanModeRefusesDowngrades(t *testing.T) {
	downgraded := fakeOverrideTool{
		fakeTool:    fakeTool{name: "shell", cap: tool.CapExecute},
		overrideCap: tool.CapRead,
	}

	lenient := New(ModePlan, AutoDeny{})
	if allowed, _ := lenient.Check(context.Background(), downgraded, nil); !allowed {
		t.Error("the default posture is unchanged: a read-classified shell call is still allowed in plan mode")
	}

	strict := New(ModePlan, AutoDeny{})
	strict.Policy.StrictPlanMode = true
	allowed, reason := strict.Check(context.Background(), downgraded, nil)
	if allowed {
		t.Error("under the strict posture a shell call must be judged as CapExecute in plan mode")
	}
	if !strings.Contains(reason, string(tool.CapExecute)) {
		t.Errorf("denial reason %q does not name the capability it was judged on", reason)
	}

	// Build and auto mode are unaffected: the ergonomics the downgrade buys are
	// kept where they are wanted.
	for _, mode := range []Mode{ModeBuild, ModeAuto} {
		g := New(mode, AutoDeny{})
		g.Policy.StrictPlanMode = true
		if allowed, reason := g.Check(context.Background(), downgraded, nil); !allowed {
			t.Errorf("%s mode: a read-classified call must still be allowed (%s)", mode, reason)
		}
	}
}

// TestStrictPlanModeKeepsUpwardReclassification pins that the strict posture
// takes the *stricter* of the two capabilities rather than always the declared
// one: a tool that reclassifies a specific call as more dangerous than its
// declared capability must keep that verdict, or the knob would weaken exactly
// the case it exists to protect.
func TestStrictPlanModeKeepsUpwardReclassification(t *testing.T) {
	upgraded := fakeOverrideTool{
		fakeTool:    fakeTool{name: "fetch", cap: tool.CapRead},
		overrideCap: tool.CapWrite,
	}
	g := New(ModePlan, AutoDeny{})
	g.Policy.StrictPlanMode = true
	if allowed, _ := g.Check(context.Background(), upgraded, nil); allowed {
		t.Error("an upward reclassification must still bind under the strict posture")
	}
}
