package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// latexPrimaryRoot returns the workspace root violations are reported relative
// to — the session workdir, which is always roots[0].
func latexPrimaryRoot(roots []sandbox.Root) string {
	if len(roots) == 0 {
		return ""
	}
	return roots[0].Path
}

// ─── workspace confinement (P52.2) ────────────────────────────────────────────
//
// A TeX compiler is a general-purpose interpreter with file I/O attached, so
// running one over source the model itself authored is an execution boundary,
// not a build step. Two layers keep it inside the workspace root:
//
//  1. Process hardening (latexHardenedFlags / latexHardenedEnv) —
//     `-no-shell-escape` closes the restricted `\write18` whitelist, and
//     `openin_any`/`openout_any=p` (paranoid) ask TeX itself to refuse dot
//     files, parent directories and absolute paths.
//  2. A static scan of the source, and of the sources it pulls in, for file
//     references that resolve outside the workspace root — the same
//     `sandbox.ValidatePath` boundary every other file-touching builtin uses.
//
// Layer 2 exists because the read half of layer 1 is no longer dependable: as
// of TeX Live 2026 `openin_any` is a documented upstream no-op (kpathsea's
// `kpse_in_name_ok` and friends always return true), so on a current
// distribution nothing but this scan stops a model-authored
// `\input{~/.ssh/id_rsa}` from being typeset into the PDF. The environment
// variables are still set — they are honoured by TeX Live 2025 and earlier and
// by MiKTeX, and they cost nothing.
//
// The scan is a heuristic layered on a hardened process, not a sandbox: TeX can
// assemble filenames at run time from macros and this cannot evaluate those. It
// does catch every *literal* path escape, which is the shape reading a host file
// actually takes.

// latexHardenedFlags builds the compiler invocation for one pass.
// `-no-shell-escape` is placed first so it can never be mistaken for the input
// filename, which is always last.
func latexHardenedFlags(outDir, texAbs string, checkOnly bool) []string {
	flags := []string{
		"-no-shell-escape",
		"-interaction=nonstopmode",
		"-halt-on-error",
		"-output-directory=" + outDir,
	}
	if checkOnly {
		flags = append(flags, "-draftmode")
	}
	return append(flags, texAbs)
}

// latexHardenedEnv returns base with TeX's own file-access settings pinned to
// paranoid. Any inherited value is dropped rather than shadowed, so the result
// carries exactly one entry per key regardless of the host's environment.
func latexHardenedEnv(base []string) []string {
	out := make([]string, 0, len(base)+3)
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "openin_any="),
			strings.HasPrefix(kv, "openout_any="),
			strings.HasPrefix(kv, "shell_escape="):
			continue
		}
		out = append(out, kv)
	}
	return append(out, "openin_any=p", "openout_any=p", "shell_escape=f")
}

// latexReadCommands maps the LaTeX/TeX commands whose braced argument names a
// file to whether that file is itself TeX source worth following.
var latexReadCommands = map[string]bool{
	"input":             true,
	"include":           true,
	"InputIfFileExists": true,
	"subfile":           true,
	"subfileinclude":    true,
	"usepackage":        true,
	"RequirePackage":    true,
	"documentclass":     true,
	"LoadClass":         true,
	"includeonly":       false,
	"IfFileExists":      false,
	"lstinputlisting":   false,
	"verbatiminput":     false,
	"includegraphics":   false,
	"includepdf":        false,
	"addbibresource":    false,
	"addglobalbib":      false,
	"bibliography":      false,
	"bibliographystyle": false,
	"pdfximage":         false,
}

// latexVerbatimEnvs is the alternation of environments whose body is displayed
// verbatim rather than interpreted.
const latexVerbatimEnvs = `(?:verbatim|Verbatim|lstlisting|minted|alltt)`

var (
	// A command, an optional bracketed option list, and one brace-delimited
	// argument containing no nested braces. Names are filtered against
	// latexReadCommands after the match.
	latexBracedRefRE = regexp.MustCompile(`\\([A-Za-z@]+)\s*(?:\[[^\]]*\])?\s*\{([^{}]*)\}`)
	// TeX's brace-less form: `\input /etc/passwd`.
	latexBareInputRE = regexp.MustCompile(`\\(input|include)\s+([^\s{}%\\]+)`)
	// `\openin\stream=path` / `\openin1=path`.
	latexOpenInRE = regexp.MustCompile(`\\openin\s*[0-9]*\s*(?:\\[A-Za-z@]+\s*)?=\s*([^\s{}%\\]+)`)
	// import's two-argument form: a directory then a file.
	latexImportRE = regexp.MustCompile(`\\(?:sub)?(?:import|inputfrom|includefrom)\*?\s*\{([^{}]*)\}\s*\{([^{}]*)\}`)
	// `\graphicspath{{dir/}{other/}}` adds search roots for \includegraphics.
	latexGraphicsPathRE = regexp.MustCompile(`(?s)\\graphicspath\s*\{((?:\s*\{[^{}]*\}\s*)+)\}`)
	latexBraceItemRE    = regexp.MustCompile(`\{([^{}]*)\}`)
	// Blocks whose contents are shown, not executed — a report that quotes
	// `\input{/etc/passwd}` in a listing must still build.
	// RE2 has no backreferences, so the closing tag is any verbatim-like \end
	// rather than the matching one — good enough to skip the block's contents.
	latexVerbatimRE = regexp.MustCompile(`(?s)\\begin\{` + latexVerbatimEnvs + `\*?\}.*?\\end\{` + latexVerbatimEnvs + `\*?\}`)
)

const (
	latexScanMaxFiles = 128
	latexScanMaxBytes = 4 << 20
)

// latexFileRef is one file reference found in TeX source.
type latexFileRef struct {
	cmd     string
	arg     string
	recurse bool
}

// checkLatexConfinement scans texAbs, and every in-workspace TeX source it
// pulls in, for file references that resolve outside root. It returns one
// human-readable message per distinct violation; an empty slice means the
// document reads only from inside the workspace.
func checkLatexConfinement(roots []sandbox.Root, texAbs string) []string {
	// The paths in flight here are already symlink-resolved (they come back out
	// of the validator), so resolve the roots the same way before comparing: on
	// macOS a workspace under /tmp or /var is reached through a symlink, and
	// validating a /private/... path against the unresolved root would flag the
	// document's own chapters as escapes.
	roots = resolvedRoots(roots)
	root := latexPrimaryRoot(roots)

	var out []string
	reported := make(map[string]bool)

	latexWalkSources(roots, texAbs, nil, func(cur string, ref latexFileRef) {
		where := cur
		if rel, relErr := filepath.Rel(root, cur); relErr == nil {
			where = rel
		}
		msg := fmt.Sprintf("%s: \\%s{%s} resolves outside the workspace root", where, ref.cmd, ref.arg)
		if !reported[msg] {
			reported[msg] = true
			out = append(out, msg)
		}
	})
	return out
}

// latexWalkSources visits texAbs and every in-workspace TeX source reachable
// from it, breadth-first and bounded by latexScanMaxFiles / latexScanMaxBytes.
//
// visit, when non-nil, receives each file's absolute path together with its
// source stripped of comments and verbatim blocks. escaped, when non-nil,
// receives every file reference that resolves outside root; such a reference is
// never followed. Both callbacks are optional so the same traversal can serve
// the confinement scan (P52.2) and the bibliography auto-detection (P52.10)
// without either drifting from the other's idea of which files are in play.
func latexWalkSources(roots []sandbox.Root, texAbs string, visit func(path, src string), escaped func(path string, ref latexFileRef)) {
	// Every path this walk derives is symlink-resolved (EvalSymlinks below, and
	// the validator's own resolution), so the roots have to live in the same
	// namespace or the document's own chapters read as escapes — on macOS a
	// workspace under /tmp or /var is reached through a symlink.
	roots = resolvedRoots(roots)
	seen := make(map[string]bool)
	queue := []string{texAbs}

	for len(queue) > 0 && len(seen) < latexScanMaxFiles {
		cur := queue[0]
		queue = queue[1:]
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			cur = real // same namespace as root, so relative refs compare cleanly
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true

		info, err := os.Stat(cur)
		if err != nil || info.IsDir() || info.Size() > latexScanMaxBytes {
			continue
		}
		data, err := os.ReadFile(cur)
		if err != nil {
			continue
		}
		src := latexVerbatimRE.ReplaceAllString(stripTeXComments(string(data)), "")
		if visit != nil {
			visit(cur, src)
		}

		for _, ref := range latexFileRefs(src) {
			cand, ok := latexResolveRef(filepath.Dir(cur), ref.arg)
			if !ok {
				continue
			}
			abs, err := sandbox.ValidatePathIn(roots, cand, sandbox.AccessRead)
			if err != nil {
				if escaped != nil {
					escaped(cur, ref)
				}
				continue
			}
			if ref.recurse {
				queue = append(queue, latexSourceCandidates(abs)...)
			}
		}
	}
}

// latexFileRefs extracts every file reference from comment-stripped source.
func latexFileRefs(src string) []latexFileRef {
	var refs []latexFileRef
	add := func(cmd, arg string, recurse bool) {
		// Comma lists (\usepackage{a,b}, \bibliography{a,b}) name one file each.
		for _, item := range strings.Split(arg, ",") {
			if strings.TrimSpace(item) != "" {
				refs = append(refs, latexFileRef{cmd: cmd, arg: item, recurse: recurse})
			}
		}
	}

	for _, m := range latexBracedRefRE.FindAllStringSubmatch(src, -1) {
		if recurse, ok := latexReadCommands[m[1]]; ok {
			add(m[1], m[2], recurse)
		}
	}
	for _, m := range latexBareInputRE.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2], true)
	}
	for _, m := range latexOpenInRE.FindAllStringSubmatch(src, -1) {
		add("openin", m[1], false)
	}
	for _, m := range latexImportRE.FindAllStringSubmatch(src, -1) {
		add("import", m[1], false)
		add("import", filepath.Join(m[1], m[2]), true)
	}
	for _, m := range latexGraphicsPathRE.FindAllStringSubmatch(src, -1) {
		for _, item := range latexBraceItemRE.FindAllStringSubmatch(m[1], -1) {
			add("graphicspath", item[1], false)
		}
	}
	return refs
}

// latexResolveRef turns a raw TeX file argument into a path to validate.
// ok is false when the argument names nothing checkable — an empty argument, or
// a name assembled from macros that only the compiler can expand.
func latexResolveRef(baseDir, arg string) (string, bool) {
	arg = strings.Trim(strings.TrimSpace(arg), `"`)
	if arg == "" {
		return "", false
	}
	if strings.HasPrefix(arg, "~") {
		// kpathsea expands a leading tilde, so it is a real escape vector.
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		arg = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(arg, "~"), "/"))
	}
	if latexRefIsRooted(arg) {
		return arg, true
	}
	if strings.ContainsAny(arg, `\#`) {
		return "", false // macro-built and not already rooted — unresolvable here
	}
	return filepath.Join(baseDir, arg), true
}

// latexRefIsRooted reports whether a TeX file argument names a location the
// compiler resolves from a filesystem root rather than from the document's
// directory.
//
// The `/`-prefix arm is what makes this correct on Windows, and it is a
// confinement bug without it: `filepath.IsAbs` requires a volume there, so
// `\input{/etc/passwd}` fell through to `filepath.Join(baseDir, "/etc/passwd")`,
// which folds the leading separator away and yields `<workspace>\etc\passwd` —
// a path that then validates as perfectly confined. Meanwhile MiKTeX resolves
// the same argument against the current drive root, so the scan reported no
// escape for a read that really does leave the workspace. This is the same
// trap `sandbox.absCandidate` documents (P32.1), hit one layer further up:
// anything that pre-joins a rooted path defeats the validator before it runs.
//
// Deliberately *not* symmetric on `\`: in TeX a leading backslash is a macro
// escape (`\input{\jobname.tex}`), not a Windows root, and treating it as a
// path would invent Windows-only false violations for documents that are fine.
// Such arguments stay unresolvable — the caller's `\#` check catches them —
// which keeps the scan's verdict identical on macOS and Windows.
func latexRefIsRooted(arg string) bool {
	return filepath.IsAbs(arg) || strings.HasPrefix(arg, "/")
}

// latexSourceCandidates returns the existing files abs could name as TeX
// source, honouring TeX's habit of supplying an extension when none is given.
func latexSourceCandidates(abs string) []string {
	exists := func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	}
	if ext := strings.ToLower(filepath.Ext(abs)); ext != "" {
		switch ext {
		case ".tex", ".ltx", ".sty", ".cls", ".def", ".clo", ".tikz", ".bib":
			if exists(abs) {
				return []string{abs}
			}
		}
		return nil
	}
	var out []string
	for _, ext := range []string{".tex", ".sty", ".cls"} {
		if exists(abs + ext) {
			out = append(out, abs+ext)
		}
	}
	return out
}

// stripTeXComments removes everything from an unescaped % to end of line, so a
// commented-out reference is not mistaken for a live one.
func stripTeXComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, line := range strings.Split(s, "\n") {
		esc := false
		cut := -1
		for i, r := range line {
			switch {
			case esc:
				esc = false
			case r == '\\':
				esc = true
			case r == '%':
				cut = i
			}
			if cut >= 0 {
				break
			}
		}
		if cut >= 0 {
			line = line[:cut]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
