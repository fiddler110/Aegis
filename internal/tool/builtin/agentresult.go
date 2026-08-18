package builtin

import (
	"github.com/fiddler110/aegis/internal/trust"
)

// P70.4: the cap and the provenance envelope on a sub-agent's result.
//
// `swarm.Result.Output` reaches the parent model through four paths in
// agent.go — a foreground `agent` call, each workflow mode's joined output, a
// debate transcript, and a background spawn read back with `task_output` — and
// before this it travelled all four of them bare and unbounded, on *both*
// backends. The subprocess backend lifts it out of the mailbox
// (`internal/swarm/subprocess.go:223`); the in-process backend returns it
// straight from `runGuarded` (`inprocess.go:82`) without the mailbox at all,
// which is why P70.2's mailbox wrap does not and should not cover it: the
// mailbox is only this channel's durability substrate under one of the two
// backends.
//
// Both halves live here rather than in internal/swarm because the boundary
// being marked is "text becomes a tool result for the parent model", and that
// boundary is in this package. A capped result also bounds the *prompt* growth
// in the chaining modes, where each teammate's output is fed to the next.

const (
	// maxAgentResult bounds one model-facing sub-agent result body in bytes,
	// before the provenance envelope is added.
	//
	// 24000 B is ~6.0k estimated tokens, the value shell and the skill-script
	// runner already chose for the same class of thing — a subprocess writing
	// a report — and small enough to bind before a local context window does
	// (TestResultCapsCanBindBeforeTheContextWindow). It is deliberately larger
	// than team_inbox's 20000: a sub-agent's report is the *point* of the call
	// the parent made, where a mailbox batch is incidental traffic.
	maxAgentResult = 24000

	// minAgentShare is the floor under a workflow teammate's share of
	// maxAgentResult. A contribution trimmed below this is a truncation notice
	// with nothing attached, which tells the parent a teammate ran but not what
	// it found. 2000 B is what task_get's output preview settled on for the
	// same question. Where the floor binds — more teammates than the budget
	// divides into — the join cap below trims the tail, and says so.
	minAgentShare = 2000
)

// agentShare divides maxAgentResult among n teammates so an over-budget batch
// loses bytes evenly instead of losing its last teammates entirely, which is
// what a single cap over the joined text does. Each share carries its own
// truncation notice, so the parent can see which teammate was cut.
func agentShare(n int) int {
	if n <= 1 {
		return maxAgentResult
	}
	if share := maxAgentResult / n; share > minAgentShare {
		return share
	}
	return minAgentShare
}

// capAgentOutput trims a sub-agent result to limit bytes, head end.
//
// Head because a sub-agent's report is a digest written top-down: the answer
// and the summary are at the top and the working is below, which is the
// posture the table in truncate.go assigns to that shape.
//
// The remainder is deliberately NOT spilled — the same exclusion web_fetch and
// team_inbox take, and for the reason that matters most here. Spilling writes
// the overflow to a workspace file that `read_file` returns with no envelope at
// all, so the bytes this item just marked as untrusted would be readable back
// unmarked. A context-budget feature must not reopen the laundering path the
// wrap closes.
func capAgentOutput(body string, limit int) string {
	out, _ := TruncateHead(body, limit, "")
	return out
}

// wrapAgentOutput marks a sub-agent's result as untrusted content before it
// re-enters the parent model's context (P70.4).
//
// This is the same laundering shape P70.2 closed one channel of: a sub-agent
// that read a poisoned web page, an MCP result or a workspace file writes what
// it read into its own report, where it previously arrived at the parent as
// plain, trusted-looking prose — losing the provenance marking web_fetch and
// MCP results carry at ingestion. The counter-argument the item was filed with
// is that a parent which *commissioned* the work is not in the same position as
// one reading a teammate's relayed prose; the answer taken is that commissioning
// the work does not vouch for what the work read, so Aegis's zero-trust reading
// applies here too.
//
// The heuristic injection scan is off, matching every other envelope outside
// the ingestion points (team_inbox, personas, skills, the network scan report):
// it is a config-gated opt-in wherever it is on, there is no per-spawn knob to
// gate it, and a sub-agent reporting on this very subject ("the file contains
// 'ignore previous instructions'") is exactly the text its keyword patterns
// over-fire on. The envelope — this is data, not instructions — is what the
// provenance gap asked for; scanning stays where ingestion is.
//
// kind names the attribute ("agent", "workflow", "debate") and name its value,
// so the parent can see which spawn shape produced the bytes.
func wrapAgentOutput(kind, name, body string) string {
	return trust.Wrap(
		"agent_untrusted_output",
		[][2]string{{kind, name}},
		"a sub-agent reporting on work this session commissioned (a sub-agent can quote text it read from the web, an MCP server or a file, so these bytes did not necessarily originate with it)",
		body,
		false,
	)
}

// wrapWorkflowOutput bounds a workflow mode's joined teammate output and wraps
// it as one untrusted body (P70.4).
//
// The join cap is a second bound over the per-teammate shares, not a
// replacement for them: agentShare's floor means a batch of more teammates than
// maxAgentResult divides into can still assemble a body over the cap, and this
// is what catches it. When it binds the tail teammates lose their contribution
// and the notice says so, which is why the shares exist — so it binds rarely.
func wrapWorkflowOutput(mode, body string) string {
	return wrapAgentOutput("workflow", mode, capAgentOutput(body, maxAgentResult))
}
