package security

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// withEmptyPath temporarily clears PATH (Windows: Path) so exec.LookPath
// can't find any binary, then restores it. Used to exercise ScanText's
// "gitleaks not installed" best-effort branch even on a machine (like this
// dev box) that actually has gitleaks on PATH.
func withEmptyPath(t *testing.T) {
	t.Helper()
	name := "PATH"
	if runtime.GOOS == "windows" {
		// os.Getenv/Setenv are case-insensitive on Windows for env lookups,
		// but LookPath uses whichever casing was actually set; PATH covers it.
		name = "PATH"
	}
	old, had := os.LookupEnv(name)
	if err := os.Setenv(name, ""); err != nil {
		t.Fatalf("clearing %s: %v", name, err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(name, old)
		} else {
			os.Unsetenv(name)
		}
	})
}

func TestScanTextNoGitleaksOnPath(t *testing.T) {
	withEmptyPath(t)

	findings, err := ScanText(context.Background(), "some PR body text with nothing interesting")
	if err != nil {
		t.Fatalf("ScanText should never error just because gitleaks is missing, got %v", err)
	}
	if findings != nil {
		t.Errorf("ScanText with no gitleaks on PATH should return nil findings, got %v", findings)
	}
}

// TestScanTextLive exercises the real gitleaks binary when it's available
// (it is on this dev box, via scoop) rather than skipping entirely — this
// covers the actual temp-dir-and-scan wiring, not just the parser.
func TestScanTextLive(t *testing.T) {
	if !lookPath("gitleaks") {
		t.Skip("gitleaks not installed on PATH")
	}

	t.Run("no secret", func(t *testing.T) {
		findings, err := ScanText(context.Background(), "chore: tidy up the README wording")
		if err != nil {
			t.Fatalf("ScanText: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("expected no findings for benign text, got %+v", findings)
		}
	})

	t.Run("aws secret", func(t *testing.T) {
		// A synthetic-looking but gitleaks-detectable AWS access key pattern.
		text := "fixes the deploy script\n\nAWS_ACCESS_KEY_ID=" + "AKIA" + "ABCDEFGHIJKLMNOP"
		findings, err := ScanText(context.Background(), text)
		if err != nil {
			t.Fatalf("ScanText: %v", err)
		}
		if len(findings) == 0 {
			t.Fatalf("expected at least one finding for an embedded AWS key, got none")
		}
		if findings[0].Tool != "gitleaks" {
			t.Errorf("finding.Tool = %q, want gitleaks", findings[0].Tool)
		}
	})
}

// TestScanGitleaksHostDirSharedHelper is a light sanity check that the
// factored-out helper still round-trips through parseGitleaks the same way
// gitleaksScanner.Scan's MethodHost branch always has.
func TestScanGitleaksHostDirSharedHelper(t *testing.T) {
	if !lookPath("gitleaks") {
		t.Skip("gitleaks not installed on PATH")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("nothing to see here"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanGitleaksHostDir(context.Background(), dir); err != nil {
		t.Fatalf("scanGitleaksHostDir: %v", err)
	}
}

// sanity: exec.LookPath actually fails once PATH is cleared, so the "no
// gitleaks on PATH" test above is exercising the intended condition and not
// silently passing because gitleaks was never found for some other reason.
func TestWithEmptyPathActuallyHidesBinaries(t *testing.T) {
	withEmptyPath(t)
	if _, err := exec.LookPath("gitleaks"); err == nil {
		t.Fatalf("expected gitleaks to be unfindable with an empty PATH")
	}
}
