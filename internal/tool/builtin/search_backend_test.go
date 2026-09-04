package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/toolpath"
)

// backendPair returns a resolver that uses ripgrep and one that forces the
// pure-Go fallback, skipping the test when ripgrep is not installed. The
// forced-off resolver is exactly what the `commands.ripgrep: off` knob gives a
// user who wants to compare backends.
func backendPair(t *testing.T) (rg, walk *toolpath.Resolver) {
	t.Helper()
	rg = toolpath.New(nil)
	if rg.Path(toolpath.Ripgrep) == "" {
		t.Skip("ripgrep not installed; backend parity cannot be checked")
	}
	return rg, toolpath.New(map[string]string{"ripgrep": "off"})
}

// fixtureTree builds a workspace exercising the cases the two backends used to
// disagree on: nested directories (multi-segment globs), a skipDir entry that
// is *not* gitignored, and a .gitignore ripgrep would otherwise honor.
func fixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":                     "package main\nfunc needle() {}\n",
		"README.md":                   "needle in prose\n",
		"internal/a/a.go":             "package a\nfunc needle() {}\n",
		"internal/a/a_test.go":        "package a\nfunc TestNeedle() {}\n",
		"internal/b/deep/c/c_test.go": "package c\nfunc needle() {}\n",
		"dist/generated.go":           "package dist\nfunc needle() {}\n",
		"ignored/thing.go":            "package ignored\nfunc needle() {}\n",
		".gitignore":                  "ignored/\n",
		".hidden/h.go":                "package h\nfunc needle() {}\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runGlob(t *testing.T, root string, r *toolpath.Resolver, pattern string) string {
	t.Helper()
	g := &globTool{root: root, cmds: r}
	res, err := g.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": pattern}))
	if err != nil || res.IsError {
		t.Fatalf("glob %q: %v %+v", pattern, err, res)
	}
	return res.Content
}

func runGrep(t *testing.T, root string, r *toolpath.Resolver, args map[string]any) string {
	t.Helper()
	g := &grepTool{root: root, cmds: r}
	res, err := g.Execute(context.Background(), mustJSON(t, args))
	if err != nil || res.IsError {
		t.Fatalf("grep %v: %v %+v", args, err, res)
	}
	return res.Content
}

// TestGlobMultiSegmentPatternUnderRipgrep is the headline regression. ripgrep
// was handed an absolute search root while inheriting the daemon's working
// directory, so its -g globs never anchored: a multi-segment pattern matched
// nothing at all under ripgrep while the Go walker found every file. Measured
// on this repo before the fix: 0 files vs 348.
func TestGlobMultiSegmentPatternUnderRipgrep(t *testing.T) {
	rg, walk := backendPair(t)
	root := fixtureTree(t)

	got := runGlob(t, root, rg, "internal/**/*_test.go")
	if !strings.Contains(got, "internal/a/a_test.go") {
		t.Errorf("ripgrep glob lost a match:\n%s", got)
	}
	if !strings.Contains(got, "internal/b/deep/c/c_test.go") {
		t.Errorf("ripgrep glob lost a deep match:\n%s", got)
	}
	if want := runGlob(t, root, walk, "internal/**/*_test.go"); got != want {
		t.Errorf("backends disagree:\n rg:   %q\n walk: %q", got, want)
	}
}

// TestSearchBackendParity pins the general contract: with ripgrep installed or
// not, the same query returns the same answer. Before the fix ripgrep honored
// .gitignore while the walker did not, so a committed-but-conventionally-
// ignored tree was searched by one backend and not the other.
func TestSearchBackendParity(t *testing.T) {
	rg, walk := backendPair(t)
	root := fixtureTree(t)

	for _, pattern := range []string{"**/*.go", "internal/**/*.go", "*.md", "**/*_test.go"} {
		if got, want := runGlob(t, root, rg, pattern), runGlob(t, root, walk, pattern); got != want {
			t.Errorf("glob %q differs:\n rg:   %q\n walk: %q", pattern, got, want)
		}
	}
	for _, args := range []map[string]any{
		{"pattern": "needle"},
		{"pattern": "NEEDLE", "ignore_case": true},
		{"pattern": "func needle", "glob": "**/*.go"},
		{"pattern": `func \w+\(\)`},
	} {
		got, want := runGrep(t, root, rg, args), runGrep(t, root, walk, args)
		if sortedLines(got) != sortedLines(want) {
			t.Errorf("grep %v differs:\n rg:   %q\n walk: %q", args, got, want)
		}
	}
}

// Both backends must exclude the same directories. dist/ is in skipDirNames but
// is not gitignored here, so it isolates the exclusion set from .gitignore
// handling; ignored/ is gitignored but not in skipDirNames, so it catches
// ripgrep honoring VCS ignores when the walker does not.
func TestSearchExclusionsMatchAcrossBackends(t *testing.T) {
	rg, walk := backendPair(t)
	root := fixtureTree(t)

	for _, r := range []*toolpath.Resolver{rg, walk} {
		out := runGlob(t, root, r, "**/*.go")
		if strings.Contains(out, "dist/") {
			t.Errorf("dist/ is in skipDirNames but was returned:\n%s", out)
		}
		if !strings.Contains(out, "ignored/thing.go") {
			t.Errorf("a gitignored-but-not-skipped file must still be found:\n%s", out)
		}
		if !strings.Contains(out, ".hidden/h.go") {
			t.Errorf("hidden directories must be searched by both backends:\n%s", out)
		}
	}
}

// grep output must be workspace-relative under both backends. Under ripgrep on
// Windows it was neither: the parser split each line on ":" to find the path,
// which splits "D:\repo\f.go:12:text" at the drive letter, so every result came
// back as an unmodified absolute path.
func TestGrepPathsAreRelativeAndSlashed(t *testing.T) {
	rg, walk := backendPair(t)
	root := fixtureTree(t)

	for name, r := range map[string]*toolpath.Resolver{"ripgrep": rg, "walk": walk} {
		out := runGrep(t, root, r, map[string]any{"pattern": "needle"})
		for _, line := range strings.Split(out, "\n") {
			if line == "" {
				continue
			}
			if strings.Contains(line, `\`) {
				t.Errorf("%s: backslash in output line %q", name, line)
			}
			if strings.HasPrefix(line, "./") || strings.HasPrefix(line, "/") {
				t.Errorf("%s: path not relative-clean: %q", name, line)
			}
			if filepath.IsAbs(strings.SplitN(line, ":", 2)[0]) {
				t.Errorf("%s: absolute path in output: %q", name, line)
			}
		}
		if !strings.Contains(out, "internal/a/a.go:2:") {
			t.Errorf("%s: expected a relative path:line hit, got:\n%s", name, out)
		}
	}
}

// A grep pattern containing a colon must not confuse the ripgrep output parser
// — the reason the parser now splits on rg's --null separator rather than ":".
func TestGrepPatternWithColonParsesCorrectly(t *testing.T) {
	rg, walk := backendPair(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("a := map[string]int{}\nkey: value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, want := runGrep(t, root, rg, map[string]any{"pattern": "key: value"}), runGrep(t, root, walk, map[string]any{"pattern": "key: value"})
	if got != want {
		t.Errorf("colon-bearing match differs:\n rg:   %q\n walk: %q", got, want)
	}
	if !strings.HasPrefix(got, "f.go:2:key: value") {
		t.Errorf("got %q", got)
	}
}

// A capped result set must say so. Both backends previously returned a
// truncated list indistinguishable from a complete one, so a model reasoned
// from the first 500 matches as though they were all of them.
func TestGrepAnnouncesTruncation(t *testing.T) {
	rg, walk := backendPair(t)
	root := t.TempDir()
	var sb strings.Builder
	for i := 0; i < grepMaxMatches+50; i++ {
		sb.WriteString("needle\n")
	}
	if err := os.WriteFile(filepath.Join(root, "many.txt"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]*toolpath.Resolver{"ripgrep": rg, "walk": walk} {
		out := runGrep(t, root, r, map[string]any{"pattern": "needle"})
		if !strings.Contains(out, "capped at") {
			t.Errorf("%s: truncated result carried no notice:\n...%s", name, tailOf(out, 200))
		}
	}
}

// TestGlobReachesAegisButGrepDoesNot pins the P38.1 fix: a skill drive
// scaffolds its own output under .aegis/security/threat-model/<run>/, and a
// model that loses that exact path had no way back — glob("**/<name>")
// reported "no files matched" against a file that plainly existed, because
// .aegis was in skipDirNames for both search tools. glob (filenames only,
// never content) now excludes .aegis from neither backend's skip set; grep
// (content search) still does, so .env and P64.1's spill/ stay unreachable
// by content search exactly as before.
func TestGlobReachesAegisButGrepDoesNot(t *testing.T) {
	rg, walk := backendPair(t)
	root := t.TempDir()
	runDir := filepath.Join(root, ".aegis", "security", "threat-model", "stride-app-2026-08-21-1347")
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(runDir, "0.1-architecture.md")
	if err := os.WriteFile(target, []byte("architecture findable_token\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, r := range map[string]*toolpath.Resolver{"ripgrep": rg, "walk": walk} {
		got := runGlob(t, root, r, "**/0.1-architecture.md")
		if !strings.Contains(got, "0.1-architecture.md") {
			t.Errorf("%s: glob did not find the file under .aegis:\n%s", name, got)
		}
	}
	if got, want := runGlob(t, root, rg, "**/0.1-architecture.md"), runGlob(t, root, walk, "**/0.1-architecture.md"); got != want {
		t.Errorf("glob backends disagree under .aegis:\n rg:   %q\n walk: %q", got, want)
	}

	for name, r := range map[string]*toolpath.Resolver{"ripgrep": rg, "walk": walk} {
		got := runGrep(t, root, r, map[string]any{"pattern": "findable_token"})
		if !strings.Contains(got, "no matches") {
			t.Errorf("%s: grep now reaches .aegis content:\n%s", name, got)
		}
	}
}

// A resolver with no ripgrep configured must behave exactly like the walker —
// this is what every other test in the package relies on, since tools built
// without a resolver must not silently reach for the host's PATH.
func TestNilResolverUsesWalker(t *testing.T) {
	root := fixtureTree(t)
	bare := &globTool{root: root}
	res, err := bare.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.go"}))
	if err != nil || res.IsError {
		t.Fatalf("glob with no resolver: %v %+v", err, res)
	}
	if want := runGlob(t, root, toolpath.New(map[string]string{"ripgrep": "off"}), "**/*.go"); res.Content != want {
		t.Errorf("nil resolver diverged from the forced walker:\n got: %q\nwant: %q", res.Content, want)
	}
}

func sortedLines(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	return strings.Join(sortStrings(lines), "\n")
}

func sortStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// VULN-09/G122: the Go grep backend used to os.ReadFile every file the walk
// visited and only then ask isBinary, so one huge file anywhere in the tree —
// a DB dump, a core file — went fully resident in the daemon's heap on any grep
// call. It now streams.
//
// A size cap was the obvious fix and is deliberately absent: ripgrep applies no
// default --max-filesize, so capping this backend alone would make the two
// disagree about a large text file, which is exactly what the search-backend
// equivalence invariant forbids. This test pins that agreement over a file well
// past any cap that was considered, so a cap cannot be reintroduced quietly.
func TestGrepBackendsAgreeOnALargeTextFile(t *testing.T) {
	rg, walk := backendPair(t)
	root := t.TempDir()

	var sb strings.Builder
	sb.WriteString("needle_at_the_top\n")
	for sb.Len() < 12<<20 {
		sb.WriteString("filler line with no match at all\n")
	}
	sb.WriteString("needle_at_the_bottom\n")
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, pattern := range []string{"needle_at_the_top", "needle_at_the_bottom"} {
		got, want := runGrep(t, root, rg, map[string]any{"pattern": pattern}), runGrep(t, root, walk, map[string]any{"pattern": pattern})
		if got != want {
			t.Errorf("backends disagree on a 12 MiB text file for %q:\n rg:   %q\n walk: %q", pattern, got, want)
		}
		if !strings.Contains(want, "big.txt:") {
			t.Errorf("the walker lost the match in a large file for %q: %q", pattern, want)
		}
	}
}

// Streaming must not change how a file's lines are numbered or trimmed, and a
// binary file must still be skipped on its head alone. The NUL sits past the
// first line so a head-only sniff is what catches it.
func TestGrepStreamingPreservesLineSemantics(t *testing.T) {
	root := t.TempDir()
	write := func(name string, content []byte) {
		if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("crlf.txt", []byte("alpha\r\nbeta needle\r\ngamma\r\n"))
	write("noeol.txt", []byte("first\nsecond needle"))
	write("empty.txt", nil)
	write("bin.dat", append([]byte("needle at the start\n"), 0x00, 'x'))

	walk := toolpath.New(map[string]string{"ripgrep": "off"})
	out := runGrep(t, root, walk, map[string]any{"pattern": "needle"})
	for _, want := range []string{"crlf.txt:2:beta needle", "noeol.txt:2:second needle"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "bin.dat") {
		t.Errorf("a binary file was searched:\n%s", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("a carriage return survived into the output: %q", out)
	}

	// An empty file has no lines, so an empty-matching pattern must not report
	// one — the whole-file split used to yield a phantom line 1 here.
	if got := runGrep(t, root, walk, map[string]any{"pattern": "^$"}); strings.Contains(got, "empty.txt") {
		t.Errorf("an empty file reported a line: %q", got)
	}
}
