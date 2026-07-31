package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFrontmatter(t *testing.T) {
	sk := parseSkill("file", "---\nname: deploy\ndescription: Ship the app\n---\n\nStep 1. build\n")
	if sk.Name != "deploy" {
		t.Errorf("name = %q, want deploy", sk.Name)
	}
	if sk.Description != "Ship the app" {
		t.Errorf("description = %q", sk.Description)
	}
	if strings.Contains(sk.Content, "---") || !strings.Contains(sk.Content, "Step 1") {
		t.Errorf("body not stripped: %q", sk.Content)
	}
}

// TestParseFrontmatterQuotedColon locks in real-YAML parsing (P32.9): a
// naive strings.Cut(line, ":") split on the first colon would truncate this
// description at "Do", dropping everything from the embedded colon onward.
func TestParseFrontmatterQuotedColon(t *testing.T) {
	sk := parseSkill("file", "---\nname: deploy\ndescription: \"Do: this thing right\"\n---\nbody\n")
	if sk.Description != "Do: this thing right" {
		t.Errorf("description = %q, want %q", sk.Description, "Do: this thing right")
	}
}

// TestParseFrontmatterMultilineValue locks in real-YAML parsing of a block
// scalar, which the old per-line split couldn't represent at all (each
// continuation line has no colon and was silently skipped).
func TestParseFrontmatterMultilineValue(t *testing.T) {
	sk := parseSkill("file", "---\ndescription: |\n  Line one\n  Line two\n---\nbody\n")
	want := "Line one\nLine two"
	if sk.Description != want {
		t.Errorf("description = %q, want %q", sk.Description, want)
	}
}

// TestParseFrontmatterCaseInsensitiveKeys preserves the old hand-rolled
// parser's case-insensitive key matching.
func TestParseFrontmatterCaseInsensitiveKeys(t *testing.T) {
	sk := parseSkill("file", "---\nName: deploy\nDESCRIPTION: Ship the app\n---\nbody\n")
	if sk.Name != "deploy" {
		t.Errorf("name = %q, want deploy", sk.Name)
	}
	if sk.Description != "Ship the app" {
		t.Errorf("description = %q, want %q", sk.Description, "Ship the app")
	}
}

// TestParseFrontmatterMalformedYAML ensures a syntactically invalid
// frontmatter block doesn't panic or propagate an error (parseSkill has no
// error return); it falls back to the default name and no description.
func TestParseFrontmatterMalformedYAML(t *testing.T) {
	sk := parseSkill("file", "---\nname: [unterminated\n---\nbody\n")
	if sk.Name != "file" {
		t.Errorf("name = %q, want fallback %q", sk.Name, "file")
	}
	if sk.Description != "" {
		t.Errorf("description = %q, want empty", sk.Description)
	}
	if !strings.Contains(sk.Content, "body") {
		t.Errorf("body not preserved: %q", sk.Content)
	}
}

func TestBuildIndexProgressiveDisclosure(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, ".aegis", "skills")
	writeSkill(t, sd, "described.md", "---\ndescription: Does a thing\n---\nfull body here\n")
	writeSkill(t, sd, "legacy.md", "no frontmatter, eager inject\n")

	idx := BuildIndex(dir, "", nil)
	if !strings.Contains(idx, "<skills_available>") {
		t.Errorf("expected skills_available block, got:\n%s", idx)
	}
	if !strings.Contains(idx, "described: Does a thing") {
		t.Errorf("described skill missing from index:\n%s", idx)
	}
	if strings.Contains(idx, "full body here") {
		t.Errorf("described skill body should NOT be eager-injected:\n%s", idx)
	}
	if !strings.Contains(idx, "eager inject") {
		t.Errorf("legacy skill should be eager-injected:\n%s", idx)
	}
}

// TestDiscoverCacheDetectsFileEdits covers P32.7's core correctness
// requirement: the signature short-circuit must never serve stale content
// after a skill file is edited, even though caching means Discover no longer
// unconditionally re-reads every file.
func TestDiscoverCacheDetectsFileEdits(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, ".aegis", "skills")
	writeSkill(t, sd, "deploy.md", "---\ndescription: Ship\n---\nfirst body\n")

	got := Discover(dir, "", nil)
	if len(got) != 1 || !strings.Contains(got[0].Content, "first body") {
		t.Fatalf("initial discover = %+v, want content containing 'first body'", got)
	}

	writeSkill(t, sd, "deploy.md", "---\ndescription: Ship\n---\nsecond body, longer\n")
	// Force a distinct mtime so the signature changes even if the edit above
	// lands within the same filesystem timestamp tick as the first write.
	bump := time.Now().Add(time.Second)
	if err := os.Chtimes(filepath.Join(sd, "deploy.md"), bump, bump); err != nil {
		t.Fatal(err)
	}

	got = Discover(dir, "", nil)
	if len(got) != 1 || !strings.Contains(got[0].Content, "second body, longer") {
		t.Fatalf("discover after edit = %+v, want updated content", got)
	}
}

// TestDiscoverCacheDetectsNestedBundledAssetChanges covers the reason P32.7's
// signature walks recursively rather than reusing persona's flat top-level
// scan: a bundled skill's asset files live in subdirectories (references/,
// scripts/), and adding one there doesn't necessarily touch the bundled
// skill's own top-level directory entry.
func TestDiscoverCacheDetectsNestedBundledAssetChanges(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, ".aegis", "skills")
	bundleDir := filepath.Join(sd, "html-report")
	writeSkill(t, bundleDir, "SKILL.md", "---\ndescription: Report\n---\nbody\n")

	got := Discover(dir, "", nil)
	if len(got) != 1 {
		t.Fatalf("discover = %+v, want 1 skill", got)
	}
	if strings.Contains(got[0].Content, "template.html") {
		t.Fatalf("asset manifest mentions template.html before it exists:\n%s", got[0].Content)
	}

	refDir := filepath.Join(bundleDir, "references")
	writeSkill(t, refDir, "template.html", "<html></html>")

	got = Discover(dir, "", nil)
	if len(got) != 1 || !strings.Contains(got[0].Content, "references/template.html") {
		t.Fatalf("discover after adding nested asset = %+v, want manifest to list it", got)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, ".aegis", "skills")
	writeSkill(t, sd, "deploy.md", "---\ndescription: Ship\n---\nrun make deploy\n")

	sk, ok := Load(dir, "", nil, "deploy")
	if !ok {
		t.Fatal("skill not found")
	}
	if !strings.Contains(sk.Content, "run make deploy") {
		t.Errorf("wrong body: %q", sk.Content)
	}
	if _, ok := Load(dir, "", nil, "missing"); ok {
		t.Error("expected missing skill to not load")
	}
}

func TestLoadBundledDirectorySkill(t *testing.T) {
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, ".aegis", "skills", "html-report")
	writeSkill(t, bundleDir, "SKILL.md", "---\ndescription: Make a report\n---\nUse the template.\n")
	writeSkill(t, bundleDir, "template.html", "<html></html>\n")
	writeSkill(t, bundleDir, "validate.py", "print('ok')\n")
	writeSkill(t, filepath.Join(bundleDir, "references"), "rubric.md", "# Rubric\n")

	sk, ok := Load(dir, "", nil, "html-report")
	if !ok {
		t.Fatal("bundled skill not found")
	}
	if sk.Description != "Make a report" {
		t.Errorf("description = %q", sk.Description)
	}
	if sk.Dir == "" {
		t.Error("expected Dir to be set for a bundled skill")
	}
	if !strings.Contains(sk.Content, "Use the template.") {
		t.Errorf("missing manifest body: %q", sk.Content)
	}
	if !strings.Contains(sk.Content, "<skill_assets") || !strings.Contains(sk.Content, "template.html") || !strings.Contains(sk.Content, "validate.py") {
		t.Errorf("expected asset manifest listing template.html and validate.py, got:\n%s", sk.Content)
	}
	if !strings.Contains(sk.Content, "references/rubric.md") {
		t.Errorf("expected nested asset references/rubric.md to be listed, got:\n%s", sk.Content)
	}
	if strings.Contains(sk.Content, "SKILL.md") {
		t.Errorf("manifest file itself should not be listed as an asset:\n%s", sk.Content)
	}

	idx := BuildIndex(dir, "", nil)
	if !strings.Contains(idx, "html-report: Make a report") {
		t.Errorf("bundled skill missing from progressive-disclosure index:\n%s", idx)
	}
}

func TestBuiltinsListsEmbeddedSkills(t *testing.T) {
	names := make(map[string]bool)
	for _, b := range Builtins() {
		if b.Description == "" {
			t.Errorf("builtin %q has no description (won't get progressive disclosure)", b.Name)
		}
		names[b.Name] = true
	}
	for _, want := range []string{"content-review", "html-report", "security-audit", "architecture-diagram", "debug-investigation", "redteam-engagement", "threat-modeling", "latex-report", "deep-research", "structured-build", "documentation-as-code"} {
		if !names[want] {
			t.Errorf("expected built-in skill %q, got: %v", want, names)
		}
	}
}

func TestDiscoverBuiltinDisabledByDefault(t *testing.T) {
	dataDir := t.TempDir()
	if err := MaterializeBuiltins(dataDir); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()

	if got := Discover(workDir, dataDir, nil); len(got) != 0 {
		t.Errorf("expected no skills with no enabled builtins, got %v", got)
	}

	got := Discover(workDir, dataDir, []string{"security-audit"})
	if len(got) != 1 || got[0].Name != "security-audit" {
		t.Errorf("expected only security-audit enabled, got %v", got)
	}

	idx := BuildIndex(workDir, dataDir, []string{"security-audit"})
	if !strings.Contains(idx, "security-audit") {
		t.Errorf("enabled builtin missing from index:\n%s", idx)
	}
}

func TestDiscoverProjectOverridesBuiltin(t *testing.T) {
	dataDir := t.TempDir()
	if err := MaterializeBuiltins(dataDir); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	sd := filepath.Join(workDir, ".aegis", "skills", "security-audit")
	writeSkill(t, sd, "SKILL.md", "---\ndescription: custom override\n---\nlocal version\n")

	got := Discover(workDir, dataDir, []string{"security-audit"})
	if len(got) != 1 {
		t.Fatalf("expected exactly one security-audit skill, got %v", got)
	}
	if got[0].Description != "custom override" {
		t.Errorf("expected project skill to override built-in, got description %q", got[0].Description)
	}
}

// TestDiscoverProjectMaterializedBuiltin covers the fix for a builtin's
// bundled reference/skeleton assets being unreachable by the model's
// sandboxed file tools: once MaterializeBuiltinsToProject has placed a
// builtin under workDir, Discover should surface it with a
// workspace-relative <skill_assets dir="..."> (no leading path separator,
// no ".." escape) instead of the absolute host path the per-user-only copy
// produces.
func TestDiscoverProjectMaterializedBuiltin(t *testing.T) {
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"threat-modeling"}); err != nil {
		t.Fatal(err)
	}

	got := Discover(workDir, "", []string{"threat-modeling"})
	if len(got) != 1 || got[0].Name != "threat-modeling" {
		t.Fatalf("expected only threat-modeling, got %v", got)
	}
	sk := got[0]
	// withAssetManifest normalizes the manifest dir to forward slashes
	// (filepath.ToSlash) so it's identical cross-platform and matches what the
	// model's file tools expect; assert against that, not filepath.Join's
	// OS-native separators (which use backslashes on Windows and would spuriously
	// fail there).
	if !strings.Contains(sk.Content, `<skill_assets dir="`+filepath.ToSlash(filepath.Join(".aegis", "builtin-skills", "threat-modeling"))+`"`) {
		t.Errorf("expected workspace-relative skill_assets dir, got:\n%s", sk.Content)
	}
	if strings.Contains(sk.Content, workDir) {
		t.Errorf("skill_assets dir should not leak the absolute workDir:\n%s", sk.Content)
	}
	if !strings.Contains(sk.Content, "references/stride.md") {
		t.Errorf("expected references/stride.md listed as an asset:\n%s", sk.Content)
	}
}

// TestDiscoverProjectMaterializedBuiltinNotWrappedUntrusted asserts a
// project-materialized builtin keeps the same trusted treatment as the
// per-user copy — it must NOT get the wrapUntrustedSkill provenance framing
// project/user-authored skill files get, since this is still first-party
// content compiled into the binary, just relocated on disk.
func TestDiscoverProjectMaterializedBuiltinNotWrappedUntrusted(t *testing.T) {
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"security-audit"}); err != nil {
		t.Fatal(err)
	}

	got := Discover(workDir, "", []string{"security-audit"})
	if len(got) != 1 {
		t.Fatalf("expected one skill, got %v", got)
	}
	if strings.Contains(got[0].Content, "skill_untrusted_content") {
		t.Errorf("project-materialized builtin should not be wrapped as untrusted:\n%s", got[0].Content)
	}
}

// TestDiscoverPrefersProjectMaterializedOverPerUserBuiltin locks in
// discoverSpecs' ordering: once a project has its own materialized copy of
// a builtin, that copy — not the per-user dataDir one — is what Discover
// returns, since it's the copy read_file can actually reach.
func TestDiscoverPrefersProjectMaterializedOverPerUserBuiltin(t *testing.T) {
	dataDir := t.TempDir()
	if err := MaterializeBuiltins(dataDir); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	if err := MaterializeBuiltinsToProject(workDir, []string{"security-audit"}); err != nil {
		t.Fatal(err)
	}
	// Diverge the project copy's SKILL.md from the per-user one so the two
	// are distinguishable.
	projSkill := filepath.Join(workDir, ".aegis", "builtin-skills", "security-audit", "SKILL.md")
	data, err := os.ReadFile(projSkill)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projSkill, append(data, []byte("\n<!-- project copy marker -->\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Discover(workDir, dataDir, []string{"security-audit"})
	if len(got) != 1 {
		t.Fatalf("expected one skill (no duplicate), got %v", got)
	}
	if !strings.Contains(got[0].Content, "project copy marker") {
		t.Errorf("expected the project-materialized copy to win, got:\n%s", got[0].Content)
	}
}

// TestDiscoverFallsBackToPerUserBuiltinBeforeProjectMaterialized covers the
// narrow window before a project's first activation-triggered materialize
// has run (or if it failed): Discover must still find the builtin via the
// per-user dataDir copy rather than returning nothing.
func TestDiscoverFallsBackToPerUserBuiltinBeforeProjectMaterialized(t *testing.T) {
	dataDir := t.TempDir()
	if err := MaterializeBuiltins(dataDir); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir() // never materialized into

	got := Discover(workDir, dataDir, []string{"security-audit"})
	if len(got) != 1 || got[0].Name != "security-audit" {
		t.Fatalf("expected fallback to per-user builtin, got %v", got)
	}
}
