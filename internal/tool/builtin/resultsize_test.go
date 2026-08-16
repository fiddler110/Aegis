package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestResultSizeComposition is P64.3's measurement harness, and it is the
// mirror image of TestBasePromptComposition_localProfile in internal/server:
// that one prints where the base prompt's tokens go, this one prints where a
// *result's* tokens go. The base prompt is bounded and paid once per turn; a
// tool result is paid on every turn it survives in the transcript until
// compaction evicts it, which on a 4k–32k local window is the larger half.
//
// It is explicitly **not** a ceiling test, and that is a decision rather than
// an omission. A base-prompt ceiling is defensible because the prompt is
// deterministic; result size depends on the workspace, so a CI failure keyed to
// it would fail on someone else's repo for reasons that are not their bug. The
// only assertions here are the ones that would make the table a lie: that the
// tools ran, and that anything over its own cap actually carries a notice.
//
// Run with -v to see the table.
func TestResultSizeComposition(t *testing.T) {
	root := resultSizeFixture(t)
	ctx := tool.WithWorkdir(context.Background(), root)

	type probe struct {
		label string
		tool  tool.Tool
		args  any
		// cap is the tool's declared result cap in bytes, 0 when it caps by
		// item or line rather than by byte.
		cap int
		end string
	}
	probes := []probe{
		{"read_file (small)", &readTool{root: root}, map[string]any{"path": "small.txt"}, 0, "head"},
		{"read_file (2000 lines, default window)", &readTool{root: root}, map[string]any{"path": "long.go"}, 0, "head"},
		{"read_file (explicit 50-line window)", &readTool{root: root}, map[string]any{"path": "long.go", "offset": 1, "limit": 50}, 0, "head"},
		{"ls (workspace root)", &lsTool{root: root}, map[string]any{"path": "."}, 0, "head"},
		{"glob **/*.go", &globTool{root: root}, map[string]any{"pattern": "**/*.go"}, 0, "head"},
		{"glob ** (everything)", &globTool{root: root}, map[string]any{"pattern": "**"}, 0, "head"},
		{"grep (rare term)", &grepTool{root: root}, map[string]any{"pattern": "needle_rare_token"}, 0, "head"},
		{"grep (common term, hits the cap)", &grepTool{root: root}, map[string]any{"pattern": "func "}, 0, "head"},
		{"repo_map (action=map)", &repomapTool{root: root}, map[string]any{"action": "map"}, 0, "head"},
	}

	type row struct {
		label      string
		bytes      int
		tokens     int
		capBytes   int
		end        string
		hasNotice  bool
		isError    bool
		lineCount  int
		errMessage string
	}
	var rows []row
	for _, p := range probes {
		raw, err := json.Marshal(p.args)
		if err != nil {
			t.Fatalf("%s: marshal args: %v", p.label, err)
		}
		res, err := p.tool.Execute(ctx, raw)
		r := row{label: p.label, capBytes: p.cap, end: p.end}
		if err != nil {
			r.errMessage = err.Error()
		} else {
			r.bytes = len(res.Content)
			r.tokens = tokenest.Estimate(res.Content)
			r.lineCount = strings.Count(res.Content, "\n") + 1
			r.isError = res.IsError
			// Three phrasings, because read_file's paging notice deliberately
			// does not say "truncated" — it says the file continues, which is
			// the accurate word for a window the caller can move.
			r.hasNotice = strings.Contains(res.Content, "truncated") ||
				strings.Contains(res.Content, "capped at") ||
				strings.Contains(res.Content, "the file continues past")
		}
		rows = append(rows, r)
	}

	// The two assertions that keep the table honest. Neither is a size bound.
	for _, r := range rows {
		if r.errMessage != "" {
			t.Errorf("%s: tool returned a transport error, so its row is meaningless: %s", r.label, r.errMessage)
			continue
		}
		if r.isError {
			t.Errorf("%s: tool reported an error result, so its row measures an error message rather than a result", r.label)
		}
		if r.capBytes > 0 && r.bytes > r.capBytes {
			t.Errorf("%s: returned %d bytes against a declared %d-byte cap — the cap is not being applied", r.label, r.bytes, r.capBytes)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\nP64.3 built-in tool result sizes (fixture workspace: %d files, %s)\n\n", resultSizeFixtureFiles, root)
	fmt.Fprintf(&b, "%-42s %9s %11s %7s %6s %8s\n", "tool call", "bytes", "est.tokens", "lines", "end", "notice")
	total := 0
	for _, r := range rows {
		notice := "-"
		if r.hasNotice {
			notice = "yes"
		}
		fmt.Fprintf(&b, "%-42s %9d %11d %7d %6s %8s\n", r.label, r.bytes, r.tokens, r.lineCount, r.end, notice)
		total += r.tokens
	}
	fmt.Fprintf(&b, "\nsum of the calls above: %d estimated tokens.\n", total)
	fmt.Fprintf(&b, "For scale, the local-profile base prompt budget is 4,550 tokens\n"+
		"(localBasePromptCeilingTokens, internal/server) — paid once per turn, where\n"+
		"these are paid on every turn they survive in the transcript.\n")
	t.Log(b.String())
}

// resultSizeFixtureFiles is the fixture's file count. Deliberately above
// grepMaxMatches' 500 so the "common term" probe reports a capped result rather
// than a small one — a table where nothing hits a cap measures nothing.
const resultSizeFixtureFiles = 60

func resultSizeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("one line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var long strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&long, "// line %d of a long source file\n", i)
	}
	if err := os.WriteFile(filepath.Join(root, "long.go"), []byte(long.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < resultSizeFixtureFiles; i++ {
		dir := filepath.Join(root, fmt.Sprintf("pkg%02d", i%6))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		var src strings.Builder
		fmt.Fprintf(&src, "package pkg%02d\n\n", i%6)
		for j := 0; j < 20; j++ {
			fmt.Fprintf(&src, "func Fn%02d_%02d() { _ = %d }\n", i, j, j)
		}
		if i == 0 {
			src.WriteString("// needle_rare_token appears exactly once in the fixture\n")
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.go", i)), []byte(src.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
