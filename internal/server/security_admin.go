package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/security"
)

// ─── GET /security/status ───────────────────────────────────────────────────

// handleSecurityStatus reports every built-in scanner's configured/resolved
// status (P15.2) — the same live-availability probe (host binary / container
// runtime / WSL) and status wording internal/tui/securityconfig.go's
// resolveCmd/toolBadge already compute for the `/security-config` TUI
// dialog, exposed here so a web UI panel (P15.6/P15.7) can show the same
// picture without a session/model turn.
func (s *Server) handleSecurityStatus(w http.ResponseWriter, r *http.Request) {
	opts := security.OptionsFromConfig(s.cfg.Security)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	descs := security.Descriptors()
	out := make([]api.SecurityToolStatus, 0, len(descs))
	for _, d := range descs {
		tc := s.cfg.Security.Tools[d.Name]
		method := tc.Method
		if method == "" {
			method = s.cfg.Security.DefaultMethod
		}
		if method == "" {
			method = "auto"
		}
		st := api.SecurityToolStatus{
			Name:     d.Name,
			Category: d.Category,
			Enabled:  tc.ToolEnabled(),
			Method:   method,
		}

		resolved, rt, _, reason := security.Resolve(ctx, d.Name, opts)
		switch resolved {
		case security.MethodHost:
			st.Resolved = "host"
			st.Status = "on PATH"
		case security.MethodContainer:
			st.Resolved = "container"
			st.Runtime = string(rt)
			st.Status = fmt.Sprintf("container (%s)", rt)
		case security.MethodWSL:
			st.Resolved = "wsl"
			st.Status = "via WSL"
		default:
			st.Resolved = "unavailable"
			st.Status = "unavailable: " + reason
			if note := security.AvailabilityNote(d.Name, reason); note != "" {
				st.Status += "; " + note
			}
		}
		out = append(out, st)
	}
	var warnings []string
	if drift := security.MultiscannerSourceDrift(opts.Multiscanner); drift != "" {
		warnings = append(warnings, drift)
	}
	writeJSON(w, http.StatusOK, api.SecurityStatusResponse{Tools: out, Warnings: warnings})
}

// ─── GET /security/baseline ─────────────────────────────────────────────────

// handleSecurityBaseline returns the project's accepted-risk suppression
// allowlist (.aegis/security-baseline.yaml, P11.8) directly against the
// daemon's own workspace — the same file `aegis security baseline` and the
// security-audit skill's triage loop already read, exposed for a web UI
// panel to show without a model turn.
func (s *Server) handleSecurityBaseline(w http.ResponseWriter, r *http.Request) {
	b, err := security.LoadBaseline(s.workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := api.SecurityBaselineResponse{Path: security.BaselinePath(s.workspace)}
	if b != nil {
		now := time.Now()
		for _, e := range b.Suppressions {
			resp.Suppressions = append(resp.Suppressions, api.SecurityBaselineEntry{
				RuleID:   e.RuleID,
				Location: e.Location,
				Reason:   e.Reason,
				Expires:  e.Expires,
				AddedBy:  e.AddedBy,
				Status:   security.SuppressionStatusLabel(e, now),
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── POST /security/install ─────────────────────────────────────────────────

// handleSecurityInstall runs a scanner's guided host install (P15.2) — the
// same security.RunGuidedInstall the `aegis security install` CLI and the
// `/security-config` TUI wizard use. Installing software is a privileged,
// host-modifying action (see RunGuidedInstall's doc comment), so this never
// runs anything unless the request explicitly confirms: with Confirm false
// (or omitted) the response only reports what command would run, mirroring
// the TUI's two-phase "show the command, then confirm" flow collapsed into
// one round trip for a non-interactive HTTP caller.
func (s *Server) handleSecurityInstall(w http.ResponseWriter, r *http.Request) {
	var req api.SecurityInstallRequest
	if !decodeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Tool)
	if name == "" {
		writeError(w, http.StatusBadRequest, "tool is required")
		return
	}
	if _, ok := security.DescriptorFor(name); !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown security tool %q", name))
		return
	}

	resp := api.SecurityInstallResponse{Tool: name}
	cmdStr, ok := security.InstallCommand(name)
	if !ok {
		resp.Error = security.NoGuidedInstallReason(name)
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Command = cmdStr

	if !req.Confirm {
		resp.Error = "set confirm: true to run this command on the host"
		writeJSON(w, http.StatusOK, resp)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	var buf strings.Builder
	err := security.RunGuidedInstall(ctx, name, &buf)
	resp.Ran = true
	resp.Output = buf.String()
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.OK = true
	}
	writeJSON(w, http.StatusOK, resp)
}
