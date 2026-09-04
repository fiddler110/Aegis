package compaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/provider"
)

// P65.2 — the file set a compaction carries forward.
//
// Before this, the model's record of which files a session had read and modified
// was whatever survived in the keep-recent tail plus whatever the free-form
// summary happened to mention — and after a *second* compaction, whatever the
// second summary happened to carry over from the first. A list of paths is a few
// dozen tokens; the alternative is the model re-discovering the workspace with
// glob and read, which is the expensive failure the P64.x line is circling from
// the result-size side.
//
// Two decisions here are worth stating because the obvious implementations of
// both are wrong.
//
// **The lists travel on the context, not on the Summarizer.** A Summarizer is
// built once per *server* (`s.compactor`, shared by every session) while a file
// set belongs to one conversation, so a `SetFiles` method would have sessions
// overwriting each other's paths — a cross-session leak, not just a wrong list.
// The lists are per-call request-scoped data about the caller, which is what a
// context value is for; this mirrors tool.WithWorkdir rather than the
// CalibratedCompactor seam, which carries a genuinely process-wide calibration.
//
// **Accumulation comes from the transcript, not from a counter.** The engine is
// rebuilt per request, so nothing on the Go side survives long enough to
// accumulate across compactions. The previous summary does: it is a message in
// the very prefix the next compaction is summarizing. So each summary re-emits
// the tagged lists, and the next one parses them back out and merges. The set
// grows monotonically across any number of compactions with no state anywhere,
// which is also why it survives a daemon restart that reloads the conversation.

// fileContextKey is the context key carrying one call's file lists.
type fileContextKey struct{}

// fileContext is the caller-supplied half of the carried-forward file set: the
// paths this run touched, which the caller classifies (the engine knows each
// tool's effective capability; this package would have to guess from tool names,
// and that table would rot the first time a tool was added).
type fileContext struct {
	read     []string
	modified []string
}

// WithFiles attaches the paths the caller has read and modified to ctx, for the
// next Compact call to fold into its summary (P65.2). It is the method half of
// engine.FileContextCompactor — a method rather than only a package function so
// the engine can discover the capability by type assertion without importing
// this package, which it must not do (engine's own tests import compaction, so
// the dependency would close a cycle).
//
// Safe to omit entirely: a compaction with no attached files still carries
// forward whatever a previous summary recorded.
func (s *Summarizer) WithFiles(ctx context.Context, read, modified []string) context.Context {
	return WithFiles(ctx, read, modified)
}

// WithFiles is the package-level form, for a caller holding no Summarizer.
func WithFiles(ctx context.Context, read, modified []string) context.Context {
	if len(read) == 0 && len(modified) == 0 {
		return ctx
	}
	return context.WithValue(ctx, fileContextKey{}, fileContext{read: read, modified: modified})
}

func filesFrom(ctx context.Context) fileContext {
	fc, _ := ctx.Value(fileContextKey{}).(fileContext)
	return fc
}

// The tags wrapping each list in a summary. They are parsed back out of the
// previous summary on the next compaction, so they are a wire format between
// successive summaries and not merely decoration — changing them silently
// breaks accumulation, which is why TestFileListsAccumulateAcrossCompactions
// drives two compactions rather than one.
const (
	readFilesOpen      = "<read-files>"
	readFilesClose     = "</read-files>"
	modifiedFilesOpen  = "<modified-files>"
	modifiedFilesClose = "</modified-files>"
)

// maxCarriedFiles bounds each list. A path list is cheap but not free, and an
// unbounded one turns the mechanism that saves tokens into one that spends them:
// a long drive can touch hundreds of files, and the lists are re-sent on every
// request for the rest of the session. When the cap bites, the *most recently*
// touched paths are kept — a compaction's own run contributes last and is what
// the model is most likely to need next — and the count of dropped paths is
// stated, on the same rule as truncNotice and omissionNote: a silently
// shortened list reads to the model as a complete one.
const maxCarriedFiles = 40

// renderFileLists formats the two lists for inclusion in a summary. Empty lists
// render as nothing at all rather than as empty tags, so a session that has
// touched no files pays nothing.
func renderFileLists(read, modified []string) string {
	var b strings.Builder
	writeList := func(open, close string, paths []string) {
		if len(paths) == 0 {
			return
		}
		kept, dropped := capPaths(paths)
		b.WriteString(open)
		b.WriteString("\n")
		for _, p := range kept {
			b.WriteString(p)
			b.WriteString("\n")
		}
		if dropped > 0 {
			fmt.Fprintf(&b, "…(%d older path(s) omitted)\n", dropped)
		}
		b.WriteString(close)
		b.WriteString("\n")
	}
	writeList(readFilesOpen, readFilesClose, read)
	writeList(modifiedFilesOpen, modifiedFilesClose, modified)
	return b.String()
}

// capPaths keeps the last maxCarriedFiles entries and reports how many were
// dropped.
func capPaths(paths []string) (kept []string, dropped int) {
	if len(paths) <= maxCarriedFiles {
		return paths, 0
	}
	return paths[len(paths)-maxCarriedFiles:], len(paths) - maxCarriedFiles
}

// parseFileLists recovers the two lists from text that a previous summary
// emitted. Unrecognized or absent tags yield empty lists — a summary written
// before P65.2, or one a model mangled, degrades to the pre-P65.2 behaviour
// rather than failing the compaction.
func parseFileLists(text string) (read, modified []string) {
	return extractList(text, readFilesOpen, readFilesClose),
		extractList(text, modifiedFilesOpen, modifiedFilesClose)
}

func extractList(text, open, close string) []string {
	i := strings.Index(text, open)
	if i < 0 {
		return nil
	}
	rest := text[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(rest[:j], "\n") {
		line = strings.TrimSpace(line)
		// Skip blanks and the elision marker renderFileLists may have written.
		if line == "" || strings.HasPrefix(line, "…") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// mergeFilePaths unions the carried-forward paths with this run's, preserving
// order (oldest first) and dropping duplicates, so the cap above keeps the
// newest. Not sorted: recency is the useful ordering here and is the one the cap
// depends on.
func mergeFilePaths(carried, current []string) []string {
	seen := make(map[string]struct{}, len(carried)+len(current))
	out := make([]string, 0, len(carried)+len(current))
	for _, group := range [][]string{carried, current} {
		for _, p := range group {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

// collectCarriedFiles walks the prefix being summarized for tagged lists a
// previous summary left behind, and merges this call's own paths on top. This is
// the whole accumulation mechanism: the prefix is where an earlier summary lives,
// so no Go-side state has to survive between compactions — or between processes.
func collectCarriedFiles(prefixText []string, current fileContext) (read, modified []string) {
	var carriedRead, carriedModified []string
	for _, text := range prefixText {
		r, m := parseFileLists(text)
		carriedRead = append(carriedRead, r...)
		carriedModified = append(carriedModified, m...)
	}
	return mergeFilePaths(carriedRead, current.read),
		mergeFilePaths(carriedModified, current.modified)
}

// prefixText pulls the text of every *previous compaction summary* out of the
// prefix being summarized, which is where the tagged lists live.
//
// The narrowing to summary messages is LLM-15. The tags are a wire format
// between successive summaries, and this code writes one end of it — so the
// reader has to be as specific as the writer. Scanning every text block meant
// anything that merely contained the tag opened the record: a model quoting the
// mechanism back in its prose, a user pasting a transcript, or a tool result
// echoed into an assistant turn could all inject paths the session never
// touched, and paths are the one part of a summary the code (not the model)
// promises is accurate. Restricting it to the synthetic message compaction
// itself spliced in — user role, opening with one of the two lead-ins — keeps
// accumulation working across any number of compactions while giving the
// mechanism exactly one author.
//
// Only TextBlocks, for the reason it always was: a tagged list can never appear
// in a tool result, and scanning those would let a file this session merely
// *printed* enter the record as one it read.
func prefixText(msgs []provider.Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Role != provider.RoleUser {
			continue
		}
		for _, blk := range m.Content {
			tb, ok := blk.(provider.TextBlock)
			if !ok {
				continue
			}
			if !isSummaryMessage(tb.Text) {
				continue
			}
			if strings.Contains(tb.Text, readFilesOpen) || strings.Contains(tb.Text, modifiedFilesOpen) {
				out = append(out, tb.Text)
			}
		}
	}
	return out
}

// isSummaryMessage reports whether text is one of the two synthetic messages a
// compaction splices in — the only place tagged file lists are ever written.
func isSummaryMessage(text string) bool {
	return strings.HasPrefix(text, summaryLeadIn) || strings.HasPrefix(text, fallbackLeadIn)
}
