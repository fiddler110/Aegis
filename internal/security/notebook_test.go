package security

import (
	"testing"
)

func TestNotebookAppendAndListPersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()

	if err := NotebookAppend(root, "acme-2026q3", "started recon against 10.0.0.0/24", []string{"recon"}); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := NotebookAppend(root, "acme-2026q3", "recon_scan found an admin panel on 10.0.0.5", nil); err != nil {
		t.Fatalf("second append: %v", err)
	}

	// A fresh call (simulating store re-instantiation / daemon restart) must
	// see both notes: the store is file-backed, not held in memory.
	entries, err := NotebookList(root, "acme-2026q3")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Text != "started recon against 10.0.0.0/24" {
		t.Errorf("entries[0].Text = %q", entries[0].Text)
	}
	if len(entries[0].Tags) != 1 || entries[0].Tags[0] != "recon" {
		t.Errorf("entries[0].Tags = %v, want [recon]", entries[0].Tags)
	}
	if entries[1].Text != "recon_scan found an admin panel on 10.0.0.5" {
		t.Errorf("entries[1].Text = %q", entries[1].Text)
	}
	for _, e := range entries {
		if e.Time.IsZero() {
			t.Error("expected a non-zero timestamp on every entry")
		}
	}
}

func TestNotebookListUnknownEngagementReturnsEmptyNotError(t *testing.T) {
	root := t.TempDir()
	entries, err := NotebookList(root, "never-started")
	if err != nil {
		t.Fatalf("list unknown engagement: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestNotebookEngagementsAreIsolated(t *testing.T) {
	root := t.TempDir()
	if err := NotebookAppend(root, "engagement-a", "note for a", nil); err != nil {
		t.Fatal(err)
	}
	if err := NotebookAppend(root, "engagement-b", "note for b", nil); err != nil {
		t.Fatal(err)
	}
	a, err := NotebookList(root, "engagement-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NotebookList(root, "engagement-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || a[0].Text != "note for a" {
		t.Errorf("engagement-a = %v", a)
	}
	if len(b) != 1 || b[0].Text != "note for b" {
		t.Errorf("engagement-b = %v", b)
	}
}

func TestNotebookAppendRejectsEmptyText(t *testing.T) {
	root := t.TempDir()
	if err := NotebookAppend(root, "eng", "   ", nil); err == nil {
		t.Error("expected an error for empty note text")
	}
}

func TestNotebookPathSanitizesEngagementName(t *testing.T) {
	root := t.TempDir()
	if err := NotebookAppend(root, "Acme Corp / Q3!!", "a note", nil); err != nil {
		t.Fatalf("append with messy engagement name: %v", err)
	}
	// The same messy name (any case/whitespace variant that sanitizes
	// identically) must resolve to the same notebook.
	entries, err := NotebookList(root, "acme corp / q3!!")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}

func TestNotebookPathRejectsUnusableName(t *testing.T) {
	if _, err := NotebookPath(t.TempDir(), "!!! ///"); err == nil {
		t.Error("expected an error for an engagement name with no usable characters")
	}
}

func TestDigestNotebookCountsMentions(t *testing.T) {
	entries := []NotebookEntry{
		{Text: "kicked off recon_scan against the lab network"},
		{Text: "nuclei flagged CVE-2021-44228 on the jenkins box"},
		{Text: "ran dast_scan baseline against https://app.lab.internal"},
	}
	d := DigestNotebook("acme", entries)
	if d.NoteCount != 3 {
		t.Errorf("NoteCount = %d, want 3", d.NoteCount)
	}
	if d.ReconMentions == 0 {
		t.Error("expected at least one recon mention")
	}
	if d.DASTMentions == 0 {
		t.Error("expected at least one dast mention")
	}
	if d.FindingHits == 0 {
		t.Error("expected the CVE-2021-44228 mention to count as a finding hit")
	}
	if d.CVELookups != 0 {
		t.Errorf("CVELookups = %d, want 0 (no cve_lookup mention)", d.CVELookups)
	}
}

func TestNotebookDigestFormatEmpty(t *testing.T) {
	d := DigestNotebook("empty-eng", nil)
	got := d.Format()
	if got == "" {
		t.Fatal("expected non-empty digest text")
	}
}
