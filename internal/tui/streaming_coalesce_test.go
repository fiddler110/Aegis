package tui

import (
	"fmt"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// TestWaitForEventCoalescesBufferedTokens is the P21.1/P21.4 regression guard:
// a burst of streamed events already buffered on the channel must be drained
// into a small, bounded number of batchEventMsgs — not one message per token.
// Each batchEventMsg becomes exactly one Update → one refresh() → one markdown
// re-render, so "number of batches" is the direct proxy for "number of renders"
// this fix bounds. If a future change reverts waitForEvent to one-event-per-msg,
// batches jumps back to n and this fails.
func TestWaitForEventCoalescesBufferedTokens(t *testing.T) {
	const n = 200
	ch := make(chan api.Event, n+1)
	for i := 0; i < n; i++ {
		ch <- api.Event{Kind: api.KindText, Text: "tok"}
	}
	close(ch)

	batches, got := 0, 0
	sawClosed := false
	for !sawClosed {
		switch msg := waitForEvent(ch)().(type) {
		case batchEventMsg:
			batches++
			got += len(msg.events)
			sawClosed = msg.closed
		case streamClosedMsg:
			sawClosed = true
		default:
			t.Fatalf("unexpected message type %T from waitForEvent", msg)
		}
	}

	if got != n {
		t.Fatalf("expected all %d buffered events delivered, got %d", n, got)
	}
	// 200 buffered tokens (well under maxEventsPerBatch) must coalesce into a
	// single batch — the whole point of the fix.
	if batches != 1 {
		t.Fatalf("expected %d buffered tokens to coalesce into 1 batch, got %d batches (per-token rendering regressed)", n, batches)
	}
}

// TestWaitForEventRespectsBatchCap verifies the drain yields control back to
// Update at maxEventsPerBatch, so a stream faster than the render loop can't
// hand back an unbounded batch and starve input handling.
func TestWaitForEventRespectsBatchCap(t *testing.T) {
	n := maxEventsPerBatch + 88
	ch := make(chan api.Event, n+1)
	for i := 0; i < n; i++ {
		ch <- api.Event{Kind: api.KindText, Text: "tok"}
	}
	close(ch)

	first, ok := waitForEvent(ch)().(batchEventMsg)
	if !ok {
		t.Fatalf("expected first drain to return a batchEventMsg")
	}
	if len(first.events) != maxEventsPerBatch {
		t.Fatalf("expected first batch capped at %d events, got %d", maxEventsPerBatch, len(first.events))
	}
	if first.closed {
		t.Fatal("first batch must not report closed while events remain buffered")
	}

	second, ok := waitForEvent(ch)().(batchEventMsg)
	if !ok {
		t.Fatalf("expected second drain to return a batchEventMsg")
	}
	if len(second.events) != 88 {
		t.Fatalf("expected remaining 88 events in the second batch, got %d", len(second.events))
	}
	if !second.closed {
		t.Fatal("second batch should observe the channel close in the same drain")
	}
}

// TestBatchEventMsgEquivalentToSequential proves the coalesced path is
// behaviorally identical to processing the same events one at a time: the
// streamed text and follow-bottom state must match, so the performance fix
// changes only how often we render, never what is rendered.
func TestBatchEventMsgEquivalentToSequential(t *testing.T) {
	events := make([]api.Event, 20)
	for i := range events {
		events[i] = api.Event{Kind: api.KindText, Text: fmt.Sprintf("streamed line %d of the reply\n", i)}
	}

	seq := followBottomTestModel(t)
	seq.appendUser("write a long answer", nil)
	seq.streaming = true
	seq.followBottom = true
	for _, ev := range events {
		seq = driveUpdate(t, seq, eventMsg(ev))
	}

	batched := followBottomTestModel(t)
	batched.appendUser("write a long answer", nil)
	batched.streaming = true
	batched.followBottom = true
	batched = driveUpdate(t, batched, batchEventMsg{events: events})

	if seq.liveText.String() != batched.liveText.String() {
		t.Fatalf("batched streamed text differs from sequential:\nseq=%q\nbatched=%q", seq.liveText.String(), batched.liveText.String())
	}
	if seq.followBottom != batched.followBottom {
		t.Fatalf("followBottom diverged: sequential=%v batched=%v", seq.followBottom, batched.followBottom)
	}
	if plainView(seq) != plainView(batched) {
		t.Fatal("batched and sequential rendering produced different transcripts")
	}
}

// TestBatchEventMsgClosedTearsDownStream checks that a channel close observed
// mid-drain (closed:true) runs the same stream teardown the dedicated
// streamClosedMsg path does — streaming cleared, status back to ready — in the
// same Update, rather than leaving the stream half-open.
func TestBatchEventMsgClosedTearsDownStream(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("answer", nil)
	m.streaming = true
	m.followBottom = true

	m = driveUpdate(t, m, batchEventMsg{
		events: []api.Event{{Kind: api.KindText, Text: "final answer\n"}},
		closed: true,
	})

	if m.streaming {
		t.Fatal("expected streaming to be cleared after a closed batch")
	}
	if m.status != "ready" {
		t.Fatalf("expected status \"ready\" after stream teardown, got %q", m.status)
	}
}
