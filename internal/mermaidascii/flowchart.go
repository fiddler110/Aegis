package mermaidascii

import (
	"regexp"
	"strings"
)

// linkRe matches one Mermaid edge operator between two node tokens, capturing
// an optional edge label. Alternatives are ordered most-specific first so the
// pipe- and dash-label forms win over the bare arrow forms.
var linkRe = regexp.MustCompile(`\s*(?:` +
	`-->\|([^|]*)\|` + // A -->|label| B
	`|--\s+(.+?)\s+-->` + // A -- label --> B
	`|==\s+(.+?)\s+==>` + // A == label ==> B
	`|-\.->` + // dotted arrow
	`|-\.-` + // dotted open
	`|==>` + // thick arrow
	`|===` + // thick open
	`|-->` + // arrow
	`|---` + // open
	`)\s*`)

// nodeTokenRe splits a node reference into its id and optional shape wrapper.
var nodeTokenRe = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*(\[\[.*\]\]|\(\(.*\)\)|\[.*\]|\(.*\)|\{.*\}|>.*\])?$`)

// skipPrefixes are statement keywords the renderer does not model; lines
// starting with one are ignored rather than failing the whole diagram.
var skipPrefixes = []string{
	"subgraph", "end", "classdef", "class ", "style ", "click ",
	"linkstyle", "direction ", "%%",
}

type fnode struct {
	id    string
	label string
}

type fedge struct {
	from, to string
	label    string
	dashed   bool
}

func renderFlowchart(src string) (string, bool) {
	dir := "TD"
	var order []string
	nodes := map[string]*fnode{}
	var edges []fedge

	ensure := func(id, label string) {
		n, ok := nodes[id]
		if !ok {
			nodes[id] = &fnode{id: id, label: label}
			order = append(order, id)
			return
		}
		// A later reference carrying a real label upgrades a bare id.
		if label != id && n.label == id {
			n.label = label
		}
	}

	lines := strings.Split(src, "\n")
	sawHeader := false
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !sawHeader {
			if strings.HasPrefix(lower, "graph") || strings.HasPrefix(lower, "flowchart") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if d := strings.ToUpper(fields[1]); isDir(d) {
						dir = d
					}
				}
				sawHeader = true
				continue
			}
		}
		if skipLine(lower) {
			continue
		}
		// Drop a trailing semicolon separator.
		line = strings.TrimSuffix(line, ";")

		ops := linkRe.FindAllStringSubmatch(line, -1)
		if len(ops) == 0 {
			if id, label, ok := parseNodeToken(line); ok {
				ensure(id, label)
			}
			continue
		}
		parts := linkRe.Split(line, -1)
		if len(parts) != len(ops)+1 {
			continue
		}
		var ids []string
		valid := true
		for _, p := range parts {
			id, label, ok := parseNodeToken(p)
			if !ok {
				valid = false
				break
			}
			ensure(id, label)
			ids = append(ids, id)
		}
		if !valid {
			continue
		}
		for i, op := range ops {
			label := op[1]
			if label == "" {
				label = op[2]
			}
			if label == "" {
				label = op[3]
			}
			edges = append(edges, fedge{
				from:   ids[i],
				to:     ids[i+1],
				label:  truncLabel(label),
				dashed: strings.Contains(op[0], "."),
			})
		}
		if len(order) > maxNodes {
			return "", false
		}
	}

	if len(order) == 0 || len(order) > maxNodes {
		return "", false
	}

	children := map[string][]string{}
	for _, e := range edges {
		children[e.from] = append(children[e.from], e.to)
	}
	layer := layize(order, children)
	horizontal := dir == "LR" || dir == "RL"
	if horizontal {
		return layoutHorizontal(order, nodes, edges, layer, dir == "RL"), true
	}
	return layoutVertical(order, nodes, edges, layer, dir == "BT"), true
}

func isDir(d string) bool {
	switch d {
	case "TD", "TB", "BT", "LR", "RL":
		return true
	}
	return false
}

func skipLine(lower string) bool {
	for _, p := range skipPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// parseNodeToken extracts the id and display label from a single node
// reference such as `A`, `A[Box]`, `B(Round)`, `C{Diamond}`, or `D((Circle))`.
func parseNodeToken(tok string) (id, label string, ok bool) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", "", false
	}
	m := nodeTokenRe.FindStringSubmatch(tok)
	if m == nil {
		return "", "", false
	}
	id = m[1]
	shape := m[2]
	if shape == "" {
		return id, id, true
	}
	inner := stripShape(shape)
	if inner == "" {
		inner = id
	}
	return id, truncLabel(inner), true
}

func stripShape(s string) string {
	trim := func(s, open, close string) (string, bool) {
		if strings.HasPrefix(s, open) && strings.HasSuffix(s, close) && len(s) >= len(open)+len(close) {
			return s[len(open) : len(s)-len(close)], true
		}
		return s, false
	}
	for _, pair := range [][2]string{
		{"[[", "]]"}, {"((", "))"}, {"[", "]"}, {"(", ")"}, {"{", "}"}, {">", "]"},
	} {
		if inner, ok := trim(s, pair[0], pair[1]); ok {
			s = inner
			break
		}
	}
	s = strings.TrimSpace(s)
	// Strip optional surrounding quotes.
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

// layize assigns a layer index to each node via longest-path from roots
// (indegree-0 nodes; the first-declared node if a cycle leaves none). Back
// edges are skipped so cycles terminate.
func layize(order []string, children map[string][]string) map[string]int {
	indeg := map[string]int{}
	for _, n := range order {
		indeg[n] = 0
	}
	for _, cs := range children {
		for _, c := range cs {
			indeg[c]++
		}
	}
	layer := map[string]int{}
	for _, n := range order {
		layer[n] = 0
	}
	var roots []string
	for _, n := range order {
		if indeg[n] == 0 {
			roots = append(roots, n)
		}
	}
	if len(roots) == 0 && len(order) > 0 {
		roots = []string{order[0]}
	}
	onStack := map[string]bool{}
	var dfs func(n string)
	dfs = func(n string) {
		onStack[n] = true
		for _, ch := range children[n] {
			if onStack[ch] {
				continue
			}
			if layer[ch] < layer[n]+1 {
				layer[ch] = layer[n] + 1
				dfs(ch)
			}
		}
		onStack[n] = false
	}
	for _, r := range roots {
		dfs(r)
	}
	return layer
}

// groupLayers buckets nodes by layer index, preserving declaration order within
// each layer.
func groupLayers(order []string, layer map[string]int) [][]string {
	maxL := 0
	for _, l := range layer {
		if l > maxL {
			maxL = l
		}
	}
	layers := make([][]string, maxL+1)
	for _, n := range order {
		l := layer[n]
		layers[l] = append(layers[l], n)
	}
	return layers
}

type placed struct {
	x, y, w, h int
}

func (p placed) cx() int     { return p.x + p.w/2 }
func (p placed) cy() int     { return p.y + p.h/2 }
func (p placed) bottom() int { return p.y + p.h - 1 }
func (p placed) right() int  { return p.x + p.w - 1 }

const (
	boxH   = 3
	vGap   = 2 // blank rows between stacked layers (vertical)
	hGap   = 3 // blank cols between boxes in a layer (vertical)
	colGap = 6 // blank cols between layers (horizontal)
	rowGap = 1 // blank rows between boxes in a column (horizontal)
)

func boxWidth(label string) int { return len([]rune(label)) + 4 }

func layoutVertical(order []string, nodes map[string]*fnode, edges []fedge, layer map[string]int, bottomToTop bool) string {
	layers := groupLayers(order, layer)
	widths := make([]int, len(layers))
	maxW := 1
	for i, ln := range layers {
		w := 0
		for j, id := range ln {
			if j > 0 {
				w += hGap
			}
			w += boxWidth(nodes[id].label)
		}
		widths[i] = w
		if w > maxW {
			maxW = w
		}
	}
	step := boxH + vGap
	height := (len(layers)-1)*step + boxH
	c := newCanvas(maxW, height)
	pos := map[string]placed{}
	for li, ln := range layers {
		startX := max((maxW-widths[li])/2, 0)
		drawLi := li
		if bottomToTop {
			drawLi = len(layers) - 1 - li
		}
		y := drawLi * step
		x := startX
		for _, id := range ln {
			w, h := drawBox(c, x, y, nodes[id].label)
			pos[id] = placed{x: x, y: y, w: w, h: h}
			x += w + hGap
		}
	}
	for _, e := range edges {
		pf, ok1 := pos[e.from]
		pt, ok2 := pos[e.to]
		if !ok1 || !ok2 {
			continue
		}
		var sx, sy, ex, ey int
		var head rune
		if pt.y < pf.y { // child drawn above parent
			sx, sy = pf.cx(), pf.y-1
			ex, ey = pt.cx(), pt.bottom()+1
			head = '▲'
		} else {
			sx, sy = pf.cx(), pf.bottom()+1
			ex, ey = pt.cx(), pt.y-1
			head = '▼'
		}
		c.connect(sx, sy, ex, ey, true, head, e.dashed)
		if e.label != "" {
			labelY := (sy + ey) / 2
			labelX := mini(sx, ex) + 1
			c.writeText(labelX, labelY-1, e.label)
		}
	}
	return c.String()
}

func layoutHorizontal(order []string, nodes map[string]*fnode, edges []fedge, layer map[string]int, rightToLeft bool) string {
	layers := groupLayers(order, layer)
	colW := make([]int, len(layers))
	heights := make([]int, len(layers))
	maxH := 1
	for i, ln := range layers {
		w, h := 0, 0
		for j, id := range ln {
			if j > 0 {
				h += rowGap
			}
			h += boxH
			if bw := boxWidth(nodes[id].label); bw > w {
				w = bw
			}
		}
		colW[i] = w
		heights[i] = h
		if h > maxH {
			maxH = h
		}
	}
	colX := make([]int, len(layers))
	x := 0
	for i := range layers {
		colX[i] = x
		x += colW[i] + colGap
	}
	width := 1
	if len(layers) > 0 {
		width = colX[len(layers)-1] + colW[len(layers)-1]
	}
	c := newCanvas(width, maxH)
	pos := map[string]placed{}
	for li, ln := range layers {
		startY := max((maxH-heights[li])/2, 0)
		drawLi := li
		if rightToLeft {
			drawLi = len(layers) - 1 - li
		}
		y := startY
		for _, id := range ln {
			w, h := drawBox(c, colX[drawLi], y, nodes[id].label)
			pos[id] = placed{x: colX[drawLi], y: y, w: w, h: h}
			y += boxH + rowGap
		}
	}
	for _, e := range edges {
		pf, ok1 := pos[e.from]
		pt, ok2 := pos[e.to]
		if !ok1 || !ok2 {
			continue
		}
		var sx, sy, ex, ey int
		var head rune
		if pt.x < pf.x { // child drawn left of parent
			sx, sy = pf.x-1, pf.cy()
			ex, ey = pt.right()+1, pt.cy()
			head = '◀'
		} else {
			sx, sy = pf.right()+1, pf.cy()
			ex, ey = pt.x-1, pt.cy()
			head = '▶'
		}
		c.connect(sx, sy, ex, ey, false, head, e.dashed)
		if e.label != "" {
			labelX := (sx + ex) / 2
			c.writeText(labelX-len([]rune(e.label))/2, mini(sy, ey)-1, e.label)
		}
	}
	return c.String()
}
