package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fiddler110/aegis/internal/lsp"
	"github.com/fiddler110/aegis/internal/tool"
)

// LSPTools returns the LSP-powered code intelligence tools.
func LSPTools(mgr *lsp.Manager, root string) []tool.Tool {
	return []tool.Tool{
		&diagnosticsTool{mgr: mgr, root: root},
		&referencesTool{mgr: mgr, root: root},
		&definitionTool{mgr: mgr, root: root},
		&hoverTool{mgr: mgr, root: root},
		&documentSymbolsTool{mgr: mgr, root: root},
		&workspaceSymbolsTool{mgr: mgr, root: root},
		&callHierarchyTool{mgr: mgr, root: root},
	}
}

// lspOpenFile resolves a workspace path, finds its LSP client, and opens the
// document so position-based requests are reliable. Returns a friendly error
// result when no server is configured or the file cannot be read.
func lspOpenFile(ctx context.Context, mgr *lsp.Manager, root, path string) (*lsp.Client, string, *tool.Result) {
	abs, err := resolvePath(root, path)
	if err != nil {
		return nil, "", &tool.Result{Content: err.Error(), IsError: true}
	}
	client := mgr.ClientForFile(abs)
	if client == nil {
		return nil, "", &tool.Result{Content: fmt.Sprintf("no LSP server configured for %s", path), IsError: true}
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", &tool.Result{Content: fmt.Sprintf("cannot read %s: %v", path, err), IsError: true}
	}
	uri := lsp.FileURIFromPath(abs)
	_ = client.DidOpen(ctx, uri, string(content), 1)
	return client, uri, nil
}

// formatLocations renders a location list as workspace-relative file:line:col.
func formatLocations(root string, locs []lsp.Location) string {
	var sb strings.Builder
	for _, l := range locs {
		fmt.Fprintf(&sb, "  %s:%d:%d\n", uriToRelPath(root, l.URI), l.StartLine, l.StartCol)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatSymbols renders a symbol list with kind and location.
func formatSymbols(root string, syms []lsp.Symbol) string {
	var sb strings.Builder
	for _, s := range syms {
		fmt.Fprintf(&sb, "  %s %s — %s:%d:%d", s.Kind, s.Name, uriToRelPath(root, s.Location.URI), s.Location.StartLine, s.Location.StartCol)
		if s.Detail != "" {
			fmt.Fprintf(&sb, " (%s)", s.Detail)
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// --- definition ---

type definitionTool struct {
	mgr  *lsp.Manager
	root string
}

func (t *definitionTool) Name() string                { return "definition" }
func (t *definitionTool) Capability() tool.Capability { return tool.CapRead }
func (t *definitionTool) Description() string {
	return "Go to the definition of the symbol at a position (file, 1-based line and col) using the LSP server. Returns the defining location(s)."
}
func (t *definitionTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"line":{"type":"integer","description":"1-based line number"},"col":{"type":"integer","description":"1-based column number"}},"required":["path","line","col"]}`)
}
func (t *definitionTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Col  int    `json:"col"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	client, uri, errRes := lspOpenFile(ctx, t.mgr, t.root, args.Path)
	if errRes != nil {
		return *errRes, nil
	}
	locs, err := client.Definition(ctx, uri, args.Line, args.Col)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("lsp definition failed: %v", err), IsError: true}, nil
	}
	if len(locs) == 0 {
		return tool.Result{Content: "no definition found"}, nil
	}
	return tool.Result{Content: fmt.Sprintf("%d definition(s):\n%s", len(locs), formatLocations(t.root, locs))}, nil
}

// --- hover ---

type hoverTool struct {
	mgr  *lsp.Manager
	root string
}

func (t *hoverTool) Name() string                { return "hover" }
func (t *hoverTool) Capability() tool.Capability { return tool.CapRead }
func (t *hoverTool) Description() string {
	return "Get hover documentation (type signature, doc comment) for the symbol at a position (file, 1-based line and col) using the LSP server."
}
func (t *hoverTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"line":{"type":"integer","description":"1-based line number"},"col":{"type":"integer","description":"1-based column number"}},"required":["path","line","col"]}`)
}
func (t *hoverTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Col  int    `json:"col"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	client, uri, errRes := lspOpenFile(ctx, t.mgr, t.root, args.Path)
	if errRes != nil {
		return *errRes, nil
	}
	doc, err := client.Hover(ctx, uri, args.Line, args.Col)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("lsp hover failed: %v", err), IsError: true}, nil
	}
	if strings.TrimSpace(doc) == "" {
		return tool.Result{Content: "no hover information"}, nil
	}
	return tool.Result{Content: doc}, nil
}

// --- document symbols ---

type documentSymbolsTool struct {
	mgr  *lsp.Manager
	root string
}

func (t *documentSymbolsTool) Name() string                { return "document_symbols" }
func (t *documentSymbolsTool) Capability() tool.Capability { return tool.CapRead }
func (t *documentSymbolsTool) Description() string {
	return "List the symbols (functions, types, methods, variables) declared in a file using the LSP server."
}
func (t *documentSymbolsTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"}},"required":["path"]}`)
}
func (t *documentSymbolsTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	client, uri, errRes := lspOpenFile(ctx, t.mgr, t.root, args.Path)
	if errRes != nil {
		return *errRes, nil
	}
	syms, err := client.DocumentSymbols(ctx, uri)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("lsp documentSymbol failed: %v", err), IsError: true}, nil
	}
	if len(syms) == 0 {
		return tool.Result{Content: "no symbols found"}, nil
	}
	return tool.Result{Content: fmt.Sprintf("%d symbol(s):\n%s", len(syms), formatSymbols(t.root, syms))}, nil
}

// --- workspace symbols ---

type workspaceSymbolsTool struct {
	mgr  *lsp.Manager
	root string
}

func (t *workspaceSymbolsTool) Name() string                { return "workspace_symbols" }
func (t *workspaceSymbolsTool) Capability() tool.Capability { return tool.CapRead }
func (t *workspaceSymbolsTool) Description() string {
	return "Search the whole workspace for symbols by name (fuzzy) using the LSP server. Provide a query and a path to any file whose language server should handle it."
}
func (t *workspaceSymbolsTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"query":{"type":"string","description":"symbol name query (fuzzy)"},"path":{"type":"string","description":"any workspace-relative file path to select the language server"}},"required":["query","path"]}`)
}
func (t *workspaceSymbolsTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	client, _, errRes := lspOpenFile(ctx, t.mgr, t.root, args.Path)
	if errRes != nil {
		return *errRes, nil
	}
	syms, err := client.WorkspaceSymbols(ctx, args.Query)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("lsp workspace/symbol failed: %v", err), IsError: true}, nil
	}
	if len(syms) == 0 {
		return tool.Result{Content: "no symbols found"}, nil
	}
	return tool.Result{Content: fmt.Sprintf("%d symbol(s):\n%s", len(syms), formatSymbols(t.root, syms))}, nil
}

// --- call hierarchy ---

type callHierarchyTool struct {
	mgr  *lsp.Manager
	root string
}

func (t *callHierarchyTool) Name() string                { return "call_hierarchy" }
func (t *callHierarchyTool) Capability() tool.Capability { return tool.CapRead }
func (t *callHierarchyTool) Description() string {
	return "Find the callers of the function/method at a position (file, 1-based line and col) using the LSP call-hierarchy protocol."
}
func (t *callHierarchyTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"line":{"type":"integer","description":"1-based line number"},"col":{"type":"integer","description":"1-based column number"}},"required":["path","line","col"]}`)
}
func (t *callHierarchyTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Col  int    `json:"col"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	client, uri, errRes := lspOpenFile(ctx, t.mgr, t.root, args.Path)
	if errRes != nil {
		return *errRes, nil
	}
	callers, err := client.IncomingCalls(ctx, uri, args.Line, args.Col)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("lsp call hierarchy failed: %v", err), IsError: true}, nil
	}
	if len(callers) == 0 {
		return tool.Result{Content: "no callers found"}, nil
	}
	return tool.Result{Content: fmt.Sprintf("%d caller(s):\n%s", len(callers), formatSymbols(t.root, callers))}, nil
}

// --- diagnostics ---

type diagnosticsTool struct {
	mgr  *lsp.Manager
	root string
}

func (t *diagnosticsTool) Name() string                { return "diagnostics" }
func (t *diagnosticsTool) Capability() tool.Capability { return tool.CapRead }
func (t *diagnosticsTool) Description() string {
	return "Get LSP diagnostics (errors, warnings) for a file. " +
		"Requires an LSP server configured for the file's language."
}
func (t *diagnosticsTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"}},"required":["path"]}`)
}
func (t *diagnosticsTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}

	client := t.mgr.ClientForFile(abs)
	if client == nil {
		return tool.Result{Content: fmt.Sprintf("no LSP server configured for %s", args.Path), IsError: true}, nil
	}

	content, err := os.ReadFile(abs)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v", args.Path, err), IsError: true}, nil
	}

	uri := lsp.FileURIFromPath(abs)
	diags, err := client.Diagnostics(ctx, uri, string(content), 1)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("lsp diagnostics failed: %v", err), IsError: true}, nil
	}
	if len(diags) == 0 {
		return tool.Result{Content: "no diagnostics"}, nil
	}

	var sb strings.Builder
	for _, d := range diags {
		fmt.Fprintf(&sb, "%s:%d:%d [%s] %s", args.Path, d.Line, d.Col, d.Severity, d.Message)
		if d.Source != "" {
			fmt.Fprintf(&sb, " (%s)", d.Source)
		}
		sb.WriteString("\n")
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}

// --- references ---

type referencesTool struct {
	mgr  *lsp.Manager
	root string
}

func (t *referencesTool) Name() string                { return "references" }
func (t *referencesTool) Capability() tool.Capability { return tool.CapRead }
func (t *referencesTool) Description() string {
	return "Find all references to a symbol at a given position in a file using the LSP server. " +
		"Returns a list of locations (file:line:col)."
}
func (t *referencesTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"line":{"type":"integer","description":"1-based line number"},"col":{"type":"integer","description":"1-based column number"}},"required":["path","line","col"]}`)
}
func (t *referencesTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Col  int    `json:"col"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}

	client := t.mgr.ClientForFile(abs)
	if client == nil {
		return tool.Result{Content: fmt.Sprintf("no LSP server configured for %s", args.Path), IsError: true}, nil
	}

	uri := lsp.FileURIFromPath(abs)
	locs, err := client.References(ctx, uri, args.Line, args.Col)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("lsp references failed: %v", err), IsError: true}, nil
	}
	if len(locs) == 0 {
		return tool.Result{Content: "no references found"}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d reference(s):\n", len(locs))
	for _, l := range locs {
		path := uriToRelPath(t.root, l.URI)
		fmt.Fprintf(&sb, "  %s:%d:%d\n", path, l.StartLine, l.StartCol)
	}
	return tool.Result{Content: strings.TrimRight(sb.String(), "\n")}, nil
}

// uriToRelPath converts a file URI back to a workspace-relative path.
func uriToRelPath(root, uri string) string {
	const prefix = "file://"
	if !strings.HasPrefix(uri, prefix) {
		return uri
	}
	path := strings.TrimPrefix(uri, prefix)
	// Remove leading slash on Windows (file:///C:/...)
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	if strings.HasPrefix(path, root) {
		rel := strings.TrimPrefix(path, root)
		rel = strings.TrimPrefix(rel, "/")
		rel = strings.TrimPrefix(rel, "\\")
		if rel != "" {
			return rel
		}
	}
	return path
}
