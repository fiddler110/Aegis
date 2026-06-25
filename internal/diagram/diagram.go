// Package diagram renders architecture and threat-model diagrams from text in
// multiple notations (Mermaid, PlantUML, C4/Structurizr, Graphviz, …) to SVG
// or PNG, plus a draw.io export. Rendering goes through Kroki, with a CLI
// fallback for the common notations when offline.
package diagram

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Common diagram types (passed through to the renderer).
const (
	TypeMermaid     = "mermaid"
	TypePlantUML    = "plantuml"
	TypeC4PlantUML  = "c4plantuml"
	TypeStructurizr = "structurizr"
	TypeGraphviz    = "graphviz"
)

// Output formats.
const (
	FormatSVG    = "svg"
	FormatPNG    = "png"
	FormatPDF    = "pdf"
	FormatDrawIO = "drawio"
)

// knownTypes are the diagram notations the harness advertises. Others are
// still forwarded to Kroki, which supports many more.
var knownTypes = map[string]bool{
	TypeMermaid: true, TypePlantUML: true, TypeC4PlantUML: true,
	TypeStructurizr: true, TypeGraphviz: true, "dot": true, "erd": true,
	"bpmn": true, "excalidraw": true, "d2": true, "nomnoml": true,
}

// Renderer turns diagram source into rendered bytes.
type Renderer interface {
	Name() string
	// Render returns the rendered bytes and their MIME content type.
	Render(ctx context.Context, dtype, source, format string) ([]byte, string, error)
}

// Render dispatches to the chain (Kroki, then CLI fallback) and additionally
// implements the synthetic "drawio" format by wrapping a rendered SVG.
func Render(ctx context.Context, r Renderer, dtype, source, format string) ([]byte, string, error) {
	dtype = strings.ToLower(strings.TrimSpace(dtype))
	format = strings.ToLower(strings.TrimSpace(format))
	if dtype == "" {
		return nil, "", fmt.Errorf("diagram type is required")
	}
	if strings.TrimSpace(source) == "" {
		return nil, "", fmt.Errorf("diagram source is empty")
	}
	if format == "" {
		format = FormatSVG
	}

	if format == FormatDrawIO {
		svg, _, err := r.Render(ctx, dtype, source, FormatSVG)
		if err != nil {
			return nil, "", err
		}
		return wrapDrawIO(svg), "application/xml", nil
	}
	return r.Render(ctx, dtype, source, format)
}

// IsKnownType reports whether the harness recognizes the notation by name.
func IsKnownType(dtype string) bool {
	return knownTypes[strings.ToLower(strings.TrimSpace(dtype))]
}

// --- Kroki renderer ---

// Kroki renders via a Kroki server.
type Kroki struct {
	base   string
	client *http.Client
}

// NewKroki builds a Kroki renderer for the given base URL.
func NewKroki(base string) *Kroki {
	return &Kroki{
		base:   strings.TrimRight(base, "/"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (k *Kroki) Name() string { return "kroki" }

func (k *Kroki) Render(ctx context.Context, dtype, source, format string) ([]byte, string, error) {
	endpoint := fmt.Sprintf("%s/%s/%s", k.base, dtype, format)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(source))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("kroki request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("kroki error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// --- CLI fallback renderer ---

// CLI renders the common notations using locally installed tools (mmdc for
// Mermaid, plantuml for PlantUML). Unsupported combinations return an error so
// a chain can move on.
type CLI struct{}

func (CLI) Name() string { return "cli" }

func (CLI) Render(ctx context.Context, dtype, source, format string) ([]byte, string, error) {
	switch dtype {
	case TypeMermaid:
		return renderMermaidCLI(ctx, source, format)
	case TypePlantUML, TypeC4PlantUML:
		return renderPlantUMLCLI(ctx, source, format)
	default:
		return nil, "", fmt.Errorf("cli renderer does not support %q", dtype)
	}
}

func renderMermaidCLI(ctx context.Context, source, format string) ([]byte, string, error) {
	if _, err := exec.LookPath("mmdc"); err != nil {
		return nil, "", fmt.Errorf("mmdc (mermaid-cli) not installed")
	}
	in, err := os.CreateTemp("", "diagram-*.mmd")
	if err != nil {
		return nil, "", err
	}
	defer os.Remove(in.Name())
	if _, err := in.WriteString(source); err != nil {
		return nil, "", err
	}
	in.Close()
	out := in.Name() + "." + format
	defer os.Remove(out)

	cmd := exec.CommandContext(ctx, "mmdc", "-i", in.Name(), "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("mmdc: %v: %s", err, strings.TrimSpace(string(b)))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, "", err
	}
	return data, mimeFor(format), nil
}

func renderPlantUMLCLI(ctx context.Context, source, format string) ([]byte, string, error) {
	if _, err := exec.LookPath("plantuml"); err != nil {
		return nil, "", fmt.Errorf("plantuml not installed")
	}
	cmd := exec.CommandContext(ctx, "plantuml", "-t"+format, "-pipe")
	cmd.Stdin = strings.NewReader(source)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("plantuml: %w", err)
	}
	return out.Bytes(), mimeFor(format), nil
}

// --- chain ---

// Chain tries each renderer in order, returning the first success.
type Chain struct {
	renderers []Renderer
}

// NewChain builds a renderer chain.
func NewChain(rs ...Renderer) *Chain { return &Chain{renderers: rs} }

func (c *Chain) Name() string { return "chain" }

func (c *Chain) Render(ctx context.Context, dtype, source, format string) ([]byte, string, error) {
	var errs []string
	for _, r := range c.renderers {
		data, ct, err := r.Render(ctx, dtype, source, format)
		if err == nil {
			return data, ct, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", r.Name(), err))
	}
	return nil, "", fmt.Errorf("all renderers failed: %s", strings.Join(errs, "; "))
}

// DefaultChain renders via Kroki first, then the local CLI tools.
func DefaultChain(krokiURL string) *Chain {
	return NewChain(NewKroki(krokiURL), CLI{})
}

// --- helpers ---

func mimeFor(format string) string {
	switch format {
	case FormatSVG:
		return "image/svg+xml"
	case FormatPNG:
		return "image/png"
	case FormatPDF:
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

// wrapDrawIO embeds a rendered SVG into a minimal, openable .drawio file as an
// image cell. The diagram remains viewable and movable in diagrams.net.
func wrapDrawIO(svg []byte) []byte {
	dataURI := "data:image/svg+xml," + url.PathEscape(string(svg))
	const tmpl = `<mxfile host="agentharness" type="device">
  <diagram id="agentharness-diagram" name="Diagram">
    <mxGraphModel dx="800" dy="600" grid="0" gridSize="10" guides="1" tooltips="1" connect="1" arrows="1" fold="1" page="1" pageScale="1" pageWidth="850" pageHeight="1100" math="0" shadow="0">
      <root>
        <mxCell id="0" />
        <mxCell id="1" parent="0" />
        <mxCell id="2" parent="1" vertex="1" style="shape=image;verticalLabelPosition=bottom;labelBackgroundColor=#ffffff;verticalAlign=top;aspect=fixed;imageAspect=0;image=%s;">
          <mxGeometry x="40" y="40" width="760" height="560" as="geometry" />
        </mxCell>
      </root>
    </mxGraphModel>
  </diagram>
</mxfile>`
	return fmt.Appendf(nil, tmpl, dataURI)
}
