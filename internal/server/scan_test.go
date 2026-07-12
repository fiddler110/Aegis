package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// newScanTestServer builds a daemon with its workspace pinned to a fresh temp
// dir, so /security/scan runs against a real (empty) directory rather than
// whatever the test process's cwd happens to be. Returns both the typed
// client and the raw base URL + token, since one test needs to send a
// deliberately malformed body the typed client can't produce.
//
// PATH is cleared for the duration of the test so exec.LookPath can't find
// any scanner binaries: every test in this file assumes scanners resolve to
// "skipped", and on a dev box that actually has trivy/grype/semgrep/etc.
// installed a real scan would run instead and blow the client's 30s timeout
// (same hermeticity trick as withEmptyPath in internal/security).
func newScanTestServer(t *testing.T) (cl *client.Client, baseURL, token, workspace string) {
	t.Helper()
	t.Setenv("PATH", "")
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	workspace = t.TempDir()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.workspace = workspace

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return client.New(ts.URL).WithToken("test-token"), ts.URL, "test-token", workspace
}

// TestHandleScanDefaultsToWholeWorkspace is the core /scan regression: no
// path/image/sbom given must scan the daemon's own workspace and come back
// with a report, not an error — even though no scanner binaries are
// installed in this test environment, RunWithOptions reports each as skipped
// rather than failing (same behavior `aegis scan` already relies on).
func TestHandleScanDefaultsToWholeWorkspace(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	resp, err := cl.Scan(context.Background(), api.ScanRequest{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if strings.TrimSpace(resp.Report) == "" {
		t.Error("expected a non-empty report")
	}
}

// TestHandleScanRejectsPathEscapingWorkspace is the security regression: a
// path that escapes the workspace root must be rejected (400), not scanned.
func TestHandleScanRejectsPathEscapingWorkspace(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	_, err := cl.Scan(context.Background(), api.ScanRequest{Path: "../../etc"})
	if err == nil {
		t.Fatal("expected an error for a path escaping the workspace")
	}
}

// TestHandleScanSBOMGeneratesArtifact exercises the sbom branch's dispatch —
// syft may not be installed in this environment, so this only asserts the
// request is routed to the SBOM path (a distinct error/report shape from a
// findings scan) rather than asserting a real SBOM was written.
func TestHandleScanSBOMGeneratesArtifact(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	resp, err := cl.Scan(context.Background(), api.ScanRequest{SBOM: true})
	if err != nil {
		// syft not installed: GenerateSBOM surfaces that as an error rather
		// than a silent empty report — acceptable in this environment, but
		// it must be a real error, not a panic or a hang.
		if !strings.Contains(err.Error(), "syft") && !strings.Contains(strings.ToLower(err.Error()), "sbom") {
			t.Fatalf("unexpected error shape: %v", err)
		}
		return
	}
	if !strings.Contains(resp.Report, "SBOM") {
		t.Errorf("report = %q, want it to mention the SBOM artifact", resp.Report)
	}
}

// TestHandleScanReturnsStructuredFields is the P15.6 regression: alongside
// the formatted text, a findings scan's response must mirror the
// security.Report structure (findings/ran/skipped) so the web UI's Security
// panel can render a findings table and a "K scanners skipped" summary
// without parsing the text. Restricting the run to trufflehog (opt-in, not
// enabled by default, so it resolves to skipped with no exec call) makes the
// expected structured shape deterministic regardless of which scanner
// binaries the test machine happens to have.
func TestHandleScanReturnsStructuredFields(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	resp, err := cl.Scan(context.Background(), api.ScanRequest{Scanners: []string{"trufflehog"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if strings.TrimSpace(resp.Report) == "" {
		t.Error("expected the formatted report text to still be present")
	}
	reason, ok := resp.Skipped["trufflehog"]
	if !ok || strings.TrimSpace(reason) == "" {
		t.Errorf("skipped = %v, want a trufflehog entry with a non-empty reason", resp.Skipped)
	}
	if len(resp.Skipped) != 1 {
		t.Errorf("skipped = %v, want only the selected scanner mentioned", resp.Skipped)
	}
	if len(resp.Ran) != 0 || len(resp.Findings) != 0 {
		t.Errorf("ran = %v, findings = %v, want none (trufflehog is skipped in this environment)",
			resp.Ran, resp.Findings)
	}
}

// TestHandleScanRejectsBadJSON checks the request body is actually decoded
// and validated rather than ignored — client.Scan always marshals valid
// JSON, so this drives the raw HTTP endpoint directly to send a malformed one.
func TestHandleScanRejectsBadJSON(t *testing.T) {
	_, baseURL, token, _ := newScanTestServer(t)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/security/scan", strings.NewReader("{not valid json"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestHandleScanImageRoutesToImageScan checks the image branch is taken
// instead of a directory scan when Image is set.
func TestHandleScanImageRoutesToImageScan(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	resp, err := cl.Scan(context.Background(), api.ScanRequest{Image: "alpine:3.20"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if strings.TrimSpace(resp.Report) == "" {
		t.Error("expected a non-empty report even with no image scanners installed")
	}
}

// TestHandleScanNetworkRejectsDisallowedTarget is the P13.5/`/scan network`
// regression: a target outside the default loopback/private allowance (and
// not declared in security.dast.allowed_targets, which this test server
// leaves empty) must be rejected before either scanner runs, same shared
// gate dast_scan uses — not silently scanned or 500'd.
func TestHandleScanNetworkRejectsDisallowedTarget(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	_, err := cl.Scan(context.Background(), api.ScanRequest{Targets: []string{"8.8.8.8"}})
	if err == nil {
		t.Fatal("expected an error for a disallowed public network target")
	}
}

// TestHandleScanNetworkRoutesToRecon checks the Targets branch is taken
// instead of a directory scan, and an allowed (loopback) target passes the
// authorization gate even with no nmap/nuclei binaries installed.
func TestHandleScanNetworkRoutesToRecon(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	resp, err := cl.Scan(context.Background(), api.ScanRequest{Targets: []string{"127.0.0.1"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if strings.TrimSpace(resp.Report) == "" {
		t.Error("expected a non-empty report even with no recon scanners installed")
	}
}

// TestHandleScanScannersFieldRestrictsSelection is the scanner-selection
// regression: Scanners: ["trufflehog"] must run only trufflehog (reported
// skipped in this environment, since it isn't installed) and never mention
// scanners outside the selection — trufflehog resolves to MethodNone
// immediately with no exec call, so this stays fast regardless of what other
// scanner binaries happen to be installed on the machine running the test.
func TestHandleScanScannersFieldRestrictsSelection(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	resp, err := cl.Scan(context.Background(), api.ScanRequest{Scanners: []string{"trufflehog"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !strings.Contains(resp.Report, "trufflehog") {
		t.Errorf("report = %q, want it to mention trufflehog", resp.Report)
	}
	for _, other := range []string{"opengrep", "kubescape", "hadolint", "osv-scanner"} {
		if strings.Contains(resp.Report, other) {
			t.Errorf("report = %q, should not mention %q outside the selection", resp.Report, other)
		}
	}
}

// TestHandleScanScannersFieldCategoryAlias checks a category token
// ("secrets") expands to its group (gitleaks + trufflehog) rather than being
// treated as a literal (nonexistent) scanner name.
func TestHandleScanScannersFieldCategoryAlias(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	resp, err := cl.Scan(context.Background(), api.ScanRequest{Scanners: []string{"secrets"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !strings.Contains(resp.Report, "trufflehog") {
		t.Errorf("report = %q, want the \"secrets\" category to include trufflehog", resp.Report)
	}
}

// TestHandleScanUnknownScannerSelectorIsBadRequest checks an unrecognized
// selector token is rejected (400) rather than silently running every
// scanner or panicking.
func TestHandleScanUnknownScannerSelectorIsBadRequest(t *testing.T) {
	cl, _, _, _ := newScanTestServer(t)
	_, err := cl.Scan(context.Background(), api.ScanRequest{Scanners: []string{"not-a-real-scanner"}})
	if err == nil {
		t.Fatal("expected an error for an unrecognized scanner selector")
	}
}

// TestHandleScanPersistsReportArtifact is the "all security scan results go
// under .aegis/security/" regression: a findings scan must leave scan.json
// behind under the scanned directory, matching the report the caller got
// back over the wire.
func TestHandleScanPersistsReportArtifact(t *testing.T) {
	cl, _, _, workspace := newScanTestServer(t)
	if _, err := cl.Scan(context.Background(), api.ScanRequest{Scanners: []string{"trufflehog"}}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	path := security.ReportArtifactPath(workspace, "scan")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected report artifact at %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Error("report artifact is empty")
	}
}
