package memory

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is a single memory item with its source context.
type Entry struct {
	Text   string  // the raw memory text
	Source string  // "user", "project", or skill name
	Score  float64 // relevance score (higher = more relevant)
}

// LoadRelevant returns the top-K most relevant memory entries for the given
// query (typically the user's latest message). It scores each entry by keyword
// overlap using TF-IDF-like weighting. If maxTokens > 0, it truncates the
// result to approximately that many tokens (estimated at 4 chars/token).
//
// When query is empty, it falls back to returning all entries (same as Load).
func (s Sources) LoadRelevant(query string, maxEntries int, maxTokens int) []Entry {
	cached, df, n := s.cachedEntries()
	if len(cached) == 0 {
		return nil
	}
	// Copy before scoring/sorting in place: cached is shared (and may be
	// reused by concurrent callers) via the P8.5 relevance cache.
	entries := make([]Entry, len(cached))
	copy(entries, cached)

	if query == "" || maxEntries <= 0 {
		if maxEntries <= 0 {
			maxEntries = len(entries)
		}
		if maxEntries > len(entries) {
			maxEntries = len(entries)
		}
		return entries[:maxEntries]
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return entries
	}

	// Score each entry.
	for i := range entries {
		entries[i].Score = score(entries[i].Text, queryTerms, df, n)
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

	return result
}

// FormatEntries renders scored entries into a prompt-ready string.
func FormatEntries(entries []Entry) string {
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
		bySource[e.Source] = append(bySource[e.Source], e.Text)
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
	if s.cache == nil {
		entries := s.allEntries()
		df, n := buildDF(entries)
		return entries, df, n
	}

	sig := s.entriesSignature()
	s.cache.mu.Lock()
	if s.cache.relevance.entries != nil && s.cache.relevance.sig == sig {
		snap := s.cache.relevance
		s.cache.mu.Unlock()
		return snap.entries, snap.df, snap.n
	}
	s.cache.mu.Unlock()

	entries := s.allEntries()
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

// entriesSignature builds a cheap fingerprint (mtime+size per source file,
// no file content read) used to detect when cachedEntries must rebuild.
func (s Sources) entriesSignature() string {
	var sb strings.Builder
	stat := func(path string) {
		fi, err := os.Stat(path)
		if err != nil {
			sb.WriteString("-;")
			return
		}
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
	return sb.String()
}

// allEntries loads all memory entries from all sources.
func (s Sources) allEntries() []Entry {
	var entries []Entry

	if txt := readIfExists(s.GlobalMemoryPath()); txt != "" {
		for _, line := range splitEntries(txt) {
			entries = append(entries, Entry{Text: line, Source: "User memory"})
		}
	}
	if txt := readIfExists(s.ProjectMemoryPath()); txt != "" {
		for _, line := range splitEntries(txt) {
			entries = append(entries, Entry{Text: line, Source: "Project memory"})
		}
	}
	for _, dir := range s.skillDirs() {
		dirEntries, _ := loadSkillEntries(dir)
		entries = append(entries, dirEntries...)
	}
	return entries
}

func loadSkillEntries(dir string) ([]Entry, error) {
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
		txt := readIfExists(filepath.Join(dir, name))
		if txt != "" {
			title := strings.TrimSuffix(name, ".md")
			entries = append(entries, Entry{Text: txt, Source: "Skill: " + title})
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
