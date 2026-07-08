package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSelectorExactName(t *testing.T) {
	names, ok := ResolveSelector("Trufflehog") // case-insensitive
	if !ok || len(names) != 1 || names[0] != "trufflehog" {
		t.Errorf("ResolveSelector(Trufflehog) = %v, %v", names, ok)
	}
}

func TestResolveSelectorCategoryAlias(t *testing.T) {
	names, ok := ResolveSelector("secrets")
	if !ok {
		t.Fatal("expected secrets to resolve")
	}
	want := map[string]bool{"gitleaks": true, "trufflehog": true}
	if len(names) != len(want) {
		t.Fatalf("secrets = %v, want %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected scanner %q in secrets category", n)
		}
	}
}

func TestResolveSelectorUnknown(t *testing.T) {
	if _, ok := ResolveSelector("not-a-real-thing"); ok {
		t.Error("expected unknown selector to not resolve")
	}
}

func TestSelectScannersFiltersAndForceEnables(t *testing.T) {
	all := DefaultScanners()
	// trufflehog is DefaultEnabled:false; an explicit /scan trufflehog must
	// still force it on for this run.
	filtered, opts, err := SelectScanners(all, Options{}, []string{"trufflehog"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Name() != "trufflehog" {
		t.Fatalf("filtered = %v, want just trufflehog", filtered)
	}
	p := opts.Tools["trufflehog"]
	if !p.Enabled || !p.EnabledExplicit {
		t.Errorf("trufflehog policy = %+v, want Enabled+EnabledExplicit", p)
	}
}

func TestSelectScannersCategoryUnion(t *testing.T) {
	filtered, _, err := SelectScanners(DefaultScanners(), Options{}, []string{"secrets"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, sc := range filtered {
		got[sc.Name()] = true
	}
	if !got["gitleaks"] || !got["trufflehog"] || len(got) != 2 {
		t.Errorf("filtered = %v, want exactly gitleaks+trufflehog", got)
	}
}

func TestSelectScannersUnknownSelectorErrors(t *testing.T) {
	_, _, err := SelectScanners(DefaultScanners(), Options{}, []string{"not-a-real-thing"})
	if err == nil {
		t.Fatal("expected an error for an unrecognized selector")
	}
}

func TestSelectScannersNoopWhenEmpty(t *testing.T) {
	all := DefaultScanners()
	filtered, _, err := SelectScanners(all, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != len(all) {
		t.Errorf("expected no-op filter to return every scanner, got %d want %d", len(filtered), len(all))
	}
}

func TestDetectLanguagesGo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	langs := DetectLanguages(dir)
	if len(langs) != 1 || langs[0] != "go" {
		t.Errorf("DetectLanguages = %v, want [go]", langs)
	}
}

func TestDetectLanguagesMultiple(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"go.mod", "requirements.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	langs := DetectLanguages(dir)
	if len(langs) != 2 || langs[0] != "go" || langs[1] != "python" {
		t.Errorf("DetectLanguages = %v, want [go python]", langs)
	}
}

func TestDetectLanguagesSkipsVendorDirs(t *testing.T) {
	dir := t.TempDir()
	nodeModules := filepath.Join(dir, "node_modules", "some-pkg")
	if err := os.MkdirAll(nodeModules, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeModules, "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	langs := DetectLanguages(dir)
	if len(langs) != 0 {
		t.Errorf("DetectLanguages = %v, want none (node_modules should be skipped)", langs)
	}
}

func TestDetectLanguagesNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if langs := DetectLanguages(dir); len(langs) != 0 {
		t.Errorf("DetectLanguages = %v, want none", langs)
	}
}

// TestAutoEnableLanguageScannersEnablesMatch is the core "select the correct
// scanner" behavior: a Go project auto-enables gosec even though it's
// DefaultEnabled:false, with no config change required.
func TestAutoEnableLanguageScannersEnablesMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := AutoEnableLanguageScanners(dir, Options{})
	if !opts.Tools["gosec"].Enabled {
		t.Errorf("gosec should be auto-enabled for a Go project, got %+v", opts.Tools["gosec"])
	}
	if opts.Tools["gosec"].EnabledExplicit {
		t.Error("auto-enabling must not be marked EnabledExplicit — it's a suggestion, not an operator choice")
	}
}

// TestAutoEnableLanguageScannersNeverOverridesExplicitChoice is the "never
// silently override an explicit config" requirement, both directions.
func TestAutoEnableLanguageScannersNeverOverridesExplicitChoice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Tools: map[string]ToolPolicy{
		"gosec": {Enabled: false, EnabledExplicit: true},
	}}
	got := AutoEnableLanguageScanners(dir, opts)
	if got.Tools["gosec"].Enabled {
		t.Error("an explicit enabled:false must never be overridden by language auto-detection")
	}
}

func TestAutoEnableLanguageScannersNoLanguageDetectedIsNoop(t *testing.T) {
	dir := t.TempDir()
	opts := Options{DefaultMethod: "auto"}
	got := AutoEnableLanguageScanners(dir, opts)
	if len(got.Tools) != 0 {
		t.Errorf("expected no-op for a directory with no detected language, got %+v", got.Tools)
	}
}

// TestDetectLanguageSummaryDedicatedVsGeneric checks the "don't try to run
// bandit on a Rust repo" case end to end: Rust is detected but has no
// dedicated opt-in engine (Scanner == ""), while Go's is named — Rust source
// is still covered by opengrep's generic multi-language SAST, just not by a
// language-specific tool that doesn't exist for it.
func TestDetectLanguageSummaryDedicatedVsGeneric(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"go.mod", "Cargo.toml"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	summary := DetectLanguageSummary(dir)
	got := map[string]string{}
	for _, s := range summary {
		got[s.Language] = s.Scanner
	}
	if got["go"] != "gosec" {
		t.Errorf("go scanner = %q, want gosec", got["go"])
	}
	if got["rust"] != "" {
		t.Errorf("rust scanner = %q, want empty (no dedicated engine yet)", got["rust"])
	}
}

func TestWriteReportArtifactRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rep := Report{Findings: []Finding{{Tool: "trufflehog", RuleID: "AWS", Severity: SevHigh}}}
	WriteReportArtifact(dir, "scan", rep)
	path := ReportArtifactPath(dir, "scan")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected report artifact at %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Error("report artifact is empty")
	}
}
