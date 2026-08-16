package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/api"
)

// collectBGRows drives a bgEventBuffer and returns the rows it wrote, decoded.
type bgRecorder struct{ rows []api.Event }

func (r *bgRecorder) write(data string) {
	var ev api.Event
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		panic("bg_events row is not an api.Event: " + err.Error())
	}
	r.rows = append(r.rows, ev)
}

// replayText is what a client reattaching via GET /sessions/{id}/events does
// with the buffered rows: concatenate the text of every text event, in id order.
func replayText(evs []api.Event) string {
	var b strings.Builder
	for _, ev := range evs {
		if ev.Kind == api.KindText {
			b.WriteString(ev.Text)
		}
	}
	return b.String()
}

// TestBGEventBufferCoalescesTextDeltas is the P66.9 write-amplification half:
// consecutive pure text deltas inside one window become a single bg_events row,
// and the replayed text is byte-identical to the deltas that went in.
func TestBGEventBufferCoalescesTextDeltas(t *testing.T) {
	var rec bgRecorder
	buf := newBGEventBuffer(time.Hour, rec.write)
	for _, s := range []string{"Hel", "lo, ", "wor", "ld"} {
		buf.add(api.Event{Kind: api.KindText, Text: s})
	}
	if len(rec.rows) != 0 {
		t.Fatalf("deltas were written before any flush trigger: %+v", rec.rows)
	}
	buf.flush()
	if len(rec.rows) != 1 {
		t.Fatalf("wrote %d rows for 4 deltas in one window, want 1: %+v", len(rec.rows), rec.rows)
	}
	if got := replayText(rec.rows); got != "Hello, world" {
		t.Errorf("replayed text = %q, want %q", got, "Hello, world")
	}
}

// TestBGEventBufferFlushesOnKindChange: a non-delta event must never overtake
// text that was emitted before it. The held deltas are written first, then the
// event itself, so replay order matches emission order.
func TestBGEventBufferFlushesOnKindChange(t *testing.T) {
	var rec bgRecorder
	buf := newBGEventBuffer(time.Hour, rec.write)
	buf.add(api.Event{Kind: api.KindText, Text: "before"})
	buf.add(api.Event{Kind: api.KindToolCall, Tool: "read_file"})
	buf.add(api.Event{Kind: api.KindText, Text: "after"})
	buf.flush()

	if len(rec.rows) != 3 {
		t.Fatalf("rows = %+v, want text/tool_call/text", rec.rows)
	}
	if rec.rows[0].Kind != api.KindText || rec.rows[0].Text != "before" {
		t.Errorf("row 0 = %+v, want the held text flushed ahead of the tool call", rec.rows[0])
	}
	if rec.rows[1].Kind != api.KindToolCall || rec.rows[1].Tool != "read_file" {
		t.Errorf("row 1 = %+v, want the tool call written through unchanged", rec.rows[1])
	}
	if rec.rows[2].Kind != api.KindText || rec.rows[2].Text != "after" {
		t.Errorf("row 2 = %+v, want the post-tool text", rec.rows[2])
	}
}

// TestBGEventBufferFlushesBetweenTextAndThinking: text and thinking are both
// coalescable, but they are different streams — folding one into the other
// would put reasoning into the answer on replay.
func TestBGEventBufferFlushesBetweenTextAndThinking(t *testing.T) {
	var rec bgRecorder
	buf := newBGEventBuffer(time.Hour, rec.write)
	buf.add(api.Event{Kind: api.KindThinking, Text: "hmm"})
	buf.add(api.Event{Kind: api.KindText, Text: "answer"})
	buf.flush()

	if len(rec.rows) != 2 {
		t.Fatalf("rows = %+v, want one thinking row and one text row", rec.rows)
	}
	if rec.rows[0].Kind != api.KindThinking || rec.rows[0].Text != "hmm" {
		t.Errorf("row 0 = %+v, want the thinking delta on its own row", rec.rows[0])
	}
	if got := replayText(rec.rows); got != "answer" {
		t.Errorf("replayed answer text = %q, want %q (thinking must not leak into it)", got, "answer")
	}
}

// TestBGEventBufferFlushesOnWindowExpiry: a long answer must not sit entirely
// in memory until the run ends — a client reattaching mid-run needs the text so
// far. The window bounds how far behind the buffered copy can be.
func TestBGEventBufferFlushesOnWindowExpiry(t *testing.T) {
	var rec bgRecorder
	buf := newBGEventBuffer(time.Millisecond, rec.write)
	buf.add(api.Event{Kind: api.KindText, Text: "a"})
	time.Sleep(5 * time.Millisecond)
	buf.add(api.Event{Kind: api.KindText, Text: "b"})
	if len(rec.rows) != 1 || rec.rows[0].Text != "a" {
		t.Fatalf("rows after the window expired = %+v, want the first delta written", rec.rows)
	}
	buf.flush()
	if got := replayText(rec.rows); got != "ab" {
		t.Errorf("replayed text = %q, want %q", got, "ab")
	}
}

// TestBGEventBufferLosesNoTailOnFlush: the last partial window is the tail of
// the answer, which is exactly the part a reattaching client cares about.
func TestBGEventBufferLosesNoTailOnFlush(t *testing.T) {
	var rec bgRecorder
	buf := newBGEventBuffer(time.Hour, rec.write)
	buf.add(api.Event{Kind: api.KindText, Text: "tail"})
	buf.flush()
	buf.flush() // idempotent: the second flush must not duplicate the row
	if len(rec.rows) != 1 || rec.rows[0].Text != "tail" {
		t.Fatalf("rows = %+v, want exactly one row carrying the tail", rec.rows)
	}
}

// TestBGEventBufferWritesNonDeltaTextThrough: an event whose kind is text but
// which carries more than text (usage counts, an error, a tool id) must be
// written through whole rather than folded into a neighbour, which would drop
// every field but Text.
func TestBGEventBufferWritesNonDeltaTextThrough(t *testing.T) {
	var rec bgRecorder
	buf := newBGEventBuffer(time.Hour, rec.write)
	buf.add(api.Event{Kind: api.KindText, Text: "plain"})
	buf.add(api.Event{Kind: api.KindText, Text: "rich", OutputTokens: 7})
	buf.flush()

	if len(rec.rows) != 2 {
		t.Fatalf("rows = %+v, want the field-carrying text event kept separate", rec.rows)
	}
	if rec.rows[1].OutputTokens != 7 {
		t.Errorf("row 1 = %+v, want output_tokens preserved", rec.rows[1])
	}
	if got := replayText(rec.rows); got != "plainrich" {
		t.Errorf("replayed text = %q, want %q", got, "plainrich")
	}
}

// TestBGEventBufferReplayMatchesTheRawStream is the replay-correctness
// invariant in one assertion: for an arbitrary mixed event stream, the text a
// client rebuilds from the coalesced rows equals the text it would have
// rebuilt from one row per event, and the non-text events survive in order.
func TestBGEventBufferReplayMatchesTheRawStream(t *testing.T) {
	stream := []api.Event{
		{Kind: api.KindText, Text: "The "},
		{Kind: api.KindText, Text: "answer"},
		{Kind: api.KindToolCall, Tool: "grep", ToolID: "t1"},
		{Kind: api.KindToolResult, Tool: "grep", ToolID: "t1", ToolResult: "hit"},
		{Kind: api.KindText, Text: " is "},
		{Kind: api.KindText, Text: "42."},
		{Kind: api.KindTurnDone, InputTokens: 10, OutputTokens: 4},
		{Kind: api.KindDone},
	}
	var rec bgRecorder
	buf := newBGEventBuffer(time.Hour, rec.write)
	for _, ev := range stream {
		buf.add(ev)
	}
	buf.flush()

	if want, got := replayText(stream), replayText(rec.rows); want != got {
		t.Errorf("coalesced replay = %q, uncoalesced replay = %q", got, want)
	}
	if len(rec.rows) >= len(stream) {
		t.Errorf("coalescing saved no rows: %d rows for %d events", len(rec.rows), len(stream))
	}
	var kinds []api.EventKind
	for _, ev := range rec.rows {
		if ev.Kind != api.KindText {
			kinds = append(kinds, ev.Kind)
		}
	}
	want := []api.EventKind{api.KindToolCall, api.KindToolResult, api.KindTurnDone, api.KindDone}
	if len(kinds) != len(want) {
		t.Fatalf("non-text kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("non-text kinds = %v, want %v", kinds, want)
		}
	}
}
