package security

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SARIF (Static Analysis Results Interchange Format) is the shared output
// format for semgrep, trivy, grype, checkov, hadolint, and ZAP (P11.2). One
// ingester here maps any tool's SARIF report onto Finding, so adding a new
// SARIF-emitting scanner needs no bespoke parser — only tools that don't
// speak SARIF (e.g. gitleaks) keep a hand-written one.

type sarifRule struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ShortDescription struct {
		Text string `json:"text"`
	} `json:"shortDescription"`
	Help struct {
		Text string `json:"text"`
	} `json:"help"`
	Properties struct {
		SecuritySeverity string   `json:"security-severity"`
		Tags             []string `json:"tags"`
	} `json:"properties"`
}

type sarifDoc struct {
	Runs []struct {
		Tool struct {
			Driver struct {
				Name  string      `json:"name"`
				Rules []sarifRule `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		Results []struct {
			RuleID    string `json:"ruleId"`
			RuleIndex *int   `json:"ruleIndex"`
			Level     string `json:"level"`
			Message   struct {
				Text string `json:"text"`
			} `json:"message"`
			Locations []struct {
				PhysicalLocation struct {
					ArtifactLocation struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region struct {
						StartLine int `json:"startLine"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

// ParseSARIF maps a SARIF report's results onto Finding. defaultTool names
// the finding when a run's tool.driver.name is absent.
func ParseSARIF(data []byte, defaultTool string) ([]Finding, error) {
	var doc sarifDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse sarif: %w", err)
	}
	var out []Finding
	for _, run := range doc.Runs {
		toolName := firstNonEmpty(run.Tool.Driver.Name, defaultTool)
		rulesByID := make(map[string]*sarifRule, len(run.Tool.Driver.Rules))
		for i := range run.Tool.Driver.Rules {
			r := &run.Tool.Driver.Rules[i]
			rulesByID[r.ID] = r
		}
		for _, res := range run.Results {
			rule := rulesByID[res.RuleID]
			if rule == nil && res.RuleIndex != nil && *res.RuleIndex >= 0 && *res.RuleIndex < len(run.Tool.Driver.Rules) {
				rule = &run.Tool.Driver.Rules[*res.RuleIndex]
			}

			ruleID := firstNonEmpty(res.RuleID, ruleIDOf(rule))
			title := firstLine(res.Message.Text)
			if title == "" && rule != nil {
				title = firstNonEmpty(rule.ShortDescription.Text, rule.Name, ruleID)
			}
			location := ""
			if len(res.Locations) > 0 {
				pl := res.Locations[0].PhysicalLocation
				if pl.Region.StartLine > 0 {
					location = fmt.Sprintf("%s:%d", pl.ArtifactLocation.URI, pl.Region.StartLine)
				} else {
					location = pl.ArtifactLocation.URI
				}
			}
			remediation := ""
			if rule != nil {
				remediation = rule.Help.Text
			}
			out = append(out, Finding{
				Tool:        toolName,
				RuleID:      ruleID,
				Severity:    sarifSeverity(res.Level, rule),
				Title:       title,
				Location:    location,
				Description: res.Message.Text,
				Remediation: remediation,
				CWE:         cweFromRule(rule, ruleID),
			})
		}
	}
	return out, nil
}

func ruleIDOf(r *sarifRule) string {
	if r == nil {
		return ""
	}
	return r.ID
}

// cweFromRule extracts a CWE ID (P11.8) from a SARIF rule's tags — the
// authoritative source when present — falling back to the rule ID itself
// (some engines, e.g. njsscan, encode "CWE-79" directly into the rule ID
// with no separate tag). Returns "" rather than guessing when neither has one.
func cweFromRule(rule *sarifRule, ruleID string) string {
	if rule != nil {
		for _, tag := range rule.Properties.Tags {
			if c := extractCWEFromText(tag); c != "" {
				return c
			}
		}
	}
	return extractCWEFromText(ruleID)
}

// sarifSeverity resolves a Finding's Severity from most to least specific:
// a recognized severity tag on the rule, then the rule's CVSS-like
// "security-severity" score, then the result's SARIF level.
func sarifSeverity(level string, rule *sarifRule) Severity {
	if rule != nil {
		for _, tag := range rule.Properties.Tags {
			if sev := severityFromTag(tag); sev != "" {
				return sev
			}
		}
		if rule.Properties.SecuritySeverity != "" {
			if score, err := strconv.ParseFloat(rule.Properties.SecuritySeverity, 64); err == nil {
				switch {
				case score >= 9:
					return SevCritical
				case score >= 7:
					return SevHigh
				case score >= 4:
					return SevMedium
				case score > 0:
					return SevLow
				}
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return SevHigh
	case "warning":
		return SevMedium
	case "note":
		return SevLow
	default:
		return SevInfo
	}
}

// severityFromTag recognizes an exact severity keyword among a rule's SARIF
// tags (e.g. trivy emits a bare "CRITICAL"/"HIGH" tag); it deliberately does
// not substring-match so an unrelated tag like "security" never misfires.
func severityFromTag(tag string) Severity {
	switch strings.ToUpper(strings.TrimSpace(tag)) {
	case "CRITICAL":
		return SevCritical
	case "HIGH":
		return SevHigh
	case "MEDIUM", "MODERATE":
		return SevMedium
	case "LOW":
		return SevLow
	case "INFO", "INFORMATIONAL":
		return SevInfo
	default:
		return ""
	}
}
