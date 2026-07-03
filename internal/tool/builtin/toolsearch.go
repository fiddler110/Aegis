package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/tool"
)

// toolSearchTool implements deferred tool loading (P4.6). Most niche tools are
// registered as deferred: they appear only as a name+description line in the
// system prompt, keeping per-turn schema tokens low. This meta-tool searches
// the deferred set, exposes the matches so their schemas are offered on the
// next turn, and returns those schemas inline so the model can call them
// immediately.
type toolSearchTool struct{ reg *tool.Registry }

func (t *toolSearchTool) Name() string                { return "tool_search" }
func (t *toolSearchTool) Capability() tool.Capability { return tool.CapRead }
func (t *toolSearchTool) Description() string {
	return "Load additional tools that are not available by default. Pass a query describing the capability you need (e.g. \"latex diagram\", \"lsp definition\", \"cron schedule\"). Matching tools become callable and their schemas are returned."
}
func (t *toolSearchTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"query":{"type":"string","description":"keywords describing the tool capability you need"}},"required":["query"]}`)
}

func (t *toolSearchTool) Execute(_ context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	matches := t.reg.SearchDeferred(args.Query)
	if len(matches) == 0 {
		avail := t.reg.Deferred()
		if len(avail) == 0 {
			return tool.Result{Content: "no deferred tools match; all available tools are already loaded"}, nil
		}
		var names []string
		for _, in := range avail {
			names = append(names, in.Name)
		}
		return tool.Result{Content: fmt.Sprintf("no tools matched %q. Loadable tools: %s", args.Query, strings.Join(names, ", "))}, nil
	}

	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m.Name())
	}
	t.reg.Load(names...)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Loaded %d tool(s); they are now callable:\n", len(matches))
	for _, m := range matches {
		fmt.Fprintf(&sb, "\n### %s\n%s\ninput schema: %s\n", m.Name(), m.Description(), string(m.InputSchema()))
	}
	return tool.Result{Content: sb.String()}, nil
}
