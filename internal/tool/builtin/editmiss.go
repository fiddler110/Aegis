package builtin

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// editMissHintMax bounds the bytes a miss hint adds to "old_string not found".
// The model pays for them on every miss, and a miss is the one result a small
// model produces several times in a row (P88.1).
const editMissHintMax = 640

// editMissLineMax caps each quoted file line inside a hint.
const editMissLineMax = 160

// editMissHint explains why old_string did not match content, so the retry
// has the answer the way edit_section's and fill_marker's misses already do
// (they list the headings or markers that exist). A bare "not found" left a
// small model re-sending the same wrong string — three times in a row on one
// file in the P88 trial. Returns "" when it has nothing more useful to say.
//
// Cheapest diagnosis first: escaped newlines (the P88.2 signature, where the
// model's idea of the file and the file differ in kind, not detail), then a
// match that only fails on whitespace, then the closest line. It is a pure
// function of its inputs, so two identical misses still produce identical
// errors and the engine's repeated-failure breaker still sees them as one.
func editMissHint(content, oldStr string) string {
	var notes []string

	if !strings.Contains(oldStr, "\n") && strings.Contains(oldStr, `\n`) && !strings.Contains(content, `\n`) {
		notes = append(notes, `old_string contains the two characters \n but no real line break; the file has real line breaks — send old_string with actual newlines`)
	}
	if strings.Contains(content, "\r\n") && strings.Contains(oldStr, "\n") && !strings.Contains(oldStr, "\r\n") {
		if strings.Contains(content, strings.ReplaceAll(oldStr, "\n", "\r\n")) {
			notes = append(notes, "the file uses CRLF line endings; old_string matches once \\n is read as \\r\\n")
		}
	}

	lines := strings.Split(content, "\n")
	if n, ok := whitespaceInsensitiveMatch(lines, oldStr); ok {
		notes = append(notes, fmt.Sprintf("old_string matches at line %d if whitespace is ignored — copy the indentation exactly:\n%s", n+1, quoteLines(lines, n, n+strings.Count(strings.TrimRight(oldStr, "\n"), "\n"))))
	} else if n, score := closestLine(lines, firstMeaningfulLine(oldStr)); score > 0 {
		notes = append(notes, fmt.Sprintf("closest line in the file is %d:\n%s", n+1, quoteLines(lines, n-1, n+1)))
	}

	if len(notes) == 0 {
		return ""
	}
	hint := strings.Join(notes, "\n")
	if len(hint) > editMissHintMax {
		hint = hint[:editMissHintMax]
		for !utf8.ValidString(hint) {
			hint = hint[:len(hint)-1]
		}
		hint += "…"
	}
	return hint
}

// collapseSpace trims s and collapses each whitespace run to one space.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// whitespaceInsensitiveMatch reports the 0-based line where oldStr begins if
// it matches the file with every whitespace run collapsed. Only multi-token
// matches count: a one-word old_string that matches loosely says nothing.
func whitespaceInsensitiveMatch(lines []string, oldStr string) (int, bool) {
	want := collapseSpace(oldStr)
	if strings.Count(want, " ") < 1 {
		return 0, false
	}
	span := strings.Count(strings.TrimRight(oldStr, "\n"), "\n") + 1
	for i := range lines {
		end := i + span
		if end > len(lines) {
			break
		}
		if collapseSpace(strings.Join(lines[i:end], "\n")) == want {
			return i, true
		}
	}
	return 0, false
}

// firstMeaningfulLine returns old_string's first line with real content —
// usually the line a model anchors an edit on.
func firstMeaningfulLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); len(t) >= 4 {
			return t
		}
	}
	return ""
}

// closestLine finds the line sharing the most whitespace-separated tokens
// with target, scored as a fraction of target's tokens. A score under one
// half is reported as 0: a hint pointing at an unrelated line is worse than
// none, because the model will trust it.
func closestLine(lines []string, target string) (int, float64) {
	want := strings.Fields(target)
	if len(want) == 0 {
		return 0, 0
	}
	best, bestScore := 0, 0.0
	for i, l := range lines {
		have := map[string]bool{}
		for _, f := range strings.Fields(l) {
			have[f] = true
		}
		shared := 0
		for _, f := range want {
			if have[f] {
				shared++
			}
		}
		if s := float64(shared) / float64(len(want)); s > bestScore {
			best, bestScore = i, s
		}
	}
	if bestScore < 0.5 {
		return 0, 0
	}
	return best, bestScore
}

// quoteLines renders lines[from..to] (clamped, inclusive) as "  N| text".
func quoteLines(lines []string, from, to int) string {
	from = max(from, 0)
	to = min(to, len(lines)-1, from+4)
	var b strings.Builder
	for i := from; i <= to; i++ {
		l := strings.TrimRight(lines[i], "\r")
		if len(l) > editMissLineMax {
			l = l[:editMissLineMax]
			for !utf8.ValidString(l) {
				l = l[:len(l)-1]
			}
			l += "…"
		}
		fmt.Fprintf(&b, "  %d| %s\n", i+1, l)
	}
	return strings.TrimRight(b.String(), "\n")
}

// doubleEscapedContent reports whether write_file content looks like it was
// escaped twice: line breaks and tabs that arrived as the two characters `\n`
// and `\t` instead of the characters themselves (P88.2). Written as-is, a
// source file becomes one long line the compiler rejects at the first
// backslash — and the model, which cannot see that, spent the rest of the
// P88 trial run trying to patch it.
//
// It counts only escapes outside double-quoted and backquoted literals, so a
// one-line JSON fixture whose strings carry \n is left alone, and it fires
// only when real line breaks are scarce next to them, so ordinary source that
// prints "\n" is left alone too. It refuses rather than unescaping: rewriting
// content silently is a decision the model should make, with the reason.
func doubleEscapedContent(content string) (escapes, newlines int, suspect bool) {
	newlines = strings.Count(content, "\n")
	var quote byte
	for i := 0; i < len(content); i++ {
		c := content[i]
		switch {
		case c == '\\' && i+1 < len(content):
			if quote == 0 && (content[i+1] == 'n' || content[i+1] == 't') {
				escapes++
			}
			i++ // the escaped character never opens or closes a literal
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '`'):
			quote = c
		case c == '\n' && quote == '"':
			quote = 0 // a double-quoted literal does not span lines
		}
	}
	return escapes, newlines, escapes >= 3 && newlines*4 <= escapes
}
