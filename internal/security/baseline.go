package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v3"
)

// SuppressionEntry is one accepted-risk allowlist entry in
// .aegis/security-baseline.yaml (P11.8): a specific finding, matched by
// rule/CVE and optionally scoped to a location, that an operator has
// reviewed and decided not to act on right now. Expires is mandatory so
// accepted risk has a forced review date rather than becoming permanent by
// default.
type SuppressionEntry struct {
	RuleID   string `yaml:"rule_id"`
	Location string `yaml:"location,omitempty"` // optional scope; empty = suppress this rule everywhere
	Reason   string `yaml:"reason"`
	Expires  string `yaml:"expires"` // YYYY-MM-DD, required
	AddedBy  string `yaml:"added_by,omitempty"`
}

// Baseline is the parsed contents of a security-baseline.yaml file.
type Baseline struct {
	Suppressions []SuppressionEntry `yaml:"suppressions"`
}

// BaselinePath is where LoadBaseline reads dir's suppression allowlist from.
func BaselinePath(dir string) string {
	return filepath.Join(dir, ".aegis", "security-baseline.yaml")
}

// LoadBaseline reads dir's baseline file. A missing file is the common case
// (most projects have no accepted-risk allowlist) and returns (nil, nil), not
// an error.
func LoadBaseline(dir string) (*Baseline, error) {
	data, err := os.ReadFile(BaselinePath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read security baseline: %w", err)
	}
	var b Baseline
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse security baseline: %w", err)
	}
	return &b, nil
}

type suppressionStatus int

const (
	statusActive suppressionStatus = iota
	statusExpired
	statusInvalid
)

// status classifies e against now: invalid if it's missing a rule_id/reason
// or its expires date doesn't parse (never silently applied — a malformed
// entry must never suppress anything), expired if that date has passed
// (the finding it used to cover comes back into view), else active.
func (e SuppressionEntry) status(now time.Time) suppressionStatus {
	if strings.TrimSpace(e.RuleID) == "" || strings.TrimSpace(e.Reason) == "" {
		return statusInvalid
	}
	exp, err := time.Parse("2006-01-02", strings.TrimSpace(e.Expires))
	if err != nil {
		return statusInvalid
	}
	if now.After(exp) {
		return statusExpired
	}
	return statusActive
}

// matches reports whether e covers f: same normalized rule/CVE, and either
// no location scope (suppress this rule everywhere) or f's normalized
// location contains e's (substring match, so an entry can scope to a
// package/file without needing to reproduce a tool's exact location string).
func (e SuppressionEntry) matches(f Finding) bool {
	ruleID := normalizeRuleID(f.RuleID)
	if ruleID == "" || normalizeRuleID(e.RuleID) != ruleID {
		return false
	}
	if strings.TrimSpace(e.Location) == "" {
		return true
	}
	return strings.Contains(normalizeLocation(f.Location), normalizeLocation(e.Location))
}

// Apply partitions findings into kept (not suppressed) and suppressed
// (matched by an active entry), and separately returns which entries were
// expired or invalid this run so a stale/malformed baseline is always
// visible rather than silently doing nothing. A nil Baseline (no file) or
// one with no entries keeps every finding.
func (b *Baseline) Apply(findings []Finding, now time.Time) (kept, suppressed []Finding, expired, invalid []SuppressionEntry) {
	if b == nil || len(b.Suppressions) == 0 {
		return findings, nil, nil, nil
	}
	var active []SuppressionEntry
	for _, e := range b.Suppressions {
		switch e.status(now) {
		case statusActive:
			active = append(active, e)
		case statusExpired:
			expired = append(expired, e)
		default:
			invalid = append(invalid, e)
		}
	}
	for _, f := range findings {
		suppressedByAny := false
		for _, e := range active {
			if e.matches(f) {
				suppressedByAny = true
				break
			}
		}
		if suppressedByAny {
			suppressed = append(suppressed, f)
		} else {
			kept = append(kept, f)
		}
	}
	return kept, suppressed, expired, invalid
}

// SuppressionStatusLabel reports e's status as a short label ("active",
// "expired", or "invalid") for CLI/TUI display (`aegis security baseline`).
func SuppressionStatusLabel(e SuppressionEntry, now time.Time) string {
	switch e.status(now) {
	case statusActive:
		return "active"
	case statusExpired:
		return "expired"
	default:
		return "invalid"
	}
}

// formatSuppressionEntry renders one entry for Report.Format()'s
// expired/invalid notices.
func formatSuppressionEntry(e SuppressionEntry) string {
	loc := ""
	if e.Location != "" {
		loc = " @ " + e.Location
	}
	return fmt.Sprintf("%s%s (%s, expires %s)", e.RuleID, loc, e.Reason, e.Expires)
}
