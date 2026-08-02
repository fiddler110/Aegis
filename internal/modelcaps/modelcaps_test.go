package modelcaps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T, opts ...Option) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	return Open(path, opts...), path
}

func TestThinkRejectedRoundTripsAcrossOpen(t *testing.T) {
	s, path := tempStore(t)
	if _, known := s.ThinkRejected("m"); known {
		t.Fatal("fresh store claims to know a verdict")
	}
	s.SetThinkRejected("m", true)

	// The whole point of P53.5: a second process reads what the first learned.
	reopened := Open(path)
	rejected, known := reopened.ThinkRejected("m")
	if !known || !rejected {
		t.Fatalf("after reopen: rejected=%v known=%v, want true/true", rejected, known)
	}
}

func TestDeclaredOutranksPersisted(t *testing.T) {
	s, path := tempStore(t)
	s.SetThinkRejected("m", true)

	// A user declaring the model *does* accept think must be able to retract a
	// stale latch without deleting the file — otherwise a wrong cached value is
	// unrecoverable, which is the failure mode "cache, never source of truth"
	// exists to prevent.
	declared := Open(path, WithDeclared(map[string]Declared{
		"m": {Think: boolPtr(true)},
	}))
	rejected, known := declared.ThinkRejected("m")
	if !known {
		t.Fatal("declaration not reported as known")
	}
	if rejected {
		t.Fatal("declared think:true did not outrank the persisted rejection")
	}
}

func TestDeclaredThinkFalseNeedsNoProbe(t *testing.T) {
	s, _ := tempStore(t, WithDeclared(map[string]Declared{
		"never-seen": {Think: boolPtr(false)},
	}))
	rejected, known := s.ThinkRejected("never-seen")
	if !known || !rejected {
		t.Fatalf("declared think:false gave rejected=%v known=%v, want true/true", rejected, known)
	}
}

func TestDeclaredToolCallingOutranksPersisted(t *testing.T) {
	s, path := tempStore(t)
	s.SetToolCalling("m", ToolCalling{Verdict: "unsupported", Trials: 5, ToolCallTrials: 0})

	declared := Open(path, WithDeclared(map[string]Declared{"m": {ToolCalling: "ok"}}))
	tc, ok := declared.ToolCalling("m")
	if !ok || tc.Verdict != "ok" {
		t.Fatalf("declared verdict = %q (ok=%v), want ok", tc.Verdict, ok)
	}
	// The declaration overrides the verdict without fabricating a sample it
	// never took: the measured counters stay visible.
	if tc.Trials != 5 {
		t.Fatalf("declaration erased the measured sample: trials=%d", tc.Trials)
	}
}

func TestUnknownVerdictIsNeverPersisted(t *testing.T) {
	s, path := tempStore(t)
	s.SetToolCalling("m", ToolCalling{Verdict: "unknown", Trials: 3})
	s.SetToolCalling("m2", ToolCalling{Trials: 3})

	reopened := Open(path)
	for _, model := range []string{"m", "m2"} {
		if _, ok := reopened.ToolCalling(model); ok {
			t.Fatalf("%s: a verdict the probe could not justify was persisted", model)
		}
	}
}

func TestRateExcludesNoVerdictTrials(t *testing.T) {
	tc := ToolCalling{Trials: 5, ToolCallTrials: 2, NoVerdict: 1}
	rate, ok := tc.Rate()
	if !ok {
		t.Fatal("no rate from a sample with verdicts")
	}
	if rate != 0.5 { // 2 of (5-1)
		t.Fatalf("rate = %v, want 0.5", rate)
	}
	// An all-truncated sample has no rate at all — reporting 0.0 would accuse a
	// model the probe never got an answer out of.
	if _, ok := (ToolCalling{Trials: 3, NoVerdict: 3}).Rate(); ok {
		t.Fatal("all-truncated sample reported a rate")
	}
}

func TestReconcileDropsOnlyChangedDigests(t *testing.T) {
	s, _ := tempStore(t)
	s.Reconcile(map[string]string{"stable": "sha-1", "moved": "sha-a"})
	s.SetThinkRejected("stable", true)
	s.SetToolCalling("moved", ToolCalling{Verdict: "ok", Trials: 5, ToolCallTrials: 5})
	s.SetToolCalling("unknown-to-server", ToolCalling{Verdict: "ok", Trials: 5, ToolCallTrials: 5})

	dropped := s.Reconcile(map[string]string{"stable": "sha-1", "moved": "sha-b"})
	if dropped != 1 {
		t.Fatalf("dropped %d records, want 1", dropped)
	}
	if _, ok := s.Get("moved"); ok {
		t.Fatal("a re-pulled model kept the previous weights' record")
	}
	if _, ok := s.Get("stable"); !ok {
		t.Fatal("an unchanged model lost its record")
	}
	// Absence from the digest map means "couldn't ask", not "stale".
	if _, ok := s.Get("unknown-to-server"); !ok {
		t.Fatal("a model the server didn't list was invalidated on missing evidence")
	}
}

func TestReconcileAdoptsFingerprintForUnstampedRecord(t *testing.T) {
	s, _ := tempStore(t)
	s.SetThinkRejected("m", true) // written with no digest snapshot available

	s.Reconcile(map[string]string{"m": "sha-1"})
	rec, ok := s.Get("m")
	if !ok {
		t.Fatal("an unfingerprinted measurement was thrown away")
	}
	if rec.Fingerprint != "sha-1" {
		t.Fatalf("fingerprint = %q, want sha-1", rec.Fingerprint)
	}
	// Now that it is stamped it is invalidatable like any other.
	if dropped := s.Reconcile(map[string]string{"m": "sha-2"}); dropped != 1 {
		t.Fatalf("dropped %d, want 1", dropped)
	}
}

func TestNativeClaimSurvivesProbeWrites(t *testing.T) {
	s, _ := tempStore(t)
	s.SetNativeToolSupport("m", true)
	ProbeStore{S: s}.SetToolCalling("m", "unsupported", 5, 0, 0)

	rec, ok := s.Get("m")
	if !ok || rec.ToolCalling == nil {
		t.Fatal("no record after probe write")
	}
	// The manifest's claim and the measured verdict are allowed to disagree —
	// that disagreement is the informative part, so neither may erase the other.
	if rec.ToolCalling.Native == nil || !*rec.ToolCalling.Native {
		t.Fatal("probe write erased the manifest's tool-support claim")
	}
	if rec.ToolCalling.Verdict != "unsupported" {
		t.Fatalf("verdict = %q, want unsupported", rec.ToolCalling.Verdict)
	}
}

func TestCorruptFileStartsEmptyRatherThanFailing(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(path)
	if _, known := s.ThinkRejected("m"); known {
		t.Fatal("corrupt file produced a verdict")
	}
	// Still writable: a cache that can't be read must not become one that can't
	// be rebuilt.
	s.SetThinkRejected("m", true)
	if _, known := Open(path).ThinkRejected("m"); !known {
		t.Fatal("store did not recover from a corrupt file")
	}
}

func TestForgetAndEmptyPathAreSafe(t *testing.T) {
	s, _ := tempStore(t)
	s.SetThinkRejected("m", true)
	s.Forget("m")
	if _, known := s.ThinkRejected("m"); known {
		t.Fatal("Forget left the record behind")
	}

	// An empty path (a config with no data dir) must be a working in-memory
	// store, never a file dropped into the working directory.
	mem := Open("")
	mem.SetThinkRejected("m", true)
	if rejected, known := mem.ThinkRejected("m"); !known || !rejected {
		t.Fatal("path-less store lost its in-memory record")
	}
	if _, err := os.Stat(FileName); err == nil {
		t.Fatal("path-less store wrote a file into the working directory")
	}
}

func TestNilStoreIsAWorkingNoOp(t *testing.T) {
	var s *Store
	s.SetThinkRejected("m", true)
	s.SetToolCalling("m", ToolCalling{Verdict: "ok", Trials: 1, ToolCallTrials: 1})
	s.SetNativeToolSupport("m", true)
	s.Forget("m")
	if _, known := s.ThinkRejected("m"); known {
		t.Fatal("nil store reported a verdict")
	}
	if _, ok := s.ToolCalling("m"); ok {
		t.Fatal("nil store reported a sample")
	}
	if n := s.Reconcile(map[string]string{"m": "x"}); n != 0 {
		t.Fatalf("nil store dropped %d records", n)
	}
	if got := s.Models(); got != nil {
		t.Fatalf("nil store listed %v", got)
	}
}

func TestFileIsHumanReadableJSON(t *testing.T) {
	s, path := tempStore(t)
	s.SetToolCalling("qwen3:14b", ToolCalling{Verdict: "ok", Trials: 5, ToolCallTrials: 4, NoVerdict: 1})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]Record
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("store file is not decodable JSON: %v", err)
	}
	rec := decoded["qwen3:14b"]
	if rec.ToolCalling == nil || rec.ToolCalling.ToolCallTrials != 4 {
		t.Fatalf("counters did not round-trip: %+v", rec.ToolCalling)
	}
	if rec.UpdatedAt.IsZero() {
		t.Fatal("record has no timestamp")
	}
}

func boolPtr(b bool) *bool { return &b }
