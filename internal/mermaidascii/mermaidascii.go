// Package mermaidascii renders a useful subset of Mermaid diagram source
// (flowchart/graph and sequenceDiagram) into a terminal-friendly box-drawing
// ASCII string. It is best-effort and dependency-free: it never panics and
// never returns an error — an unsupported or unparseable diagram simply yields
// ok=false so callers can fall back to showing the raw source.
package mermaidascii

import (
	"strings"
)

// Output guards: refuse pathological inputs rather than emit megabytes.
const (
	maxNodes    = 60
	maxMessages = 80
	maxLabel    = 40 // node/message labels are truncated to this rune count
)

// DiagramType classifies src by its header line, returning "flowchart",
// "sequence", or "" when the type is unknown/unsupported.
func DiagramType(src string) string {
	for raw := range strings.SplitSeq(src, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(raw, "\r"))
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "graph "), lower == "graph",
			strings.HasPrefix(lower, "flowchart "), lower == "flowchart":
			return "flowchart"
		case strings.HasPrefix(lower, "sequencediagram"):
			return "sequence"
		default:
			// First meaningful line wasn't a recognized header.
			return ""
		}
	}
	return ""
}

// Render converts Mermaid source to a box-drawing ASCII diagram. ok is false
// when the source is empty, an unsupported diagram type, or cannot be parsed;
// callers then fall back to showing the raw code block. It never panics.
func Render(src string) (out string, ok bool) {
	if strings.TrimSpace(src) == "" {
		return "", false
	}
	switch DiagramType(src) {
	case "flowchart":
		return renderFlowchart(src)
	case "sequence":
		return renderSequence(src)
	default:
		return "", false
	}
}

// --- canvas ---

// canvas is a fixed-size grid of runes, space-filled, used to composite boxes
// and connectors before serializing to a string.
type canvas struct {
	rows  [][]rune
	occ   [][]bool  // cells occupied by a box, so connectors don't overwrite them
	jmask [][]uint8 // accumulated junction directions per cell (see junction)
}

// Junction direction bits, OR-ed together so that two connectors meeting at a
// cell (e.g. one parent branching to several children) compose into the right
// T- or cross-junction glyph instead of the last writer clobbering the first.
const (
	dUp uint8 = 1 << iota
	dDown
	dLeft
	dRight
)

func newCanvas(w, h int) *canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	c := &canvas{
		rows:  make([][]rune, h),
		occ:   make([][]bool, h),
		jmask: make([][]uint8, h),
	}
	for i := range c.rows {
		c.rows[i] = make([]rune, w)
		c.occ[i] = make([]bool, w)
		c.jmask[i] = make([]uint8, w)
		for j := range c.rows[i] {
			c.rows[i][j] = ' '
		}
	}
	return c
}

func (c *canvas) inBounds(x, y int) bool {
	return y >= 0 && y < len(c.rows) && x >= 0 && x < len(c.rows[y])
}

func (c *canvas) set(x, y int, r rune) {
	if c.inBounds(x, y) {
		c.rows[y][x] = r
	}
}

// setLine writes a connector rune unless the cell is occupied by a box.
func (c *canvas) setLine(x, y int, r rune) {
	if c.inBounds(x, y) && !c.occ[y][x] {
		c.rows[y][x] = r
	}
}

func (c *canvas) String() string {
	var sb strings.Builder
	// Trim trailing all-blank lines.
	last := len(c.rows) - 1
	for last >= 0 && blankRow(c.rows[last]) {
		last--
	}
	for i := 0; i <= last; i++ {
		row := c.rows[i]
		end := len(row)
		for end > 0 && row[end-1] == ' ' {
			end--
		}
		sb.WriteString(string(row[:end]))
		if i < last {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func blankRow(row []rune) bool {
	for _, r := range row {
		if r != ' ' {
			return false
		}
	}
	return true
}

// drawBox renders a bordered box whose interior text is label, with the
// top-left corner at (x,y). It returns the box width and height in cells and
// marks the covered cells occupied.
func drawBox(c *canvas, x, y int, label string) (w, h int) {
	lbl := []rune(label)
	inner := len(lbl) + 2 // one space of padding each side
	w = inner + 2         // borders
	h = 3
	// top border
	c.set(x, y, '┌')
	for i := 1; i < w-1; i++ {
		c.set(x+i, y, '─')
	}
	c.set(x+w-1, y, '┐')
	// middle
	c.set(x, y+1, '│')
	c.set(x+1, y+1, ' ')
	for i, r := range lbl {
		c.set(x+2+i, y+1, r)
	}
	c.set(x+2+len(lbl), y+1, ' ')
	c.set(x+w-1, y+1, '│')
	// bottom border
	c.set(x, y+2, '└')
	for i := 1; i < w-1; i++ {
		c.set(x+i, y+2, '─')
	}
	c.set(x+w-1, y+2, '┘')
	// mark occupancy
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			if c.inBounds(x+dx, y+dy) {
				c.occ[y+dy][x+dx] = true
			}
		}
	}
	return w, h
}

// connect draws an orthogonal connector from the exit cell (sx,sy) just outside
// the parent box to the head cell (ex,ey) just outside the child box, ending
// with the head rune. When vertical is true the connector leaves/enters along
// the row axis (TD/BT); otherwise along the column axis (LR/RL). dashed selects
// dotted line runes.
func (c *canvas) connect(sx, sy, ex, ey int, vertical bool, head rune, dashed bool) {
	vch, hch := '│', '─'
	if dashed {
		vch, hch = '╎', '╌'
	}
	if vertical {
		// Snap a near-vertical run to a straight line: the one- or two-cell
		// jog looks like corruption (┼┼) and reads worse than a clean │.
		if abs(sx-ex) <= 1 {
			ex = sx
		}
		if sx == ex {
			for y := mini(sy, ey); y <= maxi(sy, ey); y++ {
				if y == ey {
					c.setLine(sx, y, head)
				} else {
					c.setLine(sx, y, vch)
				}
			}
			c.set(ex, ey, head)
			return
		}
		mid := (sy + ey) / 2
		// vertical from sy to mid at col sx
		for y := mini(sy, mid); y <= maxi(sy, mid); y++ {
			c.setLine(sx, y, vch)
		}
		// horizontal along mid from sx to ex
		for x := mini(sx, ex); x <= maxi(sx, ex); x++ {
			c.setLine(x, mid, hch)
		}
		// vertical from mid to ey at col ex
		for y := mini(mid, ey); y <= maxi(mid, ey); y++ {
			c.setLine(ex, y, vch)
		}
		// corners: (sx,mid) turns from the vertical above toward ex; (ex,mid)
		// turns from the horizontal down toward the child.
		c.junction(sx, mid, cornerDirs(true, false, ex > sx, ex < sx))
		c.junction(ex, mid, cornerDirs(false, true, sx > ex, sx < ex))
		c.set(ex, ey, head)
		return
	}
	// horizontal primary axis
	if abs(sy-ey) <= 1 {
		ey = sy
	}
	if sy == ey {
		for x := mini(sx, ex); x <= maxi(sx, ex); x++ {
			if x == ex {
				c.setLine(x, sy, head)
			} else {
				c.setLine(x, sy, hch)
			}
		}
		c.set(ex, ey, head)
		return
	}
	mid := (sx + ex) / 2
	for x := mini(sx, mid); x <= maxi(sx, mid); x++ {
		c.setLine(x, sy, hch)
	}
	for y := mini(sy, ey); y <= maxi(sy, ey); y++ {
		c.setLine(mid, y, vch)
	}
	for x := mini(mid, ex); x <= maxi(mid, ex); x++ {
		c.setLine(x, ey, hch)
	}
	c.junction(mid, sy, cornerDirs(false, ey > sy, true, false))
	c.junction(mid, ey, cornerDirs(ey < sy, false, false, true))
	c.set(ex, ey, head)
}

// cornerDirs packs which of the four directions a junction connects into a
// direction mask (up/down/right/left report whether a segment leaves the
// junction that way), for junction() to compose and resolve to a glyph.
func cornerDirs(up, down, right, left bool) uint8 {
	var d uint8
	if up {
		d |= dUp
	}
	if down {
		d |= dDown
	}
	if right {
		d |= dRight
	}
	if left {
		d |= dLeft
	}
	return d
}

// dirsRune resolves an accumulated direction mask to the box-drawing glyph
// that connects exactly those sides.
func dirsRune(d uint8) rune {
	switch d {
	case dUp | dDown | dLeft | dRight:
		return '┼'
	case dUp | dLeft | dRight:
		return '┴'
	case dDown | dLeft | dRight:
		return '┬'
	case dUp | dDown | dRight:
		return '├'
	case dUp | dDown | dLeft:
		return '┤'
	case dUp | dRight:
		return '└'
	case dUp | dLeft:
		return '┘'
	case dDown | dRight:
		return '┌'
	case dDown | dLeft:
		return '┐'
	case dUp | dDown:
		return '│'
	case dLeft | dRight:
		return '─'
	case dUp:
		return '│'
	case dDown:
		return '│'
	case dLeft:
		return '─'
	case dRight:
		return '─'
	}
	return '┼'
}

// writeText writes s onto the canvas starting at (x,y), skipping any cell that
// is occupied by a box. It reports whether every rune was written cleanly.
func (c *canvas) writeText(x, y int, s string) bool {
	clean := true
	for i, r := range []rune(s) {
		cx := x + i
		if !c.inBounds(cx, y) || c.occ[y][cx] {
			clean = false
			continue
		}
		c.rows[y][cx] = r
	}
	return clean
}

func truncLabel(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > maxLabel {
		return string(r[:maxLabel-1]) + "…"
	}
	return s
}

// junction records that connector segments leave (x,y) in the given
// directions and (re)draws the composed box-drawing glyph. OR-ing the mask
// across calls is what lets several edges meeting at one cell — a parent
// fanning out to multiple children — resolve to a T- or cross-junction (┴ ┬ ┼)
// rather than the last edge's plain corner clobbering the earlier ones. A box
// cell is never overwritten.
func (c *canvas) junction(x, y int, dirs uint8) {
	if !c.inBounds(x, y) || c.occ[y][x] {
		return
	}
	c.jmask[y][x] |= dirs
	c.rows[y][x] = dirsRune(c.jmask[y][x])
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
