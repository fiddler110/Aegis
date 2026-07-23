package mermaidascii

import (
	"strings"
	"testing"
)

func TestDiagramType(t *testing.T) {
	cases := map[string]string{
		"graph TD\nA-->B":                           "flowchart",
		"flowchart LR\nA-->B":                       "flowchart",
		"%% a comment\n\nsequenceDiagram\nA->>B: x": "sequence",
		"graph":                 "flowchart",
		"classDiagram\nClass01": "",
		"":                      "",
		"just some prose":       "",
	}
	for src, want := range cases {
		if got := DiagramType(src); got != want {
			t.Errorf("DiagramType(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestRenderFlowchartPinned pins the exact canvas for the smallest top-down
// graph so a layout regression is caught, not just "something rendered".
func TestRenderFlowchartPinned(t *testing.T) {
	out, ok := Render("graph TD\nA-->B")
	if !ok {
		t.Fatal("Render(graph TD A-->B) returned ok=false")
	}
	want := strings.Join([]string{
		"┌───┐",
		"│ A │",
		"└───┘",
		"  │",
		"  ▼",
		"┌───┐",
		"│ B │",
		"└───┘",
	}, "\n")
	if out != want {
		t.Fatalf("flowchart canvas mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

// TestRenderSequencePinned pins the exact canvas for the smallest sequence
// diagram (participant boxes, lifelines, a labelled arrow).
func TestRenderSequencePinned(t *testing.T) {
	out, ok := Render("sequenceDiagram\nA->>B: hi")
	if !ok {
		t.Fatal("Render(sequenceDiagram A->>B) returned ok=false")
	}
	want := strings.Join([]string{
		"┌───┐        ┌───┐",
		"│ A │        │ B │",
		"└───┘        └───┘",
		"  │    hi      │",
		"  │───────────▶│",
		"  │            │",
	}, "\n")
	if out != want {
		t.Fatalf("sequence canvas mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

func TestRenderFlowchartLR(t *testing.T) {
	out, ok := Render("graph LR\nA[One] --> B[Two] --> C[Three]")
	if !ok {
		t.Fatal("ok=false")
	}
	// Left-to-right: all three boxes share rows, joined by ▶ arrows.
	for _, sub := range []string{"One", "Two", "Three", "▶"} {
		if !strings.Contains(out, sub) {
			t.Errorf("LR output missing %q:\n%s", sub, out)
		}
	}
	if strings.Count(out, "▼") != 0 {
		t.Errorf("LR output should have no vertical arrows:\n%s", out)
	}
}

func TestRenderBranchJunction(t *testing.T) {
	// A parent fanning to two children must compose a ┴ T-junction, not leave a
	// plain corner from whichever edge was drawn last (the P40.9 fork's bug).
	out, ok := Render("graph TD\nA[Start] --> B{Choice}\nB -->|yes| C[Do it]\nB -->|no| D[Skip]")
	if !ok {
		t.Fatal("ok=false")
	}
	if !strings.Contains(out, "┴") {
		t.Errorf("expected a ┴ branch junction:\n%s", out)
	}
	for _, sub := range []string{"Start", "Choice", "Do it", "Skip"} {
		if !strings.Contains(out, sub) {
			t.Errorf("branch output missing node %q:\n%s", sub, out)
		}
	}
	// No corrupt double-junction glyphs.
	if strings.Contains(out, "┼┼") {
		t.Errorf("branch output has a corrupt junction run:\n%s", out)
	}
}

func TestRenderNodeShapes(t *testing.T) {
	// Every shape wrapper contributes its inner label as the box text.
	out, ok := Render("graph TD\nA[Box] --> B(Round) --> C{Diamond} --> D((Circle))")
	if !ok {
		t.Fatal("ok=false")
	}
	for _, sub := range []string{"Box", "Round", "Diamond", "Circle"} {
		if !strings.Contains(out, sub) {
			t.Errorf("output missing shape label %q:\n%s", sub, out)
		}
	}
}

func TestRenderEdgeLabelForms(t *testing.T) {
	// Labels are placed beside the connector where there is room (the
	// left-to-right layout guarantees it); a cramped single-column vertical
	// chain may omit them, which the renderer is allowed to do.
	for _, src := range []string{
		"graph LR\nA -->|go| B",
		"graph LR\nA -- go --> B",
	} {
		out, ok := Render(src)
		if !ok {
			t.Fatalf("ok=false for %q", src)
		}
		if !strings.Contains(out, "go") {
			t.Errorf("edge label 'go' missing for %q:\n%s", src, out)
		}
	}
}

func TestRenderSkipsDirectives(t *testing.T) {
	// classDef/style/subgraph/comment lines must not fail the whole render.
	src := strings.Join([]string{
		"graph TD",
		"%% a comment",
		"subgraph one",
		"A[Start] --> B[End]",
		"end",
		"classDef foo fill:#f00",
		"style A fill:#0f0",
	}, "\n")
	out, ok := Render(src)
	if !ok {
		t.Fatalf("directives should be skipped, got ok=false:\n%s", out)
	}
	if !strings.Contains(out, "Start") || !strings.Contains(out, "End") {
		t.Errorf("expected Start/End nodes:\n%s", out)
	}
}

func TestRenderRejects(t *testing.T) {
	cases := []string{
		"",                            // empty
		"   \n  ",                     // whitespace
		"classDiagram\nClass01",       // unsupported type
		"sequenceDiagram",             // no messages
		"graph TD\n%% only a comment", // no nodes
	}
	for _, src := range cases {
		if out, ok := Render(src); ok {
			t.Errorf("Render(%q) = ok=true, want false (out=%q)", src, out)
		}
	}
}

func TestRenderNodeCap(t *testing.T) {
	// A chain n0-->n1-->...-->n(maxNodes+4) declares more than maxNodes nodes.
	var chain strings.Builder
	chain.WriteString("graph TD\n")
	for i := range maxNodes + 4 {
		chain.WriteString("n" + itoa(i) + "-->n" + itoa(i+1) + "\n")
	}
	if _, ok := Render(chain.String()); ok {
		t.Error("expected ok=false for an over-cap node count")
	}
}

func TestRenderMessageCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")
	for i := range maxMessages + 5 {
		b.WriteString("A->>B: m" + itoa(i) + "\n")
	}
	if _, ok := Render(b.String()); ok {
		t.Error("expected ok=false for an over-cap message count")
	}
}

func TestRenderHandlesCRLF(t *testing.T) {
	out, ok := Render("graph TD\r\nA[Start]\r\nA-->B[End]\r\n")
	if !ok {
		t.Fatalf("CRLF input should render, got ok=false:\n%s", out)
	}
	if !strings.Contains(out, "Start") || !strings.Contains(out, "End") {
		t.Errorf("CRLF render missing nodes:\n%s", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("output leaked a carriage return:\n%q", out)
	}
}

// itoa is a tiny dependency-free int→string for the cap tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
