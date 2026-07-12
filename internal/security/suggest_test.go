package security

import (
	"strings"
	"testing"
)

func TestSuggestNextSteps(t *testing.T) {
	contains := func(suggestions []string, substr string) bool {
		for _, s := range suggestions {
			if strings.Contains(s, substr) {
				return true
			}
		}
		return false
	}

	cases := []struct {
		name    string
		entries []NotebookEntry
		want    string // a substring expected somewhere in the suggestions
	}{
		{
			name:    "empty notebook suggests starting recon",
			entries: nil,
			want:    "recon_scan",
		},
		{
			name: "no recon logged yet suggests recon_scan",
			entries: []NotebookEntry{
				{Text: "kicked off engagement, scope confirmed with client"},
			},
			want: "run recon_scan",
		},
		{
			name: "recon done, web app found, no dast logged suggests dast_scan",
			entries: []NotebookEntry{
				{Text: "recon_scan found a running web app at https://app.lab.internal"},
			},
			want: "dast_scan",
		},
		{
			name: "findings present with no documentation note suggests documenting",
			entries: []NotebookEntry{
				{Text: "recon_scan complete"},
				{Text: "nuclei flagged CVE-2023-12345 as a finding on the jenkins box"},
			},
			want: "documenting",
		},
		{
			name: "findings already documented does not repeat the documentation suggestion",
			entries: []NotebookEntry{
				{Text: "recon_scan complete"},
				{Text: "documented the CVE-2023-12345 finding in the report via write_file"},
			},
			want: "", // checked separately below
		},
		{
			name: "cve mentioned with no cve_lookup logged suggests cve_lookup",
			entries: []NotebookEntry{
				{Text: "recon_scan complete"},
				{Text: "nuclei flagged CVE-2023-12345"},
			},
			want: "cve_lookup",
		},
		{
			name: "cve_lookup already logged does not repeat the suggestion",
			entries: []NotebookEntry{
				{Text: "recon_scan complete"},
				{Text: "nuclei flagged CVE-2023-12345"},
				{Text: "ran cve_lookup for CVE-2023-12345, got remediation guidance"},
			},
			want: "", // checked separately below
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SuggestNextSteps(c.entries)
			if len(got) == 0 {
				t.Fatal("expected at least one suggestion")
			}
			if c.want != "" && !contains(got, c.want) {
				t.Errorf("suggestions %v do not contain %q", got, c.want)
			}
		})
	}

	// Negative checks that need the "no other case's text also matches"
	// nuance to be explicit rather than folded into the table's substring check.
	t.Run("documented findings do not re-suggest documentation", func(t *testing.T) {
		entries := []NotebookEntry{
			{Text: "recon_scan complete"},
			{Text: "documented the CVE-2023-12345 finding in the report via write_file"},
		}
		got := SuggestNextSteps(entries)
		if contains(got, "documenting") {
			t.Errorf("did not expect a documentation suggestion, got %v", got)
		}
	})

	t.Run("logged cve_lookup does not re-suggest cve_lookup", func(t *testing.T) {
		entries := []NotebookEntry{
			{Text: "recon_scan complete"},
			{Text: "nuclei flagged CVE-2023-12345"},
			{Text: "ran cve_lookup for CVE-2023-12345, got remediation guidance"},
		}
		got := SuggestNextSteps(entries)
		if contains(got, "run security_advise(action=\"cve_lookup\")") {
			t.Errorf("did not expect a cve_lookup suggestion, got %v", got)
		}
	})

	t.Run("fully worked engagement has no rule-triggered gaps", func(t *testing.T) {
		entries := []NotebookEntry{
			{Text: "recon_scan complete, no web apps found, no findings"},
		}
		got := SuggestNextSteps(entries)
		if len(got) == 0 {
			t.Fatal("expected at least the fallback suggestion")
		}
	})
}

func TestSuggestNextStepsNeverReturnsEmpty(t *testing.T) {
	// However sparse or noisy the notebook, callers should always get at
	// least one actionable (or fallback) line rather than an empty slice.
	inputs := [][]NotebookEntry{
		nil,
		{},
		{{Text: "unrelated note about lunch"}},
		{{Text: "recon_scan done"}, {Text: "dast_scan done"}, {Text: "security_scan done"}},
	}
	for _, in := range inputs {
		if got := SuggestNextSteps(in); len(got) == 0 {
			t.Errorf("SuggestNextSteps(%v) returned no suggestions", in)
		}
	}
}
