package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/agentdef"
	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/google/uuid"
)

const maxAgentDuration = 10 * time.Minute

// maxSpawnDepth bounds sub-agent recursion (an agent spawning an agent ...).
const maxSpawnDepth = 3

// maxParallelAgents bounds how many teammates one 'parallel' workflow call may
// spawn concurrently (D1). The 'agents' array length is model-controlled JSON
// with no other limit, so without this a single tool call could fan out
// arbitrarily wide; paired with the shared cost ledger (subAgentRunner reading
// swarm.CostTrackerFromContext) this keeps both the concurrency burst and the
// total spend bounded.
const maxParallelAgents = 8

// checkpointIDFrom returns the parent turn's checkpoint id, if ctx carries a
// Snapshotter, or "" otherwise. The in-process swarm backend already
// captures a sub-agent's file writes for free (its ctx keeps the same
// Snapshotter value all the way down); the subprocess backend needs this id
// threaded through SpawnConfig/WorkerSpec explicitly so the worker process —
// which starts a whole separate ctx tree — can reconstruct an equivalent
// Snapshotter of its own (P9).
func checkpointIDFrom(ctx context.Context) string {
	if snap := checkpoint.SnapshotterFrom(ctx); snap != nil {
		return snap.CheckpointID()
	}
	return ""
}

// agentTool delegates a task to a sub-agent ("teammate"). By default it spawns
// the teammate and waits for its result synchronously, returning the teammate's
// final answer to the calling model. With background:true (and a task manager
// wired) it returns a task id immediately so the caller can keep working and
// poll the result via task_get/task_output.
type agentTool struct {
	backend swarm.Backend
	mgr     *task.Manager // optional; enables background:true

	// budgetUSD/maxTokensPerRun are the daemon's configured cost caps (P12.6),
	// threaded through so debate mode can check the shared tracker before
	// starting each additional round rather than letting a per-round sub-agent
	// spawn hit the engine's own budget abort mid-critique. Zero means
	// unlimited, same convention as engine.Options.
	budgetUSD       float64
	maxTokensPerRun int
}

// AgentToolOption configures optional behavior on the `agent` tool.
type AgentToolOption func(*agentTool)

// WithCostCaps sets the cost caps debate mode checks against (P12.6). Pass the
// same values as the daemon's configured engine.Options.BudgetUSD/MaxTokensPerRun.
func WithCostCaps(budgetUSD float64, maxTokensPerRun int) AgentToolOption {
	return func(a *agentTool) {
		a.budgetUSD = budgetUSD
		a.maxTokensPerRun = maxTokensPerRun
	}
}

// NewAgentTool builds the `agent` delegation tool over the given backend. mgr
// may be nil, which disables background delegation.
func NewAgentTool(backend swarm.Backend, mgr *task.Manager, opts ...AgentToolOption) tool.Tool {
	a := &agentTool{backend: backend, mgr: mgr}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *agentTool) Name() string { return "agent" }

func (a *agentTool) Description() string {
	return "Delegate a self-contained task to a sub-agent and get back its result. " +
		"Use this to parallelize independent work or to run a focused read-only " +
		"investigation. subagent_type selects the agent: general (multi-step work), " +
		"explore (read-only search, returns findings), plan (read-only analysis), " +
		"build (full access). A sub-agent cannot exceed your own permission mode. " +
		"For multi-agent workflows, set mode to 'sequential', 'parallel', or 'loop' " +
		"and provide an 'agents' array instead of a single prompt. For adversarial " +
		"review of any claim — a security finding, a design assertion, or a claim " +
		"about a document/plan/decision — set mode to 'debate' and provide 'claim': " +
		"a critic challenges it (grounded in cited evidence or an explicit concession), " +
		"the proposer rebuts, this repeats for 'max_rounds' (default 2), then an " +
		"arbiter issues a final UPHOLD/REVISE/REJECT verdict. Set 'domain' to 'generic' " +
		"for non-security claims (uses the general/critic/arbiter personas instead of " +
		"the security-* ones); pass 'files' to point the debate roles at specific " +
		"documents or source files to ground the debate in instead of relying on " +
		"recall. Use this when a claim is borderline, disputed, or high-stakes enough " +
		"to warrant adversarial pressure instead of a single unchallenged pass."
}

func (a *agentTool) Capability() tool.Capability { return tool.CapSpawn }

func (a *agentTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"description": {"type": "string", "description": "A short (3-5 word) description of the delegated task."},
			"prompt": {"type": "string", "description": "The full task/instructions for the sub-agent (single-agent mode)."},
			"subagent_type": {"type": "string", "description": "Which agent to use: general, explore, plan, or build.", "enum": ["general", "explore", "plan", "build"]},
			"background": {"type": "boolean", "description": "If true, spawn the sub-agent detached and return a task id immediately instead of waiting."},
			"mode": {"type": "string", "description": "Workflow mode: 'sequential' (agents run in order, each receiving prior output), 'parallel' (agents run concurrently), 'loop' (single agent re-runs until it outputs DONE or max_iterations is reached), 'debate' (adversarial propose/critique/rebut/arbitrate over a claim).", "enum": ["sequential", "parallel", "loop", "debate"]},
			"agents": {"type": "array", "description": "List of sub-agents for workflow mode.", "items": {"type": "object", "properties": {"description": {"type": "string"}, "prompt": {"type": "string"}, "subagent_type": {"type": "string"}}, "required": ["prompt"]}},
			"max_iterations": {"type": "integer", "description": "Maximum iterations for loop mode (default 5)."},
			"claim": {"type": "string", "description": "Debate mode only: the claim/finding/design assertion to subject to adversarial critique."},
			"domain": {"type": "string", "description": "Debate mode only: 'security' (default) or 'generic' — selects the default persona trio when proposer/critic/arbiter persona aren't set explicitly.", "enum": ["security", "generic"]},
			"files": {"type": "array", "description": "Debate mode only: file paths the debate roles should read for grounding before proposing/critiquing/rebutting (e.g. the documents a claim is about).", "items": {"type": "string"}},
			"proposer_persona": {"type": "string", "description": "Debate mode only: persona for the proposer role (default security-researcher, or general if domain is generic)."},
			"critic_persona": {"type": "string", "description": "Debate mode only: persona for the critic role (default security-critic, or critic if domain is generic)."},
			"arbiter_persona": {"type": "string", "description": "Debate mode only: persona for the arbiter role (default security-arbiter, or arbiter if domain is generic)."},
			"max_rounds": {"type": "integer", "description": "Debate mode only: maximum critique/rebuttal rounds before arbitration (default 2)."}
		},
		"required": []
	}`)
}

// workflowAgent describes one agent in a multi-agent workflow.
type workflowAgent struct {
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	SubagentType string `json:"subagent_type"`
}

func (a *agentTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Description     string          `json:"description"`
		Prompt          string          `json:"prompt"`
		SubagentType    string          `json:"subagent_type"`
		Background      bool            `json:"background"`
		Mode            string          `json:"mode"`
		Agents          []workflowAgent `json:"agents"`
		MaxIterations   int             `json:"max_iterations"`
		Claim           string          `json:"claim"`
		Domain          string          `json:"domain"`
		Files           []string        `json:"files"`
		ProposerPersona string          `json:"proposer_persona"`
		CriticPersona   string          `json:"critic_persona"`
		ArbiterPersona  string          `json:"arbiter_persona"`
		MaxRounds       int             `json:"max_rounds"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{Content: "agent: invalid input: " + err.Error(), IsError: true}, nil
	}

	if args.Mode == "debate" {
		claim := args.Claim
		if claim == "" {
			claim = args.Prompt
		}
		if claim == "" {
			return tool.Result{Content: "agent: 'claim' is required for debate mode", IsError: true}, nil
		}
		claim = debate.WithFiles(claim, args.Files)
		return a.executeDebate(ctx, claim, args.Domain, args.ProposerPersona, args.CriticPersona, args.ArbiterPersona, args.MaxRounds)
	}

	// Workflow mode: mode field + agents array.
	if args.Mode != "" {
		if len(args.Agents) == 0 && args.Mode != "loop" {
			return tool.Result{Content: "agent: 'agents' array is required for sequential/parallel modes", IsError: true}, nil
		}
		if args.Mode == "loop" && args.Prompt == "" && len(args.Agents) == 0 {
			return tool.Result{Content: "agent: 'prompt' or 'agents[0]' is required for loop mode", IsError: true}, nil
		}
		return a.executeWorkflow(ctx, args.Mode, args.Agents, args.Prompt, args.SubagentType, args.MaxIterations)
	}

	if args.Prompt == "" {
		return tool.Result{Content: "agent: prompt is required", IsError: true}, nil
	}

	if depth := swarm.DepthFromContext(ctx); depth >= maxSpawnDepth {
		return tool.Result{
			Content: fmt.Sprintf("agent: maximum sub-agent depth (%d) reached; not spawning", maxSpawnDepth),
			IsError: true,
		}, nil
	}

	def, known := agentdef.Resolve(args.SubagentType)
	childMode := clampMode(swarm.ParentModeFromContext(ctx), def.Mode)

	cfg := swarm.SpawnConfig{
		Name:         fmt.Sprintf("%s-%s", def.Name, uuid.NewString()[:8]),
		Team:         "default",
		Prompt:       args.Prompt,
		SystemPrompt: def.SystemPrompt,
		Mode:         childMode,
		Depth:        swarm.DepthFromContext(ctx) + 1,
		CheckpointID: checkpointIDFrom(ctx),
	}

	if args.Background {
		if a.mgr == nil {
			return tool.Result{Content: "agent: background delegation is not available in this context", IsError: true}, nil
		}
		return a.spawnBackground(ctx, cfg, args.Description, def.Name)
	}

	agentCtx, agentCancel := context.WithTimeout(ctx, maxAgentDuration)
	defer agentCancel()
	h, err := a.backend.Spawn(agentCtx, cfg)
	if err != nil {
		return tool.Result{Content: "agent: spawn failed: " + err.Error(), IsError: true}, nil
	}
	res, err := h.Wait(agentCtx)
	if err != nil {
		return tool.Result{Content: "agent: " + err.Error(), IsError: true}, nil
	}
	if res.Failed() {
		return tool.Result{Content: fmt.Sprintf("sub-agent %s failed: %s", res.AgentID, res.Err), IsError: true}, nil
	}

	out := res.Output
	if out == "" {
		out = "(sub-agent produced no output)"
	}
	if !known && args.SubagentType != "" {
		out = fmt.Sprintf("(unknown subagent_type %q; used %q)\n\n%s", args.SubagentType, def.Name, out)
	}
	return tool.Result{Content: out}, nil
}

func (a *agentTool) executeWorkflow(ctx context.Context, mode string, agents []workflowAgent, fallbackPrompt, fallbackType string, maxIter int) (tool.Result, error) {
	if depth := swarm.DepthFromContext(ctx); depth >= maxSpawnDepth {
		return tool.Result{Content: fmt.Sprintf("agent: maximum sub-agent depth (%d) reached", maxSpawnDepth), IsError: true}, nil
	}
	if mode == "parallel" && len(agents) > maxParallelAgents {
		return tool.Result{
			Content: fmt.Sprintf("agent: parallel workflow requested %d agents, exceeding the max of %d; split into smaller batches", len(agents), maxParallelAgents),
			IsError: true,
		}, nil
	}

	spawn := func(agentCtx context.Context, wa workflowAgent, extraContext string) (string, error) {
		prompt := wa.Prompt
		if extraContext != "" {
			prompt = extraContext + "\n\n---\n\n" + prompt
		}
		def, _ := agentdef.Resolve(wa.SubagentType)
		childMode := clampMode(swarm.ParentModeFromContext(ctx), def.Mode)
		cfg := swarm.SpawnConfig{
			Name:         fmt.Sprintf("%s-%s", def.Name, uuid.NewString()[:8]),
			Team:         "workflow",
			Prompt:       prompt,
			SystemPrompt: def.SystemPrompt,
			Mode:         childMode,
			Depth:        swarm.DepthFromContext(ctx) + 1,
			CheckpointID: checkpointIDFrom(ctx),
		}
		h, err := a.backend.Spawn(agentCtx, cfg)
		if err != nil {
			return "", err
		}
		res, err := h.Wait(agentCtx)
		if err != nil {
			return "", err
		}
		if res.Failed() {
			return "", errors.New(res.Err)
		}
		if res.Output == "" {
			return "(no output)", nil
		}
		return res.Output, nil
	}

	agentCtx, agentCancel := context.WithTimeout(ctx, maxAgentDuration*time.Duration(max(len(agents), 1)+1))
	defer agentCancel()

	switch mode {
	case "sequential":
		var context string
		var outputs []string
		for i, wa := range agents {
			out, err := spawn(agentCtx, wa, context)
			if err != nil {
				return tool.Result{Content: fmt.Sprintf("sequential: agent %d failed: %v", i+1, err), IsError: true}, nil
			}
			outputs = append(outputs, fmt.Sprintf("=== Agent %d ===\n%s", i+1, out))
			context = out
		}
		return tool.Result{Content: strings.Join(outputs, "\n\n")}, nil

	case "parallel":
		type result struct {
			idx int
			out string
			err error
		}
		ch := make(chan result, len(agents))
		for i, wa := range agents {
			go func(idx int, wa workflowAgent) {
				out, err := spawn(agentCtx, wa, "")
				ch <- result{idx: idx, out: out, err: err}
			}(i, wa)
		}
		outputs := make([]string, len(agents))
		var errs []string
		for range agents {
			r := <-ch
			if r.err != nil {
				errs = append(errs, fmt.Sprintf("agent %d: %v", r.idx+1, r.err))
			} else {
				outputs[r.idx] = fmt.Sprintf("=== Agent %d ===\n%s", r.idx+1, r.out)
			}
		}
		if len(errs) > 0 {
			return tool.Result{Content: "parallel: " + strings.Join(errs, "; "), IsError: true}, nil
		}
		return tool.Result{Content: strings.Join(outputs, "\n\n")}, nil

	case "loop":
		if maxIter <= 0 {
			maxIter = 5
		}
		wa := workflowAgent{Prompt: fallbackPrompt, SubagentType: fallbackType}
		if len(agents) > 0 {
			wa = agents[0]
		}
		var lastOut string
		for i := range maxIter {
			out, err := spawn(agentCtx, wa, lastOut)
			if err != nil {
				return tool.Result{Content: fmt.Sprintf("loop: iteration %d failed: %v", i+1, err), IsError: true}, nil
			}
			lastOut = out
			if strings.Contains(out, "DONE") {
				return tool.Result{Content: fmt.Sprintf("(loop completed in %d iteration(s))\n\n%s", i+1, out)}, nil
			}
		}
		return tool.Result{Content: fmt.Sprintf("(loop reached max iterations %d)\n\n%s", maxIter, lastOut)}, nil

	default:
		return tool.Result{Content: fmt.Sprintf("agent: unknown mode %q", mode), IsError: true}, nil
	}
}

// executeDebate runs a multi-agent debate (P12.1) over claim: a critic
// challenges it for up to max_rounds (default debate.DefaultMaxRounds),
// grounded in cited evidence or an explicit concession (P12.3), the proposer
// rebuts each round, and an arbiter issues a final verdict over the full
// transcript. Each role runs as a real sub-agent through the existing
// swarm.Backend seam — no new spawn mechanism — so the critic has the same
// tool access (grep/read_file/security_scan) any other sub-agent gets under
// its clamped permission mode.
func (a *agentTool) executeDebate(ctx context.Context, claim, domain, proposerPersona, criticPersona, arbiterPersona string, maxRounds int) (tool.Result, error) {
	if depth := swarm.DepthFromContext(ctx); depth >= maxSpawnDepth {
		return tool.Result{Content: fmt.Sprintf("agent: maximum sub-agent depth (%d) reached; not starting debate", maxSpawnDepth), IsError: true}, nil
	}

	childMode := clampMode(swarm.ParentModeFromContext(ctx), "build")
	runRole := func(roleCtx context.Context, systemPrompt, prompt string) (string, error) {
		cfg := swarm.SpawnConfig{
			Name:         fmt.Sprintf("debate-%s", uuid.NewString()[:8]),
			Team:         "debate",
			Prompt:       prompt,
			SystemPrompt: systemPrompt,
			Mode:         childMode,
			Depth:        swarm.DepthFromContext(ctx) + 1,
			CheckpointID: checkpointIDFrom(ctx),
		}
		h, err := a.backend.Spawn(roleCtx, cfg)
		if err != nil {
			return "", err
		}
		res, err := h.Wait(roleCtx)
		if err != nil {
			return "", err
		}
		if res.Failed() {
			return "", errors.New(res.Err)
		}
		return res.Output, nil
	}

	tracker, _ := swarm.CostTrackerFromContext(ctx).(*cost.Tracker)
	debateCfg := debate.Config{
		Domain:          domain,
		ProposerPersona: proposerPersona,
		CriticPersona:   criticPersona,
		ArbiterPersona:  arbiterPersona,
		MaxRounds:       maxRounds,
		Tracker:         tracker,
		BudgetUSD:       a.budgetUSD,
		MaxTokens:       a.maxTokensPerRun,
	}

	debateMaxRounds := debateCfg.MaxRounds
	if debateMaxRounds <= 0 {
		debateMaxRounds = debate.DefaultMaxRounds
	}
	debateCtx, debateCancel := context.WithTimeout(ctx, maxAgentDuration*time.Duration(2*debateMaxRounds+2))
	defer debateCancel()

	transcript, err := debate.Run(debateCtx, claim, debateCfg, runRole)
	if err != nil {
		return tool.Result{Content: "agent: debate failed: " + err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: transcript.Format()}, nil
}

// spawnBackground launches the teammate as a detached background task. The
// spawn happens inside the task's RunFunc so the teammate runs under the task's
// own (request-independent) context and survives the call that created it.
// task.Manager.Start derives jobCtx from context.Background(), so any shared
// cost ledger on the caller's ctx would otherwise be silently lost — a
// background spawn would fall back to its own fresh BudgetUSD allowance,
// defeating the D1 fan-out fix (internal/server subAgentRunner). Carry it
// forward explicitly by closing over the tracker read here rather than
// relying on context propagation across that detach point.
func (a *agentTool) spawnBackground(ctx context.Context, cfg swarm.SpawnConfig, description, agentName string) (tool.Result, error) {
	tracker := swarm.CostTrackerFromContext(ctx)
	title := description
	if title == "" {
		title = "sub-agent " + agentName
	}
	tk, err := a.mgr.Start(task.Spec{Kind: "subagent", Title: title}, func(jobCtx context.Context, emit func(string)) (string, error) {
		jobCtx, jobCancel := context.WithTimeout(jobCtx, maxAgentDuration)
		defer jobCancel()
		if tracker != nil {
			jobCtx = swarm.WithCostTracker(jobCtx, tracker)
		}
		h, err := a.backend.Spawn(jobCtx, cfg)
		if err != nil {
			return "", err
		}
		res, err := h.Wait(jobCtx)
		if err != nil {
			return "", err
		}
		if res.Failed() {
			return "", errors.New(res.Err)
		}
		emit(res.Output)
		return res.Output, nil
	})
	if err != nil {
		return tool.Result{Content: "agent: " + err.Error(), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("Spawned background sub-agent %q (task id %s). Poll with task_get; read its result with task_output.", agentName, tk.ID)}, nil
}

// modeRank assigns an ordinal to each permission posture.
func modeRank(m string) int {
	switch m {
	case "auto":
		return 2
	case "build":
		return 1
	default: // "plan" or unrecognised
		return 0
	}
}

// clampMode returns the more restrictive of the parent and requested modes. A
// child may only restrict, never escalate, the caller's posture.
func clampMode(parent, requested string) string {
	if requested == "" {
		requested = "build"
	}
	if modeRank(requested) > modeRank(parent) {
		return parent
	}
	return requested
}
