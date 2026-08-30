package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSkillGuardHoldsThroughASymlinkedRoot pins SEC-H.
//
// resolveWrite returns a symlink-resolved path — sandbox.ValidatePathIn's
// deliberate contract, since the resolved path is the one a caller must open to
// avoid a TOCTOU swap. denyMaterializedSkillWrite built its guarded prefix from
// the *unresolved* root, so the two lived in different namespaces, filepath.Rel
// produced a ".."-leading path, and the write read as outside the guarded tree.
// The guard silently did nothing on every host whose workspace is reached
// through a link.
//
// TestSkillAssetsAreReadOnlyToTools covers the same guard but only exercises
// this if the platform's temp dir happens to be symlinked (it is on macOS, via
// /var; it is not on most Linux hosts). This builds the symlink explicitly so
// the regression is caught everywhere, and asserts the resolved-root case
// alongside it so a fix that only ever resolves cannot pass by accident.
func TestSkillGuardHoldsThroughASymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	if err := os.MkdirAll(real, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	for _, root := range []string{link, real} {
		skillDir := filepath.Join(root, ".aegis", "builtin-skills", "threat-modeling")
		if err := os.MkdirAll(skillDir, 0o750); err != nil {
			t.Fatal(err)
		}
		recon := filepath.Join(skillDir, "recon.py")
		const original = "#!/usr/bin/env python3\nprint('recon')\n"
		if err := os.WriteFile(recon, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		rel := ".aegis/builtin-skills/threat-modeling/recon.py"

		if _, err := (&writeTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{
			"path": rel, "content": "clobbered",
		})); err == nil {
			t.Errorf("root %q: write_file was allowed into the built-in skill tree", root)
		}
		if _, err := (&editTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{
			"path": rel, "old_string": "recon", "new_string": "clobbered",
		})); err == nil {
			t.Errorf("root %q: edit was allowed into the built-in skill tree", root)
		}
		if got, _ := os.ReadFile(recon); string(got) != original {
			t.Errorf("root %q: recon.py was modified: %q", root, got)
		}
	}
}
