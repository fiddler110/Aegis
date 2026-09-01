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
	"github.com/fiddler110/aegis/internal/reqorigin"
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

// configHTTPError lets a patchConfigSection build func attach a specific HTTP
// status to a validation failure (e.g. an unrecognized sandbox backend)
// instead of the 500 patchConfigSection uses by default for a plain error (an
// unexpected config.Load/write failure).
type configHTTPError struct {
	status int
	msg    string
}

func (e *configHTTPError) Error() string { return e.msg }

// getConfigSection is the shared shape behind every GET /config/<section>
// handler (P78.8): resolve scope, load the merged config, and hand both to
// build to produce the response body. Every section's GET follows exactly
// this shape — only what build reads off cfg differs.
func getConfigSection[Resp any](w http.ResponseWriter, r *http.Request, s *Server, build func(scope string, cfg *config.Config) Resp) {
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
	writeJSON(w, http.StatusOK, build(scope, cfg))
}

// patchConfigSection is the shared shape behind every PATCH /config/<section>
// handler (P78.8): decode the request body, resolve which config file to
// write to, build the section's patch value, write it, and respond with the
// resulting effective value.
//
// scopeOf extracts the request's scope field. build produces the T to write;
// it is handed a lazy, memoized config.Load() so a section whose patch
// semantics genuinely are partial-update (sandbox, security, cost) can read
// the current config to carry forward fields the request didn't touch,
// while a section that is always a full replace (skills — see
// api.ConfigSkillsPatchRequest's doc comment) can simply never call it, the
// same as its handler did before this helper existed. write picks
// PatchGlobal<Section>/PatchProject<Section> for the resolved scope; respond
// builds the response body from the resolved scope and the written T.
func patchConfigSection[Req any, T any](
	w http.ResponseWriter, r *http.Request, s *Server,
	scopeOf func(Req) string,
	build func(req Req, loadCfg func() (*config.Config, error)) (T, error),
	write func(scope string) func(T) error,
	respond func(scope string, v T) any,
) {
	var req Req
	if !decodeBody(w, r, &req) {
		return
	}
	scope, ok := s.resolveScope(scopeOf(req))
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be \"project\" or \"global\"")
		return
	}

	var loadedCfg *config.Config
	var loadErr error
	loaded := false
	loadCfg := func() (*config.Config, error) {
		if !loaded {
			loadedCfg, loadErr = config.Load()
			loaded = true
		}
		return loadedCfg, loadErr
	}

	patch, err := build(req, loadCfg)
	if err != nil {
		var he *configHTTPError
		if errors.As(err, &he) {
			writeError(w, he.status, he.msg)
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if err := write(scope)(patch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// P81.14/P81.3: every accepted config PATCH weakens or strengthens a
	// security posture (sandbox backend, redaction, cost ceilings, ...) and
	// previously left no record at all. requested is the caller's own body;
	// patch is what was actually applied — the fully-resolved value after
	// merging with whatever the section's build func carried forward from
	// loadCfg(), so it reflects the real after-state even for a
	// partial-update section.
	if s.audit != nil {
		s.audit.ConfigPatch(r.URL.Path, r.RemoteAddr, req, patch)
	}
	writeJSON(w, http.StatusOK, respond(scope, patch))
}

// ─── /config/sandbox ────────────────────────────────────────────────────────

// sandboxConfigResponse pairs the requested scope's configured sandbox.*
// values with the daemon's actual, currently running sandbox backend
// (P25.2). The configured values alone can't be trusted: an unrecognized
// backend, a container runtime that failed to initialize at startup, etc.
// all silently degrade to the unsandboxed local backend, and s.sandbox /
// s.sandboxFallback(Reason) — set once at daemon startup by SelectSandbox —
// are the only source of truth for what's actually live.
func (s *Server) sandboxBackendLabel() string {
	if s.sandbox == nil {
		return ""
	}
	return s.sandbox.Name()
}

func (s *Server) sandboxConfigResponse(scope, backend, runtime string, priority []string, image string, network bool) api.ConfigSandboxResponse {
	active := s.sandboxBackendLabel()
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
	getConfigSection(w, r, s, func(scope string, cfg *config.Config) api.ConfigSandboxResponse {
		return s.sandboxConfigResponse(
			scope, cfg.Sandbox.Backend, cfg.Sandbox.Runtime, cfg.Sandbox.Priority, cfg.Sandbox.Image, cfg.Sandbox.Network,
		)
	})
}

func (s *Server) handlePatchConfigSandbox(w http.ResponseWriter, r *http.Request) {
	patchConfigSection(w, r, s,
		func(req api.ConfigSandboxPatchRequest) string { return req.Scope },
		func(req api.ConfigSandboxPatchRequest, loadCfg func() (*config.Config, error)) (config.SandboxPatch, error) {
			cfg, err := loadCfg()
			if err != nil {
				return config.SandboxPatch{}, err
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
			// Apply the same alias/validation table Load() uses (P25.2), so a
			// PATCH naming a runtime directly ("podman") is normalized to the
			// correct backend+runtime pair, and a genuinely unknown backend is
			// rejected here rather than silently written to disk and only
			// discovered next start.
			normalized := config.SandboxConfig{Backend: patch.Backend, Runtime: patch.Runtime}
			if err := normalized.Normalize(); err != nil {
				return config.SandboxPatch{}, &configHTTPError{status: http.StatusBadRequest, msg: err.Error()}
			}
			patch.Backend, patch.Runtime = normalized.Backend, normalized.Runtime
			// P81.3/FIND-03: the same refusal Server.New already applies at
			// startup (wireSecurityWarnings -> unsandboxedAutoExecError) was
			// never consulted here, so a daemon that started with a real
			// sandbox could be PATCHed straight into the one combination that
			// means unattended RCE — auto-approved execution over an
			// unsandboxed backend — through an entirely legitimate API call.
			// "" and "local" both mean the unsandboxed local backend
			// (sandboxKnownBackends' own comment); sandboxFallback/reason are
			// false/"" because this is an explicit operator choice, not the
			// runtime's own detection falling back to local.
			if patch.Backend == "" || patch.Backend == "local" {
				if err := unsandboxedAutoExecError(cfg.Permission, patch.Backend, false, ""); err != nil {
					return config.SandboxPatch{}, &configHTTPError{status: http.StatusBadRequest, msg: err.Error()}
				}
			}
			return patch, nil
		},
		func(scope string) func(config.SandboxPatch) error {
			if scope == "project" {
				return func(p config.SandboxPatch) error {
					return config.PatchProjectSandboxWithOrigin(p, reqorigin.Web)
				}
			}
			return config.PatchGlobalSandbox
		},
		func(scope string, patch config.SandboxPatch) any {
			return s.sandboxConfigResponse(scope, patch.Backend, patch.Runtime, patch.Priority, patch.Image, patch.Network)
		},
	)
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

func securityConfigResponse(scope string, p config.SecurityPatch) api.ConfigSecurityResponse {
	return api.ConfigSecurityResponse{
		Scope:            scope,
		EgressThenWrite:  p.EgressThenWrite,
		NetworkAllowList: p.NetworkAllowList,
		DefaultMethod:    p.DefaultMethod,
		Tools:            toSecurityToolsWire(p.Tools),
		DAST:             api.DASTConfigWire{AllowedTargets: p.DAST.AllowedTargets, AllowActive: p.DAST.AllowActive},
	}
}

func (s *Server) handleGetConfigSecurity(w http.ResponseWriter, r *http.Request) {
	getConfigSection(w, r, s, func(scope string, cfg *config.Config) api.ConfigSecurityResponse {
		return securityConfigResponse(scope, config.SecurityPatch{
			EgressThenWrite:  cfg.Security.EgressThenWrite,
			NetworkAllowList: cfg.Security.NetworkAllowList,
			DefaultMethod:    cfg.Security.DefaultMethod,
			Tools:            cfg.Security.Tools,
			DAST:             cfg.Security.DAST,
		})
	})
}

func (s *Server) handlePatchConfigSecurity(w http.ResponseWriter, r *http.Request) {
	patchConfigSection(w, r, s,
		func(req api.ConfigSecurityPatchRequest) string { return req.Scope },
		func(req api.ConfigSecurityPatchRequest, loadCfg func() (*config.Config, error)) (config.SecurityPatch, error) {
			cfg, err := loadCfg()
			if err != nil {
				return config.SecurityPatch{}, err
			}
			patch := config.SecurityPatch{
				EgressThenWrite:  cfg.Security.EgressThenWrite,
				NetworkAllowList: cfg.Security.NetworkAllowList,
				DefaultMethod:    cfg.Security.DefaultMethod,
				Tools:            cfg.Security.Tools,
				DAST:             cfg.Security.DAST,
				WSLDistro:        cfg.Security.WSLDistro,
				Debate:           cfg.Security.Debate,
				Multiscanner:     cfg.Security.Multiscanner,
				Netscanner:       cfg.Security.Netscanner,
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
			return patch, nil
		},
		func(scope string) func(config.SecurityPatch) error {
			if scope == "project" {
				return func(p config.SecurityPatch) error {
					return config.PatchProjectSecurityWithOrigin(p, reqorigin.Web)
				}
			}
			return config.PatchGlobalSecurity
		},
		func(scope string, patch config.SecurityPatch) any {
			return securityConfigResponse(scope, patch)
		},
	)
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
	getConfigSection(w, r, s, func(scope string, cfg *config.Config) api.ConfigSkillsResponse {
		return api.ConfigSkillsResponse{
			Scope:          scope,
			BuiltinEnabled: cfg.Skills.BuiltinEnabled,
			Available:      availableBuiltinSkills(),
		}
	})
}

func (s *Server) handlePatchConfigSkills(w http.ResponseWriter, r *http.Request) {
	patchConfigSection(w, r, s,
		func(req api.ConfigSkillsPatchRequest) string { return req.Scope },
		// api.ConfigSkillsPatchRequest is always a full replace (see its own
		// doc comment: config.PatchGlobalSkillsEnabled/PatchProjectSkillsEnabled
		// already require the full desired set, not a delta) — so, unlike
		// sandbox/security/cost, this build func never calls loadCfg. That is
		// intentional, not the P78.8-flagged bug it looked like at a glance:
		// investigated and confirmed there is nothing here for a base config
		// load to merge onto.
		func(req api.ConfigSkillsPatchRequest, _ func() (*config.Config, error)) ([]string, error) {
			return req.BuiltinEnabled, nil
		},
		func(scope string) func([]string) error {
			if scope == "project" {
				return config.PatchProjectSkillsEnabled
			}
			return config.PatchGlobalSkillsEnabled
		},
		func(scope string, names []string) any {
			return api.ConfigSkillsResponse{
				Scope:          scope,
				BuiltinEnabled: names,
				Available:      availableBuiltinSkills(),
			}
		},
	)
}

// ─── /config/cost ────────────────────────────────────────────────────────────

// costConfigResponse builds the GET/PATCH /config/cost response from a
// config.CostPatch-shaped value (P78.8). `aegis harden`/POST /config/harden
// could already write this block via PatchGlobalCost/PatchProjectCost, but
// there was no way to read or partially patch it directly the way
// sandbox/security/skills could — this fills that gap, following the exact
// same pattern.
func costConfigResponse(scope string, p config.CostPatch) api.ConfigCostResponse {
	return api.ConfigCostResponse{
		Scope:                 scope,
		BudgetUSD:             p.BudgetUSD,
		MaxTokensPerRun:       p.MaxTokensPerRun,
		MaxWallClockPerRunSec: p.MaxWallClockPerRunSec,
		MaxTurnStallSec:       p.MaxTurnStallSec,
		SessionCapUSD:         p.SessionCapUSD,
		DailyCapUSD:           p.DailyCapUSD,
		SessionTokenCap:       p.SessionTokenCap,
		DailyTokenCap:         p.DailyTokenCap,
		AlertThreshold:        p.AlertThreshold,
	}
}

func (s *Server) handleGetConfigCost(w http.ResponseWriter, r *http.Request) {
	getConfigSection(w, r, s, func(scope string, cfg *config.Config) api.ConfigCostResponse {
		return costConfigResponse(scope, config.CostPatch{
			BudgetUSD:                cfg.Cost.BudgetUSD,
			MaxTokensPerRun:          cfg.Cost.MaxTokensPerRun,
			MaxGeneratedTokensPerRun: cfg.Cost.MaxGeneratedTokensPerRun,
			MaxWallClockPerRunSec:    cfg.Cost.MaxWallClockPerRunSec,
			MaxTurnStallSec:          cfg.Cost.MaxTurnStallSec,
			SessionCapUSD:            cfg.Cost.SessionCapUSD,
			DailyCapUSD:              cfg.Cost.DailyCapUSD,
			SessionTokenCap:          cfg.Cost.SessionTokenCap,
			DailyTokenCap:            cfg.Cost.DailyTokenCap,
			AlertThreshold:           cfg.Cost.AlertThreshold,
		})
	})
}

func (s *Server) handlePatchConfigCost(w http.ResponseWriter, r *http.Request) {
	patchConfigSection(w, r, s,
		func(req api.ConfigCostPatchRequest) string { return req.Scope },
		func(req api.ConfigCostPatchRequest, loadCfg func() (*config.Config, error)) (config.CostPatch, error) {
			cfg, err := loadCfg()
			if err != nil {
				return config.CostPatch{}, err
			}
			patch := config.CostPatch{
				BudgetUSD:                cfg.Cost.BudgetUSD,
				MaxTokensPerRun:          cfg.Cost.MaxTokensPerRun,
				MaxGeneratedTokensPerRun: cfg.Cost.MaxGeneratedTokensPerRun,
				MaxWallClockPerRunSec:    cfg.Cost.MaxWallClockPerRunSec,
				MaxTurnStallSec:          cfg.Cost.MaxTurnStallSec,
				SessionCapUSD:            cfg.Cost.SessionCapUSD,
				DailyCapUSD:              cfg.Cost.DailyCapUSD,
				SessionTokenCap:          cfg.Cost.SessionTokenCap,
				DailyTokenCap:            cfg.Cost.DailyTokenCap,
				AlertThreshold:           cfg.Cost.AlertThreshold,
			}
			if req.BudgetUSD != nil {
				patch.BudgetUSD = *req.BudgetUSD
			}
			if req.MaxTokensPerRun != nil {
				patch.MaxTokensPerRun = *req.MaxTokensPerRun
			}
			if req.MaxWallClockPerRunSec != nil {
				patch.MaxWallClockPerRunSec = *req.MaxWallClockPerRunSec
			}
			if req.MaxTurnStallSec != nil {
				patch.MaxTurnStallSec = *req.MaxTurnStallSec
			}
			if req.SessionCapUSD != nil {
				patch.SessionCapUSD = *req.SessionCapUSD
			}
			if req.DailyCapUSD != nil {
				patch.DailyCapUSD = *req.DailyCapUSD
			}
			if req.SessionTokenCap != nil {
				patch.SessionTokenCap = *req.SessionTokenCap
			}
			if req.DailyTokenCap != nil {
				patch.DailyTokenCap = *req.DailyTokenCap
			}
			if req.AlertThreshold != nil {
				patch.AlertThreshold = *req.AlertThreshold
			}
			return patch, nil
		},
		func(scope string) func(config.CostPatch) error {
			if scope == "project" {
				return config.PatchProjectCost
			}
			return config.PatchGlobalCost
		},
		func(scope string, patch config.CostPatch) any {
			return costConfigResponse(scope, patch)
		},
	)
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
		writeSandbox = func(p config.SandboxPatch) error {
			return config.PatchProjectSandboxWithOrigin(p, reqorigin.Web)
		}
		writeSecurity = func(p config.SecurityPatch) error {
			return config.PatchProjectSecurityWithOrigin(p, reqorigin.Web)
		}
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
