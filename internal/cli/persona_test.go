package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

func runPersona(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newPersonaCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// chdirTemp switches into a fresh temp directory for the duration of the
// test, restoring the original working directory on cleanup. Used so
// project-scoped commands (persona new/use write under ./.aegis/) never
// touch the real repo the tests run in.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWd) })
	return dir
}

func TestPersonaListShowsBuiltins(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	out, err := runPersona(t, "list")
	if err != nil {
		t.Fatalf("persona list: %v", err)
	}
	if !strings.Contains(out, "developer") || !strings.Contains(out, "security") {
		t.Errorf("list missing expected built-ins:\n%s", out)
	}
}

func TestPersonaNewThenShowRoundTrip(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	if _, err := runPersona(t, "new", "incident-responder", "--description", "Incident lead"); err != nil {
		t.Fatalf("persona new: %v", err)
	}
	if _, err := os.Stat(".aegis/personas/incident-responder.md"); err != nil {
		t.Fatalf("scaffold file not created: %v", err)
	}

	out, err := runPersona(t, "show", "incident-responder")
	if err != nil {
		t.Fatalf("persona show: %v", err)
	}
	if !strings.Contains(out, "Incident lead") || !strings.Contains(out, "INCIDENT RESPONDER") {
		t.Errorf("show output missing scaffolded content:\n%s", out)
	}
}

func TestPersonaNewRejectsDuplicate(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	if _, err := runPersona(t, "new", "dup"); err != nil {
		t.Fatalf("first new: %v", err)
	}
	if _, err := runPersona(t, "new", "dup"); err == nil {
		t.Error("expected error creating a persona file that already exists")
	}
}

func TestPersonaUseSetsProjectDefault(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	out, err := runPersona(t, "use", "developer")
	if err != nil {
		t.Fatalf("persona use: %v", err)
	}
	if !strings.Contains(out, "developer") || !strings.Contains(out, "project") {
		t.Errorf("unexpected output: %q", out)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.DefaultPersona != "developer" {
		t.Errorf("DefaultPersona = %q, want developer", cfg.DefaultPersona)
	}

	// The default should now be reflected in list/show.
	listOut, _ := runPersona(t, "list")
	if !strings.Contains(listOut, "default") {
		t.Errorf("list should mark the default persona:\n%s", listOut)
	}
	showOut, _ := runPersona(t, "show", "developer")
	if !strings.Contains(showOut, "Default:") {
		t.Errorf("show should report the default persona:\n%s", showOut)
	}
}

func TestPersonaUseGlobalWritesUserConfig(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	if _, err := runPersona(t, "use", "sre", "--global"); err != nil {
		t.Fatalf("persona use --global: %v", err)
	}
	data, err := os.ReadFile(config.GlobalConfigPath())
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if !strings.Contains(string(data), `default_persona: "sre"`) {
		t.Errorf("global config missing default_persona:\n%s", data)
	}
}

func TestPersonaUseRejectsUnknown(t *testing.T) {
	redirectConfigDir(t)
	chdirTemp(t)

	if _, err := runPersona(t, "use", "does-not-exist"); err == nil {
		t.Error("expected error for unknown persona")
	}
}
