package security

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

func TestParseOpengrepSARIF(t *testing.T) {
	data := []byte(`{"runs":[{
		"tool":{"driver":{"name":"opengrep","rules":[
			{"id":"go.lang.security.audit.sqli","shortDescription":{"text":"possible SQL injection"}},
			{"id":"generic.secrets.key","shortDescription":{"text":"hardcoded key"}}
		]}},
		"results":[
			{"ruleId":"go.lang.security.audit.sqli","level":"error","message":{"text":"possible SQL injection"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"db/query.go"},"region":{"startLine":42}}}]},
			{"ruleId":"generic.secrets.key","level":"warning","message":{"text":"hardcoded key"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"cfg.go"},"region":{"startLine":7}}}]}
		]
	}]}`)
	findings, err := ParseSARIF(data, "opengrep")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	if findings[0].Severity != SevHigh {
		t.Errorf("error level should map to HIGH, got %s", findings[0].Severity)
	}
	if findings[0].Location != "db/query.go:42" {
		t.Errorf("location = %q", findings[0].Location)
	}
	if findings[1].Severity != SevMedium {
		t.Errorf("warning level should map to MEDIUM, got %s", findings[1].Severity)
	}
	if findings[0].Tool != "opengrep" {
		t.Errorf("tool = %q, want opengrep", findings[0].Tool)
	}
}

func TestParseTrivySARIF(t *testing.T) {
	data := []byte(`{"runs":[{
		"tool":{"driver":{"name":"trivy","rules":[
			{"id":"CVE-2024-1234","shortDescription":{"text":"RCE in foo"},"help":{"text":"upgrade foo to 1.0.1"},
			 "properties":{"tags":["CRITICAL"]}},
			{"id":"DS002","shortDescription":{"text":"root user"},"help":{"text":"add USER"},
			 "properties":{"tags":["HIGH"]}}
		]}},
		"results":[
			{"ruleId":"CVE-2024-1234","level":"error","message":{"text":"bad"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"go.sum"}}}]},
			{"ruleId":"DS002","level":"error","message":{"text":"runs as root"},
			 "locations":[{"physicalLocation":{"artifactLocation":{"uri":"Dockerfile"}}}]}
		]
	}]}`)
	findings, err := ParseSARIF(data, "trivy")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	var vuln, misc *Finding
	for i := range findings {
		switch findings[i].RuleID {
		case "CVE-2024-1234":
			vuln = &findings[i]
		case "DS002":
			misc = &findings[i]
		}
	}
	if vuln == nil || vuln.Severity != SevCritical || vuln.Remediation != "upgrade foo to 1.0.1" {
		t.Errorf("vuln finding wrong: %+v", vuln)
	}
	if misc == nil || misc.Remediation != "add USER" {
		t.Errorf("misconfig finding wrong: %+v", misc)
	}
}

func TestParseGitleaks(t *testing.T) {
	data := []byte(`[{"RuleID":"aws-key","Description":"AWS Access Key","File":"app\\secrets.go","StartLine":12}]`)
	findings, err := parseGitleaks(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	f := findings[0]
	if f.Severity != SevHigh {
		t.Errorf("severity = %s, want HIGH", f.Severity)
	}
	if f.Location != "app/secrets.go:12" {
		t.Errorf("location = %q (should use forward slashes)", f.Location)
	}
}

func TestParseGitleaksEmpty(t *testing.T) {
	findings, err := parseGitleaks([]byte("  \n"))
	if err != nil || findings != nil {
		t.Errorf("empty gitleaks output should yield no findings, got %v %v", findings, err)
	}
}

// TestParseTrufflehog covers the JSON-lines shape (one object per line, not
// a single array the way gitleaks writes) and the verifyAttempted split: the
// same raw line ("Verified": true/false) means something different depending
// on whether --no-verification was passed.
func TestParseTrufflehog(t *testing.T) {
	data := []byte(`{"DetectorName":"AWS","Verified":true,"Redacted":"AKIA****","SourceMetadata":{"Data":{"Filesystem":{"file":"app\\secrets.go","line":12}}}}
{"DetectorName":"GitHub","Verified":false,"Redacted":"ghp_****","SourceMetadata":{"Data":{"Filesystem":{"file":"config.yaml","line":3}}}}`)

	findings, err := parseTrufflehog(data, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	if findings[0].Severity != SevHigh {
		t.Errorf("severity = %s, want HIGH", findings[0].Severity)
	}
	if findings[0].Location != "app/secrets.go:12" {
		t.Errorf("location = %q (should use forward slashes)", findings[0].Location)
	}
	if findings[0].Verification != VerificationVerified {
		t.Errorf("finding[0].Verification = %q, want %q", findings[0].Verification, VerificationVerified)
	}
	if findings[1].Verification != VerificationUnverified {
		t.Errorf("finding[1].Verification = %q, want %q", findings[1].Verification, VerificationUnverified)
	}
}

// TestParseTrufflehogNotAttempted is the "not guessed" requirement (mirrors
// Reachability's posture): when verification was never attempted
// (--no-verification, the default), a finding must render as
// VerificationUnknown, never VerificationUnverified — trufflehog's own
// "Verified" field is always false in that mode, which is not the same claim
// as "checked and confirmed inactive".
func TestParseTrufflehogNotAttempted(t *testing.T) {
	data := []byte(`{"DetectorName":"AWS","Verified":false,"Redacted":"AKIA****","SourceMetadata":{"Data":{"Filesystem":{"file":"secrets.go","line":1}}}}`)
	findings, err := parseTrufflehog(data, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	if findings[0].Verification != VerificationUnknown {
		t.Errorf("Verification = %q, want %q (unknown, not unverified) when verification wasn't attempted", findings[0].Verification, VerificationUnknown)
	}
}

func TestParseTrufflehogEmpty(t *testing.T) {
	findings, err := parseTrufflehog([]byte("  \n"), false)
	if err != nil || findings != nil {
		t.Errorf("empty trufflehog output should yield no findings, got %v %v", findings, err)
	}
}

// TestTrufflehogResolveRefusesContainerWithVerify is the P13.2.2 requirement:
// verify:true must force host-only, never silently drop verification or run
// it through the network-isolated scanner-container path.
func TestTrufflehogResolveRefusesContainerWithVerify(t *testing.T) {
	withDetectRuntime(t, func(ctx context.Context, priority []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimeDocker, true
	})

	opts := Options{Tools: map[string]ToolPolicy{
		"trufflehog": {Enabled: true, Method: "container", Image: "trufflesecurity/trufflehog@sha256:" + strings.Repeat("a", 64), Verify: true},
	}}
	method, _, _, reason := trufflehogScanner{}.Resolve(context.Background(), opts)
	if method != MethodNone {
		t.Errorf("method = %v, want MethodNone when verify:true forces container away", method)
	}
	if !strings.Contains(reason, "verify") || !strings.Contains(reason, "host") {
		t.Errorf("reason = %q, want it to mention verify + host requirement", reason)
	}
}

// TestTrufflehogResolveAllowsContainerWithoutVerify confirms the refusal
// above is specific to verify:true, not a blanket ban on container mode.
func TestTrufflehogResolveAllowsContainerWithoutVerify(t *testing.T) {
	withDetectRuntime(t, func(ctx context.Context, priority []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimeDocker, true
	})

	opts := Options{Tools: map[string]ToolPolicy{
		"trufflehog": {Enabled: true, Method: "container", Image: "trufflesecurity/trufflehog@sha256:" + strings.Repeat("a", 64)},
	}}
	method, _, _, reason := trufflehogScanner{}.Resolve(context.Background(), opts)
	if method != MethodContainer {
		t.Errorf("method = %v, reason = %q; want MethodContainer when verify is unset", method, reason)
	}
}

// fakeScanner lets us test RunAll without external binaries.
type fakeScanner struct {
	name      string
	available bool
	findings  []Finding
}

func (f fakeScanner) Name() string { return f.name }
func (f fakeScanner) Resolve(context.Context, Options) (Method, sandbox.ContainerRuntime, string, string) {
	if f.available {
		return MethodHost, "", "", ""
	}
	return MethodNone, "", "", "not installed"
}
func (f fakeScanner) Scan(context.Context, string, Method, sandbox.ContainerRuntime, string, Options) ([]Finding, error) {
	return f.findings, nil
}

func TestRunAllAggregatesAndSorts(t *testing.T) {
	scanners := []Scanner{
		fakeScanner{name: "low", available: true, findings: []Finding{{Tool: "a", Severity: SevLow, Title: "l"}}},
		fakeScanner{name: "crit", available: true, findings: []Finding{{Tool: "b", Severity: SevCritical, Title: "c"}}},
		fakeScanner{name: "missing", available: false},
	}
	rep := RunAll(context.Background(), ".", scanners)
	if len(rep.Findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(rep.Findings))
	}
	if rep.Findings[0].Severity != SevCritical {
		t.Errorf("findings not sorted by severity: %+v", rep.Findings)
	}
	if rep.Skipped["missing"] != "not installed" {
		t.Errorf("missing scanner not reported skipped: %v", rep.Skipped)
	}
	if rep.RanVia["low"] != "host" || rep.RanVia["crit"] != "host" {
		t.Errorf("expected host as the ran-via method for available scanners: %v", rep.RanVia)
	}
	out := rep.Format()
	if !strings.Contains(out, "Findings: 2") || !strings.Contains(out, "CRITICAL") {
		t.Errorf("format output unexpected: %q", out)
	}
}

// fakeRelevanceScanner wraps fakeScanner with a RelevanceChecker so
// PlanScanners' relevance-gating branch can be tested without depending on
// hadolint/kubescape's real file-detection heuristics.
type fakeRelevanceScanner struct {
	fakeScanner
	relevant bool
	reason   string
}

func (f fakeRelevanceScanner) Relevant(string) (bool, string) { return f.relevant, f.reason }

// TestPlanScannersSkipsIrrelevantScanner is the "don't run hadolint on a
// repo with no Dockerfile" behavior at the PlanScanners level: an available
// scanner that says it's not relevant is skipped with its own reason,
// without ever reaching Resolve/Scan.
func TestPlanScannersSkipsIrrelevantScanner(t *testing.T) {
	sc := fakeRelevanceScanner{
		fakeScanner: fakeScanner{name: "hadolint-like", available: true},
		relevant:    false,
		reason:      "no Dockerfile found in workspace",
	}
	plan := PlanScanners(context.Background(), ".", []Scanner{sc}, Options{})
	if len(plan) != 1 || !plan[0].Skipped || plan[0].Reason != "no Dockerfile found in workspace" {
		t.Errorf("plan = %+v, want a single skipped entry with the relevance reason", plan)
	}
}

// TestPlanScannersExplicitEnableBypassesRelevanceGate checks the "explicit
// always wins" rule (same one AutoEnableLanguageScanners follows): an
// operator/user who explicitly enabled the tool gets it to run even when
// Relevant says no — an explicit `/scan hadolint` or
// security.tools.hadolint.enabled: true must never be silently overridden.
func TestPlanScannersExplicitEnableBypassesRelevanceGate(t *testing.T) {
	sc := fakeRelevanceScanner{
		fakeScanner: fakeScanner{name: "hadolint-like", available: true},
		relevant:    false,
		reason:      "no Dockerfile found in workspace",
	}
	opts := Options{Tools: map[string]ToolPolicy{
		"hadolint-like": {Enabled: true, EnabledExplicit: true},
	}}
	plan := PlanScanners(context.Background(), ".", []Scanner{sc}, opts)
	if len(plan) != 1 || plan[0].Skipped {
		t.Errorf("plan = %+v, want the explicitly-enabled scanner to run despite Relevant()==false", plan)
	}
}

// fakeImageScanner lets us test ScanImage without external binaries.
type fakeImageScanner struct {
	name     string
	method   Method
	findings []Finding

	// Recorded so a test can prove ScanImage forwards the resolution rather
	// than re-deriving it — the netscanner image reference reaches the scanner
	// only through this hand-off (P55.7).
	sawMethod  Method
	sawRuntime sandbox.ContainerRuntime
	sawImage   string
}

func (f fakeImageScanner) Name() string { return f.name }
func (f fakeImageScanner) Resolve(context.Context, Options) (Method, sandbox.ContainerRuntime, string, string) {
	if f.method == MethodNone {
		return MethodNone, "", "", "not installed"
	}
	return f.method, "", "", ""
}
func (f fakeImageScanner) ScanImage(_ context.Context, _ string, method Method, rt sandbox.ContainerRuntime, scannerImage string) ([]Finding, error) {
	f.sawMethod, f.sawRuntime, f.sawImage = method, rt, scannerImage
	return f.findings, nil
}

func TestScanImageAggregatesAndSorts(t *testing.T) {
	scanners := []ImageScanner{
		fakeImageScanner{name: "low", method: MethodHost, findings: []Finding{{Tool: "a", Severity: SevLow, Title: "l"}}},
		fakeImageScanner{name: "crit", method: MethodHost, findings: []Finding{{Tool: "b", Severity: SevCritical, Title: "c"}}},
		fakeImageScanner{name: "missing", method: MethodNone},
	}
	rep := ScanImage(context.Background(), "alpine:3.20", scanners, Options{})
	if len(rep.Findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(rep.Findings))
	}
	if rep.Findings[0].Severity != SevCritical {
		t.Errorf("findings not sorted by severity: %+v", rep.Findings)
	}
	if rep.Skipped["missing"] != "not installed" {
		t.Errorf("missing scanner not reported skipped: %v", rep.Skipped)
	}
}

// TestScanImageRunsContainerResolvedScanners is the P55.7 inversion of what
// this test used to assert. A container-resolved image scanner used to be
// skipped outright, because the only container runner Aegis had denied network
// and so could not reach a registry. The netscanner image supplies exactly that
// missing property, so the run now goes ahead — and the resolution has to reach
// the scanner intact, since the image it runs *out of* is carried nowhere else.
func TestScanImageRunsContainerResolvedScanners(t *testing.T) {
	scanners := []ImageScanner{
		fakeImageScanner{name: "container-only", method: MethodContainer, findings: []Finding{{Tool: "x", Severity: SevHigh}}},
	}
	rep := ScanImage(context.Background(), "alpine:3.20", scanners, Options{})
	if len(rep.Findings) != 1 {
		t.Fatalf("expected the container-resolved scanner to run, got %+v / skipped %v", rep.Findings, rep.Skipped)
	}
	if rep.RanVia["container-only"] != string(MethodContainer) {
		t.Errorf("RanVia = %v, want container", rep.RanVia)
	}
}

// TestScanImageForwardsTheResolvedImage: the netscanner reference comes from
// Resolve and is used by nothing else, so dropping it here would send every
// container image scan to an empty image reference.
func TestScanImageForwardsTheResolvedImage(t *testing.T) {
	var got fakeImageScanner
	sc := recordingImageScanner{inner: fakeImageScanner{name: "trivy", method: MethodContainer,
		findings: []Finding{{Tool: "trivy", Severity: SevHigh}}}, out: &got}
	rep := ScanImage(context.Background(), "alpine:3.20", []ImageScanner{sc}, Options{})
	if len(rep.Findings) != 1 {
		t.Fatalf("scanner did not run: %+v", rep.Skipped)
	}
	if got.sawMethod != MethodContainer || got.sawImage != "localhost/aegis-netscanner:v1" {
		t.Errorf("forwarded method/image = %q/%q, want container/localhost/aegis-netscanner:v1", got.sawMethod, got.sawImage)
	}
}

// recordingImageScanner resolves to a fixed netscanner reference and captures
// what ScanImage was handed.
type recordingImageScanner struct {
	inner fakeImageScanner
	out   *fakeImageScanner
}

func (r recordingImageScanner) Name() string { return r.inner.name }
func (r recordingImageScanner) Resolve(context.Context, Options) (Method, sandbox.ContainerRuntime, string, string) {
	return MethodContainer, sandbox.RuntimePodman, "localhost/aegis-netscanner:v1", ""
}
func (r recordingImageScanner) ScanImage(_ context.Context, _ string, method Method, rt sandbox.ContainerRuntime, scannerImage string) ([]Finding, error) {
	r.out.sawMethod, r.out.sawRuntime, r.out.sawImage = method, rt, scannerImage
	return r.inner.findings, nil
}

// TestRunWithOptionsDedupsAcrossScanners is the P11.8 regression: two
// scanners independently flagging the same CVE at the same location must
// collapse to one finding in the aggregated report, not two.
func TestRunWithOptionsDedupsAcrossScanners(t *testing.T) {
	scanners := []Scanner{
		fakeScanner{name: "osv-scanner", available: true, findings: []Finding{
			{Tool: "osv-scanner", RuleID: "CVE-2024-5555", Severity: SevHigh, Location: "pkg@1.0.0 (go.sum)"},
		}},
		fakeScanner{name: "trivy", available: true, findings: []Finding{
			{Tool: "trivy", RuleID: "CVE-2024-5555", Severity: SevCritical, Location: "go.sum"},
		}},
	}
	rep := RunWithOptions(context.Background(), t.TempDir(), scanners, Options{})
	if len(rep.Findings) != 1 {
		t.Fatalf("expected the duplicate CVE finding deduped to 1, got %d: %+v", len(rep.Findings), rep.Findings)
	}
	if rep.Findings[0].Severity != SevCritical {
		t.Errorf("expected the higher-severity copy kept, got %+v", rep.Findings[0])
	}
	if len(rep.Findings[0].SeenBy) != 1 || rep.Findings[0].SeenBy[0] != "osv-scanner" {
		t.Errorf("expected SeenBy to record osv-scanner, got %v", rep.Findings[0].SeenBy)
	}
}

// TestRunWithOptionsAppliesBaselineSuppression is the P11.8 regression for
// the suppression allowlist: a finding matched by an active baseline entry
// must move to Suppressed, not disappear or stay in Findings.
func TestRunWithOptionsAppliesBaselineSuppression(t *testing.T) {
	dir := t.TempDir()
	writeBaseline(t, dir, `
suppressions:
  - rule_id: "CVE-2024-7777"
    reason: "accepted risk, tracked in JIRA-123"
    expires: "2099-01-01"
`)
	scanners := []Scanner{
		fakeScanner{name: "trivy", available: true, findings: []Finding{
			{Tool: "trivy", RuleID: "CVE-2024-7777", Severity: SevHigh, Location: "go.sum"},
			{Tool: "trivy", RuleID: "CVE-2024-8888", Severity: SevMedium, Location: "go.sum"},
		}},
	}
	rep := RunWithOptions(context.Background(), dir, scanners, Options{})
	if len(rep.Findings) != 1 || rep.Findings[0].RuleID != "CVE-2024-8888" {
		t.Fatalf("expected only the unsuppressed finding in Findings, got %+v", rep.Findings)
	}
	if len(rep.Suppressed) != 1 || rep.Suppressed[0].RuleID != "CVE-2024-7777" {
		t.Fatalf("expected the accepted-risk finding in Suppressed, got %+v", rep.Suppressed)
	}
	out := rep.Format()
	if !strings.Contains(out, "Suppressed by baseline: 1") {
		t.Errorf("Format should surface the suppression count, got %q", out)
	}
}

func TestRunWithOptionsBaselineParseErrorFailsSafe(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".aegis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BaselinePath(dir), []byte("not: [valid: yaml:"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanners := []Scanner{
		fakeScanner{name: "trivy", available: true, findings: []Finding{
			{Tool: "trivy", RuleID: "CVE-2024-9999", Severity: SevHigh, Location: "go.sum"},
		}},
	}
	rep := RunWithOptions(context.Background(), dir, scanners, Options{})
	if len(rep.Findings) != 1 {
		t.Fatalf("a broken baseline must never suppress findings, got %+v", rep.Findings)
	}
	if rep.BaselineError == "" {
		t.Error("expected BaselineError to be set for malformed YAML")
	}
}

// TestRunWithProgressRecordsHostFallbackAdvisory is the P55.4 report half: a
// scan that ran every tool on the host because the container was unavailable
// must say so in the Report, not just resolve that way silently. The whole
// point of P55.4 was that findings can differ between machines, and a report
// is the artifact that outlives the terminal it was printed in.
func TestRunWithProgressRecordsHostFallbackAdvisory(t *testing.T) {
	const toolA, toolB = "test-p554-report-a", "test-p554-report-b"
	for _, name := range []string{toolA, toolB} {
		withTestDescriptor(t, ScannerDescriptor{Name: name, Binary: "go", DefaultEnabled: true})
	}
	// No runtime, so both covered tools fall back to their (present) host
	// binary — the exact shape of a stopped podman machine.
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return "", false
	})
	opts := Options{Multiscanner: msPolicy(testImageID, toolA, toolB)}
	scanners := []Scanner{
		fakeScanner{name: toolA, available: true},
		fakeScanner{name: toolB, available: true},
	}

	rep := RunWithOptions(context.Background(), t.TempDir(), scanners, opts)
	if len(rep.Advisories) != 1 {
		t.Fatalf("advisories = %v, want exactly one collapsed advisory", rep.Advisories)
	}
	for _, want := range []string{toolA, toolB, "unpinned", "isn't available now"} {
		if !strings.Contains(rep.Advisories[0], want) {
			t.Errorf("advisory = %q, want it to mention %q", rep.Advisories[0], want)
		}
	}
	if !strings.Contains(rep.Format(), "Note: ") {
		t.Errorf("Format() does not surface the advisory:\n%s", rep.Format())
	}
	// The tools still ran and still reported: an advisory is not a skip.
	if len(rep.Ran) != 2 || len(rep.Skipped) != 0 {
		t.Errorf("ran = %v, skipped = %v, want both tools run and nothing skipped", rep.Ran, rep.Skipped)
	}
}

// TestRunWithProgressNoAdvisoryWithoutMultiscanner is the not-nagging half:
// an operator with no multiscanner image configured never asked for the
// container path, so a host run is the plan rather than a fallback and the
// report must stay exactly as it was before P55.4.
func TestRunWithProgressNoAdvisoryWithoutMultiscanner(t *testing.T) {
	const tool = "test-p554-report-plain"
	withTestDescriptor(t, ScannerDescriptor{Name: tool, Binary: "go", DefaultEnabled: true})
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return "", false
	})

	rep := RunWithOptions(context.Background(), t.TempDir(), []Scanner{fakeScanner{name: tool, available: true}}, Options{})
	if len(rep.Advisories) != 0 {
		t.Fatalf("advisories = %v, want none (no multiscanner configured)", rep.Advisories)
	}
	if strings.Contains(rep.Format(), "Note: ") {
		t.Errorf("Format() printed an advisory it should not have:\n%s", rep.Format())
	}
}
