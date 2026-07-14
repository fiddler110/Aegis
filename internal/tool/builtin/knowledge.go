package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/knowledge"
	"github.com/fiddler110/aegis/internal/tool"
)

// KnowledgeProvider resolves the knowledge store for a given root directory
// (P25.9), letting project_knowledge follow a session's own Workdir instead
// of always querying the daemon's default-workspace store. Implemented by
// *server.Server; passed in via Options.KnowledgeProvider so this package
// doesn't need to import internal/server.
type KnowledgeProvider interface {
	KnowledgeStoreFor(root string) (*knowledge.Store, error)
}

// KnowledgeProviderFunc adapts a plain function to KnowledgeProvider.
type KnowledgeProviderFunc func(root string) (*knowledge.Store, error)

func (f KnowledgeProviderFunc) KnowledgeStoreFor(root string) (*knowledge.Store, error) {
	return f(root)
}

// --- project_knowledge ---

type projectKnowledgeTool struct {
	store    *knowledge.Store // fallback store, used when provider is nil or errors
	provider KnowledgeProvider
	root     string // fallback root, used when no per-call context workdir is set
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

	store := t.storeFor(ctx)
	results, err := store.Search(ctx, args.Query, args.Limit)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("search failed: %v", err), IsError: true}, nil
	}
	if len(results) == 0 {
		total, _ := store.DocCount(ctx)
		return tool.Result{Content: fmt.Sprintf("no results for %q (index contains %d documents — run `aegis knowledge index` to rebuild)", args.Query, total)}, nil
	}

	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n\n", i+1, r.Path, r.Title, r.Snippet)
	}
	return tool.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}

// storeFor resolves the knowledge store for this call's effective root: the
// session-scoped store via provider when one is wired and resolves cleanly,
// else the fixed fallback store (today's pre-P25.9 behavior).
func (t *projectKnowledgeTool) storeFor(ctx context.Context) *knowledge.Store {
	if t.provider != nil {
		root := effectiveRoot(ctx, t.root)
		if store, err := t.provider.KnowledgeStoreFor(root); err == nil && store != nil {
			return store
		}
	}
	return t.store
}

// KnowledgeTools returns the tools backed by a knowledge store. provider, when
// non-nil, resolves a session-scoped store per call (P25.9); store is the
// fallback used when provider is nil or a lookup fails, and root is the
// fallback root used when no context workdir is set.
func KnowledgeTools(store *knowledge.Store, provider KnowledgeProvider, root string) []tool.Tool {
	return []tool.Tool{&projectKnowledgeTool{store: store, provider: provider, root: root}}
}
