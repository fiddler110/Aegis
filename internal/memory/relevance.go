package memory

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is a single memory item with its source context.
type Entry struct {
	Text   string  // the raw memory text
	Source string  // "user", "project", or skill name
	Score  float64 // relevance score (higher = more relevant)
	// ModTime is the mtime of the file the entry was read from, zero when
	// unknown. P67.5: the mtime is already stat'd to key the P8.5 relevance
	// cache, so carrying it here costs no extra I/O and lets FormatEntries
	// render a memory's age — often the thing that decides whether the model
	// should trust it.
	ModTime time.Time
}

// RecentTool is one tool call from the run's recent history, used by the
// P67.5 reference-vs-gotcha bias. Failed records whether that call returned an
// error: a tool that is being used *successfully* is exactly the case where a
// how-to-use-it reference entry is dead weight.
type RecentTool struct {
	Name   string
	Failed bool
}

// RecallOptions carries the per-run context around a LoadRelevant call
// (P67.5). Every field is optional; the zero value reproduces the pre-P67.5
// behavior exactly, which is what plain LoadRelevant passes.
type RecallOptions struct {
	// Surfaced is the run's already-injected set. It is a pointer that the
	// *caller* owns and scopes to one run — deliberately not package-level
	// state, because "already injected" is only meaningful within a single
	// run's conversation, and a global set would silently starve the next run
	// of the memories the previous one consumed. Nil means no dedupe.
	Surfaced *RecallState
	// RecentTools is the run's recent tool calls, most recent last. Empty
	// disables the reference-vs-gotcha bias.
	RecentTools []RecentTool
}

// RecallState remembers which entries a single run has already had injected,
// so a top-scoring entry is not re-injected on every turn it keeps winning
// (P67.5). It is safe for concurrent use and safe to call on a nil receiver,
// which is what makes RecallOptions{} — and therefore a zero-value Sources
// used in tests — behave exactly as before.
type RecallState struct {
	mu       sync.Mutex
	surfaced map[string]bool
}

// NewRecallState returns an empty per-run surfaced set. Construct one per run
// (never one per process): see the RecallOptions.Surfaced comment.
func NewRecallState() *RecallState { return &RecallState{} }

// surfaceKey identifies an entry across turns. Source+Text rather than a
// pointer or index: the underlying corpus is re-read and re-sorted between
// turns, so identity has to be by content.
func surfaceKey(e Entry) string { return e.Source + "\x00" + e.Text }

// Seen reports whether the entry has already been injected this run. A nil
// RecallState has seen nothing.
func (r *RecallState) Seen(e Entry) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.surfaced[surfaceKey(e)]
}

// Mark records the entry as injected. A nil RecallState records nothing.
func (r *RecallState) Mark(e Entry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.surfaced == nil {
		r.surfaced = make(map[string]bool)
	}
	r.surfaced[surfaceKey(e)] = true
}

// Reset forgets everything surfaced so far — for a run that has been compacted
// or rewound, where the model no longer holds the earlier injections.
func (r *RecallState) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.surfaced = nil
}

// LoadRelevant returns the top-K most relevant memory entries for the given
// query (typically the user's latest message). It scores each entry by keyword
// overlap using TF-IDF-like weighting. If maxTokens > 0, it truncates the
// result to approximately that many tokens (estimated at 4 chars/token).
//
// When query is empty, it falls back to returning all entries (same as Load).
//
// This is LoadRelevantFor with no per-run context: no dedupe, no tool bias.
func (s Sources) LoadRelevant(query string, maxEntries int, maxTokens int) []Entry {
	return s.LoadRelevantFor(query, maxEntries, maxTokens, RecallOptions{})
}

// LoadRelevantFor is LoadRelevant with the P67.5 per-run context applied:
// entries already injected earlier in the run are dropped, and scoring is
// biased by what the run has recently been doing with its tools. Entries it
// returns are marked surfaced, so the next turn of the same run will not see
// them again.
func (s Sources) LoadRelevantFor(query string, maxEntries int, maxTokens int, opts RecallOptions) []Entry {
	cached, df, n := s.cachedEntries()
	if len(cached) == 0 {
		return nil
	}
	// Copy before scoring/sorting in place: cached is shared (and may be
	// reused by concurrent callers) via the P8.5 relevance cache.
	entries := make([]Entry, 0, len(cached))
	// P67.5: drop already-surfaced entries BEFORE scoring, not after. Filtering
	// after the top-K cut would spend the entry budget on candidates that are
	// then thrown away, so a run whose top two memories were injected on turn 1
	// would get an empty recall block on turn 2 rather than the next two best.
	for _, e := range cached {
		if opts.Surfaced.Seen(e) {
			continue
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil
	}

	if query == "" || maxEntries <= 0 {
		if maxEntries <= 0 {
			maxEntries = len(entries)
		}
		if maxEntries > len(entries) {
			maxEntries = len(entries)
		}
		result := entries[:maxEntries]
		markSurfaced(opts.Surfaced, result)
		return result
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		markSurfaced(opts.Surfaced, entries)
		return entries
	}

	// Score each entry. The TF-IDF core is untouched by P67.5; the tool bias is
	// a multiplier applied around it, so an entry that matches nothing still
	// scores 0 and cannot be boosted into the result on tool context alone.
	for i := range entries {
		entries[i].Score = score(entries[i].Text, queryTerms, df, n) *
			toolBias(entries[i].Text, opts.RecentTools)
	}

	// Sort by score descending.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})

	// Limit entries.
	if maxEntries > len(entries) {
		maxEntries = len(entries)
	}
	result := entries[:maxEntries]

	// Limit by approximate token count.
	if maxTokens > 0 {
		charBudget := maxTokens * 4
		var kept []Entry
		chars := 0
		for _, e := range result {
			if chars+len(e.Text) > charBudget && len(kept) > 0 {
				break
			}
			kept = append(kept, e)
			chars += len(e.Text)
		}
		result = kept
	}

	markSurfaced(opts.Surfaced, result)
	return result
}

// markSurfaced records everything actually returned to a caller as injected
// (P67.5). Marking happens at the point of return rather than at the point of
// scoring, so entries dropped by the entry/token budget stay eligible for the
// next turn instead of being burned without ever reaching the model.
func markSurfaced(st *RecallState, entries []Entry) {
	if st == nil {
		return
	}
	for _, e := range entries {
		st.Mark(e)
	}
}

// Age-bucket thresholds for FormatEntries (P67.5). Deliberately coarse: the
// question a memory's age answers is "is this still likely to be true?", which
// needs an order of magnitude, not a timestamp — and a coarse bucket also
// costs fewer prompt tokens than a date.
const (
	// ageRecentCutoff: below this an entry is rendered "just now" — no useful
	// distinction between 3 and 40 minutes old.
	ageRecentCutoff = time.Hour
	// ageHourlyCutoff: below this the age renders in whole hours.
	ageHourlyCutoff = 24 * time.Hour
	// ageDailyCutoff: below this the age renders in whole days; above it, in
	// whole months (a "month" being 30 days here — calendar-accurate months
	// would buy nothing at this resolution).
	ageDailyCutoff = 30 * 24 * time.Hour
	ageMonth       = 30 * 24 * time.Hour
)

// formatAge renders an entry's age into the coarse bucket ladder above.
// Negative ages (a file mtime in the future — clock skew, or a checkout that
// stamped the future) render as "just now" rather than a nonsense negative.
func formatAge(d time.Duration) string {
	switch {
	case d < ageRecentCutoff:
		return "just now"
	case d < ageHourlyCutoff:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < ageDailyCutoff:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	default:
		return fmt.Sprintf("%dmo ago", int(d/ageMonth))
	}
}

// FormatEntries renders scored entries into a prompt-ready string, annotating
// each with its age (P67.5).
func FormatEntries(entries []Entry) string {
	return FormatEntriesAt(entries, time.Now())
}

// FormatEntriesAt is FormatEntries with the reference "now" injected, so the
// age rendering is testable without sleeping or faking file mtimes.
//
// P67.5: an entry with a zero ModTime (source unknown, or a hand-built Entry)
// renders exactly as it did before this item — no annotation — rather than
// claiming an age derived from the zero time.
func FormatEntriesAt(entries []Entry, now time.Time) string {
	if len(entries) == 0 {
		return ""
	}
	var sections []string
	bySource := make(map[string][]string)
	var order []string
	for _, e := range entries {
		if _, seen := bySource[e.Source]; !seen {
			order = append(order, e.Source)
		}
		item := e.Text
		if !e.ModTime.IsZero() {
			item = "(" + formatAge(now.Sub(e.ModTime)) + ") " + item
		}
		bySource[e.Source] = append(bySource[e.Source], item)
	}
	for _, src := range order {
		items := bySource[src]
		var sb strings.Builder
		sb.WriteString("## " + src + "\n\n")
		for _, item := range items {
			sb.WriteString("- " + item + "\n")
		}
		sections = append(sections, sb.String())
	}
	return strings.Join(sections, "\n")
}

// relevanceSnapshot is the cached result of allEntries() plus its derived
// document-frequency table, valid as long as sig still matches the
// underlying files' mtime/size (P8.5).
type relevanceSnapshot struct {
	sig     string
	entries []Entry
	df      map[string]int
	n       float64
}

// cachedEntries returns allEntries() and its TF-IDF document-frequency table,
// rebuilding only when a source file's mtime/size has changed since the last
// call (P8.5) — otherwise every LoadRelevant call re-reads, re-tokenizes, and
// rebuilds the full document-frequency table from scratch even when nothing
// changed. Zero-value Sources (cache == nil, used in tests) always recompute.
func (s Sources) cachedEntries() ([]Entry, map[string]int, float64) {
	// P67.5: statSources is the single stat pass that both keys the cache and
	// supplies each entry's ModTime, so freshness costs no I/O the cache was
	// not already paying for.
	sig, mtimes := s.statSources()
	if s.cache == nil {
		entries := s.allEntries(mtimes)
		df, n := buildDF(entries)
		return entries, df, n
	}

	s.cache.mu.Lock()
	if s.cache.relevance.entries != nil && s.cache.relevance.sig == sig {
		snap := s.cache.relevance
		s.cache.mu.Unlock()
		return snap.entries, snap.df, snap.n
	}
	s.cache.mu.Unlock()

	entries := s.allEntries(mtimes)
	df, n := buildDF(entries)

	s.cache.mu.Lock()
	s.cache.relevance = relevanceSnapshot{sig: sig, entries: entries, df: df, n: n}
	s.cache.mu.Unlock()
	return entries, df, n
}

// buildDF computes the document-frequency table used for IDF weighting.
func buildDF(entries []Entry) (map[string]int, float64) {
	df := make(map[string]int)
	for _, e := range entries {
		seen := make(map[string]bool)
		for _, t := range tokenize(e.Text) {
			if !seen[t] {
				df[t]++
				seen[t] = true
			}
		}
	}
	return df, float64(len(entries))
}

// statSources builds a cheap fingerprint (mtime+size per source file, no file
// content read) used to detect when cachedEntries must rebuild, and — P67.5 —
// returns the per-path mtimes from that same stat pass so allEntries can stamp
// each Entry with the age of the file it came from. One walk, two consumers:
// adding a second stat pass just to learn ages would double the syscalls the
// P8.5 cache exists to avoid.
func (s Sources) statSources() (string, map[string]time.Time) {
	var sb strings.Builder
	mtimes := make(map[string]time.Time)
	stat := func(path string) {
		fi, err := os.Stat(path)
		if err != nil {
			sb.WriteString("-;")
			return
		}
		mtimes[path] = fi.ModTime()
		fmt.Fprintf(&sb, "%d:%d;", fi.ModTime().UnixNano(), fi.Size())
	}
	stat(s.GlobalMemoryPath())
	stat(s.ProjectMemoryPath())
	for _, dir := range s.skillDirs() {
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			sb.WriteString("-;")
			continue
		}
		names := make([]string, 0, len(dirEntries))
		for _, e := range dirEntries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			stat(filepath.Join(dir, name))
		}
	}
	return sb.String(), mtimes
}

// allEntries loads all memory entries from all sources. mtimes comes from the
// statSources pass and may be nil or missing a path, in which case the entry
// carries a zero ModTime and renders without an age (P67.5).
//
// Every entry from one memory.md shares that file's mtime: memory.md is
// append-only line-per-entry, so per-entry timestamps would need parsing the
// "- (YYYY-MM-DD)" prefix Append writes — and that prefix is absent from
// hand-edited files. The file mtime is the honest, always-available answer,
// and it is an upper bound on how stale any entry in the file can be trusted
// to be.
func (s Sources) allEntries(mtimes map[string]time.Time) []Entry {
	var entries []Entry

	globalPath := s.GlobalMemoryPath()
	if txt := readIfExists(globalPath); txt != "" {
		for _, line := range splitEntries(txt) {
			entries = append(entries, Entry{Text: line, Source: "User memory", ModTime: mtimes[globalPath]})
		}
	}
	projectPath := s.ProjectMemoryPath()
	if txt := readIfExists(projectPath); txt != "" {
		for _, line := range splitEntries(txt) {
			entries = append(entries, Entry{Text: line, Source: "Project memory", ModTime: mtimes[projectPath]})
		}
	}
	for _, dir := range s.skillDirs() {
		dirEntries, _ := loadSkillEntries(dir, mtimes)
		entries = append(entries, dirEntries...)
	}
	return entries
}

func loadSkillEntries(dir string, mtimes map[string]time.Time) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, e := range dirEntries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		txt := readIfExists(path)
		if txt != "" {
			title := strings.TrimSuffix(name, ".md")
			entries = append(entries, Entry{Text: txt, Source: "Skill: " + title, ModTime: mtimes[path]})
		}
	}
	return entries, nil
}

// splitEntries breaks a memory file into individual entries (one per line,
// stripping leading "- " markers).
func splitEntries(text string) []string {
	var entries []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		entries = append(entries, line)
	}
	return entries
}

// score computes a TF-IDF-like relevance score for text against query terms.
func score(text string, queryTerms []string, df map[string]int, n float64) float64 {
	textTerms := tokenize(text)
	if len(textTerms) == 0 {
		return 0
	}

	// Term frequency in this document.
	tf := make(map[string]int)
	for _, t := range textTerms {
		tf[t]++
	}

	var total float64
	for _, q := range queryTerms {
		freq := tf[q]
		if freq == 0 {
			continue
		}
		docFreq := df[q]
		if docFreq == 0 {
			docFreq = 1
		}
		idf := math.Log(1 + n/float64(docFreq))
		total += float64(freq) * idf
	}

	// Normalize by document length to avoid bias toward long entries.
	return total / float64(len(textTerms))
}

// The P67.5 reference-vs-gotcha bias. These multiply the TF-IDF score of an
// entry that mentions a tool the run has recently called; an entry that
// mentions no such tool is untouched (factor 1).
//
// The rule, in full:
//
//	entry mentions a recently-called tool AND reads as a gotcha  -> boost
//	entry mentions a recently-called tool, reads as pure reference,
//	    and every recent call of that tool succeeded              -> damp
//	otherwise                                                     -> 1
//
// The asymmetry is deliberate. The boost does not care whether the call
// succeeded: a gotcha is most valuable at the moment the tool is in use, and a
// call that "succeeded" is exactly the case where a silent gotcha bites. The
// damp *does* care: a reference on how to use a tool is only dead weight once
// the model is demonstrably driving the tool correctly, so a single failing
// call withdraws the damp and lets the how-to back in.
//
// Both factors are bounded well inside one order of magnitude on purpose. The
// bias is meant to reorder near-ties, not to let tool context overrule the
// query — an entry the query does not match at all scores 0 and stays 0 under
// either factor.
const (
	gotchaBoostFactor   = 1.5
	referenceDampFactor = 0.5
)

// gotchaMarkers are the tokens that make an entry read as a gotcha rather than
// a reference. Token-level (post-tokenize) matching, so "don" is here to catch
// "don't" — tokenize splits on the apostrophe — as is "doesn" for "doesn't".
var gotchaMarkers = map[string]bool{
	"gotcha": true, "gotchas": true, "caveat": true, "caveats": true,
	"careful": true, "caution": true, "beware": true, "warning": true,
	"warn": true, "bug": true, "buggy": true, "broken": true, "breaks": true,
	"fails": true, "failing": true, "failure": true, "wrong": true,
	"never": true, "avoid": true, "don": true, "dont": true, "doesn": true,
	"silently": true, "pitfall": true, "trap": true, "crash": true,
	"crashes": true, "hangs": true, "deadlock": true, "surprising": true,
	"unexpected": true,
}

// toolBias returns the P67.5 multiplier for one entry given the run's recent
// tool calls. It returns 1 (no bias) when there is no tool context, or when
// the entry mentions none of the tools in it.
func toolBias(text string, recent []RecentTool) float64 {
	if len(recent) == 0 {
		return 1
	}
	// Collapse the call history to one record per tool: did we call it, and
	// did every call of it succeed?
	type usage struct{ allSucceeded bool }
	used := make(map[string]usage, len(recent))
	for _, c := range recent {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		if name == "" {
			continue
		}
		u, seen := used[name]
		if !seen {
			u.allSucceeded = true
		}
		if c.Failed {
			u.allSucceeded = false
		}
		used[name] = u
	}
	if len(used) == 0 {
		return 1
	}

	// Tool names survive tokenize intact (underscores are word characters
	// there), so "read_file" in an entry matches the read_file tool without
	// substring matching — which would also fire on an unrelated word that
	// happens to contain the name.
	terms := make(map[string]bool)
	isGotcha := false
	for _, t := range tokenize(text) {
		terms[t] = true
		if gotchaMarkers[t] {
			isGotcha = true
		}
	}

	damp := false
	for name, u := range used {
		if !terms[name] {
			continue
		}
		if isGotcha {
			// Boost wins outright over damp: an entry that names two tools and
			// carries a gotcha is a gotcha entry.
			return gotchaBoostFactor
		}
		if u.allSucceeded {
			damp = true
		}
	}
	if damp {
		return referenceDampFactor
	}
	return 1
}

// tokenize splits text into lowercase word tokens.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var word strings.Builder
	for _, r := range text {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			word.WriteRune(r)
		} else {
			if word.Len() > 0 {
				tokens = append(tokens, word.String())
				word.Reset()
			}
		}
	}
	if word.Len() > 0 {
		tokens = append(tokens, word.String())
	}
	return tokens
}
