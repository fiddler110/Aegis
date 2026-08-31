package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeGlobalConfig writes the operator's own ~/.config/aegis/config.yaml, the
// layer a project may tighten but never loosen.
func writeGlobalConfig(t *testing.T, yaml string) {
	t.Helper()
	path := GlobalConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
}

// cloudProject sets up a workspace whose resolved provider is a metered cloud
// endpoint, so the P81.15 shipped ceilings are in force and there is something
// for a project config to try to loosen.
func cloudProject(t *testing.T, projectYAML string) *Config {
	t.Helper()
	redirectConfigDir(t)
	chdirTemp(t)
	clearEnv(t,
		"AEGIS_COST_DAILY_CAP_USD", "AEGIS_COST_SESSION_CAP_USD",
		"AEGIS_PROVIDER_DEFAULT", "AEGIS_PROVIDER_BASE_URL",
	)
	writeGlobalConfig(t, "provider:\n  default: anthropic\n")
	writeProjectConfig(t, projectYAML)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// TestProjectCannotLoosenACloudSpendCap is the P81.15 hole the shipped
// ceilings were worthless without: `cost` is projectSettable, so before
// projectMayTighten any cloned repository could delete the cap that exists to
// bound it.
//
// The zero case is the one that matters and the one a "smaller is tighter"
// comparison gets backwards. `daily_cap_usd: 0` is documented as *unlimited*,
// so it is simultaneously the smallest number and the weakest bound; it also
// counts as "stated" to koanf, which suppresses the shipped default entirely.
// Both halves have to be handled or the ceiling silently is not there.
func TestProjectCannotLoosenACloudSpendCap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		project   string
		wantDaily float64
		why       string
	}{
		{
			name:      "zero means unlimited and must not survive",
			project:   "cost:\n  daily_cap_usd: 0\n",
			wantDaily: DefaultCloudDailyCapUSD,
			why:       "0 is the documented way to say unlimited; a cloned repo saying it deletes the ceiling",
		},
		{
			name:      "raising the ceiling is not the project's call",
			project:   "cost:\n  daily_cap_usd: 500\n",
			wantDaily: DefaultCloudDailyCapUSD,
			why:       "the bill is the operator's, so a repo may not raise what it is bounded by",
		},
		{
			name:      "negative is unlimited by the same reading as MaxTurnStall",
			project:   "cost:\n  daily_cap_usd: -1\n",
			wantDaily: DefaultCloudDailyCapUSD,
			why:       "non-positive normalizes to unlimited on both sides",
		},
		{
			name:      "tightening is free and needs no prompt",
			project:   "cost:\n  daily_cap_usd: 2\n",
			wantDaily: 2,
			why:       "a repo asking for a smaller budget is harmless and must not be reverted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cloudProject(t, tc.project)
			if cfg.Cost.DailyCapUSD != tc.wantDaily {
				t.Errorf("cost.daily_cap_usd = %v, want %v — %s",
					cfg.Cost.DailyCapUSD, tc.wantDaily, tc.why)
			}
		})
	}
}

// TestTighteningACapRaisesNoTrustPrompt is the reason projectMayTighten exists
// as its own policy rather than being folded into frozenUntilTrusted. A repo
// pinning a smaller budget is ordinary; if that raised the workspace-trust
// prompt, the prompt that also guards permission/sandbox/mcp would start
// firing on harmless edits, which is how a prompt stops being read (P81.33).
func TestTighteningACapRaisesNoTrustPrompt(t *testing.T) {
	cfg := cloudProject(t, "cost:\n  daily_cap_usd: 2\n")

	if cfg.WorkspaceTrust.Frozen {
		t.Error("tightening a spend cap froze the workspace; it must not need a trust decision")
	}
	for _, line := range cfg.WorkspaceTrust.Changes {
		if strings.HasPrefix(line, "cost.") {
			t.Errorf("trust prompt names %q; projectMayTighten keys are handled "+
				"unconditionally and must not add lines to the prompt", line)
		}
	}
	if cfg.Cost.DailyCapUSD != 2 {
		t.Errorf("cost.daily_cap_usd = %v, want 2", cfg.Cost.DailyCapUSD)
	}
}

// TestTrustDoesNotUnlockLooseningASpendCap pins the deliberate difference from
// frozenUntilTrusted. Trusting a repository is a judgement about what its code
// may do on this machine; a runaway loop in a trusted repository bills exactly
// what one in an untrusted repository bills, so the clamp is unconditional.
func TestTrustDoesNotUnlockLooseningASpendCap(t *testing.T) {
	redirectConfigDir(t)
	dir := chdirTemp(t)
	clearEnv(t, "AEGIS_COST_DAILY_CAP_USD", "AEGIS_PROVIDER_DEFAULT", "AEGIS_PROVIDER_BASE_URL")
	writeGlobalConfig(t, "provider:\n  default: anthropic\n")
	writeProjectConfig(t, "cost:\n  daily_cap_usd: 0\n")
	if err := TrustWorkspace(dir); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WorkspaceTrust.Trusted {
		t.Fatal("test setup: workspace should be trusted")
	}
	if cfg.Cost.DailyCapUSD != DefaultCloudDailyCapUSD {
		t.Errorf("cost.daily_cap_usd = %v in a TRUSTED workspace, want %v — trust is a "+
			"decision about what code may do, not about whose money is spent",
			cfg.Cost.DailyCapUSD, DefaultCloudDailyCapUSD)
	}
}

// TestOperatorCeilingIsWhatAProjectIsMeasuredAgainst checks the comparison is
// against the operator's own layers rather than the shipped default, so a
// global config tighter than the default is not something a project can undo
// by naming the default.
func TestOperatorCeilingIsWhatAProjectIsMeasuredAgainst(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)
	clearEnv(t, "AEGIS_COST_DAILY_CAP_USD", "AEGIS_PROVIDER_DEFAULT", "AEGIS_PROVIDER_BASE_URL")
	writeGlobalConfig(t, "provider:\n  default: anthropic\ncost:\n  daily_cap_usd: 5\n")
	writeProjectConfig(t, "cost:\n  daily_cap_usd: 40\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cost.DailyCapUSD != 5 {
		t.Errorf("cost.daily_cap_usd = %v, want 5 — the operator set a ceiling tighter than "+
			"the shipped default and a project must not be able to climb back to it",
			cfg.Cost.DailyCapUSD)
	}
}

// TestLoopbackProviderLeavesBoundsAlone is the control. With no metered
// endpoint there is no shipped ceiling, the baseline bound is 0/unlimited, and
// nothing a project says about it is "looser" — a bound on unpriced local
// inference could only ever be a false stop.
func TestLoopbackProviderLeavesBoundsAlone(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)
	clearEnv(t, "AEGIS_COST_DAILY_CAP_USD", "AEGIS_PROVIDER_DEFAULT", "AEGIS_PROVIDER_BASE_URL")
	writeGlobalConfig(t, "provider:\n  default: openai\n  base_url: http://localhost:11434/v1\n")
	writeProjectConfig(t, "cost:\n  daily_cap_usd: 500\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cost.DailyCapUSD != 500 {
		t.Errorf("cost.daily_cap_usd = %v, want 500 — with no operator ceiling in force "+
			"there is nothing for a project to loosen", cfg.Cost.DailyCapUSD)
	}
}

// TestLooserBoundTreatsZeroAsUnlimited pins the comparator directly, because
// it is the one piece of this whose bug would be invisible: every key it
// guards documents 0 as unlimited, so an ordinary numeric comparison reads the
// weakest possible value as the strongest and admits exactly what the policy
// exists to reject.
func TestLooserBoundTreatsZeroAsUnlimited(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cfg, base  float64
		wantLooser bool
	}{
		{"zero against a real ceiling is the removal case", 0, 50, true},
		{"negative reads as unlimited too", -1, 50, true},
		{"raising a real ceiling", 500, 50, true},
		{"lowering a real ceiling", 2, 50, false},
		{"equal is not looser", 50, 50, false},
		{"no operator ceiling means nothing is looser", 500, 0, false},
		{"introducing a bound where there was none", 10, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := looserBound(floatValue(tc.cfg), floatValue(tc.base))
			if got != tc.wantLooser {
				t.Errorf("looserBound(%v, %v) = %v, want %v", tc.cfg, tc.base, got, tc.wantLooser)
			}
		})
	}
}

// TestLooserBoundFailsClosedOnAnUnorderedKind covers a bound added to the
// table later whose type the comparator has no ordering for: it must be
// treated as looser and reverted, not silently admitted.
func TestLooserBoundFailsClosedOnAnUnorderedKind(t *testing.T) {
	if !looserBound(stringValue("anything"), stringValue("baseline")) {
		t.Error("a differing value of an unordered kind must be reported as looser (fail closed)")
	}
	if looserBound(stringValue("same"), stringValue("same")) {
		t.Error("an identical value is never looser, whatever its kind")
	}
}

// floatValue / stringValue build addressable reflect.Values for the
// comparator tests, which exercise looserBound directly rather than through a
// Config walk.
func floatValue(f float64) reflect.Value { return reflect.ValueOf(&f).Elem() }
func stringValue(s string) reflect.Value { return reflect.ValueOf(&s).Elem() }
