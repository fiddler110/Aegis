package security

import (
	"bytes"
	"fmt"
	"os/exec"
)

// P70.3 (bound half): runJSON and runContainerCLI used cmd.Output(), which
// reads a subprocess's entire stdout into memory before either of them get a
// chance to look at it. Every scanner this package shells out to is either a
// third-party binary or a container image, so "well-behaved" is an
// assumption, not a guarantee — a scanner that is rogue, compromised, or
// simply has a bug that makes it loop-print is otherwise given as much heap
// as the OS will grant before Aegis notices anything is wrong.
//
// maxScanOutputBytes is picked to match maxReadBytes and spillMaxBytes in
// internal/tool/builtin: those are the tree's existing answer to "how much
// output is legitimate to hold in memory for one call" for tool results of
// this shape (a file read, a spilled remainder), and there is no reason a
// scanner report should get a different number than the walk/spill caps
// already do. It is deliberately generous relative to the model-facing caps
// in truncate.go: 64 MiB comfortably covers a SARIF/JSON report for a large
// monorepo scan with thousands of findings, where truncate.go's 20-32 KiB
// caps are sized for what's worth spending context tokens on, not for what a
// scanner may legitimately produce before that separate cap trims it for the
// model.
const maxScanOutputBytes = 64 << 20 // 64 MiB

// boundedWriter caps how many bytes of a subprocess's stdout are retained.
// Excess bytes are counted but discarded rather than buffered, so a rogue or
// runaway scanner's real output size is knowable without ever holding all of
// it. Writes always report success and the pipe is always drained to
// completion (see runBoundedOutput) — a boundedWriter must never be the thing
// that makes cmd.Wait() block on a child stuck writing to a full, unread
// pipe.
type boundedWriter struct {
	limit int
	buf   []byte
	total int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.total += int64(len(p))
	if len(w.buf) < w.limit {
		room := w.limit - len(w.buf)
		if room > len(p) {
			room = len(p)
		}
		w.buf = append(w.buf, p[:room]...)
	}
	return len(p), nil
}

// overflowed reports whether more than limit bytes were written — i.e.
// whether buf is a truncated prefix rather than the whole of stdout.
func (w *boundedWriter) overflowed() bool { return w.total > int64(w.limit) }

// runBoundedOutput is cmd.Output() with a memory ceiling on stdout: at most
// maxScanOutputBytes is ever held in the returned slice, and a subprocess
// that writes more than that is reported as a scan failure rather than
// silently handed to a SARIF/JSON parser as a truncated document. Parsing a
// truncated report is worse than refusing it outright — it fails unpredictably
// deep inside encoding/json, or (worse) succeeds on a partial object and
// reports confidently wrong findings, the same failure shape the gosec
// two-phase warm-cache guard in method.go exists to avoid for a different
// cause.
//
// Stderr is still captured whole (scanner stderr is diagnostic text, not the
// attacker/rogue-controlled channel this bound is defending against) and
// attached to the returned *exec.ExitError exactly as cmd.Output() would,
// since callers — interpretOSVError above all — read ee.Stderr from what
// runJSON returns.
func runBoundedOutput(cmd *exec.Cmd) ([]byte, error) {
	return runBoundedOutputLimit(cmd, maxScanOutputBytes)
}

// runBoundedOutputLimit is runBoundedOutput with the byte ceiling as a
// parameter, so tests can pin the overflow behavior without waiting on 64 MiB
// of real subprocess output.
func runBoundedOutputLimit(cmd *exec.Cmd, limit int) ([]byte, error) {
	stdout := &boundedWriter{limit: limit}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	if stdout.overflowed() {
		return nil, fmt.Errorf("scanner output exceeded %d bytes (%d bytes produced); refusing to parse a possibly-truncated report", limit, stdout.total)
	}

	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			ee.Stderr = stderr.Bytes()
		}
		if len(stdout.buf) == 0 {
			return nil, runErr
		}
	}
	return stdout.buf, nil
}
