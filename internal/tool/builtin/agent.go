package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/scottymacleod/agentharness/internal/agentdef"
	"github.com/scottymacleod/agentharness/internal/swarm"
	"github.com/scottymacleod/agentharness/internal/task"
	"github.com/scottymacleod/agentharness/internal/tool"
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
		"build (full access). A sub-agent cannot exceed your own permission mode."
}

func (a *agentTool) Capability() tool.Capability { return tool.CapSpawn }

func (a *agentTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"description": {"type": "string", "description": "A short (3-5 word) description of the delegated task."},
			"prompt": {"type": "string", "description": "The full task/instructions for the sub-agent."},
			"subagent_type": {"type": "string", "description": "Which agent to use: general, explore, plan, or build.", "enum": ["general", "explore", "plan", "build"]},
			"background": {"type": "boolean", "description": "If true, spawn the sub-agent detached and return a task id immediately instead of waiting. Poll its result with task_get/task_output."}
		},
		"required": ["prompt"]
	}`)
}

func (a *agentTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Description  string `json:"description"`
		Prompt       string `json:"prompt"`
		SubagentType string `json:"subagent_type"`
		Background   bool   `json:"background"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return tool.Result{Content: "agent: invalid input: " + err.Error(), IsError: true}, nil
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
	// A sub-agent may only restrict, never escalate, the caller's posture.
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

// clampMode returns the more restrictive of the parent and requested modes. A
// plan-mode (read-only) parent forces a plan-mode child.
func clampMode(parent, requested string) string {
	if parent == "plan" {
		return "plan"
	}
	if requested == "" {
		return "build"
	}
	return requested
}
