package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitWorkspace makes a temp directory look like a git checkout, which is what
// ensureReportGitignore keys on.
func gitWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func sampleReport() Report {
	return Report{Findings: []Finding{{Tool: "trufflehog", RuleID: "AWS", Severity: SevHigh}}}
}

// TestWriteReportArtifactRoundTrip is the "a scan's findings survive the run
// that produced them" regression: whatever ReportArtifactPath names under the
// active options is the file WriteReportArtifact filled.
func TestWriteReportArtifactRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	dir := t.TempDir()

	got := WriteReportArtifact(dir, "scan", sampleReport(), WithDataDir(dataDir))
	if got.Err != nil {
		t.Fatalf("WriteReportArtifact: %v", got.Err)
	}
	path := ReportArtifactPath(dir, "scan", WithDataDir(dataDir))
	if got.Path != path {
		t.Errorf("write path = %q, ReportArtifactPath = %q; the two must agree or a caller reports a file nobody wrote", got.Path, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected report artifact at %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Error("report artifact is empty")
	}
}

// TestReportArtifactDefaultsOutsideTheScannedDirectory is the P81.32 (FIND-32)
// default: a report is a ranked map of a repository's weaknesses, and it no
// longer lands inside the repository where a routine `git add -A` would carry
// it wherever that repository is mirrored.
func TestReportArtifactDefaultsOutsideTheScannedDirectory(t *testing.T) {
	dataDir := t.TempDir()
	dir := gitWorkspace(t)

	got := WriteReportArtifact(dir, "scan", sampleReport(), WithDataDir(dataDir))
	if got.Err != nil {
		t.Fatalf("WriteReportArtifact: %v", got.Err)
	}
	if got.InWorkspace {
		t.Error("default write reported InWorkspace")
	}
	if !strings.HasPrefix(got.Path, dataDir) {
		t.Errorf("report path = %q, want it under the data dir %q", got.Path, dataDir)
	}
	if _, err := os.Stat(filepath.Join(dir, ".aegis")); !os.IsNotExist(err) {
		t.Errorf("a default scan created %s in the scanned repository; nothing should be written there", filepath.Join(dir, ".aegis"))
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Error("a default scan wrote a .gitignore; there is nothing in the repository to ignore")
	}
	if _, err := os.ReadFile(got.Path); err != nil {
		t.Fatalf("expected report artifact at %s: %v", got.Path, err)
	}
}

// TestReportArtifactInWorkspaceIsOptInAndGitignored covers the other half of
// P81.32: the in-repository location still exists, and choosing it also
// ignores it and says so — a silent mitigation is one nobody knows to rely on.
func TestReportArtifactInWorkspaceIsOptInAndGitignored(t *testing.T) {
	dir := gitWorkspace(t)

	got := WriteReportArtifact(dir, "scan", sampleReport(), InWorkspace())
	if got.Err != nil {
		t.Fatalf("WriteReportArtifact: %v", got.Err)
	}
	if !got.InWorkspace {
		t.Error("opted-in write did not report InWorkspace")
	}
	want := filepath.Join(dir, ".aegis", "security", "scan.json")
	if got.Path != want {
		t.Errorf("report path = %q, want %q", got.Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected report artifact at %s: %v", want, err)
	}
	if !got.GitignoreUpdated {
		t.Error("expected the write to report adding a .gitignore entry")
	}
	if !strings.Contains(got.Note(), ".gitignore") {
		t.Errorf("Note() = %q, want it to tell the operator the entry was added", got.Note())
	}
	ignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("expected a .gitignore: %v", err)
	}
	if !strings.Contains(string(ignore), reportGitignoreEntry) {
		t.Errorf(".gitignore = %q, want it to contain %q", ignore, reportGitignoreEntry)
	}

	// A second scan must not append the entry again, nor claim it did.
	again := WriteReportArtifact(dir, "scan", sampleReport(), InWorkspace())
	if again.GitignoreUpdated {
		t.Error("second write reported updating .gitignore again")
	}
	ignore2, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(ignore2), reportGitignoreEntry) != 1 {
		t.Errorf(".gitignore accumulated duplicate entries: %q", ignore2)
	}
}

// TestReportArtifactGitignoreRespectsExistingCoverage: an operator who already
// ignored .aegis/ has made this decision, and a redundant appended rule reads
// as the tool not having looked.
func TestReportArtifactGitignoreRespectsExistingCoverage(t *testing.T) {
	for _, existing := range []string{".aegis/\n", "/.aegis/*\n", ".aegis/security/\n"} {
		dir := gitWorkspace(t)
		path := filepath.Join(dir, ".gitignore")
		if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		got := WriteReportArtifact(dir, "scan", sampleReport(), InWorkspace())
		if got.GitignoreUpdated {
			t.Errorf("existing entry %q: reported an update that was not needed", strings.TrimSpace(existing))
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != existing {
			t.Errorf("existing entry %q: .gitignore was rewritten to %q", strings.TrimSpace(existing), after)
		}
	}
}

// TestReportArtifactSkipsGitignoreOutsideAGitRepo: with nothing to commit
// there is nothing to protect against, and a .gitignore in a directory the
// operator never version-controlled is unexplained litter.
func TestReportArtifactSkipsGitignoreOutsideAGitRepo(t *testing.T) {
	dir := t.TempDir()
	got := WriteReportArtifact(dir, "scan", sampleReport(), InWorkspace())
	if got.Err != nil {
		t.Fatalf("WriteReportArtifact: %v", got.Err)
	}
	if got.GitignoreUpdated {
		t.Error("wrote a .gitignore outside a git repository")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Error("a .gitignore was created outside a git repository")
	}
}

// TestReportScopeSlugSeparatesSameNamedWorkspaces: two checkouts of the same
// repository share a base name, and in one shared data directory the second
// scan must not silently overwrite the first's report.
func TestReportScopeSlugSeparatesSameNamedWorkspaces(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a", "aegis")
	b := filepath.Join(root, "b", "aegis")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	slugA, slugB := reportScopeSlug(a), reportScopeSlug(b)
	if slugA == slugB {
		t.Fatalf("two workspaces named %q collided on slug %q", filepath.Base(a), slugA)
	}
	for _, slug := range []string{slugA, slugB} {
		if !strings.HasPrefix(slug, "aegis-") {
			t.Errorf("slug %q does not lead with the recognizable base name", slug)
		}
		if strings.Contains(slug, string(os.PathSeparator)) {
			t.Errorf("slug %q is not a single path segment", slug)
		}
	}
	if reportScopeSlug(a) != slugA {
		t.Error("reportScopeSlug is not stable across calls")
	}
}

// TestReportArtifactNeverEmitsAScannedCredential is the P81.32 companion to
// the location change: wherever the report lands, the credential a secret
// scanner found in the source must not travel in it.
//
// The control is in the parsers, not in a scrubbing pass after the fact — a
// Finding has no field for source text, and each parser reads only the
// tool's own rule metadata, location, and pre-redacted display form. Every
// tool below reports the raw secret in its native output; none of them get to
// put it in a Finding. (internal/security/redact.go is a different control on
// a different path: it scrubs tool-read *file content* headed to a cloud
// provider, and never runs over a scan report.)
func TestReportArtifactNeverEmitsAScannedCredential(t *testing.T) {
	// Synthetic, and never a real credential: the point is that this exact
	// byte sequence does not survive into the artifact.
	const credential = "AKIAIOSFODNN7EXAMPLE-wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

	gitleaksOut := []byte(`[{"RuleID":"aws-access-token","Description":"AWS Access Key","File":"app/config.go","StartLine":12,` +
		`"Secret":"` + credential + `","Match":"awsKey = \"` + credential + `\""}]`)
	trufflehogOut := []byte(`{"DetectorName":"AWS","Verified":false,"Redacted":"AKIA****","Raw":"` + credential + `",` +
		`"RawV2":"` + credential + `","SourceMetadata":{"Data":{"Filesystem":{"file":"app/config.go","line":12}}}}`)
	sarifOut := []byte(`{"runs":[{"tool":{"driver":{"name":"opengrep","rules":[{"id":"hardcoded-secret"}]}},` +
		`"results":[{"ruleId":"hardcoded-secret","level":"error","message":{"text":"hardcoded credential"},` +
		`"locations":[{"physicalLocation":{"artifactLocation":{"uri":"app/config.go"},` +
		`"region":{"startLine":12,"snippet":{"text":"awsKey = \"` + credential + `\""}}}}]}]}]}`)

	var findings []Finding
	for _, tc := range []struct {
		name  string
		parse func() ([]Finding, error)
	}{
		{"gitleaks", func() ([]Finding, error) { return parseGitleaks(gitleaksOut) }},
		{"trufflehog", func() ([]Finding, error) { return parseTrufflehog(trufflehogOut, false) }},
		{"sarif", func() ([]Finding, error) { return ParseSARIF(sarifOut, "opengrep") }},
	} {
		parsed, err := tc.parse()
		if err != nil {
			t.Fatalf("%s: parse: %v", tc.name, err)
		}
		if len(parsed) == 0 {
			t.Fatalf("%s: expected the fixture to produce a finding, or this test proves nothing", tc.name)
		}
		findings = append(findings, parsed...)
	}

	dataDir := t.TempDir()
	dir := t.TempDir()
	got := WriteReportArtifact(dir, "scan", Report{Findings: findings}, WithDataDir(dataDir))
	if got.Err != nil {
		t.Fatalf("WriteReportArtifact: %v", got.Err)
	}
	data, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), credential) {
		t.Errorf("the scanned credential reached the emitted report at %s", got.Path)
	}
	// The rendered form the model and the operator see is the same document.
	if rendered := (Report{Findings: findings}).Format(); strings.Contains(rendered, credential) {
		t.Error("the scanned credential reached the rendered report")
	}
}
