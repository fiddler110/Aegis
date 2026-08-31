package engine

// P78.2: split out of engine.go — the batch tool-execution path (running a
// round of tool calls, sequential or parallel, and the synthetic result text
// for interrupted/cancelled calls) that sits on top of the round scheduler in
// toolround.go. Purely a file move; no behavior changed.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/fiddler110/aegis/internal/trace"
)

// redactSecretsFn is a seam over security.RedactText so tests can stub in
// findings without needing the real gitleaks binary on PATH — mirrors
// gitpr.go's scanPRTextForSecrets seam over security.ScanText (P24.6 /
// FIND-13).
var redactSecretsFn = security.RedactText

// maxParallelTools bounds how many tool calls run concurrently in one round.
const maxParallelTools = 8

// runTools executes the requested tools and returns tool-result blocks in the
// same order they were requested (as required for tool-use/result pairing).
//
// When the model requests several tools, read/network calls run fully
// concurrently with everything else; write/execute calls take a shared
// exclusive lock so they never race with each other (P8.6 — reads are no
// longer blocked behind a concurrent write/execute call in the same round).
// Event emission is serialized so streamed output is never interleaved
// mid-write. A single tool call takes the simple sequential path.
//
// P67.7: the scheduler itself lives in toolround.go and can be fed one call at a
// time while the model turn is still streaming. This function is the batch entry
// point — the caller that already has the whole slice — and `started` is the
// round the stream may have opened, or nil. Calls already dispatched from the
// stream are skipped rather than re-run; only the tail the stream did not
// dispatch is added here.
func (e *Engine) runTools(ctx context.Context, toolUses []provider.ToolUseBlock, emit EmitFunc, started *toolRound) ([]provider.Block, []trace.ToolCall, error) {
	// The sequential path stays for the single-call case, which is most rounds,
	// and it is only reachable when the stream dispatched nothing: a round that
	// already has a goroutine in flight cannot be re-run in-order.
	if started == nil && len(toolUses) <= 1 {
		return e.runToolsSequential(ctx, toolUses, emit)
	}

	r := started
	if r == nil {
		r = e.newToolRound(ctx, emit)
	}
	// Everything the stream did not already dispatch. The prefix property holds
	// because the stream adds calls in arrival order and stops adding at the
	// first ineligible one, so `pending` is always a prefix of toolUses.
	for _, tu := range toolUses[min(r.pending(), len(toolUses)):] {
		r.add(tu)
	}
	return r.wait()
}

// capRound applies the P67.1 aggregate bound to a completed round, rewriting the
// result blocks in place.
//
// It runs after every result has been emitted, which is the point: the user has
// already seen each tool's full (per-call-capped) output in the UI and the trace
// records what ran, so what this trims is only the *model's* copy — the one whose
// size is the problem. Trimming before emission would hide output from the human
// to save the model's context, which is the wrong trade in both directions.
//
// A hook that returns the wrong number of results is ignored rather than trusted:
// the alternative is pairing a tool_result with the wrong tool_use id, which
// every provider rejects and which would be a much worse failure than an
// unbounded round.
func (e *Engine) capRound(ctx context.Context, results []provider.Block) {
	if e.roundResultCap == nil || len(results) <= 1 {
		return
	}
	texts := make([]string, len(results))
	for i, blk := range results {
		tr, ok := blk.(provider.ToolResultBlock)
		if !ok {
			// An interrupted round leaves nil blocks behind; nothing to bound.
			return
		}
		texts[i] = tr.Content
	}
	// Through toolCtx for the same reason collectWrittenFiles is (P66.10/ARCH-03):
	// the spill lands in a workspace, and on a session with a custom workdir the
	// bare run context would put it in the daemon's instead — where read_file
	// cannot reach it, which turns the locator into a dead end.
	capped := e.roundResultCap(e.toolCtx(ctx), texts)
	if len(capped) != len(results) {
		e.logger.Warn("round result cap returned the wrong number of results; ignoring",
			"want", len(results), "got", len(capped))
		return
	}
	for i, blk := range results {
		tr := blk.(provider.ToolResultBlock)
		if capped[i] == tr.Content {
			continue
		}
		tr.Content = capped[i]
		results[i] = tr
	}
}

// toolTargetPath extracts a tool call's filesystem target from its JSON input,
// used to order same-path writes and reads within one tool round. Builtin file
// tools (read_file, write_file, edit_file, multiedit, ls, …) all name their
// target "path"; the value is cleaned so equivalent spellings ("f.py",
// "./f.py") compare equal. Returns "" when the input carries no "path" string,
// so non-file tools never gate one another. Matching is exact after cleaning:
// on a case-insensitive filesystem two differently-cased spellings of one path
// won't be ordered, but a model emitting a write→read pair reuses the same
// string, so this is a non-issue in practice and avoids wrongly serializing
// distinct paths on a case-sensitive filesystem.
func toolTargetPath(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var probe struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &probe); err != nil || probe.Path == "" {
		return ""
	}
	return filepath.Clean(probe.Path)
}

// runToolsSequential is the simple in-order path used for a single tool call.
func (e *Engine) runToolsSequential(ctx context.Context, toolUses []provider.ToolUseBlock, emit EmitFunc) ([]provider.Block, []trace.ToolCall, error) {
	results := make([]provider.Block, 0, len(toolUses))
	traces := make([]trace.ToolCall, 0, len(toolUses))
	for _, tu := range toolUses {
		select {
		case <-ctx.Done():
			return nil, nil, ErrInterrupted
		default:
		}

		emit(Event{Kind: KindToolCall, ToolName: tu.Name, ToolID: tu.ID, ToolInput: tu.Input})
		// P39.17: both edges of the call. The start edge says the round is
		// under way; the finish edge is what keeps a long round of many
		// short tools from ever looking silent.
		beat(ctx)
		start := time.Now()
		content, isErr := e.executeTool(ctx, tu)
		beat(ctx)
		traces = append(traces, trace.ToolCall{Name: tu.Name, DurationMS: time.Since(start).Milliseconds(), IsError: isErr})
		emit(Event{Kind: KindToolResult, ToolName: tu.Name, ToolID: tu.ID, ToolResult: content, ToolIsError: isErr})

		results = append(results, provider.ToolResultBlock{
			ToolUseID: tu.ID,
			Content:   content,
			IsError:   isErr,
		})
	}
	return results, traces, nil
}

// serializeTool reports whether a tool call must run exclusively (write/
// execute capabilities), preventing it from racing other tool calls in the
// same round. Capability is evaluated per-call (tool.EffectiveCapability,
// P25.4c) so a call a tool reclassifies as narrower than its usual
// capability — e.g. a read-only shell command — isn't serialized behind
// concurrent writes/execs for no reason. Unknown tools are treated as serial
// out of caution.
//
// It classifies under toolCtx, the same context executeTool builds and hands
// the gate, because a per-call capability can depend on the session's workdir
// (CRIT-3) and the scheduling decision must be the decision the gate made.
func (e *Engine) serializeTool(ctx context.Context, name string, input json.RawMessage) bool {
	if e.tools == nil {
		return true
	}
	t, ok := e.tools.Get(name)
	if !ok {
		return true
	}
	switch tool.EffectiveCapability(e.toolCtx(ctx), t, input) {
	case tool.CapWrite, tool.CapExecute:
		return true
	default:
		return false
	}
}

// interruptedNotStartedText is the synthetic result for an orphaned call the
// runtime is known never to have begun. It is the pre-P65.1 wording, kept
// verbatim because for this half it was always true.
func interruptedNotStartedText(name string) string {
	return fmt.Sprintf("tool call interrupted; %s did not run", name)
}

// interruptedMalformedText is the synthetic result for an orphaned call whose
// arguments never parsed as valid JSON at all (P74.14). Unlike
// interruptedNotStartedText, this is not a claim about timing — the call
// could not have dispatched regardless of whether the run was interrupted,
// because its arguments are truncated or malformed. Telling the model
// "interrupted" here invites a retry of the same call verbatim; the model
// needs to know instead that it must reissue the call with valid arguments.
func interruptedMalformedText(name string) string {
	return fmt.Sprintf("tool call never dispatched; %s's arguments were malformed or truncated JSON."+
		" Reissue the call with valid arguments.", name)
}

// siblingCancelledText is the result reported for a call that never started
// because an earlier write/execute call in the same parallel round failed
// (P67.4). This is the one cancellation case where "nothing happened" is
// honestly assertable — the call had not reached Execute — so unlike
// interruptedMaybeRanText it says so plainly, and the model is free to re-issue
// it once it has dealt with the failure.
func siblingCancelledText(name, failed string) string {
	if failed == "" {
		return fmt.Sprintf("%s%s did not run because another call in the same round failed. Nothing was executed.", siblingCancelledPrefix, name)
	}
	return fmt.Sprintf("%s%s did not run because %s failed earlier in the same round. Nothing was executed.", siblingCancelledPrefix, name, failed)
}

// siblingCancelledPrefix and roundCancelledMarker are the two stable markers
// that identify a result as an artifact of a P67.4 round cancellation rather
// than as something a tool actually reported. isRoundCancelledResult is the
// only reader; the tool-failure breaker uses it (see toolfailure.go).
const (
	siblingCancelledPrefix = "tool call skipped; "
	roundCancelledMarker   = "The round was cancelled because "
)

// isRoundCancelledResult reports whether a tool result was produced by
// cancelling the round rather than by running the tool (P67.4).
func isRoundCancelledResult(content string) bool {
	return strings.HasPrefix(content, siblingCancelledPrefix) || strings.Contains(content, roundCancelledMarker)
}

// siblingCancelledSuffix explains a cancellation to a call that was already
// running when it happened. Appended to (never substituted for) whatever the
// tool itself reported, because that text may describe real work the call had
// already done before the cancellation reached it.
func siblingCancelledSuffix(failed string) string {
	if failed == "" {
		return roundCancelledMarker + "another call in it failed."
	}
	return fmt.Sprintf("%s%s failed.", roundCancelledMarker, failed)
}

// interruptedMaybeRanText is the synthetic result for an orphaned call that had
// reached its Execute when the run was cancelled (P65.1).
//
// The second sentence is the whole point of the split, and it is a claim about
// how a model reads the first: told an effect is *uncertain* it re-checks, told
// the effect did not happen it re-runs. Re-running a `shell` that already
// deleted half a directory, or a write that already landed, is the failure this
// wording exists to prevent — and the tool most likely to be running when a
// stall bound fires is the long one that stalled, which is exactly the class
// where "did not run" is both most likely false and most costly to believe.
func interruptedMaybeRanText(name string) string {
	return fmt.Sprintf("tool call interrupted while running; %s may have partially completed."+
		" Verify before assuming its effects did or did not land.", name)
}

// interruptedMaybeRanSafeText is interruptedMaybeRanText's counterpart for a
// call tool.EffectiveReplay classifies ReplaySafe (P65.4): the call is known
// to be harmless to reissue with the same input, so the model doesn't need to
// go verify anything first — it can just ask again if it still needs the
// result.
func interruptedMaybeRanSafeText(name string) string {
	return fmt.Sprintf("tool call interrupted while running; %s may or may not have completed."+
		" This call is idempotent — it is safe to simply retry it with the same input if you still need the result.", name)
}

// repairOrphanedToolUses scans the conversation for tool_use blocks in assistant
// messages that have no matching tool_result in a subsequent user message, and
// injects synthetic error results. This prevents providers from rejecting a
// conversation that was interrupted mid-tool-round (e.g. by context cancel).
//
// P65.1: started carries the tool_use IDs the runtime is known to have begun
// executing, so the synthetic result can tell "never started" from "may have
// run" instead of asserting the first for both. Every mechanism Aegis has for
// bounding a run cancels the run context mid-flight by design — MaxTurnStall
// (on by default, and the only bound covering the tool-execution phase),
// MaxWallClockPerRun, a user interrupt, a TUI quit-while-streaming — so this
// path is reached routinely rather than exceptionally, and the drive's reset
// ladder then hands the next context whatever this function wrote. An unknown
// ID is treated as not started, which is the pre-P65.1 behaviour and the only
// honest answer when there is no record either way.
//
// tools, when non-nil, is consulted via tool.EffectiveReplay for every orphan
// classified "may have run" (P65.4): a ReplaySafe tool gets a synthetic
// result telling the model it's fine to simply reissue the call, instead of
// the universally conservative "verify before assuming" wording every tool
// got before this existed. Nil tools (every caller predating this, and every
// existing test) keeps that universal wording exactly as it was.
func repairOrphanedToolUses(msgs []provider.Message, started map[string]struct{}, tools *tool.Registry) []provider.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Collect all resolved tool_use IDs.
	resolved := make(map[string]bool, len(msgs))
	for _, msg := range msgs {
		if msg.Role != provider.RoleUser {
			continue
		}
		for _, b := range msg.Content {
			if tr, ok := b.(provider.ToolResultBlock); ok {
				resolved[tr.ToolUseID] = true
			}
		}
	}

	// Check whether any assistant message has unresolved tool_use blocks.
	hasOrphans := false
	for _, msg := range msgs {
		if msg.Role != provider.RoleAssistant {
			continue
		}
		for _, b := range msg.Content {
			if tu, ok := b.(provider.ToolUseBlock); ok && !resolved[tu.ID] {
				hasOrphans = true
				break
			}
		}
		if hasOrphans {
			break
		}
	}
	if !hasOrphans {
		return msgs
	}

	// Rebuild the message list, inserting synthetic error results after each
	// assistant message that has orphaned tool_use blocks.
	out := make([]provider.Message, 0, len(msgs)+1)
	skip := make(map[int]bool) // next-user-message indices already merged
	for i, msg := range msgs {
		if skip[i] {
			continue
		}
		out = append(out, msg)
		if msg.Role != provider.RoleAssistant {
			continue
		}

		var synth []provider.Block
		for _, b := range msg.Content {
			if tu, ok := b.(provider.ToolUseBlock); ok && !resolved[tu.ID] {
				_, ran := started[tu.ID]
				text := interruptedNotStartedText(tu.Name)
				switch {
				case ran:
					text = interruptedMaybeRanText(tu.Name)
					if tools != nil {
						if t, ok := tools.Get(tu.Name); ok && tool.EffectiveReplay(t, tu.Input) == tool.ReplaySafe {
							text = interruptedMaybeRanSafeText(tu.Name)
						}
					}
				case !json.Valid(tu.Input):
					// P74.14: a call that never reached dispatch and whose
					// arguments never parsed at all was never going to run,
					// interruption or not — see interruptedMalformedText.
					text = interruptedMalformedText(tu.Name)
				}
				synth = append(synth, provider.ToolResultBlock{
					ToolUseID: tu.ID,
					Content:   text,
					IsError:   true,
				})
			}
		}
		if len(synth) == 0 {
			continue
		}

		nextIdx := i + 1
		if nextIdx < len(msgs) && msgs[nextIdx].Role == provider.RoleUser {
			// Merge synthetic results into the existing user message.
			combined := make([]provider.Block, len(msgs[nextIdx].Content)+len(synth))
			copy(combined, msgs[nextIdx].Content)
			copy(combined[len(msgs[nextIdx].Content):], synth)
			out = append(out, provider.Message{Role: provider.RoleUser, Content: combined})
			skip[nextIdx] = true
		} else {
			out = append(out, provider.Message{Role: provider.RoleUser, Content: synth})
		}
	}
	return out
}

// registeredToolNames lists every tool registered on reg (regardless of
// exposure/deferred state — the model should be told every real name, not
// just what's currently exposed), sorted and comma-joined for use in a
// model-visible error message (P39.2): a small local model that invents a
// tool name can self-correct from this list instead of spending a turn
// guessing at a name that doesn't exist.
func registeredToolNames(reg *tool.Registry) string {
	if reg == nil {
		return "(none)"
	}
	all := reg.All()
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// toolCtx decorates ctx with everything a tool call needs to resolve the same
// way regardless of *which* engine path invoked it: the registry actually
// governing this run, the per-session workdir (P25.1) and the extra workspace
// roots.
//
// It is a helper rather than three lines inside executeTool because
// executeTool is not the only caller of a tool — collectWrittenFiles reads
// files back for the output guard by calling read_file directly. Before P66.10
// that second path used the bare run context, so read_file fell back to its
// construction-time root (the daemon workspace) and, on any session with a
// custom workdir, the guard silently validated nothing (ARCH-03). Any future
// direct tool invocation must go through here too.
func (e *Engine) toolCtx(ctx context.Context) context.Context {
	// One call, one capability verdict (M5). The gate, the scheduler, the
	// checkpoint decision and the written/read-path bookkeeping all ask
	// tool.EffectiveCapability for the same call, and the shell tool answers by
	// doing filesystem I/O per argv token — so without this the question is
	// asked repeatedly, with the approval round-trip sitting in the middle of
	// it. The memo lives exactly as long as this call's context.
	ctx = tool.WithCapabilityMemo(ctx)
	ctx = tool.WithRegistry(ctx, e.tools)
	if e.workdir != "" {
		ctx = tool.WithWorkdir(ctx, e.workdir)
	}
	ctx = tool.WithExtraRoots(ctx, e.extraRoots)
	return tool.WithContextWindow(ctx, e.effectiveContextWindow())
}

// executeTool looks up and runs a single tool, converting failures into
// model-visible error results rather than aborting the whole run.
func (e *Engine) executeTool(ctx context.Context, tu provider.ToolUseBlock) (string, bool) {
	if e.tools == nil {
		return fmt.Sprintf("no tools available (requested %q)", tu.Name), true
	}
	ctx = e.toolCtx(ctx)
	t, ok := e.tools.Get(tu.Name)
	if !ok {
		return fmt.Sprintf("unknown tool %q; registered tools: %s", tu.Name, registeredToolNames(e.tools)), true
	}
	if e.gate != nil {
		if allowed, reason := e.gate.Check(ctx, t, tu.Input); !allowed {
			e.logger.Info("tool call blocked by gate", "tool", tu.Name, "reason", reason)
			return reason, true
		}
	}
	if e.hooks != nil {
		if err := e.hooks.PreToolUse(ctx, tu.Name, tu.Input); err != nil {
			e.logger.Info("tool call blocked by hook", "tool", tu.Name, "err", err)
			return fmt.Sprintf("blocked by hook: %v", err), true
		}
	}
	// P65.1: mark the call started here rather than at the top of executeTool.
	// The distinction the synthetic interrupted result has to make is "did this
	// call's effects begin", and everything above this line — an unknown tool, a
	// gate refusal, a hook veto — returns without the tool ever running, so
	// marking at entry would over-warn on exactly the calls whose "did not run"
	// is provably true. Those branches return a result of their own and so are
	// only orphaned if the whole round is discarded by a cancel, which is the
	// case this is trying to describe accurately.
	e.markToolStarted(tu.ID)
	if e.onToolStarted != nil {
		e.onToolStarted(tu.ID, tu.Name, tu.Input)
	}
	res, err := t.Execute(ctx, tu.Input)
	// P65.4: the durable record's only job is telling a future process whether
	// this call's effect began — that question is answered the instant Execute
	// returns, success or error alike, so this clears before any of the
	// result-shaping below (which can itself fail without changing the answer).
	if e.onToolFinished != nil {
		e.onToolFinished(tu.ID)
	}
	content, isErr := res.Content, res.IsError
	if err != nil {
		e.logger.Warn("tool execution error", "tool", tu.Name, "err", err)
		content, isErr = fmt.Sprintf("tool error: %v", err), true
	}
	// P74.9: a legitimately empty, non-error result is indistinguishable from a
	// failure to a model, and a local model in particular tends to re-issue the
	// call. Applies after the error path above so a real error keeps its own
	// message rather than being read as "empty".
	if !isErr {
		content = builtin.NormalizeEmptyResult(tu.Name, content)
	}
	// P63.8: effective capability (P25.4c), not the static one — a tool that
	// reclassifies into CapWrite for a specific call would otherwise have its
	// written paths go unrecorded, costing that call its output-guard file
	// validation and its quarantine-on-fail rollback. Same reasoning that moved
	// ContextualGate (P32.2) and ScopeGate (P63.3) off the static capability,
	// and it makes this branch agree with the redaction branch below, which
	// reads the same tool and the same input.
	if !isErr && tool.EffectiveCapability(ctx, t, tu.Input) == tool.CapWrite {
		paths := writtenPathsFromInput(tu.Input)
		if len(paths) == 0 {
			// P32.6: writtenPathsFromInput only recognizes "path"/"file_path"/
			// "edits[].path". A write-capability tool using a different input
			// shape (an MCP tool, or a future builtin) silently gets no
			// output-guard file validation and no quarantine-on-fail rollback —
			// surface that gap instead of letting it degrade silently.
			e.logger.Warn("write-capability tool call yielded no paths for output-guard coverage", "tool", tu.Name)
		}
		e.recordWrittenPaths(paths)
	}
	// P65.2: record what this call read, on the same effective-capability rule
	// (P25.4c) the write branch above uses — so a `cat` routed through shell is
	// recorded the way a read_file call would be. Errors are excluded: a failed
	// read tells the model nothing about the file and would put a path the
	// session never saw into the carried set.
	if !isErr && tool.EffectiveCapability(ctx, t, tu.Input) == tool.CapRead {
		e.recordReadPaths(writtenPathsFromInput(tu.Input))
	}
	if !isErr && e.redactSecrets && tool.EffectiveCapability(ctx, t, tu.Input) == tool.CapRead {
		// P24.12 / FIND-09: opt-in scrub of tool-read file content for secret
		// patterns before it's appended to the conversation sent to whichever
		// provider is configured (a cloud API by default has no visibility
		// restriction on what a file read surfaces). Strictly best-effort and
		// never blocking — a scan failure or gitleaks being absent leaves
		// content untouched, since the tool result must still reach the model
		// either way. Effective capability (P25.4c) so a `cat` of a
		// secrets-bearing file gets the same scrub a read_file call would.
		if redacted, findings, scanErr := redactSecretsFn(ctx, content); scanErr == nil && len(findings) > 0 {
			content = redacted
			e.logger.Info("redacted secret pattern(s) from tool output", "tool", tu.Name, "count", len(findings))
		}
	}
	if e.hooks != nil {
		e.hooks.PostToolUse(ctx, tu.Name, tu.Input, content, isErr)
	}
	return content, isErr
}
