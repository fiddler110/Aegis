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
	// 30s rather than 10s: resolution already costs an image inspect plus a
	// cache probe per DB-backed tool, and P55.6's database-age probe adds one
	// more container start. Timing out would report the databases unreadable
	// when the only problem was a cold container runtime.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	descs := security.Descriptors()
	out := make([]api.SecurityToolStatus, 0, len(descs))
	fallbacks := map[string]string{}
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

		res := security.ResolveDetailed(ctx, d.Name, opts)
		switch res.Method {
		case security.MethodHost:
			st.Resolved = "host"
			st.Status = "on PATH"
			// Matches the TUI picker's wording exactly (see
			// securityconfig.go's resolveCmd), which SecurityToolStatus.Status
			// documents itself as mirroring. The full reason goes into
			// Warnings below rather than onto this row.
			if res.FallbackWhy != "" {
				st.Status = "on PATH (multiscanner container unavailable)"
				fallbacks[d.Name] = res.FallbackWhy
			}
		case security.MethodContainer:
			st.Resolved = "container"
			st.Runtime = string(res.Runtime)
			st.Status = fmt.Sprintf("container (%s)", res.Runtime)
		case security.MethodWSL:
			st.Resolved = "wsl"
			st.Status = "via WSL"
		default:
			st.Resolved = "unavailable"
			st.Status = "unavailable: " + res.Reason
			if note := security.AvailabilityNote(d.Name, res.Reason); note != "" {
				st.Status += "; " + note
			}
		}
		out = append(out, st)
	}
	var warnings []string
	if drift := security.MultiscannerSourceDrift(opts.Multiscanner); drift != "" {
		warnings = append(warnings, drift)
	}
	// The fallback advisory goes in Warnings rather than a new per-tool field
	// for the reason Warnings already exists: like source drift, one cause
	// affects every covered scanner at once, so a per-row copy would render as
	// fourteen problems in a UI list instead of one. The per-tool half of the
	// signal is already on the row, in Status.
	if advisory := security.HostFallbackAdvisory(fallbacks); advisory != "" {
		warnings = append(warnings, advisory)
	}
	// Database age is a Warnings entry only when it's actually stale: an
	// operator polling this endpoint wants to know when the data went bad, not
	// to be told its age on every poll. `aegis security status` prints the full
	// per-tool table; this is the alarm half of it.
	if ages := security.DatabaseAges(ctx, opts); ages.Warning(time.Now()) != "" {
		warnings = append(warnings, ages.Warning(time.Now()))
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
