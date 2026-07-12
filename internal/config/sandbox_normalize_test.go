package config

import (
	"strings"
	"testing"
)

// TestSandboxNormalizeAliasesRuntimeNames covers P25.2: typing a container
// runtime name straight into sandbox.backend (the mistake CLAUDE.md's own
// "local, Docker, Podman, WSL containers, Apple Containers" phrasing invites)
// must resolve to backend=container + the matching runtime, not silently
// fall through to the unsandboxed local backend.
func TestSandboxNormalizeAliasesRuntimeNames(t *testing.T) {
	tests := []struct {
		backend     string
		wantBackend string
		wantRuntime string
	}{
		{"docker", "container", "docker"},
		{"podman", "container", "podman"},
		{"wsl", "container", "wslc"},
		{"wslc", "container", "wslc"},
		{"apple", "container", "container"},
		{"Podman", "container", "podman"}, // case-insensitive
		{" docker ", "container", "docker"},
	}
	for _, tt := range tests {
		t.Run(tt.backend, func(t *testing.T) {
			c := SandboxConfig{Backend: tt.backend}
			if err := c.Normalize(); err != nil {
				t.Fatalf("normalize(%q): unexpected error: %v", tt.backend, err)
			}
			if c.Backend != tt.wantBackend {
				t.Errorf("backend = %q, want %q", c.Backend, tt.wantBackend)
			}
			if c.Runtime != tt.wantRuntime {
				t.Errorf("runtime = %q, want %q", c.Runtime, tt.wantRuntime)
			}
		})
	}
}

// TestSandboxNormalizePreservesExplicitRuntime ensures an alias never
// clobbers a runtime the operator already set explicitly.
func TestSandboxNormalizePreservesExplicitRuntime(t *testing.T) {
	c := SandboxConfig{Backend: "docker", Runtime: "wslc"}
	if err := c.Normalize(); err != nil {
		t.Fatalf("normalize: unexpected error: %v", err)
	}
	if c.Backend != "container" {
		t.Errorf("backend = %q, want %q", c.Backend, "container")
	}
	if c.Runtime != "wslc" {
		t.Errorf("runtime = %q, want preserved %q", c.Runtime, "wslc")
	}
}

// TestSandboxNormalizeKnownBackendsPassThrough covers the legitimate values
// SelectSandbox switches on — normalize must leave them untouched.
func TestSandboxNormalizeKnownBackendsPassThrough(t *testing.T) {
	for _, backend := range []string{"", "local", "container", "auto", "os"} {
		c := SandboxConfig{Backend: backend, Runtime: "podman"}
		if err := c.Normalize(); err != nil {
			t.Errorf("normalize(%q): unexpected error: %v", backend, err)
		}
		if c.Backend != backend {
			t.Errorf("backend = %q, want unchanged %q", c.Backend, backend)
		}
	}
}

// TestSandboxNormalizeRejectsUnknownBackend is the regression test for the
// P25.2 root cause: before this fix, any unrecognized backend value fell
// through SelectSandbox's default case and ran commands unsandboxed on the
// host with no warning. It must now fail fast instead.
func TestSandboxNormalizeRejectsUnknownBackend(t *testing.T) {
	c := SandboxConfig{Backend: "kubernetes"}
	err := c.Normalize()
	if err == nil {
		t.Fatal("normalize: expected error for unknown backend, got nil")
	}
	if !strings.Contains(err.Error(), "kubernetes") {
		t.Errorf("error %q should name the offending value", err.Error())
	}
}

// TestLoadAliasesSandboxBackendEnvVar is the end-to-end regression test for
// the roadmap's acceptance criterion: AEGIS_SANDBOX_BACKEND=podman must
// resolve through Load() to a real container backend, not silently pass
// through as an unrecognized value that SelectSandbox would run locally.
func TestLoadAliasesSandboxBackendEnvVar(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("AEGIS_SANDBOX_BACKEND", "podman")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Sandbox.Backend != "container" {
		t.Errorf("sandbox.backend = %q, want %q", cfg.Sandbox.Backend, "container")
	}
	if cfg.Sandbox.Runtime != "podman" {
		t.Errorf("sandbox.runtime = %q, want %q", cfg.Sandbox.Runtime, "podman")
	}
}

// TestLoadFailsFastOnUnknownSandboxBackend covers the other half of the
// acceptance criterion: a genuinely unrecognized backend must fail Load()
// with a clear message instead of reaching the daemon and running
// unsandboxed on the host.
func TestLoadFailsFastOnUnknownSandboxBackend(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("AEGIS_SANDBOX_BACKEND", "kubernetes")

	if _, err := Load(); err == nil {
		t.Fatal("Load: expected error for unknown sandbox.backend, got nil")
	}
}
