package security

import "regexp"

// cweTagRe recognizes a CWE identifier in a SARIF rule tag or rule ID, in
// any of the common forms tools emit it: "CWE-79", "cwe:79", "cwe_79".
var cweTagRe = regexp.MustCompile(`(?i)cwe[-_: ]?(\d+)`)

// extractCWEFromText returns the bare CWE number (e.g. "79") found in s, or
// "" if none is present. Used against SARIF rule tags/IDs only — never a
// title or free-text description — so a Finding's CWE is exactly as
// trustworthy as whatever the scanner itself tagged the rule with.
func extractCWEFromText(s string) string {
	if m := cweTagRe.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	return ""
}

// cweASVS maps a curated set of common CWE IDs to an OWASP ASVS 4.0.3
// chapter/requirement label (P11.8). Deliberately not exhaustive: only CWEs
// confident enough to state as a specific, recognizable ASVS reference are
// included. Everything else is left unmapped — a wrong standards claim
// would be worse than none, the same posture already taken for
// Reachability. A known-vulnerable dependency (SCA findings, no CWE tag)
// has no dedicated ASVS chapter and is intentionally never guessed at here.
var cweASVS = map[string]string{
	"20":  "V5.1 Input Validation",
	"601": "V5.1 Input Validation",
	"79":  "V5.3 Output Encoding and Injection Prevention",
	"89":  "V5.3 Output Encoding and Injection Prevention",
	"78":  "V5.3 Output Encoding and Injection Prevention",
	"94":  "V5.3 Output Encoding and Injection Prevention",
	"90":  "V5.3 Output Encoding and Injection Prevention",
	"643": "V5.3 Output Encoding and Injection Prevention",
	"611": "V5.5 Deserialization Prevention",
	"502": "V5.5 Deserialization Prevention",
	"776": "V5.5 Deserialization Prevention",
	"22":  "V12.3 File Execution Requirements",
	"23":  "V12.3 File Execution Requirements",
	"434": "V12.4 File Storage Requirements",
	"918": "V12.6 SSRF Protection",
	"798": "V6.4 Secret Management",
	"259": "V6.4 Secret Management",
	"321": "V6.4 Secret Management",
	"327": "V6.2 Algorithms",
	"326": "V6.2 Algorithms",
	"330": "V6.3 Random Values",
	"338": "V6.3 Random Values",
	"862": "V4 Access Control",
	"863": "V4 Access Control",
	"285": "V4 Access Control",
	"639": "V4.3 Other Access Control Considerations",
	"287": "V2 Authentication",
	"306": "V2 Authentication",
	"307": "V2.2 General Authenticator Requirements",
	"384": "V3 Session Management",
	"613": "V3.3 Session Logout and Timeout",
	"352": "V4.2 Operation Level Access Control",
	"209": "V7 Error Handling and Logging",
	"532": "V7 Error Handling and Logging",
	"200": "V8 Data Protection",
	"311": "V9 Communications",
	"319": "V9 Communications",
	"295": "V9.2 Server Communications Security",
	"400": "V11 Business Logic (resource consumption)",
	"770": "V11 Business Logic (resource consumption)",
	"16":  "V14 Configuration",
}

// toolASVS is a fallback for the handful of scanners that flag real
// findings with no CWE to key off: gitleaks (secrets have no CWE tag in its
// own report format), the IaC/Dockerfile linters (misconfiguration by
// definition), and nmap (an open/exposed service is a configuration issue,
// not a CWE-tagged vulnerability).
var toolASVS = map[string]string{
	"gitleaks":   "V6.4 Secret Management",
	"trufflehog": "V6.4 Secret Management",
	"kubescape":  "V14 Configuration",
	"hadolint":   "V14 Configuration",
	"nmap":       "V14 Configuration",
}

// asvsFor derives a best-effort ASVS label for f: CWE-based mapping first
// (works for any SARIF-sourced finding across every SAST/DAST tool that
// tags CWE — opengrep/gosec/bandit/brakeman/njsscan/ZAP), then a
// tool-name fallback, then trivy's own misconfig-vs-CVE split (trivy shares
// one tool name across both SCA and IaC findings, distinguished here by
// whether its rule ID is a bare CVE). Returns "" when nothing matches —
// never guessed.
func asvsFor(f Finding) string {
	if f.CWE != "" {
		if label, ok := cweASVS[f.CWE]; ok {
			return label
		}
	}
	if f.Tool == "trivy" && !cveRe.MatchString(f.RuleID) {
		return "V14 Configuration"
	}
	if label, ok := toolASVS[f.Tool]; ok {
		return label
	}
	return ""
}

// assignASVS fills Finding.ASVS for every finding that doesn't already have
// one (P11.8), mutating in place.
func assignASVS(findings []Finding) {
	for i := range findings {
		if findings[i].ASVS == "" {
			findings[i].ASVS = asvsFor(findings[i])
		}
	}
}
