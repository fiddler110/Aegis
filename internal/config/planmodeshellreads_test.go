package config

import "testing"

// TestPlanModeShellReadsEnabledDefaultsByTrust is P81.20/FIND-20's posture
// change: absent an explicit operator preference, the classifier-downgrade
// convenience default now depends on whether the workspace has a current
// trust grant, rather than being unconditionally true. This is what makes
// the posture an operator gets when reviewing an unreviewed repository not
// depend on classifyShellCommand's correctness by default (the shared
// structural cause behind CRIT-1, CRIT-2 and P79.1) while leaving a trusted
// workspace's ergonomics unchanged.
func TestPlanModeShellReadsEnabledDefaultsByTrust(t *testing.T) {
	var unset PermissionConfig // PlanModeShellReads left nil: no operator preference

	if got := unset.PlanModeShellReadsEnabled(false); got != false {
		t.Errorf("untrusted workspace, no preference: PlanModeShellReadsEnabled(false) = %v, want false", got)
	}
	if got := unset.PlanModeShellReadsEnabled(true); got != true {
		t.Errorf("trusted workspace, no preference: PlanModeShellReadsEnabled(true) = %v, want true", got)
	}
}

// TestPlanModeShellReadsEnabledExplicitOverridesTrust pins that an operator's
// explicit setting (in either direction) wins regardless of trust — the
// flag and both postures already existed and P81.20 must not take either
// away, only change what happens when the operator has not chosen.
func TestPlanModeShellReadsEnabledExplicitOverridesTrust(t *testing.T) {
	trueVal, falseVal := true, false

	forcedOn := PermissionConfig{PlanModeShellReads: &trueVal}
	if got := forcedOn.PlanModeShellReadsEnabled(false); got != true {
		t.Errorf("explicit true, untrusted workspace: PlanModeShellReadsEnabled(false) = %v, want true", got)
	}

	forcedOff := PermissionConfig{PlanModeShellReads: &falseVal}
	if got := forcedOff.PlanModeShellReadsEnabled(true); got != false {
		t.Errorf("explicit false, trusted workspace: PlanModeShellReadsEnabled(true) = %v, want false", got)
	}
}
