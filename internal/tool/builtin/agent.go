package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/scottymacleod/agentharness/internal/agentdef"
	"github.com/scottymacleod/agentharness/internal/swarm"
	"github.com/scottymacleod/agentharness/internal/tool"
)

// maxSpawnDepth bounds sub-agent recursion (an agent spawning an agent ...).
const maxSpawnDepth = 3

// agentTool delegates a task to a sub-agent ("teammate"). For the in-process
// backend it spawns the teammate and waits for its result synchronously,
// returning the teammate's final answer to the calling model.
type agentTool struct {
	backend swarm.Backend
}

// NewAgentTool builds the `agent` delegation tool over the given backend.
func NewAgentTool(backend swarm.Backend) tool.Tool {
	return &agentTool{backend: backend}
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
			"subagent_type": {"type": "string", "description": "Which agent to use: general, explore, plan, or build.", "enum": ["general", "explore", "plan", "build"]}
		},
		"required": ["prompt"]
	}`)
}

func (a *agentTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Description  string `json:"description"`
		Prompt       string `json:"prompt"`
		SubagentType string `json:"subagent_type"`
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

	h, err := a.backend.Spawn(ctx, cfg)
	if err != nil {
		return tool.Result{Content: "agent: spawn failed: " + err.Error(), IsError: true}, nil
	}
	res, err := h.Wait(ctx)
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
