package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"strconv"

	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/toolpath"
)

// Result caps, shared by both backends so a query's output size does not depend
// on which one ran. The grep cap is a total across all files, applied while
// parsing; ripgrep is additionally given --max-count (which is per *file*) at
// the same value, so one enormous generated file cannot produce megabytes of
// output that we then throw away.
const (
	globMaxResults = 1000
	grepMaxMatches = 500
	// grepSpillMaxMatches is how far past the inline cap grep keeps collecting
	// so the remainder can be spilled to a file (P64.1). glob needs no
	// equivalent: it already collects every match and then cuts, so its
	// remainder costs nothing extra to keep.
	//
	// It is a separate number from grepMaxMatches because the two bound
	// different costs. grepMaxMatches bounds the model's *context*; this bounds
	// the *walk*, and the walk is what CLAUDE.md's streaming-cancel note is
	// about — cancelling ripgrep at the cap took a common pattern from 964ms to
	// 46ms on a 40k-file tree, and collecting without limit would hand that
	// back. 4x the inline cap, chosen on an ad-hoc measurement over this repo
	// recorded with P64.1's write-up in releases.md: `func ` collects 2,000
	// matches in ~90ms against ~50ms for 500 — the extra 40ms buys a 4x-larger
	// recoverable set, where uncapped collection on the same query costs ~900ms
	// for a set no model will page through anyway. Deliberately *not* left
	// behind as a test: a wall-clock assertion over whatever repo the suite
	// happens to run in is the flaky-by-construction shape this item's own
	// "report, do not gate" rule exists to avoid.
	grepSpillMaxMatches = 2000
	// maxGrepLineBytes bounds one match line while streaming ripgrep's output.
	// A minified bundle or a generated data file can be one enormous line, and
	// the default bufio.Scanner buffer (64KB) would abort the whole search on
	// it rather than return the matches found so far.
	maxGrepLineBytes = 1 << 20
)

// truncNotice labels a capped result set. Both backends used to hit their cap
// and return the truncated list with nothing to distinguish it from a complete
// one, so a model could not tell "these are all the matches" from "these are
// the first 500 of an unknown number" — and would reason from the partial list
// as though it were exhaustive.
//
// It also names the ordering caveat. Which subset you get when capped depends
// on traversal order, and ripgrep's parallel walk does not visit files in the
// same order as the Go walker (nor necessarily the same order twice). Forcing
// ripgrep's --sort path would make it deterministic but costs ~4x on this repo
// (220ms -> 900ms measured), which is more than the whole speed advantage, so
// the honest fix is to say the set is partial rather than to pay for an
// ordering the caller did not ask for.
func truncNotice(kind string, n int) string {
	return fmt.Sprintf("\n\n[%s: capped at %d results; more exist. This is a partial, unordered subset — narrow the pattern or add a glob filter to see the rest.]", kind, n)
}

// rgArgsCommon are the flags every ripgrep invocation carries, and they are the
// parity contract between the two search backends. Without them the same query
// returns different results depending on whether ripgrep happens to be
// installed — the "two machines, different answers" failure this codebase
// avoids elsewhere (see the scanner method decision in CLAUDE.md).
//
//   - --no-ignore-vcs: ripgrep honors .gitignore by default, the Go walker does
//     not. Committed-but-conventionally-ignored trees (this repo ships a built
//     web UI in internal/server/webui/dist) were searched by one backend and
//     not the other. Turning VCS-ignore off and applying skipDir explicitly
//     below makes the exclusion set identical.
//   - --hidden: the Go walker descends into dotted directories, so ripgrep must
//     too; skipDir still removes .git and friends.
//
// The excludes are generated from skipDir rather than written out twice, so a
// directory added to one backend's skip set cannot be forgotten in the other.
func rgArgsCommon() []string { return rgArgsForSkip(skipDirNames) }

// rgArgsForGlob is rgArgsCommon minus the .aegis exclude — see skipDirForGlob.
func rgArgsForGlob() []string { return rgArgsForSkip(globSkipDirNames) }

func rgArgsForSkip(skip []string) []string {
	args := []string{"--hidden", "--no-ignore-vcs"}
	for _, d := range skip {
		args = append(args, "-g", "!"+d+"/")
	}
	return args
}

// runRg executes ripgrep rooted at dir. Two details matter and both were bugs:
//
// cmd.Dir was never set, so ripgrep inherited the daemon's working directory
// while being handed an absolute search root. Its -g globs are matched against
// the path as walked, so a multi-segment pattern like "internal/**/*_test.go"
// matched nothing at all (measured: 0 files via the tool, 348 via the Go
// fallback) whenever the daemon's cwd was not the workspace root — which for a
// daemon serving a session elsewhere is the normal case.
//
// Rooting at dir and searching "." also makes ripgrep emit workspace-relative
// paths, which is what the tool's output contract wants anyway and what the
// absolute-path form could not produce on Windows: the old parser split each
// line on ":" to find the path, and "D:\repo\f.go:12:text" splits at the drive
// letter, so every result came back as an unmodified absolute path.
func runRg(ctx context.Context, rg, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, rg, args...)
	cmd.Dir = dir
	return cmd.Output()
}

// relFromRg normalizes one path as ripgrep prints it when searching ".":
// strips the leading "./" (or ".\" on Windows) and converts separators, so
// output is byte-identical to the Go walker's.
func relFromRg(p string) string {
	p = filepath.ToSlash(p)
	return strings.TrimPrefix(p, "./")
}

// --- glob ---

type globTool struct {
	root string
	cmds *toolpath.Resolver
}

// rgPath returns the configured ripgrep binary, or "" to use the Go fallback.
// A nil resolver (tools constructed in tests without one) means fallback.
func (t *globTool) rgPath() string { return resolverPath(t.cmds, toolpath.Ripgrep) }

func resolverPath(r *toolpath.Resolver, key string) string {
	if r == nil {
		return ""
	}
	return r.Path(key)
}

func (t *globTool) Name() string                { return "glob" }
func (t *globTool) Capability() tool.Capability { return tool.CapRead }

// Replay: a pure read (P65.4) — see readTool.Replay.
func (t *globTool) Replay(json.RawMessage) tool.ReplayClass { return tool.ReplaySafe }
func (t *globTool) Description() string {
	return "Find files in the workspace matching a glob pattern (e.g. **/*.go). Returns matching paths."
}
func (t *globTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"pattern":{"type":"string","description":"glob pattern, ** matches any depth"}},"required":["pattern"]}`)
}
func (t *globTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return tool.Result{Content: "pattern is required", IsError: true}, nil
	}
	root := effectiveRoot(ctx, t.root)

	if rg := t.rgPath(); rg != "" {
		if res, ok := t.executeRg(ctx, rg, root, args.Pattern); ok {
			return res, nil
		}
		// Fall through to the Go walker rather than reporting "no files
		// matched": a ripgrep that failed to start or died mid-walk must not be
		// indistinguishable from an empty result set.
	}

	var matches []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirForGlob(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if matchGlob(args.Pattern, rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if walkErr != nil {
		return tool.Result{Content: fmt.Sprintf("walk failed: %v", walkErr), IsError: true}, nil
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return tool.Result{Content: "no files matched"}, nil
	}
	return tool.Result{Content: capGlob(ctx, root, matches)}, nil
}

// capGlob joins matches, appending a truncation notice when the cap bites.
// P64.3: the cut itself lives in capItems (truncate.go) with every other
// result cap in the tree.
func capGlob(ctx context.Context, root string, matches []string) string {
	return spillItems(ctx, root, "glob", matches, globMaxResults, len(matches) > globMaxResults)
}

// executeRg lists files with ripgrep. The bool reports whether ripgrep ran to a
// usable conclusion; false means the caller should use the Go walker instead.
// An empty result is a legitimate conclusion (ok=true) — only a failure to run
// is not.
func (t *globTool) executeRg(ctx context.Context, rg, root, pattern string) (tool.Result, bool) {
	args := append([]string{"--files"}, rgArgsForGlob()...)
	args = append(args, "-g", pattern, "--", ".")
	out, err := runRg(ctx, rg, root, args...)
	if err != nil && len(out) == 0 {
		// Exit 1 with no output is ripgrep's "no matches", not a failure.
		if ee, isExit := err.(*exec.ExitError); !isExit || ee.ExitCode() != 1 {
			return tool.Result{}, false
		}
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return tool.Result{Content: "no files matched"}, true
	}
	var matches []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		matches = append(matches, relFromRg(line))
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return tool.Result{Content: "no files matched"}, true
	}
	return tool.Result{Content: capGlob(ctx, root, matches)}, true
}

// matchGlob supports ** (any depth) in addition to standard path.Match syntax.
func matchGlob(pattern, name string) bool {
	if strings.Contains(pattern, "**") {
		re := globToRegexp(pattern)
		return re.MatchString(name)
	}
	ok, err := filepath.Match(pattern, name)
	if err == nil && ok {
		return true
	}
	// Also try matching just the base name for convenience (e.g. "*.go").
	base, _ := filepath.Match(pattern, filepath.Base(name))
	return base
}

func globToRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile("$^") // matches nothing
	}
	return re
}

// --- grep ---

type grepTool struct {
	root string
	cmds *toolpath.Resolver
	// scanContent enables the DR-1 conditional untrusted-content marker; see
	// filetrust.go and builtin.Options.ScanFileReads.
	scanContent bool
}

func (t *grepTool) rgPath() string { return resolverPath(t.cmds, toolpath.Ripgrep) }

func (t *grepTool) Name() string                { return "grep" }
func (t *grepTool) Capability() tool.Capability { return tool.CapRead }

// Replay: a pure read (P65.4) — see readTool.Replay.
func (t *grepTool) Replay(json.RawMessage) tool.ReplayClass { return tool.ReplaySafe }
func (t *grepTool) Description() string {
	return "Search workspace file contents with a regular expression. Returns matching lines as path:line:text."
}
func (t *grepTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"pattern":{"type":"string","description":"RE2 regular expression"},"glob":{"type":"string","description":"optional file glob filter"},"ignore_case":{"type":"boolean"}},"required":["pattern"]}`)
}
func (t *grepTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignore_case"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}

	root := effectiveRoot(ctx, t.root)

	if rg := t.rgPath(); rg != "" {
		if res, ok := t.executeRg(ctx, rg, root, args.Pattern, args.Glob, args.IgnoreCase); ok {
			return res, nil
		}
		// See globTool.Execute: a ripgrep that could not run falls back to the
		// walker rather than masquerading as an empty result.
	}

	pat := args.Pattern
	if args.IgnoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("invalid regexp: %v", err), IsError: true}, nil
	}

	var out []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if args.Glob != "" && !matchGlob(args.Glob, rel) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := os.ReadFile(path)
		if err != nil || isBinary(data) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d:%s", rel, i+1, strings.TrimRight(line, "\r")))
				// P64.1: keep walking past the inline cap up to the collection
				// ceiling, so the matches beyond grepMaxMatches are recoverable
				// from a spill file instead of discarded. The inline result is
				// byte-identical to what it was.
				if len(out) >= grepSpillMaxMatches {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != context.Canceled {
		return tool.Result{Content: fmt.Sprintf("search failed: %v", walkErr), IsError: true}, nil
	}
	if len(out) == 0 {
		return tool.Result{Content: "no matches"}, nil
	}
	content := spillItems(ctx, root, "grep", out, grepMaxMatches, len(out) >= grepMaxMatches)
	return tool.Result{Content: markSuspiciousGrepOutput(t.scanContent, content)}, nil
}

// markSuspiciousGrepOutput is DR-1 for grep: matching lines are workspace file
// content reaching the model just as a read does, only in smaller pieces. Both
// backends go through this, since a marker one of them applies and the other
// does not is the divergence this file's search-backend invariant exists to
// prevent.
func markSuspiciousGrepOutput(scan bool, content string) string {
	return markSuspiciousFileContent(scan,
		"a grep over workspace files (these are lines from files in the workspace, not from the user)",
		nil, content)
}

// executeRg searches with ripgrep. The bool reports whether it ran to a usable
// conclusion; false means fall back to the Go walker.
//
// --null makes ripgrep terminate the file name with a NUL instead of a colon,
// which is what makes the output parseable at all. Splitting on ":" — the old
// approach — cannot distinguish the path from the match on any line, and broke
// outright on Windows, where "D:\repo\f.go:12:text" splits at the drive letter
// and the path came back unmodified and absolute.
func (t *grepTool) executeRg(ctx context.Context, rg, root, pattern, glob string, ignoreCase bool) (tool.Result, bool) {
	argv := []string{"--null", "--line-number", "--no-heading", "--color=never"}
	argv = append(argv, rgArgsCommon()...)
	// Per-file, not total (ripgrep has no total limit). Raised to the spill
	// ceiling with the total collection ceiling in P64.1, so the two backends
	// still agree on how much they collect — a per-file limit left at the
	// inline cap would let the Go walker spill 2,000 matches from one big file
	// where ripgrep spilled 500.
	argv = append(argv, "--max-count", strconv.Itoa(grepSpillMaxMatches), "-e", pattern)
	if ignoreCase {
		argv = append(argv, "-i")
	}
	if glob != "" {
		argv = append(argv, "-g", glob)
	}
	argv = append(argv, "--", ".")

	// Stream ripgrep's output and stop it as soon as the cap is reached, rather
	// than buffering a whole run and discarding the tail. ripgrep has no global
	// match limit (--max-count is per file), so on a large tree a common pattern
	// made it walk everything to produce output we then threw away: on a
	// 40k-file tree, a query capped at 500 measured 964ms buffered against 42ms
	// for the Go walker, which stops early via SkipAll. Cancelling the context
	// closes that gap while keeping ripgrep's large win on queries that are not
	// capped (no-match over the same tree: 845ms vs the walker's 3.3s).
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(runCtx, rg, argv...)
	cmd.Dir = root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return tool.Result{}, false
	}
	if err := cmd.Start(); err != nil {
		return tool.Result{}, false
	}

	var results []string
	capped := false
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), maxGrepLineBytes)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		// rg --null emits: <path>\0<lineno>:<content>
		if path, rest, found := strings.Cut(line, "\x00"); found {
			line = relFromRg(path) + ":" + rest
		}
		results = append(results, line)
		if len(results) >= grepMaxMatches {
			capped = true
		}
		if len(results) >= grepSpillMaxMatches {
			break
		}
	}
	scanErr := sc.Err()
	cancel()
	waitErr := cmd.Wait()

	// A run we cut short reports a kill, and a scanner we stopped reading from
	// can report a broken pipe; neither is a failure when we already have
	// results. Only a genuine failure with nothing to show falls back.
	if !capped && len(results) == 0 {
		if scanErr != nil {
			return tool.Result{}, false
		}
		if ee, isExit := waitErr.(*exec.ExitError); waitErr != nil && (!isExit || ee.ExitCode() != 1) {
			return tool.Result{}, false
		}
		return tool.Result{Content: "no matches"}, true
	}

	content := spillItems(ctx, root, "grep", results, grepMaxMatches, capped)
	return tool.Result{Content: markSuspiciousGrepOutput(t.scanContent, content)}, true
}

// skipDirNames are directories neither search backend descends into. Kept as
// data rather than a switch so rgArgsCommon can translate the same list into
// ripgrep excludes — the two backends previously disagreed about this set,
// which is how the same grep returned different results on two machines.
var skipDirNames = []string{".git", "node_modules", "vendor", ".idea", ".vscode", "dist", "build", ".aegis"}

func skipDir(name string) bool {
	for _, d := range skipDirNames {
		if name == d {
			return true
		}
	}
	return false
}

// globSkipDirNames is skipDirNames minus .aegis — see skipDirForGlob.
var globSkipDirNames = func() []string {
	out := make([]string, 0, len(skipDirNames)-1)
	for _, d := range skipDirNames {
		if d != ".aegis" {
			out = append(out, d)
		}
	}
	return out
}()

// skipDirForGlob is grep's skipDir with .aegis let back in. glob returns
// filenames only, never file content, so it carries none of the reason grep
// stays out of .aegis (P64.1's spill/ is deliberately grep-unreachable, and
// .env holds secrets) — but a skill drive scaffolds its own output under
// .aegis/security/threat-model/<run>/, and a model that loses that exact path
// mid-drive had no way back in: glob("**/<name>") reported "no files
// matched" against a file that plainly existed, and the drive stalled
// (P38.1 re-test, AiGateway, 2026-08-21). pathsuggest.go's findMatching
// already draws this same line for the file-not-found hint, filed from an
// earlier instance of the identical failure (P38.1 re-test, 2026-08-09);
// glob just hadn't caught up to it.
func skipDirForGlob(name string) bool {
	return skipDir(name) && name != ".aegis"
}

func isBinary(data []byte) bool {
	n := min(len(data), 8000)
	for i := range n {
		if data[i] == 0 {
			return true
		}
	}
	return false
}
