package drive

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSuiteFile writes one run-dir file with the given body, failing the test
// on error. Used to build a controlled suite whose fingerprint we can reason
// about byte-for-byte.
func writeSuiteFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSuiteFingerprintStableAndContentSensitive covers requirement (a): the
// fingerprint is stable across calls on an unchanged suite, and changes iff a
// suite file's content changes. It also proves the `.quality-stamp.json` stamp
// (a `.json` file) is excluded from the fingerprint, so writing the stamp cannot
// perturb the value it records.
func TestSuiteFingerprintStableAndContentSensitive(t *testing.T) {
	dir := t.TempDir()
	writeSuiteFile(t, dir, "0-assessment.md", "assessment v1\n")
	writeSuiteFile(t, dir, "1.1-model.mmd", "flowchart LR\n")
	writeSuiteFile(t, dir, "inventory.yaml", "id: x\n")

	fp1, err := SuiteFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	fp2, err := SuiteFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint not stable across calls: %s vs %s", fp1, fp2)
	}

	// Writing the stamp (a .json file) must not change the fingerprint.
	if err := WriteQualityStamp(dir, fp1); err != nil {
		t.Fatal(err)
	}
	fpAfterStamp, err := SuiteFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fpAfterStamp != fp1 {
		t.Errorf("writing .quality-stamp.json changed the fingerprint: %s -> %s", fp1, fpAfterStamp)
	}

	// Editing a suite file must change the fingerprint.
	writeSuiteFile(t, dir, "0-assessment.md", "assessment v2 (edited)\n")
	fpEdited, err := SuiteFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fpEdited == fp1 {
		t.Error("editing a suite file must change the fingerprint")
	}

	// Adding a new suite file must also change the fingerprint.
	writeSuiteFile(t, dir, "3-findings.md", "FIND-01\n")
	fpAdded, err := SuiteFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fpAdded == fpEdited {
		t.Error("adding a suite file must change the fingerprint")
	}
}

// TestQualityStampRoundTrip covers requirement (b): a written stamp reads back
// with the same fingerprint, and ShouldSkipQualityPass returns true only while
// the on-disk suite still matches that fingerprint — a subsequent edit (or a
// missing stamp) makes it return false, re-triggering the quality pass.
func TestQualityStampRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeSuiteFile(t, dir, "0-assessment.md", "assessment\n")
	writeSuiteFile(t, dir, "3-findings.md", "findings\n")

	// No stamp yet: the pass must run.
	if ShouldSkipQualityPass(dir) {
		t.Error("with no stamp, ShouldSkipQualityPass must be false")
	}

	fp, err := SuiteFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteQualityStamp(dir, fp); err != nil {
		t.Fatal(err)
	}

	stamp, ok := ReadQualityStamp(dir)
	if !ok {
		t.Fatal("stamp must read back after being written")
	}
	if stamp.Fingerprint != fp {
		t.Errorf("round-tripped fingerprint = %q, want %q", stamp.Fingerprint, fp)
	}
	if stamp.ReviewedAt == "" {
		t.Error("stamp must record a reviewed_at timestamp")
	}

	// Matching stamp on an unchanged suite: skip the pass.
	if !ShouldSkipQualityPass(dir) {
		t.Error("a matching stamp on an unchanged suite must skip the quality pass")
	}

	// Edit a suite file: the fingerprint no longer matches, so the pass re-fires.
	writeSuiteFile(t, dir, "3-findings.md", "findings edited\n")
	if ShouldSkipQualityPass(dir) {
		t.Error("after editing a suite file the stale stamp must NOT skip the quality pass")
	}
}

// TestQualityStampInvalidStamps confirms the guard treats absent, malformed, and
// empty-fingerprint stamps as un-reviewed rather than accidentally skipping.
func TestQualityStampInvalidStamps(t *testing.T) {
	dir := t.TempDir()
	writeSuiteFile(t, dir, "0-assessment.md", "assessment\n")

	// Malformed JSON.
	if err := os.WriteFile(filepath.Join(dir, QualityStampFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadQualityStamp(dir); ok {
		t.Error("malformed stamp must not parse")
	}
	if ShouldSkipQualityPass(dir) {
		t.Error("malformed stamp must not skip the quality pass")
	}

	// Well-formed JSON but empty fingerprint.
	if err := os.WriteFile(filepath.Join(dir, QualityStampFile), []byte(`{"fingerprint":"","reviewed_at":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadQualityStamp(dir); ok {
		t.Error("empty-fingerprint stamp must be treated as absent")
	}
	if ShouldSkipQualityPass("") {
		t.Error("empty run dir must not skip the quality pass")
	}
}
