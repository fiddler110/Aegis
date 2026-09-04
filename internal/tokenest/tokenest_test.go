package tokenest

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// TestEstimateDenseScriptsCostMoreThanASCII proves the estimator doesn't
// undercount CJK text the way a flat chars/4 heuristic would: the same number
// of runes in a dense script (each carrying roughly a full token's worth of
// information) must estimate to noticeably more tokens than the same rune count
// of plain ASCII.
func TestEstimateDenseScriptsCostMoreThanASCII(t *testing.T) {
	ascii := "aaaaaaaaaa" // 10 runes, ASCII
	cjk := "一二三四五六七八九十"   // 10 runes, CJK Unified Ideographs

	asciiEst := Estimate(ascii)
	cjkEst := Estimate(cjk)

	if asciiEst != 3 { // (10+3)/4 = 3
		t.Errorf("Estimate(ascii) = %d, want 3", asciiEst)
	}
	if cjkEst != 10 { // one token per dense-script rune
		t.Errorf("Estimate(cjk) = %d, want 10", cjkEst)
	}
	if cjkEst <= asciiEst {
		t.Errorf("expected CJK text to estimate to more tokens than the same rune count of ASCII: cjk=%d ascii=%d", cjkEst, asciiEst)
	}
}

// TestEstimateOtherScriptsMiddleGround proves non-ASCII, non-dense scripts
// (Cyrillic here) are priced between ASCII and CJK, not lumped in with either.
func TestEstimateOtherScriptsMiddleGround(t *testing.T) {
	cyrillic := "привет мир" // 9 Cyrillic letters + 1 ASCII space
	got := Estimate(cyrillic)
	if got != 6 { // 9 "other" runes -> (9+1)/2=5, plus 1 ASCII space -> (1+3)/4=1
		t.Errorf("Estimate(cyrillic) = %d, want 6", got)
	}
}

func TestIsDenseScript(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'a', false},
		{'一', true},  // CJK Unified Ideographs
		{'あ', true},  // Hiragana
		{'ア', true},  // Katakana
		{'한', true},  // Hangul syllable
		{'п', false}, // Cyrillic
		{'€', false}, // currency symbol
		{'😀', false}, // emoji
	}
	for _, tt := range tests {
		if got := isDenseScript(tt.r); got != tt.want {
			t.Errorf("isDenseScript(%q) = %v, want %v", tt.r, got, tt.want)
		}
	}
}

// TestMessagesIsScriptAware is the P41.1 regression guard: the whole-
// conversation estimate compaction gates on must count a CJK-heavy message
// far higher than a flat chars/4 heuristic would, so compaction the engine
// decides is needed is never silently no-op'd by a cruder estimate.
func TestMessagesIsScriptAware(t *testing.T) {
	cjk := "一二三四五六七八九十" // 10 CJK runes = 30 UTF-8 bytes; flat chars/4 -> 7
	msgs := []provider.Message{
		{Content: []provider.Block{provider.TextBlock{Text: cjk}}},
	}
	got := Messages("", msgs)
	if got != 10 { // one token per dense rune, script-aware
		t.Errorf("Messages(cjk) = %d, want 10", got)
	}
	flat := len(cjk) / 4 // the old undercounting heuristic
	if got <= flat {
		t.Errorf("script-aware estimate %d should exceed flat chars/4 estimate %d for CJK text", got, flat)
	}
}

// TestMessageCountsImageAndThinkingBlocks is the LLM-07 regression guard: both
// block types were priced at zero, so a vision turn or a reasoning model's
// replayed thinking was invisible to the one estimate that decides when to
// compact. Under-counting is the direction that hurts — it compacts late, after
// the backend has already dropped the oldest turns — so the assertions are that
// each block costs something, and that thinking costs at least its own text.
func TestMessageCountsImageAndThinkingBlocks(t *testing.T) {
	thinkText := "let me work through this carefully before answering"
	m := provider.Message{Content: []provider.Block{
		provider.ImageBlock{MediaType: "image/png", Data: "aGVsbG8="},
		provider.ThinkingBlock{Text: thinkText, Signature: "c2ln"},
	}}

	got := Message(m)
	if got <= ImageBlockTokens {
		t.Errorf("Message = %d, want more than the image charge alone (%d) — thinking is not free", got, ImageBlockTokens)
	}
	if want := ImageBlockTokens + Estimate(thinkText) + Estimate("c2ln"); got != want {
		t.Errorf("Message = %d, want %d (image constant + thinking text + signature)", got, want)
	}

	// An image's cost must not depend on how many base64 bytes it happens to
	// carry: the block holds compressed bytes and providers price pixels.
	big := provider.Message{Content: []provider.Block{
		provider.ImageBlock{MediaType: "image/png", Data: strings.Repeat("A", 100_000)},
	}}
	small := provider.Message{Content: []provider.Block{
		provider.ImageBlock{MediaType: "image/png", Data: "aGk="},
	}}
	if Message(big) != Message(small) {
		t.Errorf("image cost varies with encoded size: big=%d small=%d", Message(big), Message(small))
	}
}

// TestMessagePricesEveryWireBlockType fails when a new provider.Block type is
// added without a decision about what it costs. Counting a block as free is a
// defensible answer; not noticing is not, which is exactly how images and
// thinking stayed free (LLM-07).
func TestMessagePricesEveryWireBlockType(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block provider.Block
	}{
		{"text", provider.TextBlock{Text: "hello there friend"}},
		{"tool_use", provider.ToolUseBlock{Name: "read_file", Input: []byte(`{"path":"a.go"}`)}},
		{"tool_result", provider.ToolResultBlock{Content: "file contents here"}},
		{"image", provider.ImageBlock{MediaType: "image/png", Data: "aGk="}},
		{"thinking", provider.ThinkingBlock{Text: "reasoning about the task"}},
	} {
		if n := Message(provider.Message{Content: []provider.Block{tc.block}}); n <= 0 {
			t.Errorf("%s block estimated at %d tokens; it is on the wire and is not free", tc.name, n)
		}
	}
}
