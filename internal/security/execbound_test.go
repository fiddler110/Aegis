package security

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// TestBoundedWriterRetainsUpToLimit is the unit-level proof for boundedWriter
// itself: bytes past the limit are counted (total) but never retained (buf),
// and overflowed() is exact about the boundary — writing precisely the limit
// must not itself read as overflow.
func TestBoundedWriterRetainsUpToLimit(t *testing.T) {
	w := &boundedWriter{limit: 10}
	n, err := w.Write([]byte("0123456789")) // exactly the limit
	if err != nil || n != 10 {
		t.Fatalf("Write() = %d, %v, want 10, nil", n, err)
	}
	if w.overflowed() {
		t.Error("writing exactly the limit must not overflow")
	}
	if string(w.buf) != "0123456789" {
		t.Errorf("buf = %q, want the full write", w.buf)
	}

	n, err = w.Write([]byte("X")) // one byte past the limit
	if err != nil || n != 1 {
		t.Fatalf("Write() = %d, %v, want 1, nil (a bounded writer must never error the caller)", n, err)
	}
	if !w.overflowed() {
		t.Error("writing past the limit must overflow")
	}
	if string(w.buf) != "0123456789" {
		t.Errorf("buf = %q, want unchanged — no byte past the limit is ever retained", w.buf)
	}
	if w.total != 11 {
		t.Errorf("total = %d, want 11 (every byte counted, even discarded ones)", w.total)
	}
}

// TestBoundedWriterSplitAcrossManyWrites pins that the accounting is correct
// across multiple Write calls of varying size straddling the limit — the
// shape a real io.Copy from a subprocess pipe produces (chunked, not one
// call), not just the single-call case above.
func TestBoundedWriterSplitAcrossManyWrites(t *testing.T) {
	w := &boundedWriter{limit: 5}
	chunks := []string{"ab", "cd", "ef", "gh"} // 8 bytes total, limit 5
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatalf("Write(%q): %v", c, err)
		}
	}
	if string(w.buf) != "abcde" {
		t.Errorf("buf = %q, want %q", w.buf, "abcde")
	}
	if w.total != 8 {
		t.Errorf("total = %d, want 8", w.total)
	}
	if !w.overflowed() {
		t.Error("8 bytes written against a 5-byte limit must overflow")
	}
}

// execBoundHelperProcess is the child end of the runBoundedOutput* tests
// below: re-running this test binary as a subprocess, the same self-exec
// trick osv_test.go's TestOSVHelperProcess uses, and for the same reason
// (P34.7) — a fake shaped like a real *exec.Cmd exit/stdout/stderr rather
// than a fixture asserting behavior the test author assumed a scanner has.
func execBoundHelperProcess(t *testing.T, stdoutBytes int, stderr string, exitCode int) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestExecBoundHelperProcess")
	cmd.Env = append(os.Environ(),
		"AEGIS_EXECBOUND_HELPER=1",
		"AEGIS_EXECBOUND_HELPER_STDOUT_BYTES="+strconv.Itoa(stdoutBytes),
		"AEGIS_EXECBOUND_HELPER_STDERR="+stderr,
		"AEGIS_EXECBOUND_HELPER_EXIT="+strconv.Itoa(exitCode),
	)
	return cmd
}

// TestExecBoundHelperProcess is not a real test: it's inert unless the parent
// sets AEGIS_EXECBOUND_HELPER, in which case it writes a deterministic run of
// 'A' bytes to stdout, an arbitrary string to stderr, then exits with the
// requested code.
func TestExecBoundHelperProcess(t *testing.T) {
	if os.Getenv("AEGIS_EXECBOUND_HELPER") != "1" {
		return
	}
	n, _ := strconv.Atoi(os.Getenv("AEGIS_EXECBOUND_HELPER_STDOUT_BYTES"))
	if n > 0 {
		os.Stdout.Write([]byte(strings.Repeat("A", n)))
	}
	os.Stderr.Write([]byte(os.Getenv("AEGIS_EXECBOUND_HELPER_STDERR")))
	code, _ := strconv.Atoi(os.Getenv("AEGIS_EXECBOUND_HELPER_EXIT"))
	os.Exit(code)
}

// TestRunBoundedOutputPassesThroughUnderLimit is the ordinary case: output
// under the cap comes back byte-for-byte, same as cmd.Output() would have
// returned.
func TestRunBoundedOutputPassesThroughUnderLimit(t *testing.T) {
	cmd := execBoundHelperProcess(t, 100, "", 0)
	out, err := runBoundedOutputLimit(cmd, 1000)
	if err != nil {
		t.Fatalf("runBoundedOutputLimit: %v", err)
	}
	if len(out) != 100 {
		t.Errorf("got %d bytes, want 100", len(out))
	}
}

// TestRunBoundedOutputRefusesOverflow is the P70.3 headline case: a scanner
// (rogue, compromised, or just buggy) that produces more than the cap must
// not have its output silently truncated into whatever a JSON/SARIF parser
// makes of the prefix — it must come back as an explicit, honest error
// naming the cap, with no bytes returned to a caller that might otherwise
// try to parse a partial document into a confident wrong finding count.
func TestRunBoundedOutputRefusesOverflow(t *testing.T) {
	const limit = 1024
	cmd := execBoundHelperProcess(t, limit*4, "", 0) // well over the cap, clean exit
	out, err := runBoundedOutputLimit(cmd, limit)
	if err == nil {
		t.Fatal("runBoundedOutputLimit() = nil error, want an overflow error")
	}
	if out != nil {
		t.Errorf("runBoundedOutputLimit() returned %d bytes on overflow, want nil — never hand a truncated report to a parser", len(out))
	}
	if !strings.Contains(err.Error(), strconv.Itoa(limit)) {
		t.Errorf("error %q should name the %d-byte cap", err, limit)
	}
}

// TestRunBoundedOutputOverflowIsFatalRegardlessOfExitCode pins that overflow
// is checked before the exit-code tolerance: a scanner exiting non-zero with
// output produced is normally tolerated (scanners exit non-zero on findings),
// but that tolerance must never let an over-cap run slip through as "output
// was produced, so this is fine".
func TestRunBoundedOutputOverflowIsFatalRegardlessOfExitCode(t *testing.T) {
	const limit = 1024
	cmd := execBoundHelperProcess(t, limit*4, "", 3) // over cap AND non-zero exit
	out, err := runBoundedOutputLimit(cmd, limit)
	if err == nil || out != nil {
		t.Fatalf("runBoundedOutputLimit() = %d bytes, %v, want nil, an overflow error", len(out), err)
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("error %q should read as an overflow, not an exit-code failure", err)
	}
}

// TestRunBoundedOutputToleratesNonZeroExitWithOutput mirrors runJSON's
// documented tolerance (scanners.go): a non-zero exit with output under the
// cap is not an error, matching the pre-existing cmd.Output() contract.
func TestRunBoundedOutputToleratesNonZeroExitWithOutput(t *testing.T) {
	cmd := execBoundHelperProcess(t, 50, "", 2)
	out, err := runBoundedOutputLimit(cmd, 1000)
	if err != nil {
		t.Fatalf("runBoundedOutputLimit: %v, want nil (non-zero exit with output is tolerated)", err)
	}
	if len(out) != 50 {
		t.Errorf("got %d bytes, want 50", len(out))
	}
}

// TestRunBoundedOutputPreservesStderrOnEmptyOutputError pins the contract
// interpretOSVError (osv.go) depends on: a non-zero exit with empty stdout
// must come back as an *exec.ExitError carrying the subprocess's stderr,
// exactly as cmd.Output() populates it — runBoundedOutput no longer calls
// cmd.Output() directly, so this has to be done by hand now, and a silent
// regression here would break P34.12's "128 means no dependencies" reading
// without any test in this file noticing.
func TestRunBoundedOutputPreservesStderrOnEmptyOutputError(t *testing.T) {
	cmd := execBoundHelperProcess(t, 0, "No package sources found\n", 128)
	out, err := runBoundedOutputLimit(cmd, 1000)
	if out != nil {
		t.Errorf("got %d bytes, want nil (empty stdout, non-zero exit)", len(out))
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("got %T (%v), want *exec.ExitError", err, err)
	}
	if !strings.Contains(string(ee.Stderr), "No package sources found") {
		t.Errorf("ee.Stderr = %q, want it to carry the subprocess's stderr", ee.Stderr)
	}
}
