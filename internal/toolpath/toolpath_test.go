package toolpath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeBinary writes an executable-looking file and returns its path.
func fakeBinary(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPathOverrideByAbsolutePath(t *testing.T) {
	bin := fakeBinary(t, "myrg")
	r := New(map[string]string{"ripgrep": bin})
	st := r.Status(Ripgrep)
	if !st.Available() {
		t.Fatalf("configured binary should resolve: %+v", st)
	}
	if st.Path != bin {
		t.Errorf("Path = %q, want %q", st.Path, bin)
	}
	if st.Configured != bin {
		t.Errorf("Configured should record the raw value, got %q", st.Configured)
	}
}

// A configured path that does not exist must fail loudly rather than silently
// falling back to PATH — the user named a specific binary for a reason, and a
// silent fallback is exactly what makes a config knob look broken.
func TestPathOverrideMissingIsAnError(t *testing.T) {
	r := New(map[string]string{"ripgrep": filepath.Join(t.TempDir(), "nope", "rg")})
	st := r.Status(Ripgrep)
	if st.Available() {
		t.Fatal("a missing configured path must not resolve")
	}
	if st.Err == "" {
		t.Error("want an explanatory error")
	}
}

// A bare name still goes through PATH, so `ripgrep: rg-14` works without the
// user having to write an absolute path.
func TestPathOverrideBareNameUsesPATH(t *testing.T) {
	r := New(map[string]string{"ripgrep": "definitely-not-a-real-binary-xyz"})
	st := r.Status(Ripgrep)
	if st.Available() {
		t.Fatal("unexpected resolution")
	}
	if st.Err == "" {
		t.Error("want an error naming the missing command")
	}
}

func TestDisableKeywords(t *testing.T) {
	for _, v := range []string{"off", "OFF", "false", "no", "none", "disabled", "0"} {
		r := New(map[string]string{"ripgrep": v})
		st := r.Status(Ripgrep)
		if !st.Disabled {
			t.Errorf("%q should disable the tool, got %+v", v, st)
		}
		if st.Path != "" {
			t.Errorf("%q left a path set: %q", v, st.Path)
		}
	}
}

// Disabling must be reachable even when the binary is installed — that is the
// whole point of the knob (reproduce a bug against the built-in fallback).
func TestDisableBeatsAnInstalledBinary(t *testing.T) {
	if New(nil).Path(Git) == "" {
		t.Skip("git not installed; nothing to override")
	}
	if p := New(map[string]string{"git": "off"}).Path(Git); p != "" {
		t.Errorf("disable should win over PATH, got %q", p)
	}
}

func TestNilResolverConfigIsPATHLookup(t *testing.T) {
	r := New(nil)
	st := r.Status(Git)
	if st.Configured != "" {
		t.Errorf("no override should leave Configured empty, got %q", st.Configured)
	}
	// git may or may not be installed; either outcome must be self-describing.
	if !st.Available() && st.Err == "" {
		t.Error("an unavailable tool must explain itself")
	}
}

func TestUnknownKeysReported(t *testing.T) {
	r := New(map[string]string{"ripgrep": "rg", "ripgrp": "rg", "aardvark": "x"})
	got := r.UnknownKeys()
	if len(got) != 2 || got[0] != "aardvark" || got[1] != "ripgrp" {
		t.Errorf("UnknownKeys() = %v, want [aardvark ripgrp]", got)
	}
}

func TestStatusesCoversRegistry(t *testing.T) {
	got := New(nil).Statuses()
	if len(got) != len(Registry) {
		t.Fatalf("Statuses() returned %d, want %d", len(got), len(Registry))
	}
	for i, st := range got {
		if st.Key != Registry[i].Key {
			t.Errorf("order drift at %d: %q vs %q", i, st.Key, Registry[i].Key)
		}
		if st.Purpose == "" || st.Fallback == "" {
			t.Errorf("%s must document its purpose and fallback — `aegis doctor` renders both", st.Key)
		}
	}
}

func TestInstallHintIsActionable(t *testing.T) {
	for _, spec := range Registry {
		mgr, cmd := spec.InstallHint()
		if cmd == "" {
			t.Errorf("%s has no install hint for %s", spec.Key, runtime.GOOS)
			continue
		}
		if mgr == "" {
			t.Errorf("%s returned a command with no manager name", spec.Key)
		}
	}
}

// Resolution is cached, so the search tools can call Path on every invocation
// without paying for a PATH walk each time.
func TestStatusIsCached(t *testing.T) {
	r := New(nil)
	first := r.Status(Ripgrep)
	r.mu.Lock()
	r.cache[Ripgrep] = Status{Spec: first.Spec, Path: "sentinel"}
	r.mu.Unlock()
	if got := r.Path(Ripgrep); got != "sentinel" {
		t.Errorf("second call bypassed the cache: %q", got)
	}
}
