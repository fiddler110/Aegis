// Package security integrates external security scanners behind a single
// normalized findings model, so the agent (acting as a security platform
// architect) can identify issues uniformly regardless of the underlying tool.
package security

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	// Resolve reports how this scanner would run right now under opts:
	// MethodHost (its binary is on PATH and policy allows it), MethodContainer
	// (a container runtime is available and policy allows/requires it), or
	// MethodNone with a human-readable reason — never a silent skip (P11.1).
	// image is only meaningful when method == MethodContainer.
	Resolve(ctx context.Context, opts Options) (method Method, runtime sandbox.ContainerRuntime, image string, reason string)
	// Scan analyzes dir using the method/runtime/image Resolve returned and
	// returns normalized findings.
	Scan(ctx context.Context, dir string, method Method, runtime sandbox.ContainerRuntime, image string) ([]Finding, error)
}

// DefaultScanners returns the built-in filesystem scanners.
func DefaultScanners() []Scanner {
	return []Scanner{
		semgrepScanner{},
		trivyScanner{},
		gitleaksScanner{},
		kubescapeScanner{},
		hadolintScanner{},
	}
}

// Report is the aggregated outcome of a scan run.
type Report struct {
	Findings []Finding         `json:"findings"`
	Ran      []string          `json:"ran"`     // scanners that executed
	RanVia   map[string]string `json:"ran_via"` // scanner -> "host" or "container" (P11.1)
	Skipped  map[string]string `json:"skipped"` // scanner -> reason (disabled / unavailable / error)
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
	rep := newReport()
	for _, sc := range scanners {
		method, rt, image, reason := sc.Resolve(ctx, opts)
		if method == MethodNone {
			rep.Skipped[sc.Name()] = reason
			continue
		}
		findings, err := sc.Scan(ctx, dir, method, rt, image)
		rep.record(sc.Name(), method, findings, err)
	}
	rep.sortFindings()
	return rep
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
		return r.Findings[i].Severity.rank() > r.Findings[j].Severity.rank()
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
