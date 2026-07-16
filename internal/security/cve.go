package security

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// This file implements P13.4's CVE lookup: query the NVD REST API by CVE ID
// or keyword. It follows the same outbound-HTTP shape internal/tool/builtin's
// web tools already use (context-scoped request, bounded body read, non-2xx
// treated as an error rather than a panic) — see internal/tool/builtin/web.go
// and websearch_providers.go's doSearchRequest. Unlike web_fetch, the target
// host is fixed (not attacker/model-supplied), so there's no SSRF surface
// here and no need for that package's private-IP-blocking dialer.

// nvdDefaultBaseURL is NVD's CVE 2.0 REST API. Overridable via
// CVEOptions.BaseURL for tests (see cve_test.go) and any future
// self-hosted mirror.
const nvdDefaultBaseURL = "https://services.nvd.nist.gov/rest/json/cves/2.0"

// nvdAPIKeyEnv is the optional environment variable carrying an NVD API key.
// Per this codebase's secrets convention (CLAUDE.md: "Secrets ... come only
// from the environment"), no config field exists for it — an unauthenticated
// caller is still fully functional, just rate-limited to NVD's public
// ~5 requests/30s tier instead of ~50/30s.
const nvdAPIKeyEnv = "NVD_API_KEY"

// CVEOptions is one cve_lookup call's parameters.
type CVEOptions struct {
	// CVEID looks up one specific CVE (e.g. "CVE-2021-44228"). Mutually
	// exclusive with Keyword and Product/Version.
	CVEID string
	// Keyword runs NVD's free-text keyword search against CVE titles/
	// descriptions. Mutually exclusive with CVEID and Product/Version. This
	// matches on prose, not on the affected product, so it's prone to
	// false-positive matches against an unrelated product that happens to
	// share vocabulary with the real one (see Product/Version below for the
	// precise alternative).
	Keyword string
	// Product and Version (both required together) run a CPE-based NVD
	// lookup (P34.4) instead of free text: they're folded into an NVD
	// "virtualMatchString" (cpe:2.3:*:*:<product>:<version>:*:*:*:*:*:*:*,
	// vendor left wildcarded since callers — e.g. an nmap service/version
	// banner — usually don't know it) so NVD matches against the actual
	// affected-product field of each CVE record rather than free-text
	// keyword search. Mutually exclusive with CVEID and Keyword.
	Product string
	Version string
	// Limit bounds keyword/CPE-search results (default/max enforced internally).
	Limit int

	// BaseURL overrides nvdDefaultBaseURL (tests only).
	BaseURL string
	// Client overrides the default HTTP client (tests only).
	Client *http.Client
	// APIKey overrides nvdAPIKeyEnv (tests only); production callers leave
	// this empty and rely on the environment variable.
	APIKey string
}

// CVERecord is one normalized NVD result.
type CVERecord struct {
	ID          string
	Description string
	Severity    string // e.g. "CRITICAL"; empty if NVD has no CVSS score yet
	BaseScore   float64
	Published   string
	References  []string
}

const (
	defaultCVELimit = 5
	maxCVELimit     = 20
	cveHTTPTimeout  = 20 * time.Second
)

// LookupCVE queries the NVD REST API for a specific CVE ID or a keyword
// search, returning normalized records. Rate limiting (NVD returns 403 or
// 429 once the unauthenticated caller exceeds ~5 requests/30s) is surfaced
// as a clear, typed-message error rather than a retry loop or a hang — the
// caller (the security_advise tool, ultimately the model or operator)
// decides whether to back off and retry, matching this tool's "guarded,
// human/model stays in the loop" design.
func LookupCVE(ctx context.Context, opts CVEOptions) ([]CVERecord, error) {
	cveID := strings.TrimSpace(opts.CVEID)
	keyword := strings.TrimSpace(opts.Keyword)
	product := strings.TrimSpace(opts.Product)
	version := strings.TrimSpace(opts.Version)
	hasCPE := product != "" || version != ""

	modes := 0
	for _, set := range []bool{cveID != "", keyword != "", hasCPE} {
		if set {
			modes++
		}
	}
	if modes == 0 {
		return nil, fmt.Errorf("cve_lookup requires one of cve_id, keyword, or product+version")
	}
	if modes > 1 {
		return nil, fmt.Errorf("cve_lookup takes exactly one of cve_id, keyword, or product+version")
	}
	if hasCPE && (product == "" || version == "") {
		return nil, fmt.Errorf("cve_lookup's product+version match requires both fields, got product=%q version=%q", product, version)
	}

	base := opts.BaseURL
	if base == "" {
		base = nvdDefaultBaseURL
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultCVELimit
	}
	if limit > maxCVELimit {
		limit = maxCVELimit
	}

	q := url.Values{}
	switch {
	case cveID != "":
		q.Set("cveId", strings.ToUpper(cveID))
	case hasCPE:
		q.Set("virtualMatchString", cpeVirtualMatchString(product, version))
		q.Set("resultsPerPage", strconv.Itoa(limit))
	default:
		q.Set("keywordSearch", keyword)
		q.Set("resultsPerPage", strconv.Itoa(limit))
	}
	endpoint := base + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv(nvdAPIKeyEnv))
	}
	if apiKey != "" {
		req.Header.Set("apiKey", apiKey)
	}

	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: cveHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nvd request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("nvd response read failed: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		msg := fmt.Sprintf("NVD API rate-limited or forbidden (status %d)", resp.StatusCode)
		if retry := resp.Header.Get("Retry-After"); retry != "" {
			msg += fmt.Sprintf("; retry after %s seconds", retry)
		}
		msg += ". The public NVD API allows ~5 requests/30s unauthenticated (~50/30s with a key) — wait and retry" +
			fmt.Sprintf(", or set %s for a higher limit", nvdAPIKeyEnv)
		return nil, fmt.Errorf("%s: %s", msg, clipText(string(body), 200))
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("nvd request failed: status %d: %s", resp.StatusCode, clipText(strings.TrimSpace(string(body)), 200))
	}

	var doc nvdResponse
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse nvd response: %w", err)
	}
	out := make([]CVERecord, 0, len(doc.Vulnerabilities))
	for _, v := range doc.Vulnerabilities {
		out = append(out, v.CVE.toRecord())
	}
	return out, nil
}

// cpeVirtualMatchString builds an NVD "virtualMatchString" from a product and
// version, wildcarding every other CPE 2.3 component (part, vendor, update,
// edition, language, sw_edition, target_sw, target_hw, other). Vendor is left
// wildcarded because the common caller (an nmap service/version banner, e.g.
// "Apache httpd 2.4.29") doesn't know it; NVD's virtualMatchString matching
// still resolves it against the product+version pair. Per CPE 2.3 naming
// convention, spaces become underscores and the string is lowercased.
func cpeVirtualMatchString(product, version string) string {
	norm := func(s string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", "_"))
	}
	return fmt.Sprintf("cpe:2.3:*:*:%s:%s:*:*:*:*:*:*:*", norm(product), norm(version))
}

// FormatCVEResults renders CVE lookup results as short human-readable text,
// the same plain-text convention Report.Format uses elsewhere in this
// package.
func FormatCVEResults(records []CVERecord) string {
	if len(records) == 0 {
		return "no CVE results found"
	}
	var b strings.Builder
	for _, r := range records {
		fmt.Fprintf(&b, "%s", r.ID)
		if r.Severity != "" {
			fmt.Fprintf(&b, " [%s", r.Severity)
			if r.BaseScore > 0 {
				fmt.Fprintf(&b, " %.1f", r.BaseScore)
			}
			b.WriteString("]")
		}
		if r.Published != "" {
			fmt.Fprintf(&b, " (published %s)", r.Published)
		}
		b.WriteString("\n")
		if r.Description != "" {
			fmt.Fprintf(&b, "  %s\n", clipText(r.Description, 400))
		}
		for i, ref := range r.References {
			if i >= 3 {
				fmt.Fprintf(&b, "  ...and %d more reference(s)\n", len(r.References)-i)
				break
			}
			fmt.Fprintf(&b, "  ref: %s\n", ref)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- NVD API 2.0 response shapes (minimal read: only fields this package surfaces) ---

type nvdResponse struct {
	Vulnerabilities []nvdVulnerability `json:"vulnerabilities"`
}

type nvdVulnerability struct {
	CVE nvdCVE `json:"cve"`
}

type nvdCVE struct {
	ID           string           `json:"id"`
	Published    string           `json:"published"`
	Descriptions []nvdDescription `json:"descriptions"`
	Metrics      nvdMetrics       `json:"metrics"`
	References   []nvdReference   `json:"references"`
}

type nvdDescription struct {
	Lang  string `json:"lang"`
	Value string `json:"value"`
}

type nvdReference struct {
	URL string `json:"url"`
}

// nvdMetrics carries whichever CVSS version NVD scored the CVE with;
// v3.1 is preferred, falling back through v3.0 then v2 when that's all
// that's available (common for older CVEs).
type nvdMetrics struct {
	CvssMetricV31 []nvdCvssMetric `json:"cvssMetricV31"`
	CvssMetricV30 []nvdCvssMetric `json:"cvssMetricV30"`
	CvssMetricV2  []nvdCvssMetric `json:"cvssMetricV2"`
}

type nvdCvssMetric struct {
	CvssData struct {
		BaseScore    float64 `json:"baseScore"`
		BaseSeverity string  `json:"baseSeverity"`
	} `json:"cvssData"`
	// v2 metrics carry severity as a sibling field, not inside cvssData.
	BaseSeverity string `json:"baseSeverity"`
}

func (c nvdCVE) toRecord() CVERecord {
	r := CVERecord{ID: c.ID, Published: c.Published}
	for _, d := range c.Descriptions {
		if d.Lang == "en" {
			r.Description = d.Value
			break
		}
	}
	if r.Description == "" && len(c.Descriptions) > 0 {
		r.Description = c.Descriptions[0].Value
	}
	switch {
	case len(c.Metrics.CvssMetricV31) > 0:
		m := c.Metrics.CvssMetricV31[0].CvssData
		r.Severity, r.BaseScore = m.BaseSeverity, m.BaseScore
	case len(c.Metrics.CvssMetricV30) > 0:
		m := c.Metrics.CvssMetricV30[0].CvssData
		r.Severity, r.BaseScore = m.BaseSeverity, m.BaseScore
	case len(c.Metrics.CvssMetricV2) > 0:
		m := c.Metrics.CvssMetricV2[0]
		r.Severity, r.BaseScore = m.BaseSeverity, m.CvssData.BaseScore
	}
	for _, ref := range c.References {
		if ref.URL != "" {
			r.References = append(r.References, ref.URL)
		}
	}
	return r
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
