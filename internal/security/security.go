// Package security integrates external security scanners behind a single
// normalized findings model, so the agent (acting as a security platform
// architect) can identify issues uniformly regardless of the underlying tool.
package security

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Severity is a normalized finding severity.
type Severity string

const (
	SevCritical Severity = "CRITICAL"
	SevHigh     Severity = "HIGH"
	SevMedium   Severity = "MEDIUM"
	SevLow      Severity = "LOW"
	SevInfo     Severity = "INFO"
)

func (s Severity) rank() int {
	switch s {
	case SevCritical:
		return 4
	case SevHigh:
		return 3
	case SevMedium:
		return 2
	case SevLow:
		return 1
	default:
		return 0
	}
}

// normalizeSeverity maps tool-specific severities onto the normalized scale.
func normalizeSeverity(s string) Severity {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "CRITICAL":
		return SevCritical
	case "HIGH", "ERROR":
		return SevHigh
	case "MEDIUM", "MODERATE", "WARNING":
		return SevMedium
	case "LOW":
		return SevLow
	default:
		return SevInfo
	}
}

// Finding is a single normalized security issue.
type Finding struct {
	Tool        string   `json:"tool"`
	RuleID      string   `json:"rule_id"`
	Severity    Severity `json:"severity"`
	Title       string   `json:"title"`
	Location    string   `json:"location"` // file:line or package/target
	Description string   `json:"description,omitempty"`
	Remediation string   `json:"remediation,omitempty"`
}

// Scanner is one external analysis tool.
type Scanner interface {
	// Name is the scanner identifier (e.g. "semgrep").
	Name() string
	// Available reports whether the scanner binary is installed.
	Available() bool
	// Scan analyzes dir and returns normalized findings.
	Scan(ctx context.Context, dir string) ([]Finding, error)
}

// DefaultScanners returns the built-in filesystem scanners.
func DefaultScanners() []Scanner {
	return []Scanner{
		semgrepScanner{},
		trivyScanner{},
		gitleaksScanner{},
	}
}

// Report is the aggregated outcome of a scan run.
type Report struct {
	Findings  []Finding         `json:"findings"`
	Ran       []string          `json:"ran"`       // scanners that executed
	Skipped   map[string]string `json:"skipped"`   // scanner -> reason (not installed / error)
}

// RunAll executes every available scanner over dir and aggregates findings,
// sorted by severity (highest first).
func RunAll(ctx context.Context, dir string, scanners []Scanner) Report {
	rep := Report{Skipped: map[string]string{}}
	for _, sc := range scanners {
		if !sc.Available() {
			rep.Skipped[sc.Name()] = "not installed"
			continue
		}
		findings, err := sc.Scan(ctx, dir)
		if err != nil {
			rep.Skipped[sc.Name()] = "error: " + err.Error()
			continue
		}
		rep.Ran = append(rep.Ran, sc.Name())
		rep.Findings = append(rep.Findings, findings...)
	}
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		return rep.Findings[i].Severity.rank() > rep.Findings[j].Severity.rank()
	})
	return rep
}

// Format renders a report as human/model-readable text.
func (r Report) Format() string {
	var b strings.Builder
	if len(r.Ran) > 0 {
		fmt.Fprintf(&b, "Scanners run: %s\n", strings.Join(r.Ran, ", "))
	}
	if len(r.Skipped) > 0 {
		var parts []string
		for name, reason := range r.Skipped {
			parts = append(parts, fmt.Sprintf("%s (%s)", name, reason))
		}
		sort.Strings(parts)
		fmt.Fprintf(&b, "Scanners skipped: %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintf(&b, "Findings: %d\n", len(r.Findings))
	if len(r.Findings) == 0 {
		return strings.TrimSpace(b.String())
	}
	b.WriteString("\n")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "[%s] %s — %s\n  %s (%s)\n", f.Severity, f.Tool, f.Title, f.Location, f.RuleID)
		if f.Remediation != "" {
			fmt.Fprintf(&b, "  fix: %s\n", f.Remediation)
		}
	}
	return strings.TrimSpace(b.String())
}
