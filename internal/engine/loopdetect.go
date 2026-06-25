package engine

import (
	"strings"

	"github.com/scottymacleod/aegis/internal/provider"
)

// loopDetector spots a stuck agent that issues the same tool calls turn after
// turn. It tracks the most recent turn signatures and reports a loop once the
// last `threshold` signatures are identical.
type loopDetector struct {
	threshold int
	recent    []string
}

func newLoopDetector(threshold int) *loopDetector {
	return &loopDetector{threshold: threshold}
}

// record adds a turn signature and reports whether a loop is now detected.
func (d *loopDetector) record(sig string) bool {
	d.recent = append(d.recent, sig)
	if len(d.recent) > d.threshold {
		d.recent = d.recent[len(d.recent)-d.threshold:]
	}
	if len(d.recent) < d.threshold {
		return false
	}
	for _, s := range d.recent[1:] {
		if s != d.recent[0] {
			return false
		}
	}
	return true
}

// turnSignature builds a stable signature for a turn's tool calls (names +
// inputs, in request order). Two turns with the same signature requested the
// exact same work — the hallmark of a loop.
func turnSignature(toolUses []provider.ToolUseBlock) string {
	var b strings.Builder
	for _, tu := range toolUses {
		b.WriteString(tu.Name)
		b.WriteByte('\x00')
		b.Write(tu.Input)
		b.WriteByte('\n')
	}
	return b.String()
}
