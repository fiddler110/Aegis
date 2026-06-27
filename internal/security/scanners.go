package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// lookPath reports whether a binary is on PATH.
func lookPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// runJSON runs a command and returns stdout. A non-zero exit is tolerated as
// long as output was produced, because scanners exit non-zero when they find
// issues.
func runJSON(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if len(out) == 0 && err != nil {
		return nil, err
	}
	return out, nil
}

func firstLine(s string) string {
	before, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(before)
}

// --- semgrep ---

type semgrepScanner struct{}

func (semgrepScanner) Name() string    { return "semgrep" }
func (semgrepScanner) Available() bool { return lookPath("semgrep") }
func (semgrepScanner) Scan(ctx context.Context, dir string) ([]Finding, error) {
	out, err := runJSON(ctx, dir, "semgrep", "--json", "--quiet", "--config", "auto", ".")
	if err != nil {
		return nil, err
	}
	return parseSemgrep(out)
}

func parseSemgrep(data []byte) ([]Finding, error) {
	var doc struct {
		Results []struct {
			CheckID string `json:"check_id"`
			Path    string `json:"path"`
			Start   struct {
				Line int `json:"line"`
			} `json:"start"`
			Extra struct {
				Message  string `json:"message"`
				Severity string `json:"severity"`
				Metadata struct {
					References []string `json:"references"`
				} `json:"metadata"`
			} `json:"extra"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse semgrep output: %w", err)
	}
	out := make([]Finding, 0, len(doc.Results))
	for _, r := range doc.Results {
		out = append(out, Finding{
			Tool:        "semgrep",
			RuleID:      r.CheckID,
			Severity:    normalizeSeverity(r.Extra.Severity),
			Title:       firstLine(r.Extra.Message),
			Location:    fmt.Sprintf("%s:%d", r.Path, r.Start.Line),
			Description: r.Extra.Message,
		})
	}
	return out, nil
}

// --- trivy ---

type trivyScanner struct{}

func (trivyScanner) Name() string    { return "trivy" }
func (trivyScanner) Available() bool { return lookPath("trivy") }
func (trivyScanner) Scan(ctx context.Context, dir string) ([]Finding, error) {
	out, err := runJSON(ctx, dir, "trivy", "fs", "--format", "json", "--quiet", ".")
	if err != nil {
		return nil, err
	}
	return parseTrivy(out)
}

func parseTrivy(data []byte) ([]Finding, error) {
	var doc struct {
		Results []struct {
			Target          string `json:"Target"`
			Vulnerabilities []struct {
				VulnerabilityID  string `json:"VulnerabilityID"`
				PkgName          string `json:"PkgName"`
				InstalledVersion string `json:"InstalledVersion"`
				FixedVersion     string `json:"FixedVersion"`
				Severity         string `json:"Severity"`
				Title            string `json:"Title"`
				Description      string `json:"Description"`
			} `json:"Vulnerabilities"`
			Misconfigurations []struct {
				ID         string `json:"ID"`
				Severity   string `json:"Severity"`
				Title      string `json:"Title"`
				Message    string `json:"Message"`
				Resolution string `json:"Resolution"`
			} `json:"Misconfigurations"`
			Secrets []struct {
				RuleID    string `json:"RuleID"`
				Severity  string `json:"Severity"`
				Title     string `json:"Title"`
				StartLine int    `json:"StartLine"`
			} `json:"Secrets"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse trivy output: %w", err)
	}
	var out []Finding
	for _, res := range doc.Results {
		for _, v := range res.Vulnerabilities {
			rem := ""
			if v.FixedVersion != "" {
				rem = fmt.Sprintf("upgrade %s to %s", v.PkgName, v.FixedVersion)
			}
			out = append(out, Finding{
				Tool:        "trivy",
				RuleID:      v.VulnerabilityID,
				Severity:    normalizeSeverity(v.Severity),
				Title:       firstNonEmpty(v.Title, v.VulnerabilityID),
				Location:    fmt.Sprintf("%s (%s %s)", res.Target, v.PkgName, v.InstalledVersion),
				Description: v.Description,
				Remediation: rem,
			})
		}
		for _, m := range res.Misconfigurations {
			out = append(out, Finding{
				Tool:        "trivy",
				RuleID:      m.ID,
				Severity:    normalizeSeverity(m.Severity),
				Title:       firstNonEmpty(m.Title, m.ID),
				Location:    res.Target,
				Description: m.Message,
				Remediation: m.Resolution,
			})
		}
		for _, s := range res.Secrets {
			out = append(out, Finding{
				Tool:     "trivy",
				RuleID:   s.RuleID,
				Severity: normalizeSeverity(firstNonEmpty(s.Severity, "HIGH")),
				Title:    firstNonEmpty(s.Title, "exposed secret"),
				Location: fmt.Sprintf("%s:%d", res.Target, s.StartLine),
			})
		}
	}
	return out, nil
}

// --- gitleaks ---

type gitleaksScanner struct{}

func (gitleaksScanner) Name() string    { return "gitleaks" }
func (gitleaksScanner) Available() bool { return lookPath("gitleaks") }
func (gitleaksScanner) Scan(ctx context.Context, dir string) ([]Finding, error) {
	report, err := os.CreateTemp("", "gitleaks-*.json")
	if err != nil {
		return nil, err
	}
	path := report.Name()
	report.Close()
	defer os.Remove(path)

	cmd := exec.CommandContext(ctx, "gitleaks", "detect", "--source", dir, "--no-git",
		"--report-format", "json", "--report-path", path, "--exit-code", "0")
	_ = cmd.Run() // gitleaks writes findings to the report file regardless

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseGitleaks(data)
}

func parseGitleaks(data []byte) ([]Finding, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, nil
	}
	var doc []struct {
		RuleID      string `json:"RuleID"`
		Description string `json:"Description"`
		File        string `json:"File"`
		StartLine   int    `json:"StartLine"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse gitleaks output: %w", err)
	}
	out := make([]Finding, 0, len(doc))
	for _, d := range doc {
		out = append(out, Finding{
			Tool:        "gitleaks",
			RuleID:      d.RuleID,
			Severity:    SevHigh, // leaked secrets are high severity by default
			Title:       firstNonEmpty(d.Description, "potential secret"),
			Location:    fmt.Sprintf("%s:%d", filepath.ToSlash(d.File), d.StartLine),
			Remediation: "rotate the exposed credential and remove it from the codebase",
		})
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
