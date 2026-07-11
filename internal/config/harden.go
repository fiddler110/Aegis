package config

import "fmt"

// Hardened cost-cap defaults, applied only to a cap that is currently unset
// (0 = unlimited) — harden tightens whatever was left wide open by default,
// it never silently overrides a value the user already chose. These are
// deliberately conservative starting points, not a prescription: edit
// cost.* in the resulting config to fit your own workflow.
const (
	HardenSessionCapUSD   = 5.0
	HardenDailyCapUSD     = 25.0
	HardenSessionTokenCap = 300_000
	HardenDailyTokenCap   = 2_000_000
)

// HardenPlan is the pure "what would a hardened profile change" computation
// shared by `aegis harden` (internal/cli/harden.go) and
// POST /config/harden (internal/server/config.go, P15.2) — kept in one place
// so the cap thresholds and the "leave an already-hardened knob alone"
// exceptions (container backend, egress_then_write already true, a cost cap
// the operator already set) can't drift between the CLI and HTTP surfaces.
// Sandbox/Security/Cost hold the ready-to-write patches; the *Changed/
// CostChanges fields distinguish "this transitioned" from "already hardened,
// left alone" for callers that want to report that back (both the CLI
// confirmation prompt and the HTTP response do).
type HardenPlan struct {
	Sandbox        SandboxPatch
	SandboxChanged bool

	Security        SecurityPatch
	SecurityChanged bool

	Cost        CostPatch
	CostChanges []string // e.g. "session_cap_usd -> $5.00", one entry per cap actually tightened
}

// ComputeHardenPlan derives the hardened patches from cfg's current values.
// It never reads or writes config files itself — callers apply the returned
// patches via PatchGlobal*/PatchProject* (or not at all, e.g. to preview).
func ComputeHardenPlan(cfg *Config) HardenPlan {
	sandboxPatch := SandboxPatch{
		Backend:  cfg.Sandbox.Backend,
		Runtime:  cfg.Sandbox.Runtime,
		Priority: cfg.Sandbox.Priority,
		Image:    cfg.Sandbox.Image,
		Network:  cfg.Sandbox.Network,
	}
	// "container" is already hardened (a specific runtime was pinned);
	// only "local" (or unset, which defaults to local) needs to move.
	sandboxChanged := sandboxPatch.Backend != "auto" && sandboxPatch.Backend != "container"
	if sandboxChanged {
		sandboxPatch.Backend = "auto"
	}

	securityPatch := SecurityPatch{
		EgressThenWrite:  true,
		NetworkAllowList: cfg.Security.NetworkAllowList,
		DefaultMethod:    cfg.Security.DefaultMethod,
		Tools:            cfg.Security.Tools,
		DAST:             cfg.Security.DAST,
	}
	securityChanged := !cfg.Security.EgressThenWrite

	costPatch := CostPatch{
		BudgetUSD:       cfg.Cost.BudgetUSD,
		MaxTokensPerRun: cfg.Cost.MaxTokensPerRun,
		SessionCapUSD:   cfg.Cost.SessionCapUSD,
		DailyCapUSD:     cfg.Cost.DailyCapUSD,
		SessionTokenCap: cfg.Cost.SessionTokenCap,
		DailyTokenCap:   cfg.Cost.DailyTokenCap,
		AlertThreshold:  cfg.Cost.AlertThreshold,
	}
	var costChanges []string
	if costPatch.SessionCapUSD <= 0 {
		costPatch.SessionCapUSD = HardenSessionCapUSD
		costChanges = append(costChanges, fmt.Sprintf("session_cap_usd -> $%.2f", HardenSessionCapUSD))
	}
	if costPatch.DailyCapUSD <= 0 {
		costPatch.DailyCapUSD = HardenDailyCapUSD
		costChanges = append(costChanges, fmt.Sprintf("daily_cap_usd -> $%.2f", HardenDailyCapUSD))
	}
	if costPatch.SessionTokenCap <= 0 {
		costPatch.SessionTokenCap = HardenSessionTokenCap
		costChanges = append(costChanges, fmt.Sprintf("session_token_cap -> %d", HardenSessionTokenCap))
	}
	if costPatch.DailyTokenCap <= 0 {
		costPatch.DailyTokenCap = HardenDailyTokenCap
		costChanges = append(costChanges, fmt.Sprintf("daily_token_cap -> %d", HardenDailyTokenCap))
	}

	return HardenPlan{
		Sandbox:         sandboxPatch,
		SandboxChanged:  sandboxChanged,
		Security:        securityPatch,
		SecurityChanged: securityChanged,
		Cost:            costPatch,
		CostChanges:     costChanges,
	}
}
