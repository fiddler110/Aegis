package builtin

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fiddler110/aegis/internal/tokenest"
)

// TestTruncateNeverExceedsTheCap is the P64.3/P64.1 budget property: the notice
// is reserved *out of* the limit, never appended on top of it. Without this, a
// tool that "caps at 24 KiB" returns 24 KiB plus a notice, and P64.1's promise
// that spilling can never add tokens to a turn is false.
//
// Mutation check (run 2026-08-14): changing TruncateHead's `keep := limit -
// truncationNoticeReserve` to `keep := limit` fails this test at every size.
func TestTruncateNeverExceedsTheCap(t *testing.T) {
	body := strings.Repeat("abcdefghij\n", 5000) // 55,000 bytes
	for _, limit := range []int{500, 4096, 24 << 10, 32 << 10} {
		for name, fn := range map[string]func(string, int, string) (string, int){
			"head": TruncateHead,
			"tail": TruncateTail,
		} {
			out, dropped := fn(body, limit, "use background:true and task_output for large commands")
			if len(out) > limit {
				t.Errorf("%s(limit=%d): returned %d bytes, over the cap", name, limit, len(out))
			}
			if dropped == 0 {
				t.Errorf("%s(limit=%d): reported nothing dropped from a %d-byte input", name, limit, len(body))
			}
			if !strings.Contains(out, "truncated") {
				t.Errorf("%s(limit=%d): capped result carries no notice", name, limit)
			}
		}
	}
}

// TestTruncateKeepsTheDeclaredEnd is the whole point of there being two
// helpers: the tool declares which end carries the information, so a log keeps
// its errors and a search result keeps its first hits.
func TestTruncateKeepsTheDeclaredEnd(t *testing.T) {
	body := "FIRST\n" + strings.Repeat("filler line\n", 4000) + "LAST\n"

	head, _ := TruncateHead(body, 4096, "")
	if !strings.Contains(head, "FIRST") {
		t.Error("TruncateHead dropped the beginning it exists to keep")
	}
	if strings.Contains(head, "LAST") {
		t.Error("TruncateHead kept the end; the cap did not bind where it should")
	}

	tail, _ := TruncateTail(body, 4096, "")
	if !strings.Contains(tail, "LAST") {
		t.Error("TruncateTail dropped the end it exists to keep")
	}
	if strings.Contains(tail, "FIRST") {
		t.Error("TruncateTail kept the beginning; the cap did not bind where it should")
	}
	// The tail notice must lead, so a model reads "this is a tail" before it
	// reads the tail rather than after.
	if !strings.HasPrefix(tail, "[truncated") {
		t.Errorf("TruncateTail put its notice somewhere other than first:\n%.120s", tail)
	}
}

// TestTruncateUnderCapIsUnchanged: a result that fits must come back
// byte-identical. Anything else would make every small tool result differ from
// what the tool produced.
func TestTruncateUnderCapIsUnchanged(t *testing.T) {
	s := "short output\n"
	for name, fn := range map[string]func(string, int, string) (string, int){
		"head": TruncateHead, "tail": TruncateTail,
	} {
		out, dropped := fn(s, 4096, "hint")
		if out != s || dropped != 0 {
			t.Errorf("%s: under-cap input was modified: %q (dropped=%d)", name, out, dropped)
		}
	}
}

// TestTruncateProducesValidUTF8 guards the failure that looks like a model
// problem: a byte cut through a multi-byte rune yields invalid UTF-8, which
// some providers reject and others silently mangle.
func TestTruncateProducesValidUTF8(t *testing.T) {
	body := strings.Repeat("日本語テキスト", 4000) // 3 bytes per rune, no newlines
	for name, fn := range map[string]func(string, int, string) (string, int){
		"head": TruncateHead, "tail": TruncateTail,
	} {
		// Sweep limits so the cut lands mid-rune at some of them.
		for limit := 1000; limit < 1010; limit++ {
			out, _ := fn(body, limit, "")
			if !utf8.ValidString(out) {
				t.Fatalf("%s(limit=%d) produced invalid UTF-8", name, limit)
			}
		}
	}
}

// TestShellNoticePreservesItsRecoveryPath: shell was the only site in the tree
// whose truncation notice named a way to get the rest, and P64.3's convention
// pass must not have flattened that away while standardising the wording.
func TestShellNoticePreservesItsRecoveryPath(t *testing.T) {
	out, _ := TruncateTail(strings.Repeat("x\n", 40000), 24<<10, "use background:true and task_output for large commands")
	for _, want := range []string{"background:true", "task_output"} {
		if !strings.Contains(out, want) {
			t.Errorf("shell truncation notice no longer names %q:\n%.200s", want, out)
		}
	}
}

// maxResultCapTokens is the direction this test pins, not a target: a byte cap
// priced above it cannot bind before the context window does, which is the
// defect P64.3 found in shell's 200 KiB (51,200 estimated tokens) and git's
// 100 KiB (25,600). 8,192 tokens is 32 KiB at tokenest's ASCII rate — half a
// 16k window, which is the smallest window the local profile plans against
// once a model is actually loaded, and still two full base prompts.
//
// This is a *shape* assertion, deliberately loose enough that tuning an
// individual cap does not trip it. The instrument that reports the real numbers
// is TestResultSizeComposition, which gates nothing on purpose.
const maxResultCapTokens = 8192

// TestResultCapsCanBindBeforeTheContextWindow enumerates every byte cap that
// bounds a model-facing result. A new capping tool belongs in this list; a cap
// that cannot pass it is not a cap.
//
// Mutation check (run 2026-08-14): restoring shell's pre-P64.3 200 << 10 fails
// this at 51,200 tokens, and git's 100 << 10 fails at 25,600 — the two values
// the item was filed against. Setting maxResultCapTokens to 6,000 fails on git
// and read_file, confirming the bound is not vacuous at the current values.
func TestResultCapsCanBindBeforeTheContextWindow(t *testing.T) {
	caps := map[string]int{
		"shell (maxShellOutput)":          maxShellOutput,
		"git (maxGitOutput)":              maxGitOutput,
		"pre-commit test (maxTestOutput)": maxTestOutput,
		"skill script":                    maxSkillScriptOutput,
		"read_file default window":        maxDefaultReadBytes,
		"web_fetch default max_chars":     20000,
		"team_inbox (maxInboxResult)":     maxInboxResult,
	}
	for name, n := range caps {
		tokens := tokenest.Estimate(strings.Repeat("a", n))
		if tokens > maxResultCapTokens {
			t.Errorf("%s caps at %d bytes = %d estimated tokens, over the %d-token bound — it cannot bind before a local context window does",
				name, n, tokens, maxResultCapTokens)
		}
	}
}

// TestCapItemsNeedsAnExplicitCappedFlag pins why capItems does not derive
// "capped" from len(items) == max: glob collects everything then cuts (exactly
// max is complete), grep stops walking at max (exactly max means more exist).
// Deriving it would put a false "more exist" notice on every glob that happens
// to match exactly 1000 files.
func TestCapItemsNeedsAnExplicitCappedFlag(t *testing.T) {
	items := make([]string, 10)
	for i := range items {
		items[i] = "f"
	}
	if got := capItems(items, 10, "glob", false); strings.Contains(got, "capped at") {
		t.Errorf("a complete result set carried a truncation notice: %q", got)
	}
	if got := capItems(items, 10, "grep", true); !strings.Contains(got, "capped at 10") {
		t.Errorf("a capped result set carried no notice: %q", got)
	}
}
