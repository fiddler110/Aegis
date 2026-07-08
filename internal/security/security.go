// Package security integrates external security scanners behind a single
// normalized findings model, so the agent (acting as a security platform
// architect) can identify issues uniformly regardless of the underlying tool.
package security

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
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

// Reachability classifies whether a vulnerable dependency's flagged code is
// actually invoked by the project, for the handful of tools that can tell
// (currently: osv-scanner's --call-analysis, govulncheck-backed for Go and
// experimental for Rust/Java). Left at ReachabilityUnknown for every other
// tool/ecosystem rather than guessed — a wrong "unreachable" claim would
// understate real risk, which is worse than saying nothing.
type Reachability string

const (
	ReachabilityUnknown     Reachability = ""            // not analyzed — most scanners/ecosystems today
	ReachabilityReachable   Reachability = "reachable"   // call-graph analysis found an actual call path to the vulnerable code
	ReachabilityUnreachable Reachability = "unreachable" // call-graph analysis confirms the code is present but never invoked
)

// weight orders Reachability as a severity tiebreaker: confirmed-reachable
// findings should surface above unanalyzed ones, which in turn surface above
// confirmed-unreachable ones, when severity is otherwise equal.
func (r Reachability) weight() int {
	switch r {
	case ReachabilityReachable:
		return 2
	case ReachabilityUnreachable:
		return 0
	default:
		return 1
	}
}

// Verification classifies whether a detected secret was confirmed still
// active via a live provider-API check (P13.2: currently only trufflehog,
// gated behind security.tools.trufflehog.verify since it makes real calls to
// third-party services using the discovered credential). Left at
// VerificationUnknown for every other tool/finding rather than guessed —
// same posture as Reachability: a wrong claim either way is worse than none.
type Verification string

const (
	VerificationUnknown    Verification = ""           // not checked — every tool except trufflehog with verify:true
	VerificationVerified   Verification = "verified"   // confirmed active against the real provider API
	VerificationUnverified Verification = "unverified" // checked and confirmed inactive/invalid
)

// Finding is a single normalized security issue.
type Finding struct {
	Tool         string       `json:"tool"`
	RuleID       string       `json:"rule_id"`
	Severity     Severity     `json:"severity"`
	Title        string       `json:"title"`
	Location     string       `json:"location"` // file:line or package/target
	Description  string       `json:"description,omitempty"`
	Remediation  string       `json:"remediation,omitempty"`
	Reachability Reachability `json:"reachability,omitempty"`
	// Verification is set only for a secret-detection finding whose tool
	// performed live verification (P13.2) — empty (VerificationUnknown) for
	// every other finding, never guessed.
	Verification Verification `json:"verification,omitempty"`
	// CWE is the numeric CWE ID (e.g. "79") extracted from a SARIF rule's
	// tags/description when present (P11.8) — never guessed from a title,
	// only ever a value the tool itself tagged the rule with. Feeds ASVS.
	CWE string `json:"cwe,omitempty"`
	// ASVS is a best-effort OWASP ASVS chapter/requirement label derived
	// from CWE (or, for a few CWE-less tools, from the tool itself) so a
	// report reads against a recognized standard instead of raw tool IDs
	// (P11.8). Left empty whenever there's no confident mapping — a wrong
	// standards claim is worse than none, same posture as Reachability.
	ASVS string `json:"asvs,omitempty"`
	// SeenBy lists every tool that independently reported this same finding
	// (same normalized rule/CVE + location) when more than one did — set
	// only on the deduped survivor, by DedupFindings (P11.8).
	SeenBy []string `json:"seen_by,omitempty"`
}

// Scanner is one external analysis tool.
type Scanner interface {
	// Name is the scanner identifier (e.g. "semgrep").
	Name() string
	// Resolve reports how this scanner would run right now under opts:
	// MethodHost (its binary is on PATH and policy allows it), MethodContainer
	// (a container runtime is available and policy allows/requires it), or
	// MethodNone with a human-readable reason — never a silent skip (P11.1).
	// image is only meaningful when method == MethodContainer.
	Resolve(ctx context.Context, opts Options) (method Method, runtime sandbox.ContainerRuntime, image string, reason string)
	// Scan analyzes dir using the method/runtime/image Resolve returned and
	// returns normalized findings. opts is the same per-tool policy passed to
	// RunWithOptions, threaded through for scanners (grype) that themselves
	// resolve a second tool (syft) internally.
	Scan(ctx context.Context, dir string, method Method, runtime sandbox.ContainerRuntime, image string, opts Options) ([]Finding, error)
}

// DefaultScanners returns the built-in filesystem scanners. opengrep is the
// default SAST engine (P11.3); semgrep and the four language-targeted
// engines (gosec/bandit/brakeman/njsscan) are included too but resolve to
// MethodNone ("opt-in tool, not enabled by default") unless explicitly
// enabled via security.tools.<name>.enabled — listed here rather than
// omitted so `aegis security status`/`/security-config` can always discover
// and toggle them, matching P11.1's "never a silent skip" posture.
func DefaultScanners() []Scanner {
	return []Scanner{
		opengrepScanner{},
		semgrepScanner{},
		gosecScanner{},
		banditScanner{},
		brakemanScanner{},
		njsscanScanner{},
		trivyScanner{},
		gitleaksScanner{},
		trufflehogScanner{},
		kubescapeScanner{},
		hadolintScanner{},
		osvScanner{},
		grypeDirScanner{},
	}
}

// Report is the aggregated outcome of a scan run.
type Report struct {
	Findings []Finding         `json:"findings"`
	Ran      []string          `json:"ran"`     // scanners that executed
	RanVia   map[string]string `json:"ran_via"` // scanner -> "host" or "container" (P11.1)
	Skipped  map[string]string `json:"skipped"` // scanner -> reason (disabled / unavailable / error)

	// Suppressed holds findings matched by an active .aegis/security-baseline.yaml
	// entry (P11.8) — never dropped outright, so a report always shows what's
	// being hidden and why rather than a silent omission.
	Suppressed []Finding `json:"suppressed,omitempty"`
	// ExpiredSuppressions/InvalidSuppressions describe baseline entries that
	// did NOT suppress anything this run: expired ones (past their `expires`
	// date — the finding they used to cover is back in Findings) and invalid
	// ones (missing rule_id/reason or an unparseable expires date, so they
	// were never applied at all). Rendered so a stale/malformed baseline is
	// visible instead of quietly doing nothing.
	ExpiredSuppressions []string `json:"expired_suppressions,omitempty"`
	InvalidSuppressions []string `json:"invalid_suppressions,omitempty"`
	// BaselineError is set if .aegis/security-baseline.yaml exists but failed
	// to parse; suppression is skipped entirely in that case (fail safe —
	// nothing is ever hidden by a baseline Aegis couldn't understand).
	BaselineError string `json:"baseline_error,omitempty"`
}

// RunAll executes every available scanner over dir (using opts' per-tool
// policy to resolve host vs container execution) and aggregates findings,
// sorted by severity (highest first). RunWithOptions is the same with
// explicit Options; RunAll keeps the pre-P11.1 zero-value call signature.
func RunAll(ctx context.Context, dir string, scanners []Scanner) Report {
	return RunWithOptions(ctx, dir, scanners, Options{})
}

// RunWithOptions is RunAll with explicit per-tool policy (P11.11).
func RunWithOptions(ctx context.Context, dir string, scanners []Scanner, opts Options) Report {
	return RunWithProgress(ctx, dir, scanners, opts, nil)
}

// ScanPhase marks where a ScanEvent falls in a single scanner's lifecycle.
type ScanPhase string

const (
	PhaseSkipped ScanPhase = "skipped" // Resolve returned MethodNone; Reason explains why
	PhaseStart   ScanPhase = "start"   // Scan is about to run
	PhaseDone    ScanPhase = "done"    // Scan returned (Err set on failure)
)

// ScanEvent is emitted by RunWithProgress before/after each scanner runs, so
// a caller (aegis scan) can print live status instead of waiting silently for
// the whole run to finish — RunWithOptions previously gave no signal at all
// until every scanner (SAST/SCA/secrets/etc., each its own subprocess) had
// completed, which reads as a hang on a large repo.
type ScanEvent struct {
	Scanner  string
	Phase    ScanPhase
	Method   Method        // meaningful for PhaseStart/PhaseDone
	Reason   string        // meaningful for PhaseSkipped
	Findings int           // meaningful for PhaseDone
	Err      error         // meaningful for PhaseDone
	Elapsed  time.Duration // meaningful for PhaseDone
}

// RunWithProgress is RunWithOptions with an optional callback invoked before
// and after each scanner runs. progress may be nil (equivalent to
// RunWithOptions); scanners still run strictly sequentially, so events arrive
// in a deterministic, non-overlapping order.
func RunWithProgress(ctx context.Context, dir string, scanners []Scanner, opts Options, progress func(ScanEvent)) Report {
	rep := newReport()
	for _, sc := range scanners {
		method, rt, image, reason := sc.Resolve(ctx, opts)
		if method == MethodNone {
			rep.Skipped[sc.Name()] = reason
			if progress != nil {
				progress(ScanEvent{Scanner: sc.Name(), Phase: PhaseSkipped, Reason: reason})
			}
			continue
		}
		if progress != nil {
			progress(ScanEvent{Scanner: sc.Name(), Phase: PhaseStart, Method: method})
		}
		start := time.Now()
		findings, err := sc.Scan(ctx, dir, method, rt, image, opts)
		rep.record(sc.Name(), method, findings, err)
		if progress != nil {
			progress(ScanEvent{Scanner: sc.Name(), Phase: PhaseDone, Method: method, Findings: len(findings), Err: err, Elapsed: time.Since(start)})
		}
	}
	// Dedup and ASVS-tag before baseline matching, so a suppression entry
	// matches the same (rule/CVE, location) key a reader would see reported
	// once, not once per tool that happened to flag it (P11.8).
	rep.Findings = DedupFindings(rep.Findings)
	assignASVS(rep.Findings)
	rep.applyBaseline(dir)
	rep.sortFindings()
	return rep
}

// applyBaseline loads dir's .aegis/security-baseline.yaml (if any) and moves
// findings matched by an active entry into Suppressed (P11.8). A missing
// file is the common case and does nothing; a malformed one fails safe —
// BaselineError is set and no findings are suppressed, rather than risking a
// broken baseline silently hiding real findings.
func (r *Report) applyBaseline(dir string) {
	b, err := LoadBaseline(dir)
	if err != nil {
		r.BaselineError = err.Error()
		return
	}
	if b == nil {
		return
	}
	kept, suppressed, expired, invalid := b.Apply(r.Findings, time.Now())
	r.Findings = kept
	r.Suppressed = suppressed
	for _, e := range expired {
		r.ExpiredSuppressions = append(r.ExpiredSuppressions, formatSuppressionEntry(e)+" — EXPIRED, no longer suppressed")
	}
	for _, e := range invalid {
		r.InvalidSuppressions = append(r.InvalidSuppressions, formatSuppressionEntry(e)+" — invalid (needs rule_id, reason, and a valid expires: YYYY-MM-DD), not applied")
	}
}

func newReport() Report {
	return Report{Skipped: map[string]string{}, RanVia: map[string]string{}}
}

// record accounts one scanner's outcome into the report: an error is treated
// as a skip (with the error as the reason), otherwise the scanner is marked
// ran (via method) and its findings appended. Shared between RunWithOptions
// and ScanImage so both aggregate/report identically.
func (r *Report) record(name string, method Method, findings []Finding, err error) {
	if err != nil {
		r.Skipped[name] = "error: " + err.Error()
		return
	}
	r.Ran = append(r.Ran, name)
	r.RanVia[name] = string(method)
	r.Findings = append(r.Findings, findings...)
}

func (r *Report) sortFindings() {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		si, sj := r.Findings[i].Severity.rank(), r.Findings[j].Severity.rank()
		if si != sj {
			return si > sj
		}
		return r.Findings[i].Reachability.weight() > r.Findings[j].Reachability.weight()
	})
}

// imageContainerFallbackUnsupported explains why ScanImage skips a tool
// that Resolve would otherwise run via a container: pulling/inspecting an
// image needs registry network egress, which the source-scanning container
// runner deliberately denies (--network none, P11.1's hardening posture).
// Extending that runner with a network-enabled exception for image scans is
// tracked as P11.5 follow-up; until then, image scanning only runs via a
// natively installed host binary.
const imageContainerFallbackUnsupported = "container fallback for image scanning is not yet supported (it would need a network-egress exception to the scanner-container hardening posture) — install this tool natively instead"

// ImageScanner is one container-image analysis tool (P11.5): unlike
// Scanner, which analyzes a source directory, an ImageScanner analyzes a
// built image by reference (e.g. "alpine:3.20" or a registry ref).
type ImageScanner interface {
	Name() string
	Resolve(ctx context.Context, opts Options) (method Method, runtime sandbox.ContainerRuntime, image string, reason string)
	// ScanImage analyzes ref using method (always MethodHost — see
	// ScanImage's doc comment) and returns normalized findings.
	ScanImage(ctx context.Context, ref string, method Method) ([]Finding, error)
}

// DefaultImageScanners returns the built-in container-image scanners.
func DefaultImageScanners() []ImageScanner {
	return []ImageScanner{
		trivyImageScanner{},
		grypeScanner{},
		dockleScanner{},
	}
}

// ScanImage runs every available ImageScanner against ref and aggregates
// findings, sorted by severity. Image scanning is host-binary only for now
// (see imageContainerFallbackUnsupported): a scanner that Resolve would run
// via a container is reported skipped with that reason rather than silently
// scanning through a container runtime that has no network access to pull
// the image.
func ScanImage(ctx context.Context, ref string, scanners []ImageScanner, opts Options) Report {
	rep := newReport()
	for _, sc := range scanners {
		method, _, _, reason := sc.Resolve(ctx, opts)
		if method == MethodNone {
			rep.Skipped[sc.Name()] = reason
			continue
		}
		if method == MethodContainer {
			rep.Skipped[sc.Name()] = imageContainerFallbackUnsupported
			continue
		}
		findings, err := sc.ScanImage(ctx, ref, method)
		rep.record(sc.Name(), method, findings, err)
	}
	// Image scans have no natural directory root to load a baseline from
	// (an image reference isn't a project checkout), so P11.8's suppression
	// baseline is scoped to RunWithOptions only — same scoping call already
	// made for image scanning's container fallback (see
	// imageContainerFallbackUnsupported above). Dedup/ASVS still apply: it's
	// common for trivy/grype/dockle to all flag the same image CVE.
	rep.Findings = DedupFindings(rep.Findings)
	assignASVS(rep.Findings)
	rep.sortFindings()
	return rep
}

// Format renders a report as human/model-readable text.
func (r Report) Format() string {
	var b strings.Builder
	if len(r.Ran) > 0 {
		names := make([]string, len(r.Ran))
		for i, n := range r.Ran {
			via := r.RanVia[n]
			if via != "" {
				names[i] = fmt.Sprintf("%s (%s)", n, via)
			} else {
				names[i] = n
			}
		}
		fmt.Fprintf(&b, "Scanners run: %s\n", strings.Join(names, ", "))
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
	if r.BaselineError != "" {
		fmt.Fprintf(&b, "Baseline error (no suppressions applied): %s\n", r.BaselineError)
	}
	if len(r.Suppressed) > 0 {
		fmt.Fprintf(&b, "Suppressed by baseline: %d\n", len(r.Suppressed))
	}
	for _, e := range r.ExpiredSuppressions {
		fmt.Fprintf(&b, "Baseline entry %s\n", e)
	}
	for _, e := range r.InvalidSuppressions {
		fmt.Fprintf(&b, "Baseline entry %s\n", e)
	}
	if len(r.Findings) == 0 {
		return strings.TrimSpace(b.String())
	}
	b.WriteString("\n")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "[%s]%s%s %s — %s\n  %s (%s)%s\n", f.Severity, reachabilityTag(f.Reachability), verificationTag(f.Verification), f.Tool, f.Title, f.Location, f.RuleID, seenByTag(f.SeenBy))
		if f.ASVS != "" {
			fmt.Fprintf(&b, "  asvs: %s\n", f.ASVS)
		}
		if f.Remediation != "" {
			fmt.Fprintf(&b, "  fix: %s\n", f.Remediation)
		}
	}
	return strings.TrimSpace(b.String())
}

// seenByTag renders a short suffix noting every other tool that
// independently flagged the same deduped finding (P11.8), empty otherwise.
func seenByTag(seenBy []string) string {
	if len(seenBy) == 0 {
		return ""
	}
	return fmt.Sprintf(" [also flagged by: %s]", strings.Join(seenBy, ", "))
}

// reachabilityTag renders a short suffix for the one severity/tool line in
// Format when reachability was actually analyzed; empty (ReachabilityUnknown)
// findings render exactly as before — no tag, no implied claim either way.
func reachabilityTag(r Reachability) string {
	switch r {
	case ReachabilityReachable:
		return " [reachable: called by this project]"
	case ReachabilityUnreachable:
		return " [not reachable: present but never called]"
	default:
		return ""
	}
}

// verificationTag renders a short suffix for a secret finding that was
// checked against the real provider API (P13.2); empty (VerificationUnknown)
// for every finding that wasn't — no tag, no implied claim either way. A
// [VERIFIED] tag is meant to be visually hard to miss: it's the signal an
// operator should weigh heavily before baseline-suppressing the finding.
func verificationTag(v Verification) string {
	switch v {
	case VerificationVerified:
		return " [VERIFIED: confirmed active credential]"
	case VerificationUnverified:
		return " [checked: not currently active]"
	default:
		return ""
	}
}
