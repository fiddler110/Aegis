package builtin

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxPathSuggestions bounds what a not-found error offers. One suggestion is
// the useful case; a long list is just noise the model has to re-read.
const maxPathSuggestions = 3

// suggestPathHint returns a " (did you mean …)" fragment naming real files that
// share the base name of a path that could not be found, or "" when there is
// nothing helpful to say.
//
// A file-not-found error is the one place a tool knows the answer and does not
// say it. The model asked for a name that exists — just not where it looked —
// and the OS reply ("The system cannot find the file specified") gives it
// nothing to correct with, so it asks again. Observed live: a phased drive
// spent 40 read_file calls and three no-progress turns asking for
// `2-stride-analysis.md` relative to the workspace root while the file sat in
// the run directory, then stalled with the phase unfinished (P38.1 re-test,
// 2026-08-09).
//
// Deliberately best-effort and bounded: a missing file is already an error, and
// a failed search must not turn it into a slower one. The walk skips the same
// directories search does and stops once it has enough.
func suggestPathHint(root, requested string) string {
	base := filepath.Base(filepath.FromSlash(requested))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	// An unsubstituted template placeholder is a glob in disguise. Skill
	// documentation names files as `skeleton-<framework>.md` and
	// `2-<framework>-analysis.md`, and a model that copies the notation
	// literally gets an OS error naming neither the placeholder nor the real
	// file. Treating `<...>` as a wildcard turns that dead end into the actual
	// candidates (P38.1 re-test, 2026-08-09 — the same mistake appeared on both
	// the read and the write path).
	if re := placeholderPattern(base); re != nil {
		if m := findMatching(root, re.MatchString, maxPathSuggestions); len(m) > 0 {
			return " — `" + base + "` looks like an unsubstituted template placeholder; did you mean " + strings.Join(m, ", or ") + "?"
		}
		return " — `" + base + "` contains an unsubstituted template placeholder: replace the `<…>` part with the real value"
	}
	matches := findMatching(root, func(name string) bool { return name == base }, maxPathSuggestions)
	if len(matches) == 0 {
		return ""
	}
	return " (did you mean " + strings.Join(matches, ", or ") + "?)"
}

// placeholderRe finds a `<…>` span in a file name.
var placeholderRe = regexp.MustCompile(`<[^>/\\]*>`)

// placeholderPattern compiles a base name containing `<…>` spans into a
// pattern matching real file names, or nil when the name has no placeholder.
func placeholderPattern(base string) *regexp.Regexp {
	if !placeholderRe.MatchString(base) {
		return nil
	}
	var b strings.Builder
	b.WriteString("^")
	last := 0
	for _, loc := range placeholderRe.FindAllStringIndex(base, -1) {
		b.WriteString(regexp.QuoteMeta(base[last:loc[0]]))
		b.WriteString(`[^/\\]*`)
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(base[last:]))
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

// findMatching walks root for files whose base name satisfies match, returning
// up to limit workspace-relative slash-separated paths.
func findMatching(root string, match func(name string) bool, limit int) []string {
	if root == "" || limit <= 0 {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree — skip it, never fail the hint
		}
		if d.IsDir() {
			// The search backends' skip set, minus `.aegis`: a skill drive's
			// output suite lives under `.aegis/`, so skipping it here would
			// hide precisely the file this hint exists to find.
			if skipDir(d.Name()) && d.Name() != ".aegis" {
				return fs.SkipDir
			}
			return nil
		}
		if !match(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	return out
}

// notFound reports whether err is an unhelpful "no such file" of the kind worth
// attaching a suggestion to. Anything else (a permission error, a directory
// where a file was wanted) already names its own cause.
func notFound(err error) bool {
	return os.IsNotExist(err)
}
