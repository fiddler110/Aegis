package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/scottymacleod/aegis/internal/agentdef"
	"github.com/scottymacleod/aegis/internal/swarm"
	"github.com/scottymacleod/aegis/internal/task"
	"github.com/scottymacleod/aegis/internal/tool"
)

const maxAgentDuration = 10 * time.Minute

// maxSpawnDepth bounds sub-agent recursion (an agent spawning an agent ...).
const maxSpawnDepth = 3

// agentTool delegates a task to a sub-agent ("teammate"). By default it spawns
// the teammate and waits for its result synchronously, returning the teammate's
// final answer to the calling model. With background:true (and a task manager
// wired) it returns a task id immediately so the caller can keep working and
// poll the result via task_get/task_output.
type agentTool struct {
	backend swarm.Backend
	mgr     *task.Manager // optional; enables background:true
}

// NewAgentTool builds the `agent` delegation tool over the given backend. mgr
// may be nil, which disables background delegation.
func NewAgentTool(backend swarm.Backend, mgr *task.Manager) tool.Tool {
	return &agentTool{backend: backend, mgr: mgr}
}

func (a *agentTool) Name() string { return "agent" }

func (a *agentTool) Description() string {
	return "Delegate a self-contained task to a sub-agent and get back its result. " +
		"Use this to parallelize independent work or to run a focused read-only " +
		"investigation. subagent_type selects the agent: general (multi-step work), " +
		"explore (read-only search, returns findings), plan (read-only analysis), " +
		"build (full access). A sub-agent cannot exceed your own permission mode. " +
		"For multi-agent workflows, set mode to 'sequential', 'parallel', or 'loop' " +
		"and provide an 'agents' array instead of a single prompt."
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
			"mode": {"type": "string", "description": "Workflow mode: 'sequential' (agents run in order, each receiving prior output), 'parallel' (agents run concurrently), 'loop' (single agent re-runs until it outputs DONE or max_iterations is reached).", "enum": ["sequential", "parallel", "loop"]},
			"agents": {"type": "array", "description": "List of sub-agents for workflow mode.", "items": {"type": "object", "properties": {"description": {"type": "string"}, "prompt": {"type": "string"}, "subagent_type": {"type": "string"}}, "required": ["prompt"]}},
			"max_iterations": {"type": "integer", "description": "Maximum iterations for loop mode (default 5)."}
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
		Description   string          `json:"description"`
		Prompt        string          `json:"prompt"`
		SubagentType  string          `json:"subagent_type"`
		Background    bool            `json:"background"`
		Mode          string          `json:"mode"`
		Agents        []workflowAgent `json:"agents"`
		MaxIterations int             `json:"max_iterations"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{Content: "agent: invalid input: " + err.Error(), IsError: true}, nil
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
	}

	if args.Background {
		if a.mgr == nil {
			return tool.Result{Content: "agent: background delegation is not available in this context", IsError: true}, nil
		}
		return a.spawnBackground(cfg, args.Description, def.Name)
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

// spawnBackground launches the teammate as a detached background task. The
// spawn happens inside the task's RunFunc so the teammate runs under the task's
// own (request-independent) context and survives the call that created it.
func (a *agentTool) spawnBackground(cfg swarm.SpawnConfig, description, agentName string) (tool.Result, error) {
	title := description
	if title == "" {
		title = "sub-agent " + agentName
	}
	tk, err := a.mgr.Start(task.Spec{Kind: "subagent", Title: title}, func(jobCtx context.Context, emit func(string)) (string, error) {
		jobCtx, jobCancel := context.WithTimeout(jobCtx, maxAgentDuration)
		defer jobCancel()
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
