package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
)

// scanResponseFromReport converts a security.Report into the wire response:
// the formatted text (unchanged pre-P15.6 contract) plus the same outcome
// mirrored into api's structured fields, so a client can render a findings
// table without parsing the text.
func scanResponseFromReport(report security.Report) api.ScanResponse {
	return api.ScanResponse{
		Report:              report.Format(),
		Findings:            apiScanFindings(report.Findings),
		Suppressed:          apiScanFindings(report.Suppressed),
		Ran:                 report.Ran,
		RanVia:              report.RanVia,
		Skipped:             report.Skipped,
		ExpiredSuppressions: report.ExpiredSuppressions,
		InvalidSuppressions: report.InvalidSuppressions,
		BaselineError:       report.BaselineError,
	}
}

func apiScanFindings(fs []security.Finding) []api.ScanFinding {
	if len(fs) == 0 {
		return nil
	}
	out := make([]api.ScanFinding, 0, len(fs))
	for _, f := range fs {
		out = append(out, api.ScanFinding{
			Tool:         f.Tool,
			RuleID:       f.RuleID,
			Severity:     string(f.Severity),
			Title:        f.Title,
			Location:     f.Location,
			Description:  f.Description,
			Remediation:  f.Remediation,
			Reachability: string(f.Reachability),
			Verification: string(f.Verification),
			CWE:          f.CWE,
			ASVS:         f.ASVS,
			SeenBy:       f.SeenBy,
		})
	}
	return out
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.ScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	opts := security.OptionsFromConfig(s.cfg.Security)

	if req.Image != "" {
		report := security.ScanImage(r.Context(), req.Image, security.DefaultImageScanners(), opts)
		security.WriteReportArtifact(s.workspace, "image", report)
		writeJSON(w, http.StatusOK, scanResponseFromReport(report))
		return
	}

	if len(req.Targets) > 0 {
		report, err := security.RunRecon(r.Context(), security.ReconOptions{
			Targets:        req.Targets,
			AllowedTargets: s.cfg.Security.DAST.AllowedTargets,
			AllowActive:    s.cfg.Security.DAST.AllowActive,
		}, opts)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		security.WriteReportArtifact(s.workspace, "network", report)
		writeJSON(w, http.StatusOK, scanResponseFromReport(report))
		return
	}

	dir := s.workspace
	if req.Path != "" {
		resolved, err := sandbox.ValidatePath(s.workspace, req.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		dir = resolved
	}

	if req.SBOM {
		sbom, method, err := security.GenerateSBOM(r.Context(), dir, opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		security.WriteSBOMArtifact(dir, sbom)
		writeJSON(w, http.StatusOK, api.ScanResponse{
			Report: fmt.Sprintf("SBOM generated via %s and written to %s (%d bytes)", method, security.SBOMArtifactPath(dir), len(sbom)),
		})
		return
	}

	scanners := security.DefaultScanners()
	if len(req.Scanners) > 0 {
		var err error
		scanners, opts, err = security.SelectScanners(scanners, opts, req.Scanners)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		opts = security.AutoEnableLanguageScanners(dir, opts)
	}

	report := security.RunWithOptions(r.Context(), dir, scanners, opts)
	security.WriteReportArtifact(dir, "scan", report)
	writeJSON(w, http.StatusOK, scanResponseFromReport(report))
}
