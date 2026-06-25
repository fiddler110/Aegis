package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/scottymacleod/aegis/internal/memory"
	"github.com/scottymacleod/aegis/internal/tool"
)

// --- remember ---

type rememberTool struct{ src memory.Sources }

func (t *rememberTool) Name() string                { return "remember" }
func (t *rememberTool) Capability() tool.Capability { return tool.CapWrite }
func (t *rememberTool) Description() string {
	return "Persist a durable fact, decision, or preference to project memory so it is available in future sessions. Use for things worth remembering long-term, not transient details."
}
func (t *rememberTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"note":{"type":"string","description":"the fact to remember, one concise sentence"}},"required":["note"]}`)
}
func (t *rememberTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Note string `json:"note"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if err := memory.Append(t.src.ProjectMemoryPath(), args.Note); err != nil {
		return tool.Result{Content: fmt.Sprintf("could not save memory: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: "saved to project memory"}, nil
}

// --- save_skill ---

type saveSkillTool struct{ src memory.Sources }

func (t *saveSkillTool) Name() string                { return "save_skill" }
func (t *saveSkillTool) Capability() tool.Capability { return tool.CapWrite }
func (t *saveSkillTool) Description() string {
	return "Save a reusable skill (a short how-to or procedure) as a markdown file so it is loaded into future sessions. Use after working out a repeatable procedure."
}
func (t *saveSkillTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"name":{"type":"string","description":"short skill name (kebab-case)"},"content":{"type":"string","description":"the skill body in markdown"}},"required":["name","content"]}`)
}
func (t *saveSkillTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	path, err := t.src.SaveSkill(args.Name, args.Content)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("could not save skill: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: "saved skill to " + path}, nil
}
