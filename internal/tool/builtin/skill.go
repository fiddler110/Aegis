package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/tool"
)

// skillTool implements progressive disclosure (P4.3): the system prompt lists
// only skill names and descriptions; this tool loads a skill's full body on
// demand.
type skillTool struct {
	root           string
	dataDir        string
	builtinEnabled []string
}

// NewSkillTool constructs the skill tool. Exposed so callers outside this
// package (the server, to activate a built-in skill on demand for a single
// session — see P22.x on-demand skill activation) can rebuild it with an
// expanded builtinEnabled list without reaching into an unexported type.
func NewSkillTool(root, dataDir string, builtinEnabled []string) tool.Tool {
	return &skillTool{root: root, dataDir: dataDir, builtinEnabled: builtinEnabled}
}

func (t *skillTool) Name() string                { return "skill" }
func (t *skillTool) Capability() tool.Capability { return tool.CapRead }
func (t *skillTool) Description() string {
	return "Load the full instructions for a named skill listed in <skills_available>. Call this before performing a task the skill covers."
}
func (t *skillTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"name":{"type":"string","description":"the skill name as shown in <skills_available>"}},"required":["name"]}`)
}
func (t *skillTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Name   string          `json:"name"`
		Mode   string          `json:"mode"`
		Agents json.RawMessage `json:"agents"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	// Corrective guard: models sometimes route a sub-agent workflow to the
	// skill tool because a skill body told them to "run the phased agent
	// workflow". The skill tool only loads a body; misrouting mode/agents here
	// used to be a silent no-op (unknown JSON fields are dropped). Point the
	// model at the `agent` tool instead of failing opaquely.
	if strings.TrimSpace(args.Mode) != "" || len(args.Agents) > 0 {
		return tool.Result{Content: "The `skill` tool only loads a skill body and takes a single `name` argument — it has no `mode` or `agents` parameter. To run a sub-agent workflow (sequential/parallel/loop/debate), call the tool named `agent` with `mode` and `agents` instead.", IsError: true}, nil
	}
	args.Name = strings.TrimSpace(args.Name)
	if args.Name == "" {
		return tool.Result{Content: "skill name is required", IsError: true}, nil
	}
	root := effectiveRoot(ctx, t.root)
	sk, ok := skills.Load(root, t.dataDir, t.builtinEnabled, args.Name)
	if !ok {
		avail := skillNames(root, t.dataDir, t.builtinEnabled)
		msg := fmt.Sprintf("no skill named %q", args.Name)
		if avail != "" {
			msg += "; available skills: " + avail
		}
		return tool.Result{Content: msg, IsError: true}, nil
	}
	return tool.Result{Content: sk.Content}, nil
}

func skillNames(root, dataDir string, enabledBuiltins []string) string {
	var names []string
	for _, sk := range skills.Discover(root, dataDir, enabledBuiltins) {
		names = append(names, sk.Name)
	}
	return strings.Join(names, ", ")
}
