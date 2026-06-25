package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scottymacleod/agentharness/internal/diagram"
	"github.com/scottymacleod/agentharness/internal/tool"
)

type diagramTool struct {
	root     string
	krokiURL string
}

func (t *diagramTool) Name() string                { return "render_diagram" }
func (t *diagramTool) Capability() tool.Capability { return tool.CapWrite }
func (t *diagramTool) Description() string {
	return "Render a diagram from text into SVG, PNG, or a draw.io file. Supports mermaid, plantuml, c4plantuml, structurizr, graphviz, and more. Provide 'path' to save the result in the workspace; omit it to get SVG/draw.io inline. Renders via Kroki with a local CLI fallback."
}
func (t *diagramTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"type":{"type":"string","description":"notation: mermaid, plantuml, c4plantuml, structurizr, graphviz, ..."},"source":{"type":"string","description":"the diagram source text"},"format":{"type":"string","enum":["svg","png","pdf","drawio"],"description":"output format (default svg)"},"path":{"type":"string","description":"workspace-relative output path (optional)"}},"required":["type","source"]}`)
}

func (t *diagramTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Type   string `json:"type"`
		Source string `json:"source"`
		Format string `json:"format"`
		Path   string `json:"path"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.Format == "" {
		args.Format = diagram.FormatSVG
	}

	r := diagram.DefaultChain(t.krokiURL)
	data, _, err := diagram.Render(ctx, r, args.Type, args.Source, args.Format)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("render failed: %v", err), IsError: true}, nil
	}

	if args.Path == "" {
		if args.Format == diagram.FormatPNG || args.Format == diagram.FormatPDF {
			return tool.Result{Content: "binary output requires a 'path' to write to", IsError: true}, nil
		}
		return tool.Result{Content: string(data)}, nil
	}

	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return tool.Result{Content: fmt.Sprintf("mkdir failed: %v", err), IsError: true}, nil
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	return tool.Result{Content: fmt.Sprintf("rendered %s diagram (%s) to %s (%d bytes)", args.Type, args.Format, args.Path, len(data))}, nil
}
