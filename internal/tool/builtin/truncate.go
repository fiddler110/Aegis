package builtin

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// P64.3: the one home for "this tool's result is too big for the context".
//
// Why a file in internal/tool/builtin rather than a new package: every caller
// is a built-in tool, the helpers need nothing from the rest of the tree, and
// internal/tool is the shared *contract* package (Tool, Registry, Capability) —
// putting a formatting helper there would make every consumer of the tool
// interface carry it. If a non-builtin tool source ever needs the same
// posture (an MCP result, a plugin), promote this file to internal/tool/trunc
// wholesale; nothing here depends on package builtin.
//
// The convention copied from the pi harness is that a tool declares *which end
// carries the information* rather than every tool guessing. Two helpers, not
// one, and the choice is a stated property of the tool:
//
//	head — the beginning is the answer: a file read, a search result, a fetched
//	       page, a digest written top-down.
//	tail — the end is the answer: a log, a test run, a build. What went wrong is
//	       the last thing printed, and the first 24 KiB of a failing `go test`
//	       is the part nobody needs.
//
// # Result posture table (P64.3)
//
// Sizes are the measured token cost of the cap itself, via internal/tokenest at
// its ASCII rate of ~4 bytes/token; see TestResultSizeComposition for what the
// tools actually return over a fixture workspace. "Remainder" is what happens
// to the bytes past the cap — with P64.1 in the tree, most of them are spilled
// to a file and named in the notice rather than discarded.
//
//	tool / site              typical      capped at            end   remainder
//	-----------------------  -----------  -------------------  ----  ---------------------------
//	shell                    0–2 KiB      24 KiB (~6.1k tok)   tail  spilled; notice names
//	                                                                 background:true+task_output
//	skill scripts            1–8 KiB      24000 B (~6.0k tok)  head  spilled; notice
//	git (diff/log/status)    0.2–20 KiB   32 KiB (~8.2k tok)   head  spilled; notice
//	git pre-commit test      0–4 KiB      24 KiB (~6.1k tok)   tail  spilled; notice
//	web_fetch                2–20 KiB     20000 B (~5.0k tok)  head  spilled; notice
//	                                      (max_chars overrides)
//	read_file (default)      0.5–32 KiB   1500 lines OR 32 KiB head  NOT spilled — the remainder
//	                                      whichever bites first       is the file, already
//	                                                                  addressable via offset/limit
//	read_file (explicit)     caller's     honored verbatim     head  caller's own window
//	read_file (byte guard)   —            50 MiB (maxReadBytes) head  refuses; points at grep
//	glob                     1–40 KiB     1000 paths           head  spilled (item-level, P64.1)
//	grep                     1–50 KiB     500 matches          head  spilled (item-level, P64.1)
//	task_get output preview  ≤2 KiB       2000 B               tail  full text via task_output
//
// Three things in that table are deliberate rather than inherited:
//
//   - shell was 200 KiB (`200 << 10`), which tokenest prices at 51,200 tokens —
//     larger than the entire context window under the local profile the P62.x
//     line spent four items shrinking, and larger than the *served* window on
//     every local model this repo has measured. A cap that cannot bind before
//     the window does is not a cap; it is a comment. 24 KiB is the value the
//     skill-script cap already chose for the same class of thing (a subprocess
//     writing to stdout), and aligning two caps in one class is not the
//     homogenising this item warns against — it is removing an 8x difference
//     nobody chose.
//
//   - shell keeps the *tail* now, where it used to keep the head. Command
//     output is a log: a failing build prints its errors last, and the head of
//     an over-cap `go test ./...` is a list of packages that passed. The head
//     was never argued for — it was what a `text[:maxOutput]` slice does. The
//     bytes are not lost either way (P64.1 spills them), so the question is
//     only which end is worth spending the inline budget on.
//
//   - read_file's default window gained a byte bound it never had. The item was
//     filed believing shell's 200 KiB was the anomaly; the instrument found the
//     largest single result any built-in tool produces is an ordinary default
//     read — 58,000 bytes / 14,501 estimated tokens for a 2,000-line source
//     file — because defaultReadLines bounds lines and nothing bounded bytes.
//     See maxDefaultReadBytes in file.go.
//
// # Notice budget
//
// Every helper here reserves the notice's own bytes *out of* the limit, so the
// returned string is never longer than the caller's cap. That is a P64.1
// requirement rather than tidiness: spilling must never be able to *add*
// tokens to a turn, and a notice appended on top of a full-size body would do
// exactly that.

// noticeReserve is how many bytes of the caller's budget are held back for the
// notice before any content is kept.
//
// Computed rather than a constant, because P64.1 made the notice variable-
// length: it now carries a spill locator whose path length is not known at
// compile time. The upper bound is exact — the byte counts in the notice are
// all at most len(s) digits, so building it with total in all three positions
// yields the longest string the real notice can be. A fixed reserve would have
// to be conservative enough to cover the longest imaginable path, which is
// budget spent on nothing on every ordinary truncation.
func noticeReserve(total int, recovery string) int {
	return len(truncationNotice("beginning", total, total, total, recovery)) + 1 // +1 for the separating newline
}

// maxShellOutput is the shell tool's result cap. Package-level rather than a
// const inside Execute so TestResultCapsCanBindBeforeTheContextWindow can
// enumerate it alongside every other cap — a cap no test can name is a cap that
// drifts. See the posture table above for why 24 KiB and why tail.
const maxShellOutput = 24 << 10

// TruncateHead keeps the beginning of s and drops the end, for results whose
// information is front-loaded. The returned string is at most limit bytes
// including the notice, and dropped reports how many bytes of s were removed
// (0 when the cap did not bind, in which case s is returned unchanged).
//
// recovery, when non-empty, is a sentence naming how the caller can get the
// dropped bytes back. It is appended inside the notice. Only the shell tool
// carried one before P64.3 (`background:true` + `task_output`); preserving it
// while giving every other site the same shape is half the point of this file.
func TruncateHead(s string, limit int, recovery string) (out string, dropped int) {
	if limit <= 0 || len(s) <= limit {
		return s, 0
	}
	keep := limit - noticeReserve(len(s), recovery)
	if keep < 0 {
		keep = 0
	}
	kept := trimToBoundary(s[:min(keep, len(s))])
	dropped = len(s) - len(kept)
	return kept + "\n" + truncationNotice("end", len(kept), len(s), dropped, recovery), dropped
}

// TruncateTail keeps the end of s and drops the beginning, for results whose
// information is at the bottom — logs, test runs, build output. The notice goes
// *first*, so a model streaming the result reads "this is the tail of a larger
// output" before it reads the output and not after.
func TruncateTail(s string, limit int, recovery string) (out string, dropped int) {
	if limit <= 0 || len(s) <= limit {
		return s, 0
	}
	keep := limit - noticeReserve(len(s), recovery)
	if keep < 0 {
		keep = 0
	}
	kept := trimFromBoundary(s[len(s)-min(keep, len(s)):])
	dropped = len(s) - len(kept)
	return truncationNotice("beginning", len(kept), len(s), dropped, recovery) + "\n" + kept, dropped
}

// truncationNotice is the single wording every capped result carries. One
// sentence, and it states all three facts a model needs to not reason from a
// partial result as though it were whole: that it is partial, which end is
// missing, and how much. Before P64.3 the tree had five different phrasings,
// three of which ("…(truncated)") said only the first.
func truncationNotice(end string, kept, total, dropped int, recovery string) string {
	msg := fmt.Sprintf("[truncated: showing %d of %d bytes; %d bytes dropped from the %s.", kept, total, dropped, end)
	if recovery != "" {
		msg += " " + strings.TrimSuffix(strings.TrimSpace(recovery), ".") + "."
	}
	return msg + "]"
}

// capItems is the item-level arm of the same convention (P64.3). glob and grep
// do not cap bytes — they cap *results*, which is why a byte-oriented policy
// applied after the fact cannot help them: by then the tail matches are gone.
// Head end in both cases (the first matches found), and the notice is
// truncNotice's, which additionally states the ordering caveat that only these
// two tools have.
//
// capped is passed rather than derived, because len(items) == max means
// different things at the two call shapes: glob collects everything and then
// cuts, so exactly max matches is a complete result; grep stops walking at max,
// so exactly max matches almost certainly means more exist.
func capItems(items []string, max int, kind string, capped bool) string {
	if !capped {
		return strings.Join(items, "\n")
	}
	return strings.Join(items[:min(len(items), max)], "\n") + truncNotice(kind, max)
}

// trimToBoundary pulls a head cut back to the last newline, or failing that to
// the last complete rune. Cutting mid-rune produces invalid UTF-8 that a
// provider may reject outright, and cutting mid-line hands the model a
// fragment that reads like real content — the worst of the two failure modes,
// because it is the one that looks fine.
func trimToBoundary(s string) string {
	if s == "" {
		return s
	}
	if i := strings.LastIndexByte(s, '\n'); i > len(s)/2 {
		return s[:i]
	}
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// trimFromBoundary is trimToBoundary's mirror for a tail cut: advance past the
// first newline so the kept text starts at a line boundary.
func trimFromBoundary(s string) string {
	if s == "" {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < len(s)/2 {
		return s[i+1:]
	}
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[1:]
	}
	return s
}
