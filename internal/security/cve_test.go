package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// All tests in this file point LookupCVE at an httptest server via
// CVEOptions.BaseURL — no live network calls, per the task's requirement.

func TestLookupCVEByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("cveId"); got != "CVE-2021-44228" {
			t.Errorf("cveId query param = %q, want CVE-2021-44228", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"vulnerabilities": [
				{
					"cve": {
						"id": "CVE-2021-44228",
						"published": "2021-12-10T10:15:09.143",
						"descriptions": [{"lang": "en", "value": "Apache Log4j2 JNDI features do not protect against attacker controlled LDAP."}],
						"metrics": {
							"cvssMetricV31": [{"cvssData": {"baseScore": 10.0, "baseSeverity": "CRITICAL"}}]
						},
						"references": [{"url": "https://logging.apache.org/log4j/2.x/security.html"}]
					}
				}
			]
		}`))
	}))
	defer srv.Close()

	records, err := LookupCVE(context.Background(), CVEOptions{CVEID: "CVE-2021-44228", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("LookupCVE: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	r := records[0]
	if r.ID != "CVE-2021-44228" {
		t.Errorf("ID = %q", r.ID)
	}
	if r.Severity != "CRITICAL" || r.BaseScore != 10.0 {
		t.Errorf("Severity/BaseScore = %q/%v, want CRITICAL/10.0", r.Severity, r.BaseScore)
	}
	if !strings.Contains(r.Description, "Log4j2") {
		t.Errorf("Description = %q", r.Description)
	}
	if len(r.References) != 1 {
		t.Errorf("References = %v", r.References)
	}

	formatted := FormatCVEResults(records)
	if !strings.Contains(formatted, "CVE-2021-44228") || !strings.Contains(formatted, "CRITICAL") {
		t.Errorf("FormatCVEResults output missing expected content: %s", formatted)
	}
}

func TestLookupCVEKeywordSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("keywordSearch"); got != "log4j" {
			t.Errorf("keywordSearch query param = %q, want log4j", got)
		}
		if got := r.URL.Query().Get("resultsPerPage"); got != "3" {
			t.Errorf("resultsPerPage = %q, want 3", got)
		}
		w.Write([]byte(`{"vulnerabilities": [
			{"cve": {"id": "CVE-2021-44228", "descriptions": [{"lang":"en","value":"log4j rce"}]}},
			{"cve": {"id": "CVE-2021-45046", "descriptions": [{"lang":"en","value":"log4j second issue"}]}}
		]}`))
	}))
	defer srv.Close()

	records, err := LookupCVE(context.Background(), CVEOptions{Keyword: "log4j", Limit: 3, BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("LookupCVE: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
}

func TestLookupCVERateLimitedReturnsClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("rate limit exceeded"))
	}))
	defer srv.Close()

	_, err := LookupCVE(context.Background(), CVEOptions{CVEID: "CVE-2021-44228", BaseURL: srv.URL})
	if err == nil {
		t.Fatal("expected an error for a 403 response")
	}
	if !strings.Contains(err.Error(), "rate-limited") {
		t.Errorf("error = %v, want a rate-limit-flavored message", err)
	}
}

func TestLookupCVETooManyRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := LookupCVE(context.Background(), CVEOptions{CVEID: "CVE-2021-44228", BaseURL: srv.URL})
	if err == nil {
		t.Fatal("expected an error for a 429 response")
	}
}

func TestLookupCVEServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	_, err := LookupCVE(context.Background(), CVEOptions{CVEID: "CVE-2021-44228", BaseURL: srv.URL})
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention status 500", err)
	}
}

func TestLookupCVERequiresIDOrKeyword(t *testing.T) {
	if _, err := LookupCVE(context.Background(), CVEOptions{}); err == nil {
		t.Error("expected an error when neither cve_id nor keyword is set")
	}
}

func TestLookupCVERejectsBothIDAndKeyword(t *testing.T) {
	if _, err := LookupCVE(context.Background(), CVEOptions{CVEID: "CVE-2021-44228", Keyword: "log4j"}); err == nil {
		t.Error("expected an error when both cve_id and keyword are set")
	}
}

func TestLookupCVESendsAPIKeyHeaderWhenSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("apiKey"); got != "test-key-123" {
			t.Errorf("apiKey header = %q, want test-key-123", got)
		}
		w.Write([]byte(`{"vulnerabilities": []}`))
	}))
	defer srv.Close()

	if _, err := LookupCVE(context.Background(), CVEOptions{CVEID: "CVE-2021-44228", BaseURL: srv.URL, APIKey: "test-key-123"}); err != nil {
		t.Fatalf("LookupCVE: %v", err)
	}
}

func TestFormatCVEResultsEmpty(t *testing.T) {
	if got := FormatCVEResults(nil); !strings.Contains(got, "no CVE results") {
		t.Errorf("FormatCVEResults(nil) = %q", got)
	}
}
