package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/tool"
)

// scopeTool declares (or clears) the per-task file-write scope enforced by
// permission.ScopeGate (P46.1): a mechanical allowlist of workspace-relative
// path globs that write_file/edit_file/multi_edit calls must stay within until
// the scope is cleared. It gives a multi-file build the "this task should only
// touch these files, refuse anything else" guarantee that no session-wide
// permission rule expresses — and, unlike prose in a persona or skill, one a
// model cannot skip under context pressure: an out-of-scope write is denied by
// the gate, not merely discouraged.
//
// Capability is CapRead: setting a scope only tightens what writes are allowed,
// touches no files, and performs no egress, so it should never itself be gated
// (it stays usable even in plan mode). Enforcement lives entirely in ScopeGate
// on the CapWrite calls it constrains.
type scopeTool struct{}

func (t *scopeTool) Name() string                { return "scope" }
func (t *scopeTool) Capability() tool.Capability { return tool.CapRead }
func (t *scopeTool) Description() string {
	return "Declare or clear a per-task file-write scope: an allowlist of workspace-relative path globs that " +
		"write_file/edit_file/multi_edit calls must stay within until you clear it. Once set, any attempt to " +
		"write a file outside the scope is refused by the permission gate, not just discouraged — use it at the " +
		"start of a task to bound the blast radius to exactly the files that task should touch, then clear it " +
		"when the task is done. Globs use \"*\"/\"?\" wildcards where \"*\" spans path separators (so \"src/**\" " +
		"and \"src/*\" both cover \"src/a/b.go\"). Actions: \"set\" (with paths) replaces the current scope; " +
		"\"clear\" removes it; \"show\" reports the active scope. Reads are never restricted."
}
func (t *scopeTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"action":{"type":"string","enum":["set","clear","show"],"description":"set replaces the scope with paths; clear removes it; show reports the current scope"},"paths":{"type":"array","items":{"type":"string"},"description":"workspace-relative path globs the task may write (action: set)"}},"required":["action"]}`)
}

func (t *scopeTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Action string   `json:"action"`
		Paths  []string `json:"paths"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	sc := permission.TaskScopeFromContext(ctx)
	if sc == nil {
		// No scope object on the context — the run isn't scope-aware (e.g. a
		// one-shot CLI path that doesn't inject one). Report clearly rather
		// than silently pretending a scope was set that nothing enforces.
		return tool.Result{Content: "task scope is not available in this run; writes are governed only by the session's permission mode and rules", IsError: true}, nil
	}

	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "set":
		var nonEmpty []string
		for _, p := range args.Paths {
			if strings.TrimSpace(p) != "" {
				nonEmpty = append(nonEmpty, strings.TrimSpace(p))
			}
		}
		if len(nonEmpty) == 0 {
			return tool.Result{Content: "action \"set\" requires at least one path glob", IsError: true}, nil
		}
		sc.Set(nonEmpty)
		return tool.Result{Content: fmt.Sprintf("task scope set — writes limited to: %s", strings.Join(sc.Patterns(), ", "))}, nil
	case "clear":
		sc.Clear()
		return tool.Result{Content: "task scope cleared — writes governed only by the session's permission mode and rules"}, nil
	case "show":
		if !sc.Active() {
			return tool.Result{Content: "no task scope is active — writes governed only by the session's permission mode and rules"}, nil
		}
		return tool.Result{Content: fmt.Sprintf("active task scope — writes limited to: %s", strings.Join(sc.Patterns(), ", "))}, nil
	default:
		return tool.Result{Content: fmt.Sprintf("unknown action %q (want set, clear, or show)", args.Action), IsError: true}, nil
	}
}
