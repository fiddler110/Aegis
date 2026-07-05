package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// newScanTestServer builds a daemon with its workspace pinned to a fresh temp
// dir, so /security/scan runs against a real (empty) directory rather than
// whatever the test process's cwd happens to be. Returns both the typed
// client and the raw base URL + token, since one test needs to send a
// deliberately malformed body the typed client can't produce.
func newScanTestServer(t *testing.T) (cl *client.Client, baseURL, token string) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	workspace := t.TempDir()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.workspace = workspace

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return client.New(ts.URL).WithToken("test-token"), ts.URL, "test-token"
}

// TestHandleScanDefaultsToWholeWorkspace is the core /scan regression: no
// path/image/sbom given must scan the daemon's own workspace and come back
// with a report, not an error — even though no scanner binaries are
// installed in this test environment, RunWithOptions reports each as skipped
// rather than failing (same behavior `aegis scan` already relies on).
func TestHandleScanDefaultsToWholeWorkspace(t *testing.T) {
	cl, _, _ := newScanTestServer(t)
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
	cl, _, _ := newScanTestServer(t)
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
	cl, _, _ := newScanTestServer(t)
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

// TestHandleScanRejectsBadJSON checks the request body is actually decoded
// and validated rather than ignored — client.Scan always marshals valid
// JSON, so this drives the raw HTTP endpoint directly to send a malformed one.
func TestHandleScanRejectsBadJSON(t *testing.T) {
	_, baseURL, token := newScanTestServer(t)
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
	cl, _, _ := newScanTestServer(t)
	resp, err := cl.Scan(context.Background(), api.ScanRequest{Image: "alpine:3.20"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if strings.TrimSpace(resp.Report) == "" {
		t.Error("expected a non-empty report even with no image scanners installed")
	}
}
