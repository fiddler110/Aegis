package memory

import (
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
	entries := s.allEntries()
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
		return entries[:maxEntries]
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return entries
	}

	// Build document frequency for IDF weighting.
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
	n := float64(len(entries))

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
