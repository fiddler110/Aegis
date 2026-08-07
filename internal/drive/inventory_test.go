package drive

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubInventoryPy is a miniature stand-in for the bundled inventory.py: in
// generate mode it derives inventory.yaml from 0.1-architecture.md, in --check
// mode it compares the on-disk sidecar against that same derivation. Same two
// modes, same contract, none of the parsing — enough to observe *when* the
// drive regenerates and what the checks then see.
const stubInventoryPy = `import os, sys
run_dir = sys.argv[1]
check = "--check" in sys.argv[2:]
src = os.path.join(run_dir, "0.1-architecture.md")
lines = open(src, encoding="utf-8").read().splitlines()
derived = "components:\n" + "".join(
    "  - id: %s\n" % l[2:].strip() for l in lines if l.startswith("- "))
inv = os.path.join(run_dir, "inventory.yaml")
if check:
    cur = open(inv, encoding="utf-8").read() if os.path.exists(inv) else ""
    if cur != derived:
        print("FAIL  components match 0.1-architecture.md")
        sys.exit(1)
    print("PASS  components match 0.1-architecture.md")
    sys.exit(0)
open(inv, "w", encoding="utf-8").write(derived)
sys.stderr.write("stub inventory.py: wrote %s\n" % inv)
`

// passingVerifyPy is the gate VerifySkillOutputs keys off; it must exist for
// verification to run at all, and passing keeps the tests below focused on the
// sidecar rather than on verify.py's own report.
const passingVerifyPy = "print('PASS all')\n"

// newStubSuite builds a workspace with one threat-model run directory and a
// skill directory carrying a passing verify.py, and returns (cwd, runDir,
// skillDir). components are written as the architecture doc's bullet list —
// the "source of truth" the stub inventory.py derives from.
func newStubSuite(t *testing.T, components ...string) (string, string, string) {
	t.Helper()
	cwd := t.TempDir()
	runDir := filepath.Join(cwd, ".aegis", "security", "threat-model", "stride-app-2026-08-07-0900")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(dir, name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(runDir, "0-assessment.md", "# Assessment\n")
	write(runDir, "0.1-architecture.md", "# Architecture\n"+strings.Join(prefixEach(components), ""))
	skillDir := t.TempDir()
	write(skillDir, "verify.py", passingVerifyPy)
	return cwd, runDir, skillDir
}

func prefixEach(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, "- "+it+"\n")
	}
	return out
}

func readInventory(t *testing.T, runDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runDir, "inventory.yaml"))
	if err != nil {
		t.Fatalf("reading inventory.yaml: %v", err)
	}
	return string(data)
}

func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A hand-edited inventory.yaml must not survive to the checks. This is the
// 142-minute-run defect in miniature: the model wrote a component ("Azure")
// into the derived sidecar that no document mentions, and the phase-6 tail then
// spent model turns reconciling it. Regeneration makes the hand edit
// unobservable — the sidecar the checks read is rebuilt from the markdown.
func TestVerifyRegeneratesHandEditedInventory(t *testing.T) {
	if pythonExe() == "" {
		t.Skip("no python interpreter on PATH")
	}
	cwd, runDir, skillDir := newStubSuite(t, "Gateway", "Store")
	writeScript(t, skillDir, "inventory.py", stubInventoryPy)

	handEdited := "components:\n  - id: Azure\n"
	writeScript(t, runDir, "inventory.yaml", handEdited)

	// Guard that the stub is a meaningful check: --check alone, with no
	// regeneration, flags the hand edit exactly as the real run's log did.
	out, err := exec.Command(pythonExe(), filepath.Join(skillDir, "inventory.py"), runDir, "--check").CombinedOutput()
	if err == nil {
		t.Fatalf("stub --check should fail against a hand-edited sidecar, got:\n%s", out)
	}

	failures, ran := VerifySkillOutputs("threat-modeling", skillDir, cwd)
	if !ran {
		t.Fatal("expected verification to run")
	}
	if failures != "" {
		t.Errorf("regeneration should have removed the drift before --check ran; failures:\n%s", failures)
	}
	got := readInventory(t, runDir)
	if strings.Contains(got, "Azure") {
		t.Errorf("hand-edited sidecar survived regeneration:\n%s", got)
	}
	for _, want := range []string{"Gateway", "Store"} {
		if !strings.Contains(got, want) {
			t.Errorf("regenerated sidecar missing %q derived from the docs:\n%s", want, got)
		}
	}
}

// Regeneration must run on EVERY verify round, not once: each round of the
// bounded fix loop is a model turn that just edited the markdown, so a
// once-only regeneration would leave the sidecar stale again from the first fix
// onward — the same bug, later in the run.
func TestVerifyRegeneratesOnEveryRound(t *testing.T) {
	if pythonExe() == "" {
		t.Skip("no python interpreter on PATH")
	}
	cwd, runDir, skillDir := newStubSuite(t, "Gateway")
	writeScript(t, skillDir, "inventory.py", stubInventoryPy)

	if _, ran := VerifySkillOutputs("threat-modeling", skillDir, cwd); !ran {
		t.Fatal("expected verification to run")
	}
	if got := readInventory(t, runDir); strings.Contains(got, "Store") {
		t.Fatalf("sidecar should not mention a component the docs lack:\n%s", got)
	}

	// A later fix round adds a component to the architecture doc.
	writeScript(t, runDir, "0.1-architecture.md", "# Architecture\n- Gateway\n- Store\n")
	failures, ran := VerifySkillOutputs("threat-modeling", skillDir, cwd)
	if !ran {
		t.Fatal("expected verification to run")
	}
	if failures != "" {
		t.Errorf("second round should re-derive and verify clean; failures:\n%s", failures)
	}
	if got := readInventory(t, runDir); !strings.Contains(got, "Store") {
		t.Errorf("second round did not re-derive the sidecar:\n%s", got)
	}
}

// stubNormalizeIDsPy canonicalizes an invented `T1.a` threat id back to `T1` in
// the architecture doc — standing in for normalize_ids.py's rewrite of the
// markdown, which is what makes ordering observable.
const stubNormalizeIDsPy = `import os, sys
p = os.path.join(sys.argv[1], "0.1-architecture.md")
text = open(p, encoding="utf-8").read()
open(p, "w", encoding="utf-8").write(text.replace("T1.a", "T1"))
`

// Ordering: the sidecar must be derived AFTER normalize_ids.py has canonicalized
// the IDs in the markdown, or it captures IDs that are about to change. drive.go
// calls normalizeSkillIDs immediately before entering the verify loop and
// regeneration is the first thing VerifySkillOutputs does, so the correct order
// holds by construction — this pins both halves, and the inverted order below
// shows the pin is not vacuous.
func TestVerifyRegeneratesAfterIDNormalization(t *testing.T) {
	if pythonExe() == "" {
		t.Skip("no python interpreter on PATH")
	}
	cwd, runDir, skillDir := newStubSuite(t, "T1.a")
	writeScript(t, skillDir, "inventory.py", stubInventoryPy)
	writeScript(t, skillDir, "normalize_ids.py", stubNormalizeIDsPy)

	// The drive's order: normalize, then verify (which regenerates first).
	if ran, err := normalizeSkillIDs("threat-modeling", skillDir, cwd); err != nil || !ran {
		t.Fatalf("normalizeSkillIDs: ran=%v err=%v", ran, err)
	}
	if _, ran := VerifySkillOutputs("threat-modeling", skillDir, cwd); !ran {
		t.Fatal("expected verification to run")
	}
	got := readInventory(t, runDir)
	if strings.Contains(got, "T1.a") || !strings.Contains(got, "T1") {
		t.Errorf("sidecar was derived from un-canonicalized IDs:\n%s", got)
	}

	// Inverted order on a fresh suite: regenerating first captures the
	// pre-normalization id and the later normalize does not revisit the
	// sidecar — precisely the stale state the ordering above avoids.
	cwd2, runDir2, skillDir2 := newStubSuite(t, "T1.a")
	writeScript(t, skillDir2, "inventory.py", stubInventoryPy)
	writeScript(t, skillDir2, "normalize_ids.py", stubNormalizeIDsPy)
	if _, ran := VerifySkillOutputs("threat-modeling", skillDir2, cwd2); !ran {
		t.Fatal("expected verification to run")
	}
	if _, err := normalizeSkillIDs("threat-modeling", skillDir2, cwd2); err != nil {
		t.Fatal(err)
	}
	if got := readInventory(t, runDir2); !strings.Contains(got, "T1.a") {
		t.Errorf("inverted-order control should have left the stale id in the sidecar:\n%s", got)
	}
}

// A regeneration that FAILS is the one signal regeneration cannot make
// tautological: nothing then knows what the documents say. It must surface as a
// verify failure the bounded fix loop acts on — deliberately unlike
// normalizeSkillIDs, whose error the drive only logs.
func TestVerifyRegenerationFailureSurfaces(t *testing.T) {
	if pythonExe() == "" {
		t.Skip("no python interpreter on PATH")
	}
	cwd, runDir, skillDir := newStubSuite(t, "Gateway")
	broken := "import sys\n" +
		"sys.stderr.write('inventory.py: could not parse Key Components table at 0.1-architecture.md:14\\n')\n" +
		"sys.exit(1)\n"
	writeScript(t, skillDir, "inventory.py", broken)
	writeScript(t, runDir, "inventory.yaml", "components:\n  - id: Azure\n")

	failures, ran := VerifySkillOutputs("threat-modeling", skillDir, cwd)
	if !ran {
		t.Fatal("a regeneration failure must not be reported as nothing-to-verify")
	}
	if failures == "" {
		t.Fatal("regeneration failure was swallowed; verify.py passing must not hide it")
	}
	for _, want := range []string{
		"inventory.py",                   // the failing command
		"could not parse Key Components", // the script's own output, verbatim
		"regenerating inventory.yaml",    // what was being attempted
		"do not hand-write or edit it",   // the remedy that is NOT "fix the sidecar"
		"0.1-architecture.md:14",         // file:line the fix loop can route on
	} {
		if !strings.Contains(failures, want) {
			t.Errorf("failure report missing %q:\n%s", want, failures)
		}
	}
}

// Every ingredient being absent must stay a silent no-op, exactly as
// normalizeSkillIDs and VerifySkillOutputs already degrade: an older skill build
// bundling no inventory.py, no run directory, no python.
func TestRegenerateInventorySidecarDegradesToNoOp(t *testing.T) {
	skillDir := t.TempDir()
	runDir := t.TempDir()
	cases := map[string][3]string{
		"no skill dir": {"", runDir, "python3"},
		"no run dir":   {skillDir, "", "python3"},
		"no python":    {skillDir, runDir, ""},
		"not bundled":  {skillDir, runDir, "python3"}, // skillDir has no inventory.py
	}
	for name, args := range cases {
		ran, err := regenerateInventorySidecar(args[0], args[1], args[2])
		if ran || err != nil {
			t.Errorf("%s: ran=%v err=%v, want a silent no-op", name, ran, err)
		}
	}
}

// The same degradation through the real entry point: a skill build with
// verify.py but no inventory.py verifies exactly as before and leaves whatever
// sidecar is on disk untouched.
func TestVerifyWithoutBundledInventoryLeavesSidecarAlone(t *testing.T) {
	if pythonExe() == "" {
		t.Skip("no python interpreter on PATH")
	}
	cwd, runDir, skillDir := newStubSuite(t, "Gateway")
	writeScript(t, runDir, "inventory.yaml", "components:\n  - id: Azure\n")

	failures, ran := VerifySkillOutputs("threat-modeling", skillDir, cwd)
	if !ran || failures != "" {
		t.Errorf("ran=%v failures=%q, want a clean run on the older skill build", ran, failures)
	}
	if got := readInventory(t, runDir); !strings.Contains(got, "Azure") {
		t.Errorf("sidecar must be untouched when inventory.py is not bundled:\n%s", got)
	}
}

// `inventory.py --check` stays in the check triple even though regeneration
// makes most of it unfailable: two of its checks assert properties of the
// MARKDOWN (an analysis file that parses at all; cross-document reference
// integrity) that generate mode reports as a warning-and-exit-0 or not at all.
func TestInventoryCheckStaysInTheTriple(t *testing.T) {
	var found bool
	for _, s := range threatModelVerifyScripts {
		if s.file != inventoryScript {
			continue
		}
		found = true
		if len(s.args) != 1 || s.args[0] != "--check" {
			t.Errorf("inventory.py must run with --check, got args %v", s.args)
		}
	}
	if !found {
		t.Error("inventory.py --check must remain in the phase-6 check triple")
	}
}
