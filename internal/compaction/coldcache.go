package compaction

import (
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
)

// ColdCacheSentinel is the fixed string a cleared tool result is replaced with
// (P67.6).
//
// **It is a wire format, not a message.** Once written into a conversation it is
// persisted with that conversation and read back by this same code on every
// later turn — that is how the pass stays idempotent, and how a second cold
// resume knows not to count an already-cleared result as work it did. Renaming
// it silently breaks accumulation: the old sentinels stop being recognized,
// every one of them is "cleared" again, and the pass reports a yield on a
// conversation it did not change. This is the same hazard the
// `<read-files>`/`<modified-files>` tags carry in the compaction summary, and it
// fails the same quiet way.
//
// It is deliberately phrased at the model rather than at the user: the model is
// the only reader that ever sees it, and it needs to know both that content is
// missing and that re-fetching is the remedy.
const ColdCacheSentinel = "[cleared: this tool result was dropped when the conversation resumed after an idle gap; re-run the tool if you still need it]"

// coldCacheMinResultChars is the size below which a tool result is left alone.
// The pass exists to stop paying prefill on large stale dumps; rewriting a
// 40-character result buys nothing and still costs the conversation an
// invalidation and a re-persist. It is well under staleSearchKeepChars so the
// two passes do not fight over the same results.
const coldCacheMinResultChars = 200

// coldClearableTools are the tools whose *results* are disposable: the model can
// obtain them again by re-running the call, and nothing outside the transcript
// depends on the bytes still being there.
//
// The item's scope is "reads, searches, shell" and this is that list made
// concrete. The membership rule is re-fetchability, not harmlessness — `shell`
// is in because its *result* is disposable even when the command it ran was not,
// and clearing a result never un-runs a command.
//
// Three deliberate exclusions, because the reason is easy to lose:
//
//   - Nothing that mutates. write_file/edit_file/multi_edit/git_commit results
//     are the confirmation that a mutation landed; pruneStaleToolResults already
//     blanks their oversized *arguments*, which is the disposable half.
//   - ask_user and the todo/task/team bookkeeping tools. Their results carry
//     state the model cannot re-derive by asking again — a user's answer is not
//     idempotent, and re-running task_get returns a *later* answer, not the one
//     the conversation reasoned from.
//   - entity_recall / project_knowledge / remember. Re-fetchable in principle,
//     but they are the run's memory: they are small, they are why the model
//     believes what it believes, and clearing them is the one case where "re-run
//     the tool" costs more than it saves.
var coldClearableTools = map[string]bool{
	// reads
	"read_file": true,
	"repomap":   true,
	// searches and static analysis
	"glob":              true,
	"grep":              true,
	"ls":                true,
	"references":        true,
	"definition":        true,
	"hover":             true,
	"diagnostics":       true,
	"document_symbols":  true,
	"workspace_symbols": true,
	"call_hierarchy":    true,
	"tool_search":       true,
	// shell and inspection commands
	"shell": true,
	"git":   true,
	// network reads
	"web_fetch":  true,
	"web_search": true,
}

// ClearColdToolResults replaces every clearable tool result except the most
// recent keep with ColdCacheSentinel, and reports how many it cleared and how
// many estimated tokens that freed.
//
// This is the *what* half of P67.6; the caller owns the *when* (the idle gap and
// the call-purpose gate). Splitting it that way is what keeps the pass usable
// from a test without a clock and keeps the purpose decision with the component
// that knows what kind of run it is.
//
// keep is floored at 1 on purpose, and the floor is the item's first named
// constraint rather than defensive habit: clearing *every* result leaves the
// model with no working context at all, and an off-by-one that keeps zero is
// easy to write and impossible to see in a diff. A caller asking for 0 gets 1.
//
// The pass is idempotent. A result already holding the sentinel is skipped
// rather than re-counted, so a conversation that resumes cold twice reports a
// yield the first time and no change the second — which is what lets the caller
// treat changed=false as "nothing to do" instead of re-persisting on every
// idle turn.
//
// Errors are never cleared. An error result is small, it is the reason the run
// took the shape it did, and re-running the call to rediscover it is the
// opposite of free.
func ClearColdToolResults(msgs []provider.Message, keep int) (out []provider.Message, cleared, freedTokens int) {
	if keep < 1 {
		keep = 1
	}

	// Tool names live on the tool_use block; the content to clear lives on the
	// matching tool_result, which is in a later message. One pass to index.
	names := make(map[string]string, len(msgs))
	for _, m := range msgs {
		for _, blk := range m.Content {
			if tu, ok := blk.(provider.ToolUseBlock); ok {
				names[tu.ID] = tu.Name
			}
		}
	}

	// Walk backwards so "most recent keep" is decided without a second pass, and
	// count only results this pass would otherwise clear — the keep-count is a
	// window over *clearable* results, not over all of them. Counting every
	// result would let a run of ask_user calls push the last real read out of
	// the protected window.
	kept := 0
	out = msgs
	copied := false
	for i := len(msgs) - 1; i >= 0; i-- {
		for bi, blk := range msgs[i].Content {
			tr, ok := blk.(provider.ToolResultBlock)
			if !ok || tr.IsError {
				continue
			}
			if !coldClearableTools[names[tr.ToolUseID]] {
				continue
			}
			if tr.Content == ColdCacheSentinel {
				// Already cleared by an earlier cold resume. It is not work, and
				// it does not consume a keep slot either: the slot exists to
				// leave the model something to read, and a sentinel is nothing
				// to read.
				continue
			}
			if len(tr.Content) < coldCacheMinResultChars {
				continue
			}
			if kept < keep {
				kept++
				continue
			}
			if !copied {
				// Copy-on-first-write, message content included: the caller's
				// slice is the live conversation and a no-op call must not
				// alias, let alone mutate, it.
				out = make([]provider.Message, len(msgs))
				copy(out, msgs)
				copied = true
			}
			if &out[i].Content[bi] == &msgs[i].Content[bi] {
				content := make([]provider.Block, len(msgs[i].Content))
				copy(content, msgs[i].Content)
				out[i].Content = content
			}
			freedTokens += tokenest.Estimate(tr.Content) - tokenest.Estimate(ColdCacheSentinel)
			tr.Content = ColdCacheSentinel
			out[i].Content[bi] = tr
			cleared++
		}
	}
	if freedTokens < 0 {
		freedTokens = 0
	}
	return out, cleared, freedTokens
}

// ClearColdToolResults implements engine.ColdCacheCompactor. The Summarizer owns
// the keep-count because it already owns keepRecent and the two are the same
// kind of decision — how much of the tail is untouchable — even though they
// count different things (messages there, clearable results here).
func (s *Summarizer) ClearColdToolResults(msgs []provider.Message) (out []provider.Message, cleared, freedTokens int) {
	return ClearColdToolResults(msgs, s.coldCacheKeep)
}
