package tui

import (
	"encoding/json"
	"time"
)

// toolEntry tracks one tool call for the sidebar activity panel. status
// "awaiting_approval" is a transient sub-state of "pending" (set when a
// KindApprovalRequest correlates to this entry, cleared back to "ok"/"err"
// by the same KindToolResult match that resolves an ordinary pending entry —
// see stream.go). startedAt backs the sidebar's slow-running color cue.
type toolEntry struct {
	name      string
	status    string // "pending" | "ok" | "err" | "awaiting_approval"
	startedAt time.Time
}

// toolCard is the in-place-updating transcript item for one tool call
// (P21.2): appended once at KindToolCall in a pending state, then the same
// item (blk) is mutated via transcriptPane.SetItemRaw to its ok/err
// rendering when the matching KindToolResult arrives — replacing the old
// two-independent-static-blocks approach. call is the pre-rendered call
// block (renderToolCall / renderEditDiff / renderShellCall / etc.),
// computed once and reused for both the pending state (redrawn every
// animation tick — see renderToolCardPending) and the final combined
// render, so neither pays for re-running a diff computation.
// P33.3 adds one earlier state: a card appended at KindToolCallStart, whose
// call block is only the tool's name until the KindToolCall carrying the
// finished arguments reconciles it in place (awaitingCall).
type toolCard struct {
	blk          *transcriptItem
	name         string
	call         string
	awaitingCall bool

	// groupLabel is the P74.4 short target descriptor (groupEntryLabel),
	// computed once at KindToolCall time from ev.ToolInput — the only event
	// that reliably carries it; the engine's KindToolResult does not repeat
	// a call's input. Empty for a non-groupable tool.
	groupLabel string

	// P75.1: per-block expand/collapse state, independent of every other
	// block and of the session-wide /tools full|compact toggle (which only
	// sets the starting value for a card whose result hasn't landed yet).
	// full mirrors !toolCompact at the moment this card's KindToolResult
	// resolved; result/resultIsErr/resultPath are stashed here because
	// nothing else keeps them once the card is dropped from pendingTools —
	// toggleFull needs them to re-render in place. hasResult is false until
	// a result actually lands (an interrupted/stuck card has nothing to
	// toggle).
	full        bool
	result      string
	resultIsErr bool
	resultPath  string
	hasResult   bool

	// writeInput is ev.ToolInput, captured only for a write_file call (P64.4):
	// the only way to re-render call as an accurate diff once the matching
	// KindToolResult's Presentation payload supplies the file's prior
	// content, since the engine's KindToolResult does not repeat a call's
	// input (see call's own doc above). Empty for every other tool.
	writeInput json.RawMessage
}

// blkItem implements toolBlock.
func (c *toolCard) blkItem() *transcriptItem { return c.blk }

// toggleFull implements toolBlock: flips this card's own expand/collapse
// state and re-renders its finished result in place (P75.1). A no-op before
// a result has landed.
func (c *toolCard) toggleFull(m *model) {
	if !c.hasResult {
		return
	}
	c.full = !c.full
	m.transcript.SetItemRaw(c.blk, renderToolCardDone(m.th, c.call, c.name, c.result, c.resultIsErr, m.transcript.Width(), m.toolMaxLinesFor(c.full), c.resultPath))
}

// toolGroup is the open collapsed card for a run of consecutive, successful
// read_file/grep/glob calls (P74.4) — model.toolsUI.state.activeReadGroup's live state.
// blk is an existing tool card's transcript item, repurposed as the group's
// one visible slot the moment a second member merges into it; no extra
// transcript item is ever created for the group itself (see
// model.foldIntoReadGroup in stream.go).
type toolGroup struct {
	blk     *transcriptItem
	entries []groupEntry

	// full is this group's own P75.1 expand/collapse state, set from
	// !toolCompact when the group is created (see model.foldIntoReadGroup)
	// and flipped independently thereafter by toggleFull.
	full bool
}

// blkItem implements toolBlock.
func (g *toolGroup) blkItem() *transcriptItem { return g.blk }

// toggleFull implements toolBlock (P75.1).
func (g *toolGroup) toggleFull(m *model) {
	g.full = !g.full
	m.transcript.SetItemRaw(g.blk, renderToolGroup(m.th, g.entries, g.full))
}

// toolBlock is one transcript item whose tool-result rendering can be
// expanded or collapsed independently of every other block (P75.1) —
// unlike the session-wide /tools full|compact toggle, which only sets the
// default state a not-yet-resolved block starts from. Implemented by
// *toolCard (a standalone result) and *toolGroup (a folded read/search
// summary, which reuses its first member's transcript item — see
// model.foldIntoReadGroup).
type toolBlock interface {
	blkItem() *transcriptItem
	toggleFull(m *model)
}
