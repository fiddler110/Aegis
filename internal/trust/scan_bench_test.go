package trust

import (
	"strings"
	"testing"
)

// benchmarkSource is a maxed-out default read_file window: ~22 KiB of ordinary
// source with nothing suspicious in it, which is the shape this scan sees on
// virtually every call now that workspace file reads go through it (DR-1).
func benchmarkSource() string {
	return strings.Repeat(
		"func handler(w http.ResponseWriter, r *http.Request) error {\n\treturn nil\n}\n", 300)
}

// BenchmarkScanForInjectionClean is the cost of the common case, and it exists
// because that cost was assumed rather than measured. The scan sits on the hot
// path of read_file and grep as well as MCP and web output, so a regression
// here is paid on every tool result the agent reads.
//
// For scale: before the invisible-copy skip in ScanForInjection this measured
// ~27ms/op on a Ryzen 3800XT, roughly half of it a second pattern sweep over a
// byte-identical copy of the input.
func BenchmarkScanForInjectionClean(b *testing.B) {
	src := benchmarkSource()
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		if hits := ScanForInjection(src); len(hits) != 0 {
			b.Fatalf("benign source flagged: %v", hits)
		}
	}
}

// BenchmarkScanForInjectionObfuscated is the same input carrying one zero-width
// character, which is what makes the stripped copy differ from the original and
// so re-enables the second pass. It bounds the worst case the skip leaves
// behind: an attacker can still force the double sweep, and this is what that
// costs.
func BenchmarkScanForInjectionObfuscated(b *testing.B) {
	src := benchmarkSource() + "​\n"
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		_ = ScanForInjection(src)
	}
}
