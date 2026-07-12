package security

import (
	"regexp"
	"strings"
)

// cveIDPattern matches a bare CVE identifier (e.g. "CVE-2021-44228") inside
// free-form note text, used by both the notebook digest and the suggestion
// rules below to detect "a finding was recorded" without requiring a tag.
var cveIDPattern = regexp.MustCompile(`(?i)cve-\d{4}-\d{4,}`)

// SuggestNextSteps applies simple, explainable rules over an engagement's
// notebook entries to produce guarded next-step suggestions (P13.4.3).
// "Guarded" means: this returns plain text only. It never calls another
// tool, never queues a scan, and never makes a second model call to decide
// what to suggest — every rule here is a direct, inspectable keyword check
// over notebook content, so a human (or the calling model) reading the
// output can see exactly why each suggestion fired and stays fully in the
// loop on whether to act on it.
//
// Each rule is independent and additive: more than one can fire for the
// same notebook (e.g. "no recon yet" and "no notes at all" can both be
// true), and callers get every one that applies rather than only the
// single highest-priority match, so a stale engagement gets a complete
// checklist instead of a single next step that goes stale itself.
func SuggestNextSteps(entries []NotebookEntry) []string {
	if len(entries) == 0 {
		return []string{
			"No notes yet for this engagement. Start with recon_scan (or manual scoping) to map the attack surface, then log what you find with security_advise(action=\"note\").",
		}
	}

	d := DigestNotebook("", entries)
	hay := notebookHaystack(entries)

	var out []string
	if d.ReconMentions == 0 {
		out = append(out, "No recon_scan (or nmap/nuclei) activity logged yet — run recon_scan to map the attack surface before forming hypotheses.")
	}
	if d.ReconMentions > 0 && d.DASTMentions == 0 && (strings.Contains(hay, "http://") || strings.Contains(hay, "https://") || strings.Contains(hay, "web app") || strings.Contains(hay, "webapp")) {
		out = append(out, "Recon found what looks like a running web application but no dast_scan has been logged — run dast_scan (baseline mode first) against it.")
	}
	if d.FindingHits > 0 && !strings.Contains(hay, "documented") && !strings.Contains(hay, "write_file") && !strings.Contains(hay, "report") {
		out = append(out, "Findings are referenced in the notebook but no note mentions documenting them — write them to the findings ledger/report before the engagement is considered done.")
	}
	if cveIDPattern.MatchString(hay) && d.CVELookups == 0 {
		out = append(out, "A CVE ID is mentioned in the notebook but no cve_lookup has been logged for it — run security_advise(action=\"cve_lookup\") to pull severity/remediation details.")
	}
	if d.ScanMentions == 0 && strings.Contains(hay, "source") {
		out = append(out, "Source code is referenced but no security_scan activity is logged — run security_scan over any in-scope repository.")
	}

	if len(out) == 0 {
		out = append(out, "No rule-based next step identified from the notebook alone — continue the current engagement phase and keep logging notes as you go.")
	}
	return out
}

// notebookHaystack lowercases and joins every note's text and tags into one
// string for the substring checks above.
func notebookHaystack(entries []NotebookEntry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(strings.ToLower(e.Text))
		b.WriteByte(' ')
		b.WriteString(strings.ToLower(strings.Join(e.Tags, " ")))
		b.WriteByte(' ')
	}
	return b.String()
}
