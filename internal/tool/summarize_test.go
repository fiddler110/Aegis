package tool

import (
	"strings"
	"testing"
)

type shortTool struct {
	fakeTool
	short string
}

func (s *shortTool) ShortDescription() string { return s.short }

// TestSummarizePrefersShortDescription: the interface is the escape hatch for a
// tool whose first sentence is a bad advertisement, so a declared summary has to
// win outright rather than being merged with or truncated against the derived
// one.
func TestSummarizePrefersShortDescription(t *testing.T) {
	tl := &shortTool{
		fakeTool: fakeTool{name: "security_scan", desc: "Run available security scanners (opengrep, trivy, gitleaks) over the workspace. Then a great deal more prose about baselines, container images and per-scanner policy."},
		short:    "Scan the workspace for security findings.",
	}
	if got := Summarize(tl); got != tl.short {
		t.Errorf("Summarize() = %q, want the declared ShortDescription %q", got, tl.short)
	}

	// An empty declaration falls back rather than emitting a bare tool name: a
	// blank line in <deferred_tools> advertises nothing at all.
	tl.short = "   "
	if got := Summarize(tl); !strings.HasPrefix(got, "Run available security scanners") {
		t.Errorf("Summarize() with a blank ShortDescription = %q, want the derived fallback", got)
	}
}

// TestSummarizeDerivesFirstSentence covers the fallback the majority of tools
// use. The parenthesis case is the one that matters in practice: these
// descriptions open with parenthetical tool lists containing "e.g. ", and a
// naive first-period cut ends the sentence inside the parentheses, producing an
// advertisement that names a tool and then stops.
func TestSummarizeDerivesFirstSentence(t *testing.T) {
	cases := []struct {
		name string
		desc string
		want string
	}{
		{
			name: "plain first sentence",
			desc: "Delete a cron job by id. The id comes from cron_list.",
			want: "Delete a cron job by id.",
		},
		{
			name: "period inside parentheses does not end it",
			desc: "Query the map (files, symbols, e.g. imports) on demand. More prose here.",
			want: "Query the map (files, symbols, e.g. imports) on demand.",
		},
		{
			name: "single sentence passes through whole",
			desc: "Search the web and return a list of result titles, URLs, and snippets.",
			want: "Search the web and return a list of result titles, URLs, and snippets.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Summarize(&fakeTool{name: "t", desc: tc.desc}); got != tc.want {
				t.Errorf("Summarize() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSummarizeCapsRunOnSentence is the boundary this exists for: the expensive
// descriptions do not have a second sentence to cut at, they have one clause
// that runs for two thousand bytes. The cap is what turns those into a line.
func TestSummarizeCapsRunOnSentence(t *testing.T) {
	long := "Run available security scanners over the workspace and return normalized findings with severity, location, rule, remediation and reachability, deduped across overlapping tools and tagged with an OWASP ASVS chapter, honoring a path-scoped baseline file."
	got := Summarize(&fakeTool{name: "t", desc: long})
	if len(got) > summaryMaxChars+len("…") {
		t.Errorf("Summarize() = %d bytes, want at most %d", len(got), summaryMaxChars+len("…"))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("Summarize() = %q, want an ellipsis marking that it was cut — a silently truncated line reads as a complete description", got)
	}
	// A word-boundary cut, not a mid-word one: the last word must be intact.
	if strings.Contains(strings.TrimSuffix(got, "…"), "  ") || strings.HasSuffix(strings.TrimSuffix(got, "…"), " ") {
		t.Errorf("Summarize() = %q, want no trailing whitespace before the ellipsis", got)
	}
	if !strings.HasPrefix(long, strings.TrimSuffix(got, "…")) {
		t.Errorf("Summarize() = %q, which is not a prefix of the description", got)
	}
}

// TestSummaryMaxCharsIsInABand pins the threshold itself, with literal numbers
// rather than the constant. Every other test here uses summaryMaxChars
// symbolically and so would survive any mutation of it — the failure mode this
// repo has now hit on three consecutive passes. Both edges are real: too low and
// the advertisement stops identifying the tool (a 40-char cap leaves
// security_scan as "Scan the workspace or a container image f…"), too high and
// the block drifts back toward printing the manuals it was shortened to avoid.
func TestSummaryMaxCharsIsInABand(t *testing.T) {
	if summaryMaxChars < 100 || summaryMaxChars > 200 {
		t.Errorf("summaryMaxChars = %d, want 100..200: below that a summary stops identifying its tool, above it the block stops being a saving", summaryMaxChars)
	}
	// And the cap has to bite on a description of the size that motivated it.
	// 2,356 bytes is security_scan's, measured.
	long := strings.Repeat("scanner findings and remediation detail ", 60)
	if got := Summarize(&fakeTool{name: "t", desc: long}); len(got) > 210 {
		t.Errorf("Summarize() of a %d-byte description = %d bytes, want it capped", len(long), len(got))
	}
}

// TestSummarizeCapDoesNotSplitRune: these descriptions carry em dashes and
// arrows, and a byte-index cut through one puts a replacement character into
// every turn's system prompt.
func TestSummarizeCapDoesNotSplitRune(t *testing.T) {
	// One long unbroken run of multi-byte runes, so no word boundary exists
	// anywhere near the cap and the hard fallback path is the one taken.
	desc := strings.Repeat("—", 200)
	got := Summarize(&fakeTool{name: "t", desc: desc})
	if strings.ContainsRune(got, '�') {
		t.Errorf("Summarize() = %q, which contains a replacement character — the cap split a rune", got)
	}
	if !strings.HasPrefix(desc, strings.TrimSuffix(got, "…")) {
		t.Errorf("Summarize() = %q, which is not a prefix of the description", got)
	}
}

// TestDeferredCarriesBothSummaryAndDescription is the split the prompt saving
// depends on: the block prints Summary, while SearchDeferred keeps matching
// against the full Description. If Info ever carried only the short form, every
// keyword trimmed out of a summary would become unfindable — a silent loss of
// tool access, which is exactly what the closure condition forbids.
func TestDeferredCarriesBothSummaryAndDescription(t *testing.T) {
	r := NewRegistry()
	desc := "Run available security scanners over the workspace. Supports gitleaks, trufflehog and kubescape."
	if err := r.RegisterDeferred(&fakeTool{name: "security_scan", desc: desc}); err != nil {
		t.Fatal(err)
	}
	def := r.Deferred()
	if len(def) != 1 {
		t.Fatalf("Deferred() = %d entries, want 1", len(def))
	}
	if def[0].Description != desc {
		t.Errorf("Info.Description = %q, want the full description", def[0].Description)
	}
	if def[0].Summary != "Run available security scanners over the workspace." {
		t.Errorf("Info.Summary = %q, want the first sentence", def[0].Summary)
	}
	if len(def[0].Summary) >= len(def[0].Description) {
		t.Errorf("Info.Summary (%d bytes) is not shorter than Info.Description (%d bytes) — the block would save nothing",
			len(def[0].Summary), len(def[0].Description))
	}
	// "kubescape" appears only in the part the prompt no longer prints.
	if got := r.SearchDeferred("kubescape"); len(got) != 1 {
		t.Errorf("SearchDeferred(%q) = %d matches, want 1: a term dropped from the summary must still be findable", "kubescape", len(got))
	}
}

var _ ShortDescriber = (*shortTool)(nil)
