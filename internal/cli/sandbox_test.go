package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scottymacleod/aegis/internal/config"
)

// redirectConfigDir points GlobalConfigPath() at a temp dir so the test never
// reads or writes the developer's real config.
func redirectConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
}

func runSandbox(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newSandboxCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestSandboxUseWritesConfig(t *testing.T) {
	redirectConfigDir(t)

	out, err := runSandbox(t, "use", "docker")
	if err != nil {
		t.Fatalf("sandbox use docker: %v", err)
	}
	if !strings.Contains(out, "docker") || !strings.Contains(strings.ToLower(out), "restart") {
		t.Errorf("unexpected output: %q", out)
	}

	data, err := os.ReadFile(config.GlobalConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgText := string(data)
	if !strings.Contains(cfgText, "backend: container") || !strings.Contains(cfgText, "runtime: docker") {
		t.Errorf("config not written as expected:\n%s", cfgText)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Sandbox.Backend != "container" || cfg.Sandbox.Runtime != "docker" {
		t.Errorf("reloaded sandbox = %+v", cfg.Sandbox)
	}
}

func TestSandboxUseAuto(t *testing.T) {
	redirectConfigDir(t)
	if _, err := runSandbox(t, "use", "auto"); err != nil {
		t.Fatalf("sandbox use auto: %v", err)
	}
	cfg, _ := config.Load()
	if cfg.Sandbox.Backend != "auto" || cfg.Sandbox.Runtime != "" {
		t.Errorf("reloaded sandbox = %+v, want backend=auto runtime empty", cfg.Sandbox)
	}
}

func TestSandboxUseRejectsUnknown(t *testing.T) {
	redirectConfigDir(t)
	if _, err := runSandbox(t, "use", "bogus"); err == nil {
		t.Fatal("expected error for unknown target, got nil")
	}
}

func TestSandboxStatusLocal(t *testing.T) {
	redirectConfigDir(t)
	// Default config has backend=local; status should not probe runtimes.
	out, err := runSandbox(t, "status")
	if err != nil {
		t.Fatalf("sandbox status: %v", err)
	}
	if !strings.Contains(out, "local") {
		t.Errorf("status output missing backend: %q", out)
	}
}
