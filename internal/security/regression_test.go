package security

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// fixtureScanner replays a recorded scanner-output fixture from testdata/
// through the real parser a live scanner's Scan would call — no binary, no
// container runtime, no network (P11.9). See testdata/README.md for what
// each fixture represents and its provenance.
type fixtureScanner struct {
	name  string
	file  string
	parse func([]byte) ([]Finding, error)
}

func (f fixtureScanner) Name() string { return f.name }
func (f fixtureScanner) Resolve(context.Context, Options) (Method, sandbox.ContainerRuntime, string, string) {
	return MethodHost, "", "", ""
}
func (f fixtureScanner) Scan(context.Context, string, Method, sandbox.ContainerRuntime, string, Options) ([]Finding, error) {
	data, err := os.ReadFile(filepath.Join("testdata", f.file))
	if err != nil {
		return nil, err
	}
	return f.parse(data)
}

func sarifFixture(name, file, tool string) fixtureScanner {
	return fixtureScanner{name: name, file: file, parse: func(b []byte) ([]Finding, error) { return ParseSARIF(b, tool) }}
}

// osvFixtureParse replays a recorded osv-scanner report the way a real scan
// parses it. The fixtures record paths relative to the scanned tree already,
// so the scan root is empty — osvRelativeSource has nothing to trim.
func osvFixtureParse(b []byte) ([]Finding, error) { return parseOSVScanner(b, "") }

// TestScanRegressionAcrossRecordedOutputs is the P11.9 regression proof: it
// drives the full aggregation pipeline (parse -> dedup -> ASVS -> baseline
// -> sort, exactly what RunWithOptions does for a live scan) over recorded
// fixtures for every parser Aegis has (SARIF-based, gitleaks, trufflehog,
// osv-scanner), including a three-way cross-tool CVE overlap and all three baseline
// entry states (active/expired/invalid), and compares the aggregated
// Report against a checked-in golden file. A change to any parser,
// DedupFindings, assignASVS, or Baseline.Apply that alters real-world
// behavior on these fixtures trips this test — see testdata/README.md to
// regenerate deliberately after reviewing the diff.
func TestScanRegressionAcrossRecordedOutputs(t *testing.T) {
	dir := t.TempDir()
	// P76.3: the fixture baseline stands in for an operator-authored one, so
	// the trust gate is pinned open — the golden records aggregation
	// behaviour, not the trust decision.
	withBaselineTrust(t, true)

	writeBaseline(t, dir, `
suppressions:
  - rule_id: "CVE-2021-23337"
    reason: "accepted risk for this fixture set: mitigated by a network policy, tracked externally"
    expires: "2099-01-01"
  - rule_id: "generic-api-key"
    reason: "long past its review date on purpose, to exercise the expired path"
    expires: "2000-01-01"
  - rule_id: "sql-injection"
    reason: "missing an expires date on purpose, to exercise the invalid path"
`)

	scanners := []Scanner{
		sarifFixture("opengrep", "opengrep_sast.sarif.json", "opengrep"),
		sarifFixture("trivy-vuln", "trivy_vuln.sarif.json", "trivy"),
		sarifFixture("trivy-misconfig", "trivy_misconfig.sarif.json", "trivy"),
		fixtureScanner{name: "gitleaks", file: "gitleaks.json", parse: parseGitleaks},
		fixtureScanner{name: "trufflehog", file: "trufflehog.jsonl", parse: func(b []byte) ([]Finding, error) { return parseTrufflehog(b, true) }},
		fixtureScanner{name: "osv-scanner", file: "osv_scanner.json", parse: osvFixtureParse},
		sarifFixture("grype", "grype_sca.sarif.json", "grype"),
		sarifFixture("zap", "zap_dast.sarif.json", "zap"),
	}

	rep := RunWithOptions(context.Background(), dir, scanners, Options{})
	assertGoldenReport(t, filepath.Join("testdata", "regression.golden.json"), rep)
}

// assertGoldenReport compares rep against the JSON file at path, following
// the same AEGIS_EVAL_UPDATE=1 convention as internal/eval.AssertGolden —
// set it to (re)write the golden file instead of comparing, then review the
// diff before committing.
func assertGoldenReport(t *testing.T, path string, rep Report) {
	t.Helper()
	got, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	got = append(got, '\n')

	if os.Getenv("AEGIS_EVAL_UPDATE") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v (rerun with AEGIS_EVAL_UPDATE=1 to create it)", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("golden report mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
