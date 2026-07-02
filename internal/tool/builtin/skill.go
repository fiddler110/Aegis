package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scottymacleod/aegis/internal/skills"
	"github.com/scottymacleod/aegis/internal/tool"
)

// skillTool implements progressive disclosure (P4.3): the system prompt lists
// only skill names and descriptions; this tool loads a skill's full body on
// demand.
type skillTool struct{ root string }

func (t *skillTool) Name() string                { return "skill" }
func (t *skillTool) Capability() tool.Capability { return tool.CapRead }
func (t *skillTool) Description() string {
	return "Load the full instructions for a named skill listed in <skills_available>. Call this before performing a task the skill covers."
}
func (t *skillTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"name":{"type":"string","description":"the skill name as shown in <skills_available>"}},"required":["name"]}`)
}
func (t *skillTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	args.Name = strings.TrimSpace(args.Name)
	if args.Name == "" {
		return tool.Result{Content: "skill name is required", IsError: true}, nil
	}
	sk, ok := skills.Load(t.root, args.Name)
	if !ok {
		avail := skillNames(t.root)
		msg := fmt.Sprintf("no skill named %q", args.Name)
		if avail != "" {
			msg += "; available skills: " + avail
		}
		return tool.Result{Content: msg, IsError: true}, nil
	}
	return tool.Result{Content: sk.Content}, nil
}

func skillNames(root string) string {
	var names []string
	for _, sk := range skills.Discover(root) {
		names = append(names, sk.Name)
	}
	return strings.Join(names, ", ")
}
