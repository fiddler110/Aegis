package security

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// withCanaryVersion seams the containerized version probe.
func withCanaryVersion(t *testing.T, fn func(ctx context.Context, rt sandbox.ContainerRuntime, image, dir, binary string, args ...string) (string, error)) {
	t.Helper()
	orig := runCanaryVersion
	runCanaryVersion = fn
	t.Cleanup(func() { runCanaryVersion = orig })
}

// withCanaryRunner seams the canary scan itself, so the reporting layer is
// testable without a container runtime.
func withCanaryRunner(t *testing.T, counts map[string]int, failures map[string]error) {
	t.Helper()
	orig := canaryRunnerFor
	canaryRunnerFor = func(name string) (canaryFindingCounter, bool) {
		if _, ok := canaryExpectations[name]; !ok {
			return nil, false
		}
		return func(context.Context, string, sandbox.ContainerRuntime, string, Options) (int, error) {
			if err, bad := failures[name]; bad {
				return 0, err
			}
			return counts[name], nil
		}, true
	}
	t.Cleanup(func() { canaryRunnerFor = orig })
}

// verifyPolicy is an enabled, ID-matching policy carrying the named tools.
func verifyPolicy(tools ...string) MultiscannerPolicy {
	p := msPolicy(testImageID, tools...)
	p.Runtime = sandbox.RuntimeDocker
	p.cacheCheck = &multiscannerCheck{}
	return p
}

// stubVerifyEnvironment makes VerifyMultiscanner's preflight (runtime probe,
// image-ID verification, cache probe) succeed without a container runtime.
func stubVerifyEnvironment(t *testing.T, cachePopulated bool) {
	t.Helper()
	withDetectRuntime(t, func(context.Context, []sandbox.ContainerRuntime) (sandbox.ContainerRuntime, bool) {
		return sandbox.RuntimeDocker, true
	})
	withInspectImageID(t, func(context.Context, sandbox.ContainerRuntime, string) (string, error) {
		return testImageID, nil
	})
	withCacheFileExists(t, cachePopulated)
	withCanaryVersion(t, func(_ context.Context, _ sandbox.ContainerRuntime, _, _, binary string, _ ...string) (string, error) {
		return binary + " 1.2.3", nil
	})
}

func resultFor(t *testing.T, results []VerifyResult, tool string) VerifyResult {
	t.Helper()
	for _, r := range results {
		if r.Tool == tool {
			return r
		}
	}
	t.Fatalf("no result for %q in %+v", tool, results)
	return VerifyResult{}
}

// TestCanaryExpectationsCoverEveryBundledTool is the guard that keeps this
// command honest as the image grows. A scanner added to the Containerfile and
// to MultiscannerTools but not here would be routed to the container by
// Resolve and never verified — which is exactly the gap P55.3 exists to close,
// reintroduced one tool at a time.
func TestCanaryExpectationsCoverEveryBundledTool(t *testing.T) {
	for _, profile := range MultiscannerProfiles() {
		for _, tool := range MultiscannerTools(profile) {
			exp, ok := canaryExpectations[tool]
			if !ok {
				t.Errorf("%s profile carries %q but canaryExpectations has no entry for it", profile, tool)
				continue
			}
			if len(exp.versionArgs) == 0 {
				t.Errorf("%q has no versionArgs — every tool gets at least a version probe", tool)
			}
			if exp.canarySkip == "" && exp.minFindings < 1 {
				t.Errorf("%q expects %d findings; the whole point is asserting a NON-ZERO count", tool, exp.minFindings)
			}
			if exp.canarySkip != "" && exp.minFindings != 0 {
				t.Errorf("%q both skips its canary and declares minFindings=%d", tool, exp.minFindings)
			}
		}
	}
}

// TestCanarySkipsStateAReason checks the P11.1 posture holds here too: a tool
// that can't be canaried says so in words, rather than quietly counting as
// verified.
func TestCanarySkipsStateAReason(t *testing.T) {
	for tool, exp := range canaryExpectations {
		if exp.canarySkip == "" {
			continue
		}
		if len(exp.canarySkip) < 20 || !strings.Contains(exp.canarySkip, tool) {
			t.Errorf("%q skip reason is not a usable explanation: %q", tool, exp.canarySkip)
		}
	}
}

// TestCanaryExpectationsHaveRunners checks every non-skipped tool can actually
// be driven — an expectation with no runner would report as a failure against
// a perfectly good image.
func TestCanaryExpectationsHaveRunners(t *testing.T) {
	for tool, exp := range canaryExpectations {
		if exp.canarySkip != "" {
			continue
		}
		if _, ok := canaryRunner(tool); !ok {
			t.Errorf("%q has a canary expectation but canaryRunner has no entry for it", tool)
		}
	}
}

// TestCanaryFixtureFeedsEveryToolShape checks the embedded fixture actually
// carries the file each family of tools needs. A fixture that lost its
// Dockerfile would fail hadolint's canary and read as a broken image.
func TestCanaryFixtureFeedsEveryToolShape(t *testing.T) {
	dir := t.TempDir()
	if err := MaterializeCanaryFixture(dir); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	for _, rel := range []string{
		"Dockerfile",            // hadolint, trivy misconfig
		"k8s-deployment.yaml",   // kubescape
		"package-lock.json",     // trivy/grype/osv-scanner/syft
		"requirements.txt",      // ditto, second ecosystem
		"credentials.env",       // gitleaks, trufflehog
		"app.py",                // bandit, opengrep
		"server.js",             // njsscan, opengrep
		"config/environment.rb", // brakeman's own project check
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("canary fixture is missing %s: %v", rel, err)
		}
	}

	// The two scanners with a RelevanceChecker would skip themselves on a
	// fixture that didn't satisfy it, so assert against their real checks
	// rather than against a filename we assume they look for.
	if ok, reason := (hadolintScanner{}).Relevant(dir); !ok {
		t.Errorf("hadolint would skip the canary fixture: %s", reason)
	}
	if ok, reason := (kubescapeScanner{}).Relevant(dir); !ok {
		t.Errorf("kubescape would skip the canary fixture: %s", reason)
	}
	if !isRailsApp(dir) {
		t.Error("brakeman refuses anything that isn't a Rails app; the canary fixture no longer looks like one")
	}
}

// TestCanaryFixturePlantsOnlyPublishedExampleSecrets guards the one rule the
// fixture must never break: it plants credentials on purpose, so every planted
// value has to be visibly synthetic. A real-looking key here would be a
// secret committed to this repo.
func TestCanaryFixturePlantsOnlyPublishedExampleSecrets(t *testing.T) {
	data, err := canaryFS.ReadFile("canary/credentials.env")
	if err != nil {
		t.Fatalf("read fixture credentials: %v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(value)
		if !strings.Contains(upper, "EXAMPLE") && !strings.Contains(upper, "NOTAREAL") && !strings.Contains(upper, "A1B2C3") {
			t.Errorf("planted credential %q is not visibly synthetic — every value here must read as fake at a glance", line)
		}
	}
}

// TestVerifyReportsZeroFindingsAsFailure is the regression test for the whole
// item. A tool that runs, exits clean, and reports nothing against a tree full
// of planted vulnerabilities has not found the code safe — it never looked.
func TestVerifyReportsZeroFindingsAsFailure(t *testing.T) {
	stubVerifyEnvironment(t, true)
	withCanaryRunner(t, map[string]int{"hadolint": 4, "opengrep": 0}, nil)

	results, err := VerifyMultiscanner(context.Background(), verifyPolicy("hadolint", "opengrep"), nil, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got := resultFor(t, results, "hadolint"); got.Status != VerifyPass {
		t.Errorf("hadolint: want pass, got %s (%s)", got.Status, got.Detail)
	}
	bad := resultFor(t, results, "opengrep")
	if bad.Status != VerifyFail {
		t.Fatalf("opengrep reported zero findings and was not failed: %+v", bad)
	}
	for _, want := range []string{"expected at least 1", "got 0", "opengrep"} {
		if !strings.Contains(bad.Detail+bad.Tool, want) {
			t.Errorf("failure detail should name %q, got %q", want, bad.Detail)
		}
	}
	if passed, failed, _, _ := VerifyCounts(results); passed != 1 || failed != 1 {
		t.Errorf("counts: want 1 passed / 1 failed, got %d / %d", passed, failed)
	}
}

// TestVerifyReportsMissingToolFromVersionProbe covers the cheaper half: grype
// was on the tool list and simply absent from the image.
func TestVerifyReportsMissingToolFromVersionProbe(t *testing.T) {
	stubVerifyEnvironment(t, true)
	withCanaryVersion(t, func(_ context.Context, _ sandbox.ContainerRuntime, _, _, binary string, _ ...string) (string, error) {
		if binary == "grype" {
			return "", errors.New("exec: \"grype\": executable file not found in $PATH")
		}
		return binary + " 1.2.3", nil
	})
	withCanaryRunner(t, map[string]int{"trivy": 3}, nil)

	results, err := VerifyMultiscanner(context.Background(), verifyPolicy("grype", "trivy"), nil, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	missing := resultFor(t, results, "grype")
	if missing.Status != VerifyFail {
		t.Fatalf("absent tool not failed: %+v", missing)
	}
	if !strings.Contains(missing.Detail, "build-image") {
		t.Errorf("failure detail should point at the fix, got %q", missing.Detail)
	}
}

// TestVerifyReportsEmptyCacheAsBlockedNotFailed keeps an operator who simply
// hasn't run update-db from being told their image is broken — the tool is
// fine, its database isn't there yet.
func TestVerifyReportsEmptyCacheAsBlockedNotFailed(t *testing.T) {
	stubVerifyEnvironment(t, false)
	withCanaryRunner(t, map[string]int{"hadolint": 2}, nil)

	results, err := VerifyMultiscanner(context.Background(), verifyPolicy("trivy", "hadolint"), nil, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	blockedRow := resultFor(t, results, "trivy")
	if blockedRow.Status != VerifyBlocked {
		t.Fatalf("trivy on an empty cache: want blocked, got %s (%s)", blockedRow.Status, blockedRow.Detail)
	}
	if !strings.Contains(blockedRow.Detail, "aegis security update-db") {
		t.Errorf("blocked detail must name the fix, got %q", blockedRow.Detail)
	}
	// hadolint needs no database, so an empty cache must not touch it.
	if got := resultFor(t, results, "hadolint"); got.Status != VerifyPass {
		t.Errorf("hadolint: want pass on an empty cache, got %s", got.Status)
	}
	passed, failed, _, blocked := VerifyCounts(results)
	if passed != 1 || failed != 0 || blocked != 1 {
		t.Errorf("counts: want 1/0/1, got %d/%d/%d", passed, failed, blocked)
	}
}

// TestVerifySkipsExcludedAndUnknownTools covers the two config-shaped cases: a
// tool the image deliberately never carries (skip, with the real reason), and
// a tool nothing knows how to verify (fail, so it can't pass silently).
func TestVerifySkipsExcludedAndUnknownTools(t *testing.T) {
	stubVerifyEnvironment(t, true)
	withCanaryRunner(t, nil, nil)

	results, err := VerifyMultiscanner(context.Background(), verifyPolicy("gosec", "definitely-not-a-scanner"), nil, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	excluded := resultFor(t, results, "gosec")
	if excluded.Status != VerifySkip {
		t.Errorf("gosec: want skip, got %s", excluded.Status)
	}
	if !strings.Contains(excluded.Detail, "Go toolchain") {
		t.Errorf("gosec skip should carry the real exclusion reason, got %q", excluded.Detail)
	}
	unknown := resultFor(t, results, "definitely-not-a-scanner")
	if unknown.Status != VerifyFail {
		t.Errorf("unknown tool: want fail (never a silent pass), got %s", unknown.Status)
	}
}

// TestVerifySkippedToolsStillGetAVersionProbe: nmap/nuclei have no possible
// filesystem canary, but "absent from the image" is still detectable for them.
func TestVerifySkippedToolsStillGetAVersionProbe(t *testing.T) {
	stubVerifyEnvironment(t, true)
	withCanaryRunner(t, nil, nil)

	results, err := VerifyMultiscanner(context.Background(), verifyPolicy("nmap"), nil, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	got := resultFor(t, results, "nmap")
	if got.Status != VerifySkip {
		t.Fatalf("nmap: want skip, got %s", got.Status)
	}
	if got.Version == "" {
		t.Error("a skipped tool must still be version-probed — that's what catches an absent binary")
	}
	if _, _, skipped, _ := VerifyCounts(results); skipped != 1 {
		t.Error("a skipped tool must not be counted as verified")
	}
}

// TestVerifyToolFilterRejectsUnknownName: a typo must not read as "everything
// checked out fine".
func TestVerifyToolFilterRejectsUnknownName(t *testing.T) {
	stubVerifyEnvironment(t, true)
	withCanaryRunner(t, nil, nil)

	if _, err := VerifyMultiscanner(context.Background(), verifyPolicy("trivy"), []string{"trivvy"}, nil); err == nil {
		t.Fatal("unknown --tool name was accepted")
	}
}

// TestVerifyRequiresAConfiguredImage keeps the preflight errors distinct from
// per-tool results: there is nothing to verify, which is not the same as
// fourteen broken scanners.
func TestVerifyRequiresAConfiguredImage(t *testing.T) {
	_, err := VerifyMultiscanner(context.Background(), MultiscannerPolicy{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "build-image") {
		t.Fatalf("want a build-image pointer, got %v", err)
	}
}

// TestVerifyProgressStreamsEveryRow: each canary is a container run, so a
// caller has to be able to print rows as they land rather than after minutes
// of silence.
func TestVerifyProgressStreamsEveryRow(t *testing.T) {
	stubVerifyEnvironment(t, true)
	withCanaryRunner(t, map[string]int{"hadolint": 1, "opengrep": 1}, nil)

	var streamed []string
	results, err := VerifyMultiscanner(context.Background(), verifyPolicy("hadolint", "opengrep"), nil, func(r VerifyResult) {
		streamed = append(streamed, r.Tool)
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(streamed) != len(results) {
		t.Errorf("streamed %d rows for %d results", len(streamed), len(results))
	}
}

// TestVerifyReportsScanErrorsAsFailures: kubescape's fatal was an error, not a
// zero count, and has to land in the table rather than aborting the run.
func TestVerifyReportsScanErrorsAsFailures(t *testing.T) {
	stubVerifyEnvironment(t, true)
	withCanaryRunner(t, map[string]int{"hadolint": 1}, map[string]error{
		"kubescape": errors.New("open /root/.kubescape/allcontrols.json: no such file or directory"),
	})

	results, err := VerifyMultiscanner(context.Background(), verifyPolicy("kubescape", "hadolint"), nil, nil)
	if err != nil {
		t.Fatalf("a failing tool must be data, not an aborted run: %v", err)
	}
	broken := resultFor(t, results, "kubescape")
	if broken.Status != VerifyFail || !strings.Contains(broken.Detail, "allcontrols.json") {
		t.Fatalf("kubescape fatal not reported usefully: %+v", broken)
	}
	if got := resultFor(t, results, "hadolint"); got.Status != VerifyPass {
		t.Errorf("one broken scanner hid the state of another: %+v", got)
	}
}

// TestFirstVersionLinePicksTheVersion covers the banners that are not one
// clean line: ANSI colour (nuclei/njsscan) and an "Application:" preamble
// (syft/grype).
func TestFirstVersionLinePicksTheVersion(t *testing.T) {
	cases := map[string]string{
		"Version: 0.72.0":                                   "Version: 0.72.0",
		"Application:   syft\nVersion:       1.48.0\nBuild": "Version: 1.48.0",
		"\x1b[34m\x1b[0m\n njsscan: v0.4.3 | Ajin Abraham":  "njsscan: v0.4.3 | Ajin Abraham",
		"[\x1b[34mINF\x1b[0m] Nuclei Engine Version: v3.11.0": "[INF] Nuclei Engine Version: v3.11.0",
	}
	for in, want := range cases {
		if got := firstVersionLine(in); got != want {
			t.Errorf("firstVersionLine(%q) = %q, want %q", in, got, want)
		}
	}
	if got := firstVersionLine(strings.Repeat("x", 80)); len([]rune(got)) > 40 {
		t.Errorf("long banner not truncated: %q", got)
	}
}
