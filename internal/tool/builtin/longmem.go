package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scottymacleod/aegis/internal/longmem"
	"github.com/scottymacleod/aegis/internal/tool"
)

// --- entity_remember ---

type entityRememberTool struct {
	store   *longmem.Store
	project string
}

func (t *entityRememberTool) Name() string                { return "entity_remember" }
func (t *entityRememberTool) Capability() tool.Capability { return tool.CapWrite }
func (t *entityRememberTool) Description() string {
	return "Persist facts about a named entity (system, file, API, person, decision) to the long-term entity store so they are available in future sessions. Use for important discoveries about the target system or codebase."
}
func (t *entityRememberTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"entity_type":{"type":"string","description":"category of the entity (e.g. 'system', 'api', 'file', 'person', 'decision')"},"entity_name":{"type":"string","description":"unique identifier for the entity"},"facts":{"type":"string","description":"concise facts to store about this entity"}},"required":["entity_type","entity_name","facts"]}`)
}
func (t *entityRememberTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		EntityType string `json:"entity_type"`
		EntityName string `json:"entity_name"`
		Facts      string `json:"facts"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.EntityType) == "" || strings.TrimSpace(args.EntityName) == "" {
		return tool.Result{Content: "entity_type and entity_name are required", IsError: true}, nil
	}
	if strings.TrimSpace(args.Facts) == "" {
		return tool.Result{Content: "facts are required", IsError: true}, nil
	}
	if err := t.store.UpsertEntity(ctx, t.project, args.EntityType, args.EntityName, args.Facts); err != nil {
		return tool.Result{Content: fmt.Sprintf("failed to save entity: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("saved entity %s:%s", args.EntityType, args.EntityName)}, nil
}

// --- entity_recall ---

type entityRecallTool struct {
	store   *longmem.Store
	project string
}

func (t *entityRecallTool) Name() string                { return "entity_recall" }
func (t *entityRecallTool) Capability() tool.Capability { return tool.CapRead }
func (t *entityRecallTool) Description() string {
	return "Search the long-term entity memory for facts about systems, files, APIs, or decisions recorded in previous sessions."
}
func (t *entityRecallTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"query":{"type":"string","description":"keywords or entity name to search for"},"limit":{"type":"integer","description":"max results (default 5)"}},"required":["query"]}`)
}
func (t *entityRecallTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"results":{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string"},"key":{"type":"string"},"snippet":{"type":"string"},"score":{"type":"number"}}}}},"required":["results"]}`)
}
func (t *entityRecallTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return tool.Result{Content: "query is required", IsError: true}, nil
	}
	if args.Limit <= 0 {
		args.Limit = 5
	}

	results, err := t.store.SearchMemory(ctx, args.Query, args.Limit)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("search failed: %v", err), IsError: true}, nil
	}
	if len(results) == 0 {
		return tool.Result{Content: fmt.Sprintf("no long-term memory found for %q", args.Query)}, nil
	}

	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. [%s] %s\n   %s\n\n", i+1, r.Kind, r.Key, r.Snippet)
	}
	return tool.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}

// LongMemTools returns the entity memory tools backed by a longmem store.
func LongMemTools(store *longmem.Store) []tool.Tool {
	project := store.ProjectName()
	return []tool.Tool{
		&entityRememberTool{store: store, project: project},
		&entityRecallTool{store: store, project: project},
	}
}
