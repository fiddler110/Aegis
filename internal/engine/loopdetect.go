package engine

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/fiddler110/aegis/internal/provider"
)

// maxLoopPeriod bounds the cycle lengths loopDetector checks for, beyond the
// plain "all identical" (period 1) case it originally supported. A model
// alternating between two (or a few) distinct tool calls — A, B, A, B, … —
// is just as stuck as one repeating a single call, but exact "last N
// signatures are all equal" matching never fires on that pattern.
const maxLoopPeriod = 4

// loopDetector spots a stuck agent that issues the same tool calls turn after
// turn — either the exact same call every time, or a short cycle of a few
// distinct calls repeating (period 1 through maxLoopPeriod).
type loopDetector struct {
	threshold int
	recent    []string
}

func newLoopDetector(threshold int) *loopDetector {
	return &loopDetector{threshold: threshold}
}

// record adds a turn signature and reports whether a loop is now detected.
// For each candidate period p (1..maxLoopPeriod), it looks for at least
// threshold/p repetitions of a length-p block at the end of the window —
// threshold/1 = threshold repeats of a single call (the original behavior),
// threshold/2 repeats of an alternating pair, and so on. A period needing
// fewer than 2 repeats to reach that confidence is skipped: one or one and a
// half repeats of a longer cycle isn't yet distinguishable from ordinary,
// varied work.
func (d *loopDetector) record(sig string) bool {
	d.recent = append(d.recent, sig)
	maxWindow := d.threshold * maxLoopPeriod
	if len(d.recent) > maxWindow {
		d.recent = d.recent[len(d.recent)-maxWindow:]
	}

	for period := 1; period <= maxLoopPeriod; period++ {
		repeats := d.threshold / period
		if repeats < 2 {
			continue
		}
		span := period * repeats
		if len(d.recent) < span {
			continue
		}
		if isRepeatingCycle(d.recent[len(d.recent)-span:], period) {
			return true
		}
	}
	return false
}

// isRepeatingCycle reports whether window consists entirely of a repeating
// block of length period, i.e. window[i] == window[i+period] for every valid
// i. period == 1 reduces to "every element is identical".
func isRepeatingCycle(window []string, period int) bool {
	for i := 0; i+period < len(window); i++ {
		if window[i] != window[i+period] {
			return false
		}
	}
	return true
}

// turnSignature builds a stable signature for a turn's tool calls (names +
// canonicalized inputs, in request order). Two turns with the same signature
// requested the exact same work — the hallmark of a loop.
func turnSignature(toolUses []provider.ToolUseBlock) string {
	var b strings.Builder
	for _, tu := range toolUses {
		b.WriteString(tu.Name)
		b.WriteByte('\x00')
		b.Write(canonicalizeToolInput(tu.Input))
		b.WriteByte('\n')
	}
	return b.String()
}

// looksVolatile reports whether a JSON scalar's string form looks like a
// per-call nonce rather than meaningful content the model chose: an RFC3339-ish
// timestamp, a UUID, or a long run of digits/hex (epoch millis/nanos, random
// nonce). Exact-string matching on raw tool input otherwise means a single
// varying byte from a field like this defeats loop detection entirely, even
// though the call is otherwise identical turn after turn.
var (
	timestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	uuidRe      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	longDigitRe = regexp.MustCompile(`^\d{9,}$`)
	longHexRe   = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)
)

func looksVolatile(s string) bool {
	return timestampRe.MatchString(s) || uuidRe.MatchString(s) || longDigitRe.MatchString(s) || longHexRe.MatchString(s)
}

// canonicalizeToolInput parses a tool call's raw JSON input and rewrites any
// scalar leaf that looksVolatile to a fixed placeholder, so loop detection
// compares on the call's meaningful content rather than being defeated by an
// incidental nonce/timestamp field. Object keys are also normalized to a
// stable order via json.Marshal's map handling. Input that isn't valid JSON
// (or isn't an object/array) is returned unchanged.
func canonicalizeToolInput(input json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		return input
	}
	out, err := json.Marshal(canonicalizeValue(v))
	if err != nil {
		return input
	}
	return out
}

const volatilePlaceholder = "‹volatile›"

func canonicalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = canonicalizeValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = canonicalizeValue(vv)
		}
		return out
	case string:
		if looksVolatile(t) {
			return volatilePlaceholder
		}
		return t
	case float64:
		if looksVolatile(strconv.FormatFloat(t, 'f', -1, 64)) {
			return volatilePlaceholder
		}
		return t
	default:
		return v
	}
}
