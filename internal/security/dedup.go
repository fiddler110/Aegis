package security

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// cveRe extracts a CVE identifier embedded anywhere in a rule ID string —
// osv-scanner joins alias IDs as "CVE-2023-1234, GHSA-xxxx-yyyy-zzzz", and
// this lets that match trivy/grype's bare "CVE-2023-1234" RuleID.
var cveRe = regexp.MustCompile(`(?i)CVE-\d{4}-\d+`)

// normalizeRuleID reduces a Finding's RuleID to a dedup-comparable form: the
// embedded CVE if present (uppercased, so case never causes a miss), else
// the trimmed/lowercased raw rule ID.
func normalizeRuleID(ruleID string) string {
	if m := cveRe.FindString(ruleID); m != "" {
		return strings.ToUpper(m)
	}
	return strings.ToLower(strings.TrimSpace(ruleID))
}

// normalizeLocation reduces a Finding's Location to a dedup-comparable form:
// SCA tools often render "pkg@version (path)" (osv-scanner, grype's SBOM
// path) while SARIF-based tools render a bare "path" or "path:line" — pull
// the trailing parenthetical path out when present so both forms compare
// equal, then drop any trailing ":<line>" and normalize slashes/case.
func normalizeLocation(loc string) string {
	loc = strings.TrimSpace(loc)
	if start := strings.LastIndex(loc, "("); start != -1 && strings.HasSuffix(loc, ")") {
		if inner := strings.TrimSpace(loc[start+1 : len(loc)-1]); inner != "" {
			loc = inner
		}
	}
	loc = filepath.ToSlash(loc)
	if idx := strings.LastIndex(loc, ":"); idx != -1 {
		if _, err := strconv.Atoi(loc[idx+1:]); err == nil {
			loc = loc[:idx]
		}
	}
	return strings.ToLower(loc)
}

// dedupKey is the (rule/CVE, location) pair overlapping tools are compared
// on. A Finding missing either half never merges with anything else — two
// findings both lacking a rule ID or location aren't safely comparable, and
// silently collapsing them risks losing a real, distinct finding.
func dedupKey(f Finding, uniq int) string {
	ruleID, loc := normalizeRuleID(f.RuleID), normalizeLocation(f.Location)
	if ruleID == "" || loc == "" {
		return fmt.Sprintf("__unique_%d", uniq)
	}
	return ruleID + "|" + loc
}

// DedupFindings collapses findings that share a normalized (CVE/rule-id,
// location) key — the common case being the same CVE independently flagged
// by multiple SCA tools (osv-scanner, grype, trivy) — into one, keeping the
// highest-severity (then highest-reachability) copy and recording every
// other tool that also flagged it on Finding.SeenBy (P11.8). Order of first
// appearance is preserved; sortFindings runs after this.
func DedupFindings(findings []Finding) []Finding {
	type group struct {
		keep  Finding
		tools map[string]bool
	}
	order := make([]string, 0, len(findings))
	groups := make(map[string]*group, len(findings))
	for i, f := range findings {
		key := dedupKey(f, i)
		g, ok := groups[key]
		if !ok {
			groups[key] = &group{keep: f, tools: map[string]bool{f.Tool: true}}
			order = append(order, key)
			continue
		}
		g.tools[f.Tool] = true
		if betterFinding(f, g.keep) {
			g.keep = f
		}
	}
	out := make([]Finding, 0, len(order))
	for _, key := range order {
		g := groups[key]
		f := g.keep
		if len(g.tools) > 1 {
			var others []string
			for t := range g.tools {
				if t != f.Tool {
					others = append(others, t)
				}
			}
			sort.Strings(others)
			f.SeenBy = others
		}
		out = append(out, f)
	}
	return out
}

// betterFinding reports whether candidate should replace current as a
// group's kept representative: higher severity wins, then higher
// reachability confidence; ties keep whichever was seen first (deterministic
// across runs since DedupFindings iterates findings in order).
func betterFinding(candidate, current Finding) bool {
	if candidate.Severity.rank() != current.Severity.rank() {
		return candidate.Severity.rank() > current.Severity.rank()
	}
	if candidate.Reachability.weight() != current.Reachability.weight() {
		return candidate.Reachability.weight() > current.Reachability.weight()
	}
	return false
}
