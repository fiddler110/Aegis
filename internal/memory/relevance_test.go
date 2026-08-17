package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadRelevantScoresHigherForMatching(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- The database uses PostgreSQL for persistence\n"+
			"- The frontend is built with React and TypeScript\n"+
			"- CI runs on GitHub Actions\n"+
			"- Security scans use Opengrep for static analysis\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: dataDir}
	entries := src.LoadRelevant("database migration PostgreSQL", 2, 0)

	if len(entries) == 0 {
		t.Fatal("expected entries")
	}
	// The database/PostgreSQL entry should score highest.
	if entries[0].Score <= 0 {
		t.Error("expected positive score for top entry")
	}
	if len(entries) > 1 && entries[0].Score < entries[1].Score {
		t.Error("top entry should have highest score")
	}
}

// TestLoadRelevantCacheInvalidatesOnFileChange verifies the P8.5 relevance
// cache (only active on a NewSources-constructed Sources, which carries a
// cache pointer) picks up edits to the underlying memory file rather than
// serving a stale snapshot forever, and that scoring a query doesn't corrupt
// the cached entries for a subsequent, differently-scored query.
func TestLoadRelevantCacheInvalidatesOnFileChange(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	memPath := filepath.Join(memDir, "memory.md")
	os.WriteFile(memPath, []byte("- The database uses PostgreSQL\n"), 0o644)

	src := NewSources(root, dataDir)

	first := src.LoadRelevant("database", 5, 0)
	if len(first) != 1 {
		t.Fatalf("got %d entries, want 1", len(first))
	}

	// A second call with a different query must not have its scores polluted
	// by the first call's in-place mutation of any shared cached slice.
	second := src.LoadRelevant("nonexistent-term-xyz", 5, 0)
	if len(second) != 1 {
		t.Fatalf("got %d entries, want 1", len(second))
	}
	if second[0].Score != 0 {
		t.Errorf("query with no matching terms should score 0, got %v (cache corruption?)", second[0].Score)
	}
	// Re-run the original query to confirm its score wasn't corrupted either.
	firstAgain := src.LoadRelevant("database", 5, 0)
	if firstAgain[0].Score != first[0].Score {
		t.Errorf("score changed across calls: %v -> %v", first[0].Score, firstAgain[0].Score)
	}

	// Editing the file (mtime/size change) must be reflected on the next call.
	os.WriteFile(memPath, []byte("- The database uses PostgreSQL\n- A brand new entry about widgets\n"), 0o644)
	updated := src.LoadRelevant("widgets", 5, 0)
	if len(updated) != 2 {
		t.Fatalf("after file edit, got %d entries, want 2 (cache not invalidated?)", len(updated))
	}
}

func TestLoadRelevantRespectsMaxEntries(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- entry one\n- entry two\n- entry three\n- entry four\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: dataDir}
	entries := src.LoadRelevant("one", 2, 0)
	if len(entries) > 2 {
		t.Errorf("expected at most 2 entries, got %d", len(entries))
	}
}

func TestLoadRelevantMaxTokens(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)

	// Each entry is ~20 chars = ~5 tokens. With maxTokens=8, should get ~1-2.
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- short entry alpha\n- short entry beta\n- short entry gamma\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: dataDir}
	entries := src.LoadRelevant("alpha", 10, 8)
	if len(entries) > 2 {
		t.Errorf("expected token-limited entries, got %d", len(entries))
	}
}

func TestLoadRelevantEmptyQuery(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte("- one\n- two\n"), 0o644)

	src := Sources{ProjectRoot: root, DataDir: dataDir}
	entries := src.LoadRelevant("", 10, 0)
	if len(entries) != 2 {
		t.Errorf("expected all 2 entries for empty query, got %d", len(entries))
	}
}

func TestLoadRelevantNoMemory(t *testing.T) {
	src := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	entries := src.LoadRelevant("test", 10, 0)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestFormatEntries(t *testing.T) {
	entries := []Entry{
		{Text: "uses PostgreSQL", Source: "Project memory"},
		{Text: "React frontend", Source: "Project memory"},
		{Text: "run tests first", Source: "User memory"},
	}
	out := FormatEntries(entries)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !contains(out, "Project memory") || !contains(out, "User memory") {
		t.Errorf("expected source headers in output: %s", out)
	}
}

// --- P67.5: already-surfaced dedupe -----------------------------------------

// TestLoadRelevantForDedupesAcrossTurns verifies that an entry injected on one
// turn of a run is not injected again on the next turn of the same run, and
// that the freed entry budget goes to the next-best candidate rather than
// shrinking the recall block — the "filter before scoring" requirement.
func TestLoadRelevantForDedupesAcrossTurns(t *testing.T) {
	root := t.TempDir()
	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- alpha widget one\n- alpha widget two\n- alpha widget three\n- alpha widget four\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: t.TempDir()}
	run := NewRecallState()

	first := src.LoadRelevantFor("alpha widget", 2, 0, RecallOptions{Surfaced: run})
	if len(first) != 2 {
		t.Fatalf("turn 1: got %d entries, want 2", len(first))
	}
	second := src.LoadRelevantFor("alpha widget", 2, 0, RecallOptions{Surfaced: run})
	// Filtering before scoring is what makes this 2 and not 0: had the filter
	// run after the top-K cut, both survivors would have been the same two
	// already-surfaced entries and the turn would recall nothing.
	if len(second) != 2 {
		t.Fatalf("turn 2: got %d entries, want 2 (dedupe must filter before the top-K cut)", len(second))
	}
	seen := map[string]bool{}
	for _, e := range first {
		seen[e.Text] = true
	}
	for _, e := range second {
		if seen[e.Text] {
			t.Errorf("turn 2 re-injected an already-surfaced entry: %q", e.Text)
		}
	}

	// Exhausting the corpus yields nothing rather than falling back to repeats.
	third := src.LoadRelevantFor("alpha widget", 2, 0, RecallOptions{Surfaced: run})
	if len(third) != 0 {
		t.Errorf("turn 3: got %d entries, want 0 once every entry has surfaced", len(third))
	}

	// Reset (compaction / rewind) makes them eligible again.
	run.Reset()
	if again := src.LoadRelevantFor("alpha widget", 2, 0, RecallOptions{Surfaced: run}); len(again) != 2 {
		t.Errorf("after Reset: got %d entries, want 2", len(again))
	}
}

// TestRecallStateIsPerRunNotGlobal pins that two runs do not share a surfaced
// set, and that a nil RecallState (the LoadRelevant / zero-value path) never
// dedupes.
func TestRecallStateIsPerRunNotGlobal(t *testing.T) {
	root := t.TempDir()
	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte("- alpha widget one\n"), 0o644)

	src := Sources{ProjectRoot: root, DataDir: t.TempDir()}

	runA := NewRecallState()
	if got := src.LoadRelevantFor("alpha", 5, 0, RecallOptions{Surfaced: runA}); len(got) != 1 {
		t.Fatalf("run A: got %d, want 1", len(got))
	}
	if got := src.LoadRelevantFor("alpha", 5, 0, RecallOptions{Surfaced: runA}); len(got) != 0 {
		t.Fatalf("run A second turn: got %d, want 0", len(got))
	}
	runB := NewRecallState()
	if got := src.LoadRelevantFor("alpha", 5, 0, RecallOptions{Surfaced: runB}); len(got) != 1 {
		t.Errorf("run B must not inherit run A's surfaced set: got %d, want 1", len(got))
	}
	// No state at all: repeated calls keep returning the entry.
	for i := 0; i < 3; i++ {
		if got := src.LoadRelevant("alpha", 5, 0); len(got) != 1 {
			t.Fatalf("LoadRelevant call %d: got %d, want 1 (nil state must not dedupe)", i, len(got))
		}
	}
}

// TestLoadRelevantForMarksOnlyReturnedEntries pins that entries dropped by the
// token budget stay eligible for a later turn — they were never injected, so
// burning them would lose them silently.
func TestLoadRelevantForMarksOnlyReturnedEntries(t *testing.T) {
	root := t.TempDir()
	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- alpha one\n- alpha two\n- alpha three\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: t.TempDir()}
	run := NewRecallState()

	// maxTokens=3 (~12 chars) admits one entry; the other two must survive.
	first := src.LoadRelevantFor("alpha", 3, 3, RecallOptions{Surfaced: run})
	if len(first) != 1 {
		t.Fatalf("got %d entries, want 1 under the token budget", len(first))
	}
	rest := src.LoadRelevantFor("alpha", 3, 0, RecallOptions{Surfaced: run})
	if len(rest) != 2 {
		t.Errorf("got %d entries, want 2 (budget-dropped entries must stay eligible)", len(rest))
	}
}

// --- P67.5: freshness --------------------------------------------------------

// TestEntriesCarryFileModTime verifies the mtime read for the P8.5 cache
// signature is threaded through to Entry.
func TestEntriesCarryFileModTime(t *testing.T) {
	root := t.TempDir()
	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	memPath := filepath.Join(memDir, "memory.md")
	os.WriteFile(memPath, []byte("- alpha one\n"), 0o644)
	want := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(memPath, want, want); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	src := Sources{ProjectRoot: root, DataDir: t.TempDir()}
	entries := src.LoadRelevant("alpha", 5, 0)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].ModTime.IsZero() {
		t.Fatal("entry carries no ModTime")
	}
	if d := entries[0].ModTime.Sub(want); d > time.Second || d < -time.Second {
		t.Errorf("ModTime = %v, want ~%v", entries[0].ModTime, want)
	}
	if out := FormatEntries(entries); !strings.Contains(out, "3d ago") {
		t.Errorf("FormatEntries did not render the age: %q", out)
	}
}

// TestFormatAgeBucketBoundaries is a mutation test on the age ladder: every
// case sits exactly on a threshold, so moving ageRecentCutoff, ageHourlyCutoff
// or ageDailyCutoff by any amount flips at least one of them.
func TestFormatAgeBucketBoundaries(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want string
	}{
		{0, "just now"},
		{-5 * time.Minute, "just now"}, // future mtime / clock skew
		{time.Hour - time.Nanosecond, "just now"},
		{time.Hour, "1h ago"},
		{23*time.Hour + 59*time.Minute, "23h ago"},
		{24 * time.Hour, "1d ago"},
		{29*24*time.Hour + 23*time.Hour, "29d ago"},
		{30 * 24 * time.Hour, "1mo ago"},
		{59 * 24 * time.Hour, "1mo ago"},
		{60 * 24 * time.Hour, "2mo ago"},
	}
	for _, tc := range cases {
		if got := formatAge(tc.age); got != tc.want {
			t.Errorf("formatAge(%v) = %q, want %q", tc.age, got, tc.want)
		}
	}
}

// TestFormatEntriesOmitsAgeWhenModTimeUnknown pins that a hand-built Entry
// renders exactly as it did before P67.5.
func TestFormatEntriesOmitsAgeWhenModTimeUnknown(t *testing.T) {
	now := time.Now()
	out := FormatEntriesAt([]Entry{{Text: "uses PostgreSQL", Source: "Project memory"}}, now)
	if !strings.Contains(out, "- uses PostgreSQL\n") {
		t.Errorf("zero ModTime must render unannotated, got %q", out)
	}
	if strings.Contains(out, "ago") {
		t.Errorf("zero ModTime must not render an age, got %q", out)
	}
	dated := FormatEntriesAt([]Entry{{Text: "uses PostgreSQL", Source: "Project memory", ModTime: now.Add(-5 * 24 * time.Hour)}}, now)
	if !strings.Contains(dated, "- (5d ago) uses PostgreSQL") {
		t.Errorf("expected age-annotated line, got %q", dated)
	}
}

// --- P67.5: reference-vs-gotcha bias ----------------------------------------

// TestToolBiasRule walks the documented rule table. The multipliers are
// asserted as literals, so changing gotchaBoostFactor or referenceDampFactor
// fails here rather than silently re-weighting recall.
func TestToolBiasRule(t *testing.T) {
	ok := []RecentTool{{Name: "read_file"}}
	failed := []RecentTool{{Name: "read_file", Failed: true}}
	mixed := []RecentTool{{Name: "read_file"}, {Name: "read_file", Failed: true}}

	cases := []struct {
		name   string
		text   string
		recent []RecentTool
		want   float64
	}{
		{"no tool context at all", "read_file reads a file", nil, 1},
		{"entry mentions no recent tool", "grep searches the tree", ok, 1},
		{"reference for a succeeding tool is damped", "read_file reads a file from the workspace", ok, 0.5},
		{"gotcha for a succeeding tool is boosted", "read_file silently truncates a large file", ok, 1.5},
		{"reference is not damped once the tool has failed", "read_file reads a file from the workspace", failed, 1},
		{"one failure among successes withdraws the damp", "read_file reads a file from the workspace", mixed, 1},
		{"gotcha is boosted whether or not the tool failed", "beware: read_file follows symlinks", failed, 1.5},
		{"substring of a longer word does not count as a mention", "readfilesystem notes", ok, 1},
		{"gotcha wins over damp when both tools are mentioned", "shell is fine but read_file is broken on symlinks", []RecentTool{{Name: "shell"}, {Name: "read_file"}}, 1.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolBias(tc.text, tc.recent); got != tc.want {
				t.Errorf("toolBias = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestToolBiasReordersRecall is the end-to-end version: with identical query
// relevance, the gotcha entry outranks the reference entry only because the
// tool it warns about is in the run's recent calls.
func TestToolBiasReordersRecall(t *testing.T) {
	root := t.TempDir()
	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte(
		"- read_file loads workspace paths\n"+
			"- read_file loads workspace paths but silently truncates\n",
	), 0o644)

	src := Sources{ProjectRoot: root, DataDir: t.TempDir()}

	// Without tool context the reference entry wins on length normalization.
	plain := src.LoadRelevant("read_file workspace paths", 5, 0)
	if len(plain) != 2 {
		t.Fatalf("got %d entries, want 2", len(plain))
	}
	if strings.Contains(plain[0].Text, "truncates") {
		t.Fatalf("precondition failed: the gotcha entry already ranks first without bias")
	}

	biased := src.LoadRelevantFor("read_file workspace paths", 5, 0, RecallOptions{
		RecentTools: []RecentTool{{Name: "read_file"}},
	})
	if len(biased) != 2 {
		t.Fatalf("got %d entries, want 2", len(biased))
	}
	if !strings.Contains(biased[0].Text, "truncates") {
		t.Errorf("gotcha entry should rank first with the tool in recent calls, got %q", biased[0].Text)
	}
}

// TestToolBiasCannotResurrectAZeroScore pins that the bias is a multiplier
// around TF-IDF, not an additive override: an entry the query does not match
// stays at 0 no matter what the run has been calling.
func TestToolBiasCannotResurrectAZeroScore(t *testing.T) {
	root := t.TempDir()
	memDir := filepath.Join(root, ".aegis")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "memory.md"), []byte("- read_file is broken on symlinks\n"), 0o644)

	src := Sources{ProjectRoot: root, DataDir: t.TempDir()}
	got := src.LoadRelevantFor("unrelated-term-xyz", 5, 0, RecallOptions{
		RecentTools: []RecentTool{{Name: "read_file"}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Score != 0 {
		t.Errorf("score = %v, want 0 (bias must not manufacture relevance)", got[0].Score)
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! Go 1.22")
	expected := []string{"hello", "world", "go", "1", "22"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: got %q, want %q", i, tok, expected[i])
		}
	}
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
