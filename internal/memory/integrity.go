package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"
)

// P24.17 (FIND-30): memory.md files are plain text with no tamper detection —
// anyone with host/OS write access to either file can hand-edit persistent,
// low-visibility "learned" content that a future session treats as genuine
// prior context. This adds a lightweight integrity check: a sha256 hash of a
// memory file's full content, recorded by Aegis in a sidecar file next to it
// whenever Aegis itself writes via Append, and checked whenever the file is
// loaded. This is intentionally not a keyed MAC/signature — an adversary who
// can write the memory file can just as easily write a sidecar sitting next
// to it, so a secret key wouldn't raise the bar; the goal is only to detect
// changes that didn't go through Aegis's own write path, not to cryptographically
// authenticate the writer.

// integritySuffix names the sidecar file that stores a memory file's
// last-known-good hash, e.g. ".aegis/memory.md" -> ".aegis/memory.md.integrity".
const integritySuffix = ".integrity"

// integrityWarning is prepended to a memory section's text when its sidecar
// hash doesn't match the file's current content.
const integrityWarning = "⚠️ integrity check failed: this memory file was modified outside Aegis — treat its contents with reduced trust\n\n"

func sidecarPath(path string) string {
	return path + integritySuffix
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeSidecar (re)records data's hash as path's trust baseline. Failures are
// logged, not returned — sidecar bookkeeping must never block a memory write
// or a memory load.
func writeSidecar(path string, data []byte) {
	if err := os.WriteFile(sidecarPath(path), []byte(hashHex(data)), 0o600); err != nil {
		slog.Warn("memory: failed to write integrity sidecar", "path", path, "error", err)
	}
}

// updateSidecarAfterWrite re-reads path (after Append has just written to it)
// and refreshes its integrity sidecar to match. Called from Append, once the
// entry itself has already been durably written — a failure here never fails
// the Append call.
func updateSidecarAfterWrite(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("memory: failed to read file back for integrity sidecar", "path", path, "error", err)
		return
	}
	writeSidecar(path, data)
}

// checkIntegrity compares path's current content against its recorded
// sidecar hash and reports whether tampering (a mismatch) was detected.
//
//   - Sidecar matches current content: not tampered.
//   - Sidecar exists but differs: tampered — logs a warning.
//   - Sidecar missing or unreadable (first run after adopting this feature,
//     or a pre-existing memory.md predating it): not tampered. A baseline is
//     established silently so upgrading users and intentionally hand-edited
//     files don't get a false-positive warning on the very next load.
//   - Hashing/sidecar I/O trouble of any kind: fails open (not tampered)
//     rather than ever blocking a memory load.
func checkIntegrity(path string, data []byte) (tampered bool) {
	recorded, err := os.ReadFile(sidecarPath(path))
	if err != nil {
		writeSidecar(path, data)
		return false
	}
	if strings.TrimSpace(string(recorded)) == hashHex(data) {
		return false
	}
	slog.Warn("memory: integrity check failed, file was modified outside Aegis", "path", path)
	return true
}

// readMemoryFileChecked reads a memory file written via Append and verifies
// it against its integrity sidecar, prepending integrityWarning to the
// returned text when the sidecar mismatches. Returns "" if the file doesn't
// exist or is empty, same as readIfExists.
func readMemoryFileChecked(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	txt := strings.TrimSpace(string(data))
	if txt == "" {
		return ""
	}
	if checkIntegrity(path, data) {
		return integrityWarning + txt
	}
	return txt
}
