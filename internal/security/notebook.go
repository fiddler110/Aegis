package security

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This file implements P13.4's "engagement notebook" — the foundation the
// rest of security_advise (CVE lookup, guarded next-step suggestions, status
// digest) hangs off. It was deliberately parked out of the P13.5/P13.8
// red-team work (see research/releases.md's P13.4 note): P13.8 ships a
// single per-engagement report file via write_file, which covers one
// exercise, but a real multi-day engagement needs notes that survive across
// sessions and daemon restarts, keyed by an operator-chosen engagement name
// rather than the ephemeral session ID.
//
// Storage choice: internal/memory's Sources type (project/user memory.md) is
// a *single* file per scope, always fully loaded into the system prompt —
// it has no notion of multiple independently-named, arbitrarily-long-lived
// notebooks, and stretching it to cover that (a map of names to files, none
// of them meant for prompt injection) would be a worse fit than a small
// dedicated store. So this is a purpose-built, file-backed store instead:
// one append-only JSONL file per sanitized engagement name, rooted under the
// daemon's per-user data directory (survives across projects/sessions, the
// same seam internal/longmem and internal/knowledge already use for
// cross-session state). JSONL (not a single JSON array) keeps appends O(1)
// and crash-safe — no read-modify-write of the whole file — mirroring why
// internal/memory.Append opens files O_APPEND rather than rewriting them.

// NotebookEntry is one timestamped note in an engagement's notebook.
type NotebookEntry struct {
	Time time.Time `json:"time"`
	Text string    `json:"text"`
	// Tags are free-form, operator- or model-supplied labels (e.g. "recon",
	// "finding", "cve"). They are never required — SuggestNextSteps and the
	// status digest also scan Text itself for the same keywords — but a tag
	// makes the intent explicit instead of relying on substring matching.
	Tags []string `json:"tags,omitempty"`
}

const maxNotebookEntry = 4096

// notebookDir is the subdirectory (under a data-dir root) engagement
// notebooks are stored in.
const notebookDir = "security/engagements"

// NotebookPath resolves the on-disk path for engagement's notebook file
// under root (typically the daemon's per-user data directory), sanitizing
// the engagement name into a safe filename. Returns an error if the name has
// no safe characters at all.
func NotebookPath(root, engagement string) (string, error) {
	name := sanitizeEngagementName(engagement)
	if name == "" {
		return "", fmt.Errorf("engagement name %q has no usable characters (letters, digits, dashes, underscores)", engagement)
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, notebookDir, name+".jsonl"), nil
}

// NotebookAppend appends a timestamped note to engagement's notebook,
// creating the notebook (and its parent directory) if this is the first
// note. text is trimmed and length-capped the same way internal/memory.Append
// caps a memory entry.
func NotebookAppend(root, engagement, text string, tags []string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty note text")
	}
	if len(text) > maxNotebookEntry {
		return fmt.Errorf("note too large (%d bytes, max %d)", len(text), maxNotebookEntry)
	}
	path, err := NotebookPath(root, engagement)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	entry := NotebookEntry{Time: time.Now().UTC(), Text: text, Tags: cleanTags(tags)}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(line, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// NotebookList returns every note recorded for engagement, oldest first. A
// notebook that doesn't exist yet (no notes recorded) returns an empty slice
// and no error — the same "not found is not an error" convention
// memory.Load's readIfExists follows.
func NotebookList(root, engagement string) ([]NotebookEntry, error) {
	path, err := NotebookPath(root, engagement)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []NotebookEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e NotebookEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// A hand-edited or truncated line is skipped rather than failing
			// the whole read — one bad line shouldn't hide every other note.
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// sanitizeEngagementName mirrors internal/memory's skill-name sanitize:
// lowercased, and only letters/digits/dash/underscore survive (spaces fold
// to dashes), so an engagement name can't escape the notebook directory or
// collide via case/whitespace variants.
func sanitizeEngagementName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func cleanTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// NotebookDigest is a summary of one engagement's notebook (P13.4.4's status
// digest): note count and simple keyword-based references to
// scans/findings, cheap enough to compute from NotebookList's output on
// every call rather than maintaining separate counters.
type NotebookDigest struct {
	Engagement    string
	NoteCount     int
	FirstNoteAt   time.Time
	LastNoteAt    time.Time
	ReconMentions int
	DASTMentions  int
	ScanMentions  int
	FindingHits   int // notes mentioning a CVE ID or the word "finding"
	CVELookups    int // notes mentioning "cve_lookup" or "cve lookup"
}

// DigestNotebook computes a NotebookDigest from an already-loaded entry
// list (see NotebookList).
func DigestNotebook(engagement string, entries []NotebookEntry) NotebookDigest {
	d := NotebookDigest{Engagement: engagement, NoteCount: len(entries)}
	for i, e := range entries {
		if i == 0 {
			d.FirstNoteAt = e.Time
		}
		d.LastNoteAt = e.Time
		hay := strings.ToLower(e.Text) + " " + strings.ToLower(strings.Join(e.Tags, " "))
		if strings.Contains(hay, "recon_scan") || strings.Contains(hay, "recon") || strings.Contains(hay, "nmap") || strings.Contains(hay, "nuclei") {
			d.ReconMentions++
		}
		if strings.Contains(hay, "dast_scan") || strings.Contains(hay, "dast") {
			d.DASTMentions++
		}
		if strings.Contains(hay, "security_scan") {
			d.ScanMentions++
		}
		if strings.Contains(hay, "finding") || cveIDPattern.MatchString(hay) {
			d.FindingHits++
		}
		if strings.Contains(hay, "cve_lookup") || strings.Contains(hay, "cve lookup") {
			d.CVELookups++
		}
	}
	return d
}

// Format renders the digest as short human-readable text, the same
// plain-text convention Report.Format uses elsewhere in this package.
func (d NotebookDigest) Format() string {
	if d.NoteCount == 0 {
		return fmt.Sprintf("Engagement %q: no notes recorded yet.", d.Engagement)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Engagement %q: %d note(s), %s to %s.\n", d.Engagement, d.NoteCount,
		d.FirstNoteAt.Format("2006-01-02"), d.LastNoteAt.Format("2006-01-02"))
	fmt.Fprintf(&b, "  recon mentions: %d | dast mentions: %d | security_scan mentions: %d\n", d.ReconMentions, d.DASTMentions, d.ScanMentions)
	fmt.Fprintf(&b, "  finding references: %d | cve_lookup references: %d\n", d.FindingHits, d.CVELookups)
	return strings.TrimRight(b.String(), "\n")
}
