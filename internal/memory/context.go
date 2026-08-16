package memory

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fiddler110/aegis/internal/trust"
)

// contextFiles are well-known context files loaded into the system prompt.
var contextFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	".aegis/context.md",
}

// LoadContext reads well-known context files (AGENTS.md, CLAUDE.md,
// .aegis/context.md) from the project root and returns their combined
// content, uncapped. Files that don't exist are silently skipped. When the
// Sources was created via NewSources, results are cached for cacheMaxAge.
//
// Callers on a budget want LoadContextCapped instead.
func (s Sources) LoadContext() string {
	return s.LoadContextCapped(0)
}

// LoadContextCapped is LoadContext with a total byte budget spent across the
// context files' contents, in the contextFiles order above. maxBytes <= 0
// means uncapped, which is what LoadContext passes.
//
// The budget is total rather than per-file: three files each held to a
// per-file cap is a three-times-larger block than the caller asked for, and
// the caller's constraint is on the assembled prompt. It covers file *content*
// only — each file additionally carries its `# <name>` header and the
// trust.Wrap provenance envelope, together about 150 bytes, which are not
// negotiable content and so are not charged against it.
//
// Posture, per the table in internal/tool/builtin/truncate.go: head kept, tail
// dropped, notice inside the kept text, and a recovery sentence naming
// read_file — a context file is a document whose orienting material is at the
// top, and unlike a tool result the dropped bytes are always still on disk at
// a path the model already knows. Truncation happens before wrapContextFile so
// the provenance envelope is never cut through the middle, and a file that
// arrives with no budget left is announced rather than silently dropped: these
// files are instructions, and a model reasoning from a silently partial set of
// them is the failure this cap must not introduce while fixing the other one.
func (s Sources) LoadContextCapped(maxBytes int) string {
	if s.cache != nil {
		s.cache.mu.Lock()
		defer s.cache.mu.Unlock()
		if s.cache.ctxCap == maxBytes && time.Now().Before(s.cache.ctxExpiry) {
			return s.cache.ctxVal
		}
		v := s.loadContextDirect(maxBytes)
		s.cache.ctxVal = v
		s.cache.ctxCap = maxBytes
		s.cache.ctxExpiry = time.Now().Add(cacheMaxAge)
		return v
	}
	return s.loadContextDirect(maxBytes)
}

// contextFileRecovery is the notice's recovery sentence. Named rather than
// inlined because the omitted-entirely arm below states the same thing.
const contextFileRecovery = "read_file on this path returns the rest"

// minContextFileBytes is the least content a truncated context file is worth
// emitting. Below it the kept head is a title and half a sentence — worse than
// the omission notice, which at least reports the file's size honestly.
const minContextFileBytes = 400

func (s Sources) loadContextDirect(maxBytes int) string {
	var sections []string
	budget := maxBytes
	for _, name := range contextFiles {
		path := filepath.Join(s.ProjectRoot, name)
		txt := readIfExists(path)
		if txt == "" {
			continue
		}
		if maxBytes > 0 {
			if budget < minContextFileBytes && len(txt) > budget {
				sections = append(sections, "# "+name+"\n\n"+omittedContextNotice(name, len(txt)))
				continue
			}
			txt = truncateContextHead(txt, budget)
			budget -= len(txt)
		}
		sections = append(sections, "# "+name+"\n\n"+wrapContextFile(name, txt))
	}
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

// truncateContextHead keeps the head of s within limit bytes *including* the
// notice, mirroring builtin.TruncateHead — which this cannot call, because
// internal/tool/builtin already imports this package. The wording is kept
// identical to truncationNotice's on purpose: P64.3's point was that the tree
// has one phrasing for "you are looking at part of something", not six.
func truncateContextHead(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	keep := limit - len(contextTruncationNotice(len(s), len(s), len(s))) - 1 // -1 for the separating newline
	if keep < 0 {
		keep = 0
	}
	kept := s[:min(keep, len(s))]
	// Prefer a line boundary; failing that, at least a valid rune boundary.
	if i := strings.LastIndexByte(kept, '\n'); i > len(kept)/2 {
		kept = kept[:i]
	} else {
		for len(kept) > 0 && !utf8.ValidString(kept) {
			kept = kept[:len(kept)-1]
		}
	}
	return kept + "\n" + contextTruncationNotice(len(kept), len(s), len(s)-len(kept))
}

func contextTruncationNotice(kept, total, dropped int) string {
	return fmt.Sprintf("[truncated: showing %d of %d bytes; %d bytes dropped from the end. %s.]",
		kept, total, dropped, contextFileRecovery)
}

func omittedContextNotice(name string, total int) string {
	return fmt.Sprintf("[omitted: %s is %d bytes and the context-file budget was spent on the files above. %s.]",
		name, total, contextFileRecovery)
}

// wrapContextFile tags a project context file's (AGENTS.md/CLAUDE.md/
// .aegis/context.md) content with the same untrusted-provenance marker used
// for file-loaded personas and skills (FIND-05/P24.4). These files live at
// the project root, not compiled into the binary — a cloned repo or
// dependency could plant one to inject instructions into every session
// opened against it, the same risk P24.4 addressed for persona/skill bodies.
// scan=false for the same reason as the persona/skill wrap: this content is
// re-injected every session and legitimately discusses its own
// instructions/role often enough that the heuristic scan would be noisy
// here; the provenance framing itself is the mitigation.
func wrapContextFile(name, content string) string {
	return trust.Wrap("context_untrusted_content", [][2]string{{"file", name}},
		"a "+name+" file loaded from the project root, not authored by Aegis",
		content, false)
}

// LoadIgnorePatterns reads a .aegisignore file from the project root
// and returns the patterns (one per line, # comments stripped). These patterns
// can be used to exclude files from agent operations.
func (s Sources) LoadIgnorePatterns() []string {
	path := filepath.Join(s.ProjectRoot, ".aegisignore")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// ShouldIgnore checks whether path matches any of the ignore patterns.
// Patterns use filepath.Match semantics (glob).
func ShouldIgnore(path string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, filepath.Base(path)); matched {
			return true
		}
		if matched, _ := filepath.Match(p, path); matched {
			return true
		}
	}
	return false
}
