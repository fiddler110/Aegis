package sandbox

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCapWriterStopsBufferingAtTheCap is the VULN-05 unit: the writer must
// stop growing at the cap no matter how much is written through it, keep
// accepting (and counting) the rest, and never report a short write — a short
// count makes os/exec close the pipe and SIGPIPE the child.
func TestCapWriterStopsBufferingAtTheCap(t *testing.T) {
	w := &capWriter{limit: 1024}
	chunk := bytes.Repeat([]byte("x"), 64<<10)
	const chunks = 160 // 10 MiB written through a 1 KiB cap
	for i := 0; i < chunks; i++ {
		n, err := w.Write(chunk)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if n != len(chunk) {
			t.Fatalf("Write returned %d, want %d (a short count SIGPIPEs the child)", n, len(chunk))
		}
		if len(w.buf) > w.limit {
			t.Fatalf("buffer grew to %d bytes, want <= %d", len(w.buf), w.limit)
		}
	}
	if want := int64(chunks*len(chunk) - 1024); w.discarded != want {
		t.Errorf("discarded = %d, want %d", w.discarded, want)
	}
	text := w.text()
	if !strings.Contains(text, "discarded unread") {
		t.Errorf("capped output carries no truncation notice: %q", text)
	}
	// The notice is small and constant-ish; the point is that the whole result
	// is bounded by the cap rather than by what the command produced.
	if len(text) > w.limit+256 {
		t.Errorf("returned text = %d bytes, want <= cap + notice", len(text))
	}
}

// TestCapWriterLeavesUnderCapOutputExactlyAsProduced pins that the bound is
// invisible to every realistic result: no notice, no copy of the notice's
// bytes, byte-identical output.
func TestCapWriterLeavesUnderCapOutputExactlyAsProduced(t *testing.T) {
	w := &capWriter{limit: 1024}
	if _, err := w.Write([]byte("hello\nworld\n")); err != nil {
		t.Fatal(err)
	}
	if got := w.text(); got != "hello\nworld\n" {
		t.Errorf("text() = %q, want the input unchanged", got)
	}
}

// TestLocalExecDoesNotBufferPastTheCap is the VULN-05 regression at the real
// boundary: a command producing far more than the cap must not leave more than
// the cap resident in the returned result. Before the fix, Exec used
// CombinedOutput, so the entire output was in the daemon's heap before any
// result cap in internal/tool/builtin/truncate.go could look at it — ten
// minutes of `cat /dev/urandom` is tens of GB and OOM-kills the daemon, which
// owns every concurrent session.
func TestLocalExecDoesNotBufferPastTheCap(t *testing.T) {
	const cap1KiB = 1024
	l := &LocalBackend{stripEnv: DefaultStripEnv, maxOutput: cap1KiB}

	// ~200 KiB of output, ~200x the test cap, from one cheap builtin.
	command := `head -c 200000 /dev/zero | tr '\0' 'x'`
	if runtime.GOOS == "windows" {
		command = `Write-Output ('x' * 200000)`
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := l.Exec(ctx, command, ExecOpts{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Exec: %v (output %d bytes)", err, len(out))
	}
	if len(out) > cap1KiB+256 {
		t.Errorf("Exec returned %d bytes for a 1 KiB cap — output is still buffered unbounded", len(out))
	}
	if !strings.Contains(out, "discarded unread") {
		t.Errorf("no truncation notice in a capped result: %q", out)
	}
	if !strings.HasPrefix(out, "xxxx") {
		t.Errorf("kept content is not the head of the command's output: %.32q", out)
	}
}
