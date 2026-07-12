package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/skills"
)

// resolveScope maps a request's scope selector ("project", "global", or "")
// onto the daemon's write destination for config.Patch{Global,Project}*.
// An explicit value is validated and used as-is; an empty value defaults to
// "project" when the daemon's workspace has a .aegis/ directory, else
// "global" — the same heuristic `aegis dry-run`/`aegis harden --project`
// document (a project scope is opt-in) but resolved automatically here since
// there's no interactive flag to ask for.
//
// This mirrors config.Load()'s own assumption that the daemon's process
// working directory is its workspace (config.ProjectConfigPath() resolves
// ".aegis/config.yaml" relative to os.Getwd(), not relative to s.workspace);
// that already holds for every real `aegis serve` invocation. Test servers
// that pin s.workspace to a temp dir without also chdir'ing into it will see
// project-scope writes land under the *process* cwd's .aegis/, not
// s.workspace's — tests that exercise project scope must os.Chdir first.
func (s *Server) resolveScope(explicit string) (scope string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(explicit)) {
	case "":
		// fall through to auto-detection below
	case "project":
		return "project", true
	case "global":
		return "global", true
	default:
		return "", false
	}
	if info, err := os.Stat(filepath.Join(s.workspace, ".aegis")); err == nil && info.IsDir() {
		return "project", true
	}
	return "global", true
}

func scopeFromQuery(r *http.Request) string {
	return r.URL.Query().Get("scope")
}

// decodeBody decodes r's JSON body into v, tolerating an empty body (treated
// as a zero-value request) the same way handleScan does — a GET-like PATCH
// with an all-default body (e.g. just {"scope":"global"}) is common for
// these endpoints.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid body")
		return false
	}
	return true
}

// ─── /config/sandbox ────────────────────────────────────────────────────────

// sandboxConfigResponse pairs the requested scope's configured sandbox.*
// values with the daemon's actual, currently running sandbox backend
// (P25.2). The configured values alone can't be trusted: an unrecognized
// backend, a container runtime that failed to initialize at startup, etc.
// all silently degrade to the unsandboxed local backend, and s.sandbox /
// s.sandboxFallback(Reason) — set once at daemon startup by SelectSandbox —
// are the only source of truth for what's actually live.
func (s *Server) sandboxConfigResponse(scope, backend, runtime string, priority []string, image string, network bool) api.ConfigSandboxResponse {
	active := ""
	if s.sandbox != nil {
		active = s.sandbox.Name()
	}
	return api.ConfigSandboxResponse{
		Scope:          scope,
		Backend:        backend,
		Runtime:        runtime,
		Priority:       priority,
		Image:          image,
		Network:        network,
		ActiveBackend:  active,
		Fallback:       s.sandboxFallback,
		FallbackReason: s.sandboxFallbackReason,
	}
}

func (s *Server) handleGetConfigSandbox(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.resolveScope(scopeFromQuery(r))
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be \"project\" or \"global\"")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxConfigResponse(
		scope, cfg.Sandbox.Backend, cfg.Sandbox.Runtime, cfg.Sandbox.Priority, cfg.Sandbox.Image, cfg.Sandbox.Network,
	))
}

func (s *Server) handlePatchConfigSandbox(w http.ResponseWriter, r *http.Request) {
	var req api.ConfigSandboxPatchRequest
	if !decodeBody(w, r, &req) {
		return
	}
	scope, ok := s.resolveScope(req.Scope)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be \"project\" or \"global\"")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	patch := config.SandboxPatch{
		Backend:  cfg.Sandbox.Backend,
		Runtime:  cfg.Sandbox.Runtime,
		Priority: cfg.Sandbox.Priority,
		Image:    cfg.Sandbox.Image,
		Network:  cfg.Sandbox.Network,
	}
	if req.Backend != nil {
		patch.Backend = *req.Backend
	}
	if req.Runtime != nil {
		patch.Runtime = *req.Runtime
	}
	if req.Priority != nil {
		patch.Priority = *req.Priority
	}
	if req.Image != nil {
		patch.Image = *req.Image
	}
	if req.Network != nil {
		patch.Network = *req.Network
	}

	// Apply the same alias/validation table Load() uses (P25.2), so a PATCH
	// naming a runtime directly ("podman") is normalized to the correct
	// backend+runtime pair, and a genuinely unknown backend is rejected here
	// rather than silently written to disk and only discovered next start.
	normalized := config.SandboxConfig{Backend: patch.Backend, Runtime: patch.Runtime}
	if err := normalized.Normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	patch.Backend, patch.Runtime = normalized.Backend, normalized.Runtime

	write := config.PatchGlobalSandbox
	if scope == "project" {
		write = config.PatchProjectSandbox
	}
	if err := write(patch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxConfigResponse(
		scope, patch.Backend, patch.Runtime, patch.Priority, patch.Image, patch.Network,
	))
}

// ─── /config/security ───────────────────────────────────────────────────────

func toSecurityToolWire(tc config.SecurityToolConfig) api.SecurityToolConfigWire {
	return api.SecurityToolConfigWire{
		Enabled:          tc.ToolEnabled(),
		Method:           tc.Method,
		Install:          tc.Install,
		Image:            tc.Image,
		TemplatesVersion: tc.TemplatesVersion,
		Verify:           tc.Verify,
	}
}

func fromSecurityToolWire(w api.SecurityToolConfigWire) config.SecurityToolConfig {
	enabled := w.Enabled
	return config.SecurityToolConfig{
		Enabled:          &enabled,
		Method:           w.Method,
		Install:          w.Install,
		Image:            w.Image,
		TemplatesVersion: w.TemplatesVersion,
		Verify:           w.Verify,
	}
}

func toSecurityToolsWire(tools map[string]config.SecurityToolConfig) map[string]api.SecurityToolConfigWire {
	if len(tools) == 0 {
		return nil
	}
	out := make(map[string]api.SecurityToolConfigWire, len(tools))
	for name, tc := range tools {
		out[name] = toSecurityToolWire(tc)
	}
	return out
}

func fromSecurityToolsWire(tools map[string]api.SecurityToolConfigWire) map[string]config.SecurityToolConfig {
	if len(tools) == 0 {
		return nil
	}
	out := make(map[string]config.SecurityToolConfig, len(tools))
	for name, tc := range tools {
		out[name] = fromSecurityToolWire(tc)
	}
	return out
}

func (s *Server) handleGetConfigSecurity(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.resolveScope(scopeFromQuery(r))
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be \"project\" or \"global\"")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.ConfigSecurityResponse{
		Scope:            scope,
		EgressThenWrite:  cfg.Security.EgressThenWrite,
		NetworkAllowList: cfg.Security.NetworkAllowList,
		DefaultMethod:    cfg.Security.DefaultMethod,
		Tools:            toSecurityToolsWire(cfg.Security.Tools),
		DAST: api.DASTConfigWire{
			AllowedTargets: cfg.Security.DAST.AllowedTargets,
			AllowActive:    cfg.Security.DAST.AllowActive,
		},
	})
}

func (s *Server) handlePatchConfigSecurity(w http.ResponseWriter, r *http.Request) {
	var req api.ConfigSecurityPatchRequest
	if !decodeBody(w, r, &req) {
		return
	}
	scope, ok := s.resolveScope(req.Scope)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be \"project\" or \"global\"")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	patch := config.SecurityPatch{
		EgressThenWrite:  cfg.Security.EgressThenWrite,
		NetworkAllowList: cfg.Security.NetworkAllowList,
		DefaultMethod:    cfg.Security.DefaultMethod,
		Tools:            cfg.Security.Tools,
		DAST:             cfg.Security.DAST,
	}
	if req.EgressThenWrite != nil {
		patch.EgressThenWrite = *req.EgressThenWrite
	}
	if req.NetworkAllowList != nil {
		patch.NetworkAllowList = *req.NetworkAllowList
	}
	if req.DefaultMethod != nil {
		patch.DefaultMethod = *req.DefaultMethod
	}
	if req.Tools != nil {
		patch.Tools = fromSecurityToolsWire(req.Tools)
	}
	if req.DAST != nil {
		patch.DAST = config.DASTConfig{AllowedTargets: req.DAST.AllowedTargets, AllowActive: req.DAST.AllowActive}
	}

	write := config.PatchGlobalSecurity
	if scope == "project" {
		write = config.PatchProjectSecurity
	}
	if err := write(patch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.ConfigSecurityResponse{
		Scope:            scope,
		EgressThenWrite:  patch.EgressThenWrite,
		NetworkAllowList: patch.NetworkAllowList,
		DefaultMethod:    patch.DefaultMethod,
		Tools:            toSecurityToolsWire(patch.Tools),
		DAST:             api.DASTConfigWire{AllowedTargets: patch.DAST.AllowedTargets, AllowActive: patch.DAST.AllowActive},
	})
}

// ─── /config/skills ──────────────────────────────────────────────────────────

// availableBuiltinSkills lists the embedded built-in skill catalog in wire
// form, so /config/skills clients (P15.7 web UI toggles) can render every
// built-in with its description instead of hard-coding the shipped names.
func availableBuiltinSkills() []api.BuiltinSkillInfo {
	builtins := skills.Builtins()
	out := make([]api.BuiltinSkillInfo, 0, len(builtins))
	for _, b := range builtins {
		out = append(out, api.BuiltinSkillInfo{Name: b.Name, Description: b.Description})
	}
	return out
}

func (s *Server) handleGetConfigSkills(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.resolveScope(scopeFromQuery(r))
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be \"project\" or \"global\"")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.ConfigSkillsResponse{
		Scope:          scope,
		BuiltinEnabled: cfg.Skills.BuiltinEnabled,
		Available:      availableBuiltinSkills(),
	})
}

func (s *Server) handlePatchConfigSkills(w http.ResponseWriter, r *http.Request) {
	var req api.ConfigSkillsPatchRequest
	if !decodeBody(w, r, &req) {
		return
	}
	scope, ok := s.resolveScope(req.Scope)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be \"project\" or \"global\"")
		return
	}
	write := config.PatchGlobalSkillsEnabled
	if scope == "project" {
		write = config.PatchProjectSkillsEnabled
	}
	if err := write(req.BuiltinEnabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.ConfigSkillsResponse{
		Scope:          scope,
		BuiltinEnabled: req.BuiltinEnabled,
		Available:      availableBuiltinSkills(),
	})
}

// ─── POST /config/harden ─────────────────────────────────────────────────────

// handleConfigHarden is the HTTP equivalent of `aegis harden` (P15.2): it
// shares config.ComputeHardenPlan with the CLI command so the cap thresholds
// and "already hardened, leave it alone" exceptions can't drift between the
// two surfaces. Nothing is written unless Confirm is true — there's no
// interactive terminal here to show an "Apply? [y/N]" prompt, so the request
// body itself carries that confirmation.
func (s *Server) handleConfigHarden(w http.ResponseWriter, r *http.Request) {
	var req api.ConfigHardenRequest
	if !decodeBody(w, r, &req) {
		return
	}
	scope, ok := s.resolveScope(req.Scope)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be \"project\" or \"global\"")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan := config.ComputeHardenPlan(cfg)

	resp := api.ConfigHardenResponse{
		Scope:           scope,
		SandboxChanged:  plan.SandboxChanged,
		SandboxBackend:  plan.Sandbox.Backend,
		SecurityChanged: plan.SecurityChanged,
		CostChanges:     plan.CostChanges,
	}

	if !req.Confirm {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	writeSandbox := config.PatchGlobalSandbox
	writeSecurity := config.PatchGlobalSecurity
	writeCost := config.PatchGlobalCost
	if scope == "project" {
		writeSandbox = config.PatchProjectSandbox
		writeSecurity = config.PatchProjectSecurity
		writeCost = config.PatchProjectCost
	}
	if err := writeSandbox(plan.Sandbox); err != nil {
		writeError(w, http.StatusInternalServerError, "write sandbox config: "+err.Error())
		return
	}
	if err := writeSecurity(plan.Security); err != nil {
		writeError(w, http.StatusInternalServerError, "write security config: "+err.Error())
		return
	}
	if err := writeCost(plan.Cost); err != nil {
		writeError(w, http.StatusInternalServerError, "write cost config: "+err.Error())
		return
	}
	resp.Applied = true
	writeJSON(w, http.StatusOK, resp)
}
