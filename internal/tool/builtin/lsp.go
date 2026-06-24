package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/scottymacleod/agentharness/internal/lsp"
	"github.com/scottymacleod/agentharness/internal/tool"
)

// LSPTools returns the LSP-powered code intelligence tools.
func LSPTools(mgr *lsp.Manager, root string) []tool.Tool {
	return []tool.Tool{
		&diagnosticsTool{mgr: mgr, root: root},
		&referencesTool{mgr: mgr, root: root},
	}
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
