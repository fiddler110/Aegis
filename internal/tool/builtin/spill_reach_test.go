package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

// TestSpillLocationIsReachableByTheModel is the one design question P64.1 says
// could change the answer: a locator the model cannot open is worse than no
// locator. It is answered here empirically rather than by reading the sandbox,
// because the read tools' confinement, the search backends' skip list and the
// spill directory's location are three separate decisions that only interact at
// runtime.
//
// The measured answers, 2026-08-14:
//
//   - <data_dir> — the obvious home, alongside builtin-skills/ and
//     model_caps.json — is NOT reachable. Every read tool resolves through
//     resolveRead → sandbox.ValidatePathIn over the workspace root plus the
//     session's additional roots, and the data dir is under none of them. This
//     is why the spill lives under the workspace instead.
//
//   - <workspace>/.aegis/spill/ IS reachable by read_file, with or without
//     offset/limit.
//
//   - It is NOT reachable by grep, and that is not fixable from here: grep has
//     no path parameter at all (it always searches the workspace root), and
//     ".aegis" is in skipDirNames so neither backend descends into it. The
//     roadmap's suggested hint — "or grep this path to search within it" —
//     therefore cannot be honored today, so spillLocator does not promise it.
//     Naming a recovery path that silently returns "no matches" is the exact
//     failure this item exists to remove.
func TestSpillLocationIsReachableByTheModel(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()                           // stands in for <data_dir>, deliberately outside root
	_ = tool.WithWorkdir(context.Background(), root) // tools here are constructed with root directly

	body := strings.Repeat("spilled line with a findable_token\n", 100)

	// (1) A spill under <data_dir> is unreadable.
	outside := filepath.Join(dataDir, "spill.txt")
	if err := os.WriteFile(outside, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rd := &readTool{root: root}
	// Not execTool: confinement rejects this as a Go error, not an error
	// Result — a harder no than expected, and worth recording as such.
	res, err := rd.Execute(context.Background(), mustJSON(t, map[string]any{"path": outside}))
	if err == nil && !res.IsError {
		t.Fatalf("read_file opened a path outside the workspace root — the confinement this test relies on is gone:\n%s", res.Content)
	}
	if err != nil {
		t.Logf("<data_dir> spill: read_file refused with a transport error — %v", err)
	} else {
		t.Logf("<data_dir> spill: read_file refused — %s", strings.TrimSpace(res.Content))
	}

	// (2) A spill under <workspace>/.aegis/spill IS readable, paging included.
	inside := filepath.Join(root, ".aegis", "spill")
	if err := os.MkdirAll(inside, 0o750); err != nil {
		t.Fatal(err)
	}
	rel := ".aegis/spill/shell-test.txt"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	res = execTool(t, rd.Execute, map[string]any{"path": rel})
	if res.IsError || !strings.Contains(res.Content, "findable_token") {
		t.Fatalf("read_file could not open the spill path the locator names (%s):\n%s", rel, res.Content)
	}
	res = execTool(t, rd.Execute, map[string]any{"path": rel, "offset": 50, "limit": 10})
	if res.IsError || strings.Count(res.Content, "\n") > 12 {
		t.Fatalf("read_file could not page the spill file with offset/limit:\n%s", res.Content)
	}

	// (3) grep cannot reach it. Asserted rather than lamented, so that if
	// either decision changes (a path parameter on grep, or .aegis leaving
	// skipDirNames) this test fails and the locator's wording gets revisited.
	gr := &grepTool{root: root}
	res = execTool(t, gr.Execute, map[string]any{"pattern": "findable_token"})
	if !strings.Contains(res.Content, "no matches") {
		t.Fatalf("grep now reaches the spill directory; spillLocator should be updated to offer it:\n%s", res.Content)
	}
}
