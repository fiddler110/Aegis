package mermaidascii

import (
	"regexp"
	"strings"
)

// seqMsgRe matches a sequence-diagram message line: A->>B: text and its
// solid/dotted/open variants. Arrow group distinguishes dotted (contains '.').
var seqMsgRe = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*(-?->>?|--?>>?)\s*([A-Za-z0-9_]+)\s*:\s*(.*)$`)

// seqParticipantRe matches `participant X` / `participant X as Alias` and the
// `actor` synonym.
var seqParticipantRe = regexp.MustCompile(`^(?:participant|actor)\s+([A-Za-z0-9_]+)(?:\s+as\s+(.+))?$`)

type seqMsg struct {
	from, to string
	label    string
	dashed   bool
}

func renderSequence(src string) (string, bool) {
	var order []string
	labels := map[string]string{}
	ensure := func(id string) {
		if _, ok := labels[id]; !ok {
			labels[id] = id
			order = append(order, id)
		}
	}

	var msgs []seqMsg
	for raw := range strings.SplitSeq(src, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		if strings.EqualFold(line, "sequencediagram") {
			continue
		}
		if m := seqParticipantRe.FindStringSubmatch(line); m != nil {
			ensure(m[1])
			if m[2] != "" {
				labels[m[1]] = truncLabel(strings.Trim(m[2], `"`))
			}
			continue
		}
		if m := seqMsgRe.FindStringSubmatch(line); m != nil {
			ensure(m[1])
			ensure(m[3])
			msgs = append(msgs, seqMsg{
				from:   m[1],
				to:     m[3],
				label:  truncLabel(m[4]),
				dashed: strings.Contains(m[2], "--"),
			})
			if len(msgs) > maxMessages {
				return "", false
			}
			continue
		}
		// Unmodeled directives (note, loop, alt, activate, ...) are skipped.
	}

	if len(order) == 0 || len(msgs) == 0 {
		return "", false
	}
	if len(order) > maxNodes || len(msgs) > maxMessages {
		return "", false
	}
	return layoutSequence(order, labels, msgs), true
}

func layoutSequence(order []string, labels map[string]string, msgs []seqMsg) string {
	// Column centers: box widths plus a gap wide enough for the longest label.
	maxLabelLen := 0
	for _, m := range msgs {
		if n := len([]rune(m.label)); n > maxLabelLen {
			maxLabelLen = n
		}
	}
	gap := max(maxLabelLen+4, 8)
	center := map[string]int{}
	boxLeft := map[string]int{}
	boxW := map[string]int{}
	x := 0
	for _, id := range order {
		w := boxWidth(labels[id])
		boxLeft[id] = x
		boxW[id] = w
		center[id] = x + w/2
		x += w + gap
	}
	width := max(x-gap, 1)

	// Height: 3 rows of participant box + 2 rows per message + tail.
	top := boxH
	rowsPerMsg := 2
	height := top + len(msgs)*rowsPerMsg + 1
	c := newCanvas(width, height)

	// Participant boxes.
	for _, id := range order {
		drawBox(c, boxLeft[id], 0, labels[id])
	}
	// Lifelines down the full body.
	for _, id := range order {
		col := center[id]
		for y := top; y < height; y++ {
			c.setLine(col, y, '│')
		}
	}
	// Messages.
	for i, m := range msgs {
		arrowY := top + i*rowsPerMsg + 1
		labelY := arrowY - 1
		cf, ct := center[m.from], center[m.to]
		if m.from == m.to {
			// Self-message: a short stub to the right of the lifeline.
			c.set(cf+1, labelY, '─')
			c.set(cf+2, labelY, '┐')
			c.set(cf+2, arrowY, '┘')
			c.set(cf+1, arrowY, '◀')
			c.writeText(cf+4, arrowY, m.label)
			continue
		}
		hch := '─'
		if m.dashed {
			hch = '╌'
		}
		lo, hi := mini(cf, ct), maxi(cf, ct)
		for xx := lo + 1; xx < hi; xx++ {
			c.setLine(xx, arrowY, hch)
		}
		if ct > cf {
			c.set(ct-1, arrowY, '▶')
		} else {
			c.set(cf-1, arrowY, '◀')
		}
		// Label centered over the arrow span.
		lbl := m.label
		if lbl != "" {
			ll := len([]rune(lbl))
			lx := (cf+ct)/2 - ll/2
			if lx <= lo {
				lx = lo + 1
			}
			c.writeText(lx, labelY, lbl)
		}
	}
	return c.String()
}
