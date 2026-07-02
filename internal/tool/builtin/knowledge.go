package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scottymacleod/aegis/internal/knowledge"
	"github.com/scottymacleod/aegis/internal/tool"
)

// --- project_knowledge ---

type projectKnowledgeTool struct {
	store *knowledge.Store
}

func (t *projectKnowledgeTool) Name() string                { return "project_knowledge" }
func (t *projectKnowledgeTool) Capability() tool.Capability { return tool.CapRead }
func (t *projectKnowledgeTool) Description() string {
	return "Search the project knowledge base — an FTS5 index of README files, documentation, and code comments. Use before reading individual files to quickly locate relevant context."
}
func (t *projectKnowledgeTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"query":{"type":"string","description":"natural language or keyword search query"},"limit":{"type":"integer","description":"max results to return (default 5, max 20)"}},"required":["query"]}`)
}
func (t *projectKnowledgeTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"results":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"title":{"type":"string"},"snippet":{"type":"string"},"score":{"type":"number"}}}},"count":{"type":"integer"}},"required":["results","count"]}`)
}
func (t *projectKnowledgeTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
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
	if args.Limit <= 0 || args.Limit > 20 {
		args.Limit = 5
	}

	results, err := t.store.Search(ctx, args.Query, args.Limit)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("search failed: %v", err), IsError: true}, nil
	}
	if len(results) == 0 {
		total, _ := t.store.DocCount(ctx)
		return tool.Result{Content: fmt.Sprintf("no results for %q (index contains %d documents — run `aegis knowledge index` to rebuild)", args.Query, total)}, nil
	}

	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n\n", i+1, r.Path, r.Title, r.Snippet)
	}
	return tool.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}

// KnowledgeTools returns the tools backed by a knowledge store.
func KnowledgeTools(store *knowledge.Store) []tool.Tool {
	return []tool.Tool{&projectKnowledgeTool{store: store}}
}
